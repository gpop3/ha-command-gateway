package nlp

import (
	"encoding/json"
	"fmt"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/utils/text"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/utils/conversion"
)

// Analyseur traite les commandes textuelles et les exécute via HA
type Analyseur struct {
	haClient                *ha.Client
	catalogue               []ha.Appareil
	dernierRafraichissement time.Time
	catalogueIndex          map[string][]ha.Appareil
	activePreselection      bool
}

// New crée un analyseur avec le client HA fourni
func New(haClient *ha.Client, activePreselection bool) *Analyseur {
	return &Analyseur{haClient: haClient, activePreselection: activePreselection}
}

// RafraichirCatalogue met à jour la liste des entités depuis HA
func (a *Analyseur) RafraichirCatalogue() error {
	// Rafraichir seulement si vide ou > 30 mins
	if len(a.catalogue) > 0 && time.Since(a.dernierRafraichissement) < 30*time.Minute {
		return nil
	}

	appareils, err := a.haClient.RecupererEntites()
	if err != nil {
		return err
	}
	a.catalogue = appareils
	a.catalogue = append(a.catalogue,
		ha.Appareil{
			EntityID:          "time.local",
			FriendlyName:      "heure",
			FriendlyNameExact: "heure",
			Domain:            "time",
		},
		ha.Appareil{
			EntityID:          "weather.forecast_maison",
			FriendlyName:      "météo",
			FriendlyNameExact: "météo",
			Domain:            "weather",
		},
		ha.Appareil{
			EntityID:          "resume_maison.local",
			FriendlyName:      "résumé maison",
			FriendlyNameExact: "résumé maison",
			Domain:            "resume_maison",
		},
		ha.Appareil{
			EntityID:          "agenda.home",
			FriendlyName:      "agenda",
			FriendlyNameExact: "agenda",
			Domain:            "agenda",
		},
	)
	a.dernierRafraichissement = time.Now()

	// Trier par domaine pour grouper les entités et accélérer le matching
	sort.Slice(a.catalogue, func(i, j int) bool {
		return a.catalogue[i].Domain < a.catalogue[j].Domain
	})

	// Indexer par domaine
	a.catalogueIndex = make(map[string][]ha.Appareil)
	for _, app := range a.catalogue {
		a.catalogueIndex[app.Domain] = append(a.catalogueIndex[app.Domain], app)
	}

	if svc, ok := ha.Lookup("agenda"); ok {
		if agenda, ok := svc.(*ha.ServiceAgenda); ok {
			agenda.SetCatalogue(a.catalogue)
		}
	}

	if svc, ok := ha.Lookup("media_player"); ok {
		if mp, ok := svc.(*ha.ServiceMediaPlayer); ok {
			mp.SetTrouverEntite(func(texte string, estAction bool, domaines []string) (ha.Appareil, int) {
				return a.TrouverMeilleurMatch(texte, estAction, domaines)
			})
			mp.ChargerSourcesSpotify()
		}
	}

	return nil
}

// GetCatalogue retourne le catalogue en mémoire
func (a *Analyseur) GetCatalogue() []ha.Appareil {
	return a.catalogue
}

// GetPieces retourne les pieces en mémoire
func (a *Analyseur) GetPieces() []ha.Piece {
	return a.haClient.GetPieces()
}

// ---- Grammaire / Prompt ----

// GenererGrammaire génère la grammaire Vosk :
func (a *Analyseur) GenererGrammaire() string {
	unique := make(map[string]bool)
	var final []string

	ajouter := func(mot string) {
		mot = strings.ToLower(strings.TrimSpace(mot))
		if mot != "" && !unique[mot] {
			unique[mot] = true
			final = append(final, mot)
		}
	}

	for _, m := range []string{"assistant", "stop", "pourcentage"} {
		ajouter(m)
	}

	for _, domaine := range ha.ListDomaines() {
		svc, ok := ha.Lookup(domaine)
		if !ok {
			continue
		}
		for _, verbe := range svc.Verbes() {
			for _, mot := range strings.Fields(verbe) {
				ajouter(mot)
			}
		}
		for _, m := range svc.MotsReconnus() {
			for _, mot := range strings.Fields(m) {
				ajouter(mot)
			}
		}
	}

	for nombre := range conversion.NombresEnLettres {
		for _, mot := range strings.Fields(nombre) {
			ajouter(mot)
		}
	}

	for _, app := range a.catalogue {
		for _, mot := range strings.Fields(strings.ToLower(app.FriendlyName)) {
			ajouter(mot)
		}
	}

	final = append(final, "[unk]")

	grammarJSON, _ := json.Marshal(final)

	return string(grammarJSON)
}

// GenererSystemPrompt génère le prompt contextuel pour Whisper
func (a *Analyseur) GenererSystemPrompt() string {
	unique := make(map[string]bool)
	var mots []string

	ajouter := func(mot string) {
		mot = strings.ToLower(strings.TrimSpace(mot))
		if len(mot) > 2 && !unique[mot] && !strings.ContainsAny(mot, "0123456789-_/.") {
			unique[mot] = true
			mots = append(mots, mot)
		}
	}

	for _, domaine := range ha.ListDomaines() {
		svc, ok := ha.Lookup(domaine)
		if !ok {
			continue
		}
		for _, verbe := range svc.Verbes() {
			for _, mot := range strings.Fields(verbe) {
				ajouter(mot)
			}
		}
		for _, m := range svc.MotsReconnus() {
			for _, mot := range strings.Fields(m) {
				ajouter(mot)
			}
		}
	}

	for _, app := range a.catalogue {
		for _, mot := range strings.Fields(strings.ToLower(app.FriendlyName)) {
			ajouter(mot)
		}
	}

	return strings.Join(mots, ", ")
}

// ---- Point d'entrée principal ----

type EtatType struct {
	TextSms   string
	TexteVoix string
	Date      *string
}

// AnalyserEtExecuter traite une commande textuelle et retourne la réponse
func (a *Analyseur) AnalyserEtExecuter(texte string) (*EtatType, string, bool, bool, *ha.Appareil) {
	nettoye := strings.ToLower(texte)

	verbe, estAction := detecterVerbe(nettoye)

	domainesCandidats := []string{}
	if estAction && a.activePreselection {
		domainesCandidats = detecterDomaines(nettoye)
	}

	if err := a.RafraichirCatalogue(); err != nil {
		return nil, verbe, false, false, nil
	}

	meilleurMatch, meilleurScore := a.TrouverMeilleurMatch(nettoye, estAction, domainesCandidats)
	if meilleurScore < 30 {
		return nil, verbe, false, false, nil
	}

	params := extraireParamsParService(nettoye, meilleurMatch.Domain)
	if _, aUnPourcentage := params["pourcentage"]; aUnPourcentage {
		estAction = true
	}

	fmt.Printf("DEBUG estAction=%v domaine=%s\n", estAction, meilleurMatch.Domain)
	estActionParDefaut := false
	if svc, ok := ha.Lookup(meilleurMatch.Domain); ok && svc.EstActionParDefaut() {
		fmt.Printf("DEBUG EstActionParDefaut → true\n")
		estActionParDefaut = true
	}

	if estAction || estActionParDefaut {
		etat := a.executerAction(meilleurMatch, verbe, params)

		return &EtatType{etat, etat, nil}, verbe, true, estAction, &meilleurMatch
	}
	etat := a.lireEtat(meilleurMatch, nettoye)
	return &etat, verbe, true, false, &meilleurMatch
}

// ---- Détection du verbe ----

// detecterVerbe parcourt tous les services enregistrés pour trouver le verbe
func detecterVerbe(texte string) (verbe string, estAction bool) {
	mots := strings.Fields(texte)
	for _, mot := range mots {
		for _, domaine := range ha.ListDomaines() {
			svc, ok := ha.Lookup(domaine)
			if !ok {
				continue
			}
			if _, ok := svc.Verbe(mot); ok {
				return mot, true
			}
		}
	}
	return "", false
}

// ---- Détection des domaines en fonction des verbes ----

// detecterDomaines parcourt tous les services enregistrés pour trouver les domaines
func detecterDomaines(texte string) (domaines []string) {
	var domainesKeys []string
	mots := strings.Fields(texte)

	for _, mot := range mots {
		for _, domaine := range ha.ListDomaines() {
			if slices.Contains(domaines, domaine) {
				continue
			}

			svc, ok := ha.Lookup(domaine)
			if !ok {
				continue
			}
			if _, ok := svc.Verbe(mot); ok {
				domainesKeys = append(domainesKeys, domaine)
			}
		}
	}

	return domainesKeys
}

// ---- Extraction des paramètres par service ----

// extraireParamsParService délègue l'extraction au service du domaine concerné.
// Si le domaine n'est pas enregistré, utilise une extraction universelle de fallback.
func extraireParamsParService(texte, domaine string) map[string]interface{} {
	if svc, ok := ha.Lookup(domaine); ok {
		return svc.ExtraireParams(texte)
	}
	// Fallback : extraction universelle via un serviceBase anonyme
	return extraireParamsUniversels(texte)
}

// extraireParamsUniversels est le fallback quand le domaine est inconnu.
// Même logique que serviceBase.ExtraireParams.
func extraireParamsUniversels(texte string) map[string]interface{} {
	params := map[string]interface{}{}

	if re := regexp.MustCompile(`(\d{1,3})\s*%`); re.MatchString(texte) {
		m := re.FindStringSubmatch(texte)
		var pct int
		_, err := fmt.Sscanf(m[1], "%d", &pct)
		if err != nil {
			return nil
		}
		params["pourcentage"] = pct
		return params
	}

	mots := strings.Fields(texte)
	for i, mot := range mots {
		if i+2 < len(mots) && mots[i+1] == "pour" && mots[i+2] == "cent" {
			if v, ok := conversion.LettreVersEntier(mot); ok {
				params["pourcentage"] = v
				return params
			}
		}
	}

	if re := regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(?:degrés?|°)`); re.MatchString(texte) {
		m := re.FindStringSubmatch(texte)
		var temp float64
		_, err := fmt.Sscanf(strings.ReplaceAll(m[1], ",", "."), "%f", &temp)
		if err != nil {
			return nil
		}
		params["temperature"] = temp
		return params
	}

	for i, mot := range mots {
		if (mot == "degrés" || mot == "degré") && i > 0 {
			if v, ok := conversion.LettreVersEntier(mots[i-1]); ok {
				params["temperature"] = float64(v)
				return params
			}
		}
	}

	return params
}

// ---- Matching ----

var motsParasites = []string{
	"min", "max", "confort", "consigne", "setpoint",
	"decalage", "décalage", "offset", "calibration",
	"batterie", "battery",
}

func (a *Analyseur) TrouverMeilleurMatch(texteNettoye string, estAction bool, domainesCandidats []string) (ha.Appareil, int) {
	motsSMS := strings.Fields(texteNettoye)

	modificateurDemande := ""
	for _, p := range motsParasites {
		if strings.Contains(texteNettoye, p) {
			modificateurDemande = p
			break
		}
	}

	var meilleurMatch ha.Appareil
	meilleurScore := 0

	candidats := []ha.Appareil{}
	for _, domaine := range domainesCandidats {
		fmt.Printf("DEBUG: 'Selection du domaine %s' pour la recherche\n", domaine)

		if entites, ok := a.catalogueIndex[domaine]; ok {
			candidats = append(candidats, entites...)
		}
	}

	if len(candidats) > 0 {
		for _, app := range candidats {
			score := a.scorerAppareil(app, motsSMS, texteNettoye, modificateurDemande, estAction)
			if score > meilleurScore {
				fmt.Printf("DEBUG: Présélection '%s' | Score: %d | Domaine: %s\n", app.FriendlyName, score, app.Domain)
				meilleurScore = score
				meilleurMatch = app
			}
		}
	}

	if meilleurScore < 50 {
		for _, app := range a.catalogue {
			score := a.scorerAppareil(app, motsSMS, texteNettoye, modificateurDemande, estAction)
			if score > meilleurScore {
				fmt.Printf("DEBUG: '%s' | Score: %d | Domaine: %s\n", app.FriendlyName, score, app.Domain)
				meilleurScore = score
				meilleurMatch = app
			}
		}
	}

	return meilleurMatch, meilleurScore
}

func (a *Analyseur) scorerAppareil(app ha.Appareil, motsSMS []string, texteNettoye, modificateurDemande string, estAction bool) int {
	nomApp := strings.ToLower(app.FriendlyName)
	idApp := strings.ToLower(app.EntityID)
	score := 0

	// Correction : "minuit" ne matche pas "min"
	if modificateurDemande == "min" && strings.Contains(nomApp, "minuit") && !strings.Contains(nomApp, " min") {
		nomApp = strings.ReplaceAll(nomApp, "minuit", "")
	}

	motsMatches := 0
	ContientLeModificateur := false

	reChiffre := regexp.MustCompile(`^\d+$`)

	for _, mot := range motsSMS {
		mot = strings.NewReplacer("?", "", ",", "", "l'", "", "d'", "", "'", "").Replace(mot)
		estUnChiffre := reChiffre.MatchString(mot)
		_, estUnNombre := conversion.NombresEnLettres[mot]

		if (len(mot) < 3 && !estUnChiffre && !estUnNombre) || mot == "est" || mot == "les" || mot == "des" {
			continue
		}

		if strings.Contains(nomApp, mot) || strings.Contains(idApp, mot) {
			// Bonus si le mot correspond à une pièce connue
			matchPiece := false
			for _, p := range a.GetPieces() {
				if strings.EqualFold(p.Name, mot) {
					matchPiece = true
					break
				}
			}
			if matchPiece {
				score += 100
			} else {
				score += 20
			}
			motsMatches++
			continue
		}

		// Fuzzy match
		for _, motHA := range strings.Fields(nomApp) {
			if len(motHA) < 3 {
				continue
			}
			maxErreurs := 1
			if len(mot) >= 6 {
				maxErreurs = 3
			}
			if text.DistanceLevenshtein(mot, motHA) <= maxErreurs {
				score += 15
				motsMatches++
				break
			}
		}
	}

	if modificateurDemande != "" && (strings.Contains(nomApp, modificateurDemande) || strings.Contains(idApp, modificateurDemande)) {
		ContientLeModificateur = true
	}

	if score < 15 {
		return score
	}

	if modificateurDemande != "" && ContientLeModificateur {
		score += 100
	}

	// Bonus/malus du domaine — chaque service définit le sien
	if svc, ok := ha.Lookup(app.Domain); ok {
		score += svc.ScoreDomaine(estAction)
	}

	if modificateurDemande == "" {
		for _, p := range motsParasites {
			if strings.Contains(nomApp, p) {
				score -= 50
			}
		}
	}

	if modificateurDemande != "" && !ContientLeModificateur {
		score -= 50
	}

	nombreMotsHA := len(strings.Fields(nomApp))
	if nombreMotsHA > motsMatches {
		score -= (nombreMotsHA - motsMatches) * 2
	}

	return score
}

// ---- Exécution ----

// executerAction délègue entièrement au service du domaine concerné.
func (a *Analyseur) executerAction(app ha.Appareil, verbe string, params map[string]interface{}) string {
	svc, ok := ha.Lookup(app.Domain)
	if !ok {
		return fmt.Sprintf("❌ Domaine '%s' non supporté.", app.Domain)
	}

	reponse, err := svc.ExecuterCommande(app, verbe, params)
	if err != nil {
		return fmt.Sprintf("❌ Échec de l'action sur %s : %v", app.FriendlyName, err)
	}
	return reponse
}

// ---- Lecture d'état ----

func (a *Analyseur) lireEtat(app ha.Appareil, texteNettoye string) EtatType {
	dateCible, demandeHistorique := text.DetecterHeure(texteNettoye)

	if demandeHistorique {
		etat, err := a.haClient.RecupererHistorique(app.EntityID, dateCible)
		if err != nil {
			return EtatType{
				fmt.Sprintf("⚠️ Erreur historique pour [%s] à %s.", app.FriendlyName, dateCible.Format("15h04")),
				i18n.T("erreur.lecture.parler"),
				&[]string{dateCible.Format("15h04")}[0],
			}
		}
		if app.Domain == "climate" {
			return EtatType{
				ha.FormaterEtatClimate(app.FriendlyName, etat),
				ha.FormaterEtatClimateVoix(app.FriendlyName, etat),
				&[]string{dateCible.Format("15h04")}[0],
			}
		}
		return EtatType{
			fmt.Sprintf("⏳ [%s] À %s, l'état était : %s.", app.FriendlyName, dateCible.Format("15h04"), etat.State),
			etat.State,
			&[]string{dateCible.Format("15h04")}[0],
		}
	}

	etat, err := a.haClient.RecupererEtatLive(app.EntityID)
	if err != nil {
		return EtatType{
			i18n.T("erreur.lecture.live", app.FriendlyName) + fmt.Sprintf(" (%v)", err),
			ha.FormaterEtatClimateVoix(app.FriendlyName, etat),
			nil,
		}
	}
	if app.Domain == "climate" {
		return EtatType{
			ha.FormaterEtatClimate(app.FriendlyName, etat),
			ha.FormaterEtatClimateVoix(app.FriendlyName, etat),
			nil,
		}
	}

	return EtatType{
		fmt.Sprintf("📊 [%s] État actuel : %s.", app.FriendlyName, etat.State),
		etat.State,
		nil,
	}
}
