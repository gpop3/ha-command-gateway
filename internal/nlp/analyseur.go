package nlp

import (
	"encoding/json"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/utils/text"
	"ha-command-gateway/pkg/types"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/conversion"
)

// Analyseur traite les commandes textuelles et les exécute via HA
type Analyseur struct {
	haClient                *ha.Client
	catalogue               []ha.Appareil
	dernierRafraichissement time.Time
	catalogueIndex          map[string][]ha.Appareil
	activePreselection      bool

	desamb     ConfigDesambiguisation
	score      ConfigScore
	muAttentes sync.Mutex
	attentes   map[string]enAttente
}

// ConfigDesambiguisation paramètre la proposition de choix multiples lorsque plusieurs entités obtiennent un score très proche.
type ConfigDesambiguisation struct {
	Active   bool
	Seuil    int
	MaxChoix int
}

// ConfigScore expose les pondérations du scoring
type ConfigScore struct {
	Minimal               int
	BonusPiece            int
	BonusMot              int
	BonusFuzzy            int
	MalusPieceSeule       int
	BonusLieuFonction     int
	BonusCouvertureExacte int
	MalusMotSuperflu      int
	MalusActionSansCible  int
}

// Candidat associe une entité au score obtenu lors du matching.
type Candidat struct {
	Appareil ha.Appareil
	Score    int
}

// enAttente mémorise une désambiguïsation en cours pour une session (canal vocal, console ou numéro SMS)
type enAttente struct {
	candidats []ha.Appareil
	verbe     string
	estAction bool
	texte     string
	expire    time.Time
}

const dureeAttenteChoix = 30 * time.Second

// New crée un analyseur avec le client HA fourni
func New(haClient *ha.Client, activePreselection bool, desamb ConfigDesambiguisation, score ConfigScore) *Analyseur {
	return &Analyseur{
		haClient:           haClient,
		activePreselection: activePreselection,
		desamb:             desamb,
		score:              score,
		attentes:           make(map[string]enAttente),
	}
}

// ---- Gestion des désambiguïsations en attente ----

func (a *Analyseur) definirAttente(session string, att enAttente) {
	att.expire = time.Now().Add(dureeAttenteChoix)
	a.muAttentes.Lock()
	a.attentes[session] = att
	a.muAttentes.Unlock()
}

func (a *Analyseur) effacerAttente(session string) {
	a.muAttentes.Lock()
	delete(a.attentes, session)
	a.muAttentes.Unlock()
}

func (a *Analyseur) attentePour(session string) (enAttente, bool) {
	a.muAttentes.Lock()
	defer a.muAttentes.Unlock()
	att, ok := a.attentes[session]
	if !ok {
		return enAttente{}, false
	}
	if time.Now().After(att.expire) {
		delete(a.attentes, session)
		return enAttente{}, false
	}
	return att, true
}

// AttenteDeChoix indique si une désambiguïsation est en attente pour la session.
func (a *Analyseur) AttenteDeChoix(session string) bool {
	_, ok := a.attentePour(session)
	return ok
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
	var phrases []string

	ajouter := func(phrase string) {
		phrase = text.Normaliser(strings.TrimSpace(phrase))
		if phrase != "" && !unique[phrase] {
			unique[phrase] = true
			phrases = append(phrases, phrase)
		}
	}

	for _, mot := range []string{i18n.T("nlp.mot.assistant"), i18n.T("nlp.mot.pourcentage"), i18n.T("nlp.mot.choix")} {
		ajouter(mot)
	}

	entitesParDomaine := make(map[string][]ha.Appareil)
	for _, app := range a.catalogue {
		entitesParDomaine[app.Domain] = append(entitesParDomaine[app.Domain], app)
	}

	for _, domaine := range ha.ListDomaines() {
		svc, ok := ha.Lookup(domaine)
		if !ok {
			continue
		}

		var verbes, mots []string
		for _, v := range svc.Verbes() {
			verbes = append(verbes, text.Normaliser(v))
		}
		for _, m := range svc.MotsReconnus() {
			mots = append(mots, text.Normaliser(m))
		}

		entites := entitesParDomaine[domaine]

		if len(entites) == 0 {
			if svc.AutoriseMotsSansEntites() {
				for _, mot := range mots {
					ajouter(mot)
				}
				for _, verbe := range verbes {
					ajouter(verbe)
				}
			}
			continue
		}

		var nomsEntites []string
		for _, entite := range entites {
			nom := text.Normaliser(entite.FriendlyName)
			if strings.ContainsAny(nom, "0123456789'") {
				continue
			}
			nomsEntites = append(nomsEntites, nom)
		}

		if len(nomsEntites) == 0 {
			continue
		}

		if len(verbes) == 0 {
			for _, nom := range nomsEntites {
				ajouter(nom)
				for _, mot := range mots {
					ajouter(nom + " " + mot)
					ajouter(mot + " " + nom)
				}
			}
			continue
		}

		estUnVerbe := make(map[string]bool)
		for _, v := range verbes {
			estUnVerbe[v] = true
		}

		for _, verbe := range verbes {
			for _, nom := range nomsEntites {
				ajouter(verbe + " " + nom)
			}
		}

		verbsAvecParams := svc.VerbsAvecParams()
		if len(verbsAvecParams) > 0 {
			for _, vp := range verbsAvecParams {
				verbe := text.Normaliser(vp.Action)
				for _, nom := range nomsEntites {
					nomMots := make(map[string]bool)
					for _, m := range strings.Fields(nom) {
						nomMots[m] = true
					}
					for _, param := range vp.Params {
						param = text.Normaliser(param)
						if !nomMots[param] {
							ajouter(verbe + " " + nom + " " + param)
						}
					}
					for _, mot := range mots {
						if !estUnVerbe[mot] && !nomMots[mot] {
							ajouter(verbe + " " + nom + " " + mot)
						}
					}
				}
			}
		}
	}

	for nombre := range conversion.NombresEnLettres() {
		ajouter(nombre)
	}

	phrases = append(phrases, "[unk]")

	grammarJSON, _ := json.Marshal(phrases)
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

// AnalyserEtExecuter traite une commande textuelle et retourne la réponse.
// `session` identifie le canal (« voix », « console » ou numéro SMS (identifiant))
func (a *Analyseur) AnalyserEtExecuter(session, texte string) (*types.Message, string, bool, bool, *ha.Appareil) {
	nettoye := strings.ToLower(texte)

	if att, ok := a.attentePour(session); ok {
		if idx, ok := interpreterChoix(nettoye, len(att.candidats)); ok {
			a.effacerAttente(session)
			choisi := att.candidats[idx]
			logx.DebugT("nlp.desambiguisation.choix", choisi.FriendlyName)
			return a.executerMatch(choisi, att.verbe, att.estAction, att.texte)
		}
		a.effacerAttente(session)
	}

	verbe, estAction := detecterVerbe(nettoye)

	domainesCandidats := []string{}
	if estAction && a.activePreselection {
		domainesCandidats = detecterDomaines(nettoye)
	}

	if err := a.RafraichirCatalogue(); err != nil {
		return nil, verbe, false, false, nil
	}

	classement := a.classerAppareils(nettoye, estAction, domainesCandidats)
	if len(classement) == 0 || classement[0].Score < a.score.Minimal {
		return nil, verbe, false, false, nil
	}

	if a.desamb.Active {
		options := candidatsProches(classement, a.desamb.Seuil, a.desamb.MaxChoix)
		if len(options) >= 2 {
			a.definirAttente(session, enAttente{
				candidats: options,
				verbe:     verbe,
				estAction: estAction,
				texte:     nettoye,
			})
			logx.DebugT("nlp.desambiguisation.propose", len(options))
			msg := a.messageDesambiguisation(options)
			return &msg, verbe, true, false, &options[0]
		}
	}

	return a.executerMatch(classement[0].Appareil, verbe, estAction, nettoye)
}

// executerMatch applique le verbe (action) ou lit l'état de l'entité choisie
func (a *Analyseur) executerMatch(app ha.Appareil, verbe string, estAction bool, texteNettoye string) (*types.Message, string, bool, bool, *ha.Appareil) {
	params := extraireParamsParService(texteNettoye, app.Domain)
	if params != nil {
		if _, aUnPourcentage := params["pourcentage"]; aUnPourcentage {
			estAction = true
		}
	}

	logx.DebugT("nlp.estaction.domaine", estAction, app.Domain)
	estActionParDefaut := false
	if svc, ok := ha.Lookup(app.Domain); ok && svc.EstActionParDefaut() {
		estActionParDefaut = true
	}

	svc, ok := ha.Lookup(app.Domain)
	if estAction || estActionParDefaut {
		if !ok {
			return nil, verbe, true, estAction, &app
		}
		etat := a.executerActionMessage(svc, app, verbe, params)
		return &etat, verbe, true, estAction, &app
	}

	if !ok {
		svc, _ = ha.Lookup("service_default")
	}

	etat := a.lireEtatMessage(svc, app, texteNettoye, params)
	return &etat, verbe, true, false, &app
}

// ---- Désambiguïsation ----

// candidatsProches renvoie les entités dont le score est à moins de `seuil` du meilleur
func candidatsProches(classement []Candidat, seuil, max int) []ha.Appareil {
	if len(classement) == 0 {
		return nil
	}
	top := classement[0]
	vus := make(map[string]bool)
	var retenus []Candidat
	for _, c := range classement {
		if top.Score-c.Score > seuil {
			break
		}
		if vus[c.Appareil.EntityID] {
			continue
		}
		vus[c.Appareil.EntityID] = true
		retenus = append(retenus, c)
		if max > 0 && len(retenus) >= max {
			break
		}
	}

	options := make([]ha.Appareil, len(retenus))
	for i, c := range retenus {
		options[i] = c.Appareil
		if len(retenus) >= 2 {
			logx.DebugT("nlp.desambiguisation.candidat", i+1, c.Appareil.FriendlyName, c.Score, top.Score-c.Score)
		}
	}
	return options
}

// interpreterChoix extrait un numéro de choix (1..n)
func interpreterChoix(texte string, n int) (int, bool) {
	for _, mot := range strings.Fields(strings.ToLower(texte)) {
		mot = strings.Trim(mot, ".,!?;:«»\"'")
		if num, ok := conversion.LettreVersEntier(mot); ok && num >= 1 && num <= n {
			return num - 1, true
		}
	}
	return 0, false
}

// messageDesambiguisation construit la question à poser pour départager les entités candidates
func (a *Analyseur) messageDesambiguisation(options []ha.Appareil) types.Message {
	placeholders := make([]string, len(options))
	params := make([]interface{}, 0, len(options)*2)

	for i, app := range options {
		nom := app.FriendlyNameExact
		if nom == "" {
			nom = app.FriendlyName
		}

		placeholders[i] = "%d : %s"
		params = append(params, i+1, nom)
	}

	motifOptions := strings.Join(placeholders, ", ")
	phrase := i18n.T("desambiguisation.invite", motifOptions)
	sms := strings.ReplaceAll(phrase, ", ", "\n")

	return types.Message{
		SMS:  types.MessageDetails{Texte: sms, Params: params},
		Voix: types.MessageDetails{Texte: phrase, Params: params},
	}
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
func extraireParamsParService(texte, domaine string) map[string]interface{} {
	if svc, ok := ha.Lookup(domaine); ok {
		return svc.ExtraireParams(texte)
	}
	return nil
}

// ---- Matching ----

var motsParasites = []string{
	"min", "max", "confort", "consigne", "setpoint",
	"decalage", "décalage", "offset", "calibration",
	"batterie", "battery",
}

// classerAppareils score les entités et renvoie les candidats triés par score
func (a *Analyseur) classerAppareils(texteNettoye string, estAction bool, domainesCandidats []string) []Candidat {
	motsSMS := strings.Fields(texteNettoye)

	modificateurDemande := ""
	for _, p := range motsParasites {
		if strings.Contains(texteNettoye, p) {
			modificateurDemande = p
			break
		}
	}

	scorer := func(pool []ha.Appareil) []Candidat {
		out := make([]Candidat, 0, len(pool))
		for _, app := range pool {
			score := a.scorerAppareil(app, motsSMS, texteNettoye, modificateurDemande, estAction)
			out = append(out, Candidat{Appareil: app, Score: score})
		}
		return out
	}

	var candidats []Candidat
	for _, domaine := range domainesCandidats {
		logx.DebugT("nlp.selection.du.domaine", domaine)
		if entites, ok := a.catalogueIndex[domaine]; ok {
			candidats = append(candidats, scorer(entites)...)
		}
	}

	meilleur := 0
	for _, c := range candidats {
		if c.Score > meilleur {
			meilleur = c.Score
		}
	}

	if meilleur < 50 {
		candidats = scorer(a.catalogue)
	}

	sort.SliceStable(candidats, func(i, j int) bool {
		return candidats[i].Score > candidats[j].Score
	})

	return candidats
}

// TrouverMeilleurMatch renvoie l'entité au meilleur score (et son score).
func (a *Analyseur) TrouverMeilleurMatch(texteNettoye string, estAction bool, domainesCandidats []string) (ha.Appareil, int) {
	classement := a.classerAppareils(texteNettoye, estAction, domainesCandidats)
	if len(classement) == 0 {
		return ha.Appareil{}, 0
	}
	return classement[0].Appareil, classement[0].Score
}

func (a *Analyseur) scorerAppareil(app ha.Appareil, motsSMS []string, texteNettoye, modificateurDemande string, estAction bool) int {
	nomApp := strings.ToLower(app.FriendlyName)
	idApp := strings.ToLower(app.EntityID)
	score := 0

	if modificateurDemande == "min" && strings.Contains(nomApp, "minuit") && !strings.Contains(nomApp, " min") {
		nomApp = strings.ReplaceAll(nomApp, "minuit", "")
	}

	motsMatches := 0
	aMatchePiece := false
	aMatcheSpecifique := false
	ContientLeModificateur := false

	reChiffre := regexp.MustCompile(`^\d+$`)

	for _, mot := range motsSMS {
		mot = strings.NewReplacer("?", "", ",", "", "l'", "", "d'", "", "'", "").Replace(mot)
		estUnChiffre := reChiffre.MatchString(mot)
		_, estUnNombre := conversion.NombresEnLettres()[mot]

		if len(mot) < 3 && !estUnChiffre && !estUnNombre {
			continue
		}

		if strings.Contains(nomApp, mot) || strings.Contains(idApp, mot) {
			matchPiece := false
			for _, p := range a.GetPieces() {
				if strings.EqualFold(p.Name, mot) {
					matchPiece = true
					break
				}
			}
			if matchPiece {
				score += a.score.BonusPiece
				aMatchePiece = true
			} else {
				score += a.score.BonusMot
				aMatcheSpecifique = true
			}
			motsMatches++
			continue
		}

		// Fuzzy match : insensible aux accents + tolérance proportionnelle
		motNorm := text.Normaliser(mot)
		for _, motHA := range strings.Fields(nomApp) {
			if len(motHA) < 3 {
				continue
			}
			motHANorm := text.Normaliser(motHA)
			maxErreurs := len(motNorm) / 4
			if maxErreurs < 1 {
				maxErreurs = 1
			}
			if text.DistanceLevenshtein(motNorm, motHANorm) <= maxErreurs {
				score += a.score.BonusFuzzy
				aMatcheSpecifique = true
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

	// Ne matcher QU'une pièce est un signal faible
	if aMatchePiece && !aMatcheSpecifique {
		score -= a.score.MalusPieceSeule
	}

	// Matcher le lieu ET la fonction = cible la plus précise.
	if aMatchePiece && aMatcheSpecifique {
		score += a.score.BonusLieuFonction
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

	if estAction && len(motsSMS) <= 1 {
		score -= a.score.MalusActionSansCible
	}

	nombreMotsHA := len(strings.Fields(nomApp))
	if nombreMotsHA > motsMatches {
		score -= (nombreMotsHA - motsMatches) * a.score.MalusMotSuperflu
	} else if motsMatches >= 2 && motsMatches == nombreMotsHA {
		score += a.score.BonusCouvertureExacte
	}

	return score
}

// ---- Helpers exécution & lecture d'état ----

// ---- Exécution ----

// executerActionMessage Execute la commande sur l'entité et récupère le message
func (a *Analyseur) executerActionMessage(svc ha.Service, app ha.Appareil, verbe string, params map[string]interface{}) types.Message {
	reponse, err := svc.ExecuterCommande(app, verbe, params)

	if err != nil {
		return types.Message{
			SMS: types.MessageDetails{
				Texte:  i18n.T("erreur.action.parler"),
				Params: []interface{}{},
			},
			Voix: types.MessageDetails{
				Texte:  i18n.T("erreur.action.parler"),
				Params: []interface{}{},
			},
		}
	}

	return types.Message{
		SMS: types.MessageDetails{
			Texte:  reponse,
			Params: []interface{}{},
		},
		Voix: types.MessageDetails{
			Texte:  reponse,
			Params: []interface{}{},
		},
	}
}

// ---- Lecture d'état ----

// lireEtatMessage Lit l'état de l'entité et récupère le message
func (a *Analyseur) lireEtatMessage(svc ha.Service, app ha.Appareil, texteNettoye string, params map[string]interface{}) types.Message {
	texteAvecChiffres := conversion.RemplacerMotsParChiffres(texteNettoye)

	dateCible, demandeHistorique := text.DetecterHeure(texteAvecChiffres)
	var dateParam time.Time
	if demandeHistorique {
		dateParam = dateCible
	}

	etat, etatCustom, err := svc.RecupererEtat(app, dateParam, params)
	if err != nil {
		return types.Message{
			SMS: types.MessageDetails{
				Texte:  i18n.T("erreur.lecture.parler"),
				Params: []interface{}{},
			},
			Voix: types.MessageDetails{
				Texte:  i18n.T("erreur.lecture.parler"),
				Params: []interface{}{},
			},
		}
	}

	return svc.EtatEnMessage(app, etat, etatCustom, dateParam)
}
