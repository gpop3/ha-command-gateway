package nlp

import (
	"encoding/json"
	"ha-command-gateway/internal/core/adapters/gemini"
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

	gemini        *gemini.Client
	geminiPrimary bool

	// IA : réglages, mémoire de conversation et confirmations, par session
	ia               ConfigIA
	numerosAutorises map[string]bool
	muSessions       sync.Mutex
	memoires         map[string]*memoireSession
	confirmations    map[string]confirmationEnAttente
	ecoutes          map[string]time.Time
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
	texte     string
	expire    time.Time
}

const dureeAttenteChoix = 30 * time.Second

// DefinirGemini branche le fallback/primaire IA
func (a *Analyseur) DefinirGemini(client *gemini.Client, primary bool) {
	a.gemini = client
	a.geminiPrimary = primary
}

// New crée un analyseur avec le client HA fourni
func New(haClient *ha.Client, activePreselection bool, desamb ConfigDesambiguisation, score ConfigScore) *Analyseur {
	return &Analyseur{
		haClient:           haClient,
		activePreselection: activePreselection,
		desamb:             desamb,
		score:              score,
		attentes:           make(map[string]enAttente),
		ia:                 configIAParDefaut(),
		numerosAutorises:   make(map[string]bool),
		memoires:           make(map[string]*memoireSession),
		confirmations:      make(map[string]confirmationEnAttente),
		ecoutes:            make(map[string]time.Time),
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

// AttenteDeChoix indique si l'assistant attend une réponse de l'utilisateur pour la
// session : choix parmi plusieurs entités, confirmation d'une action sensible, ou
// question posée par l'IA. La voix reste alors à l'écoute.
func (a *Analyseur) AttenteDeChoix(session string) bool {
	if _, ok := a.attentePour(session); ok {
		return true
	}
	return a.reponseAttendue(session)
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

	for _, domaine := range ha.ListDomaines() {
		if svc, ok := ha.Lookup(domaine); ok {
			if av, ok := svc.(ha.ServiceAvecAppareils); ok {
				a.catalogue = append(a.catalogue, av.AppareilsVirtuels()...)
			}
		}
	}

	a.dernierRafraichissement = time.Now()

	// Trier par domaine pour grouper les entités et accélérer le matching
	sort.Slice(a.catalogue, func(i, j int) bool {
		return a.catalogue[i].Domain < a.catalogue[j].Domain
	})

	a.catalogueIndex = make(map[string][]ha.Appareil)
	for _, app := range a.catalogue {
		a.catalogueIndex[app.Domain] = append(a.catalogueIndex[app.Domain], app)
	}

	for _, domaine := range ha.ListDomaines() {
		if svc, ok := ha.Lookup(domaine); ok {
			if initSvc, ok := svc.(ha.ServiceInitialisable); ok {
				initSvc.Init(a)
			}
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

	// Confirmation en attente (action sensible proposée par l'IA)
	if conf, ok := a.confirmationPour(session); ok {
		a.effacerConfirmation(session)
		switch interpreterConfirmation(nettoye) {
		case reponseOui:
			return a.executerActionsGemini(session, conf.rep, true)
		case reponseNon:
			msg := messageTexte(i18n.T("confirmation.annule"))
			return &msg, "", true, false, nil
		}
		// Autre réponse : la confirmation est abandonnée, la phrase est traitée normalement
	}
	a.effacerEcoute(session)

	if att, ok := a.attentePour(session); ok {
		if idx, ok := interpreterChoix(nettoye, len(att.candidats)); ok {
			a.effacerAttente(session)
			choisi := att.candidats[idx]
			logx.DebugT("nlp.desambiguisation.choix", choisi.FriendlyName)
			return a.executerMatch(choisi, att.texte)
		}
		a.effacerAttente(session)
	}

	if a.gemini != nil && a.geminiPrimary {
		if msg, verbe, match, isAction, app := a.tenterGemini(session, texte); match {
			return msg, verbe, match, isAction, app
		}
	}

	verbe, estAction := detecterVerbe(nettoye)

	domainesCandidats := []string{}
	if estAction && a.activePreselection {
		domainesCandidats = detecterDomaines(nettoye)
	}

	if err := a.RafraichirCatalogue(); err != nil {
		return nil, verbe, false, false, nil
	}

	classement := a.classerAppareils(nettoye, domainesCandidats)
	if len(classement) == 0 || classement[0].Score < a.score.Minimal {
		if a.gemini != nil && !a.geminiPrimary {
			if msg, verbe2, match, isAction, app := a.tenterGemini(session, texte); match {
				return msg, verbe2, match, isAction, app
			}
		}
		return nil, verbe, false, false, nil
	}

	if a.desamb.Active {
		options := candidatsProches(classement, a.desamb.Seuil, a.desamb.MaxChoix)
		if len(options) >= 2 {
			a.definirAttente(session, enAttente{
				candidats: options,
				texte:     nettoye,
			})
			logx.DebugT("nlp.desambiguisation.propose", len(options))
			msg := a.messageDesambiguisation(options)
			return &msg, verbe, true, false, &options[0]
		}
	}

	return a.executerMatch(classement[0].Appareil, nettoye)
}

// executerMatch applique le verbe (action) ou lit l'état de l'entité choisie
func (a *Analyseur) executerMatch(app ha.Appareil, texteNettoye string) (*types.Message, string, bool, bool, *ha.Appareil) {
	verbe, estAction := "", false
	if v, ok := domaineAUnVerbe(texteNettoye, app.Domain); ok {
		verbe, estAction = v, true
	}

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
		return &etat, verbe, true, estAction && !estActionParDefaut, &app
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

// domaineAUnVerbe cherche, dans le texte, un mot que le service de `domaine`
func domaineAUnVerbe(texte, domaine string) (string, bool) {
	svc, ok := ha.Lookup(domaine)
	if !ok {
		return "", false
	}
	for _, mot := range strings.Fields(texte) {
		if _, vok := svc.Verbe(mot); vok {
			return mot, true
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
func (a *Analyseur) classerAppareils(texteNettoye string, domainesCandidats []string) []Candidat {
	motsSMS := strings.Fields(texteNettoye)

	modificateurDemande := ""
	for _, p := range motsParasites {
		if strings.Contains(texteNettoye, p) {
			modificateurDemande = p
			break
		}
	}

	cacheAction := make(map[string]bool)
	estActionDomaine := func(domaine string) bool {
		if v, ok := cacheAction[domaine]; ok {
			return v
		}
		_, ok := domaineAUnVerbe(texteNettoye, domaine)
		cacheAction[domaine] = ok
		return ok
	}

	scorer := func(pool []ha.Appareil) []Candidat {
		out := make([]Candidat, 0, len(pool))
		for _, app := range pool {
			score := a.scorerAppareil(app, motsSMS, texteNettoye, modificateurDemande, estActionDomaine(app.Domain))
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
	_ = estAction
	classement := a.classerAppareils(texteNettoye, domainesCandidats)
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

// ---- IA (Gemini) ----

const (
	// maxActionsGemini borne le nombre d'actions / lectures exécutées pour une seule demande
	maxActionsGemini = 12
	// maxPlageAgenda borne la période d'agenda demandée par l'IA
	maxPlageAgenda = 120 * 24 * time.Hour
	// maxPlageHistorique borne la période d'historique demandée par l'IA
	maxPlageHistorique = 31 * 24 * time.Hour
)

// messageTexte construit un message à partir d'un texte libre (réponse de l'IA).
// Côté SMS / API le texte repasse par un fmt.Sprintf(texte, params...) : les « % »
// y sont échappés, sinon « 45 % d'humidité » devenait « %!d(MISSING) ».
// Côté voix le texte est lu tel quel.
func messageTexte(texte string) types.Message {
	return types.Message{
		SMS:  types.MessageDetails{Texte: i18n.Echapper(texte)},
		Voix: types.MessageDetails{Texte: texte},
	}
}

// trouverAppareil cherche une entité par son entity_id dans le catalogue.
func (a *Analyseur) trouverAppareil(entityID string) *ha.Appareil {
	for _, cand := range a.catalogue {
		if cand.EntityID == entityID {
			c := cand
			return &c
		}
	}
	return nil
}

// tenterGemini interroge l'IA et valide strictement sa réponse avant toute
// exécution : Gemini propose, le code décide. Trois types de réponse :
//   - speak  : réponse parlée (état lu dans le contexte, discussion) ;
//   - read   : lecture via les services HA (météo future, agenda passé/futur, heure...) ;
//   - history : état d'une entité dans le passé (historique HA sur une période) ;
//   - action : une ou plusieurs commandes (« ouvre salon 1 et 2 »).
func (a *Analyseur) tenterGemini(session, texte string) (*types.Message, string, bool, bool, *ha.Appareil) {
	_ = a.RafraichirCatalogue()

	// Contexte réduit aux entités pertinentes (désactivable : GEMINI_PRESELECTION=false)
	var retenus map[string]bool
	if a.ia.Preselection && a.ia.ContexteMax > 0 {
		retenus = a.preselectionIA(texte)
	}
	contexte, err := a.haClient.ContexteJSON(a.GetPieces(), retenus)
	if err != nil {
		logx.WarnT("gemini.contexte.erreur", err)
		return nil, "", false, false, nil
	}
	capacites, err := json.Marshal(ha.CapacitesIA())
	if err != nil {
		return nil, "", false, false, nil
	}

	rep, err := a.gemini.Interroger(a.historiquePour(session), texte, contexte, string(capacites))
	if err != nil {
		logx.WarnT("gemini.appel.erreur", err)
		return nil, "", false, false, nil
	}
	a.memoriser(session, texte, rep)

	switch rep.Type {
	case "speak":
		if rep.AttendReponse {
			a.definirEcoute(session)
		}
		msg := messageTexte(rep.ReponseVocale)
		return &msg, "", true, false, nil
	case "read":
		return a.executerLecturesGemini(rep)
	case "history":
		return a.executerHistoriqueGemini(rep)
	case "action":
		return a.executerActionsGemini(session, rep, false)
	}
	return nil, "", false, false, nil
}

// validerActionIA vérifie qu'une action proposée par l'IA est légitime : entité
// existante du bon domaine, domaine non interdit, verbe connu, action autorisée.
func (a *Analyseur) validerActionIA(act gemini.Action) (*ha.Appareil, ha.Service, string, bool) {
	app := a.trouverAppareil(act.EntityID)
	if app == nil || app.Domain != act.Domain || ha.DomaineExcluIA(app.Domain) {
		logx.WarnT("gemini.entite.rejetee", act.EntityID)
		return nil, nil, "", false
	}
	svc, ok := ha.Lookup(act.Domain)
	if !ok {
		return nil, nil, "", false
	}
	action, vok := svc.Verbe(act.Verbe)
	if !vok {
		logx.WarnT("gemini.verbe.rejete", act.Verbe, act.Domain)
		return nil, nil, "", false
	}
	if !ha.ActionIAAutorisee(act.Domain, action) {
		logx.WarnT("gemini.action.interdite", action, act.Domain)
		return nil, nil, "", false
	}
	return app, svc, action, true
}

// parametresBruts convertit la liste {nom, valeur} de l'IA en map.
func parametresBruts(act gemini.Action) map[string]string {
	m := make(map[string]string, len(act.Parametres))
	for _, p := range act.Parametres {
		if nom := strings.TrimSpace(p.Nom); nom != "" {
			m[nom] = strings.TrimSpace(p.Valeur)
		}
	}
	return m
}

// actionPreparee est une action de l'IA validée, prête à être exécutée.
type actionPreparee struct {
	app    *ha.Appareil
	svc    ha.Service
	action string
	verbe  string
	params map[string]interface{}
}

// executerActionsGemini valide puis exécute une ou plusieurs actions proposées par
// l'IA. Garde-fous : numéros de téléphone autorisés seulement, et confirmation orale
// avant un SMS ou une automatisation (sauf si `confirme`, c'est-à-dire déjà confirmée).
func (a *Analyseur) executerActionsGemini(session string, rep *gemini.Reponse, confirme bool) (*types.Message, string, bool, bool, *ha.Appareil) {
	actions := rep.Actions
	if len(actions) > maxActionsGemini {
		actions = actions[:maxActionsGemini]
	}

	var prepares []actionPreparee
	numeroRefuse := false
	for _, act := range actions {
		app, svc, action, ok := a.validerActionIA(act)
		if !ok {
			continue
		}

		params := svc.ExtraireParams(act.Complement)
		if params == nil {
			params = map[string]interface{}{}
		}
		for k, v := range a.haClient.ParametresIAValides(*app, parametresBruts(act)) {
			params[k] = v
		}

		if !a.destinatairesAutorises(app.Domain, params) {
			logx.WarnT("gemini.numero.refuse", app.EntityID)
			numeroRefuse = true
			continue
		}
		prepares = append(prepares, actionPreparee{app: app, svc: svc, action: action, verbe: act.Verbe, params: params})
	}

	if len(prepares) == 0 {
		if numeroRefuse {
			msg := messageTexte(i18n.T("garde.numero.refuse"))
			return &msg, "", true, false, nil
		}
		// Rien de valide : on laisse la main au NLP classique
		return nil, "", false, false, nil
	}

	// Confirmation orale des actions sensibles
	if a.ia.Confirmation && !confirme {
		var descriptions []string
		for _, p := range prepares {
			if d, sensible := decrireActionSensible(*p.app, p.action, p.params); sensible {
				descriptions = append(descriptions, d)
			}
		}
		if len(descriptions) > 0 {
			a.definirConfirmation(session, confirmationEnAttente{rep: rep})
			msg := messageTexte(i18n.T("confirmation.demande", strings.Join(descriptions, " ; ")))
			return &msg, "", true, false, prepares[0].app
		}
	}

	premier := prepares[0].app
	premierVerbe, dernierTexte := "", ""
	nbOK := 0
	for _, p := range prepares {
		retour, err := p.svc.ExecuterCommande(*p.app, p.verbe, p.params)
		if err != nil {
			logx.WarnT("gemini.action.erreur", p.app.EntityID, err)
			continue
		}
		nbOK++
		if premierVerbe == "" {
			premierVerbe = p.verbe
		}
		dernierTexte = retour
	}

	total := len(actions)
	switch {
	case nbOK == 0:
		msg := messageTexte(i18n.T("erreur.action.parler"))
		return &msg, premierVerbe, true, false, premier
	case total == 1:
		// Une seule action réussie : comportement historique (« j'ai <verbe> <appareil> »)
		msg := types.Message{
			SMS:  types.MessageDetails{Texte: dernierTexte},
			Voix: types.MessageDetails{Texte: dernierTexte},
		}
		return &msg, premierVerbe, true, true, premier
	case nbOK == total:
		texte := strings.TrimSpace(rep.ReponseVocale)
		if texte == "" {
			texte = i18n.T("gemini.actions.ok", nbOK)
		}
		msg := messageTexte(texte)
		return &msg, premierVerbe, true, false, premier
	default:
		msg := messageTexte(i18n.T("gemini.actions.partiel", nbOK, total))
		return &msg, premierVerbe, true, false, premier
	}
}

// executerLecturesGemini exécute les lectures demandées par l'IA en réutilisant
// les services HA (météo, agenda, heure...) : le message est construit et prononcé
// par le code, l'IA ne relit jamais le contenu (pas d'injection via un titre d'agenda).
func (a *Analyseur) executerLecturesGemini(rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil) {
	var msgs []types.Message
	var premier *ha.Appareil

	for i, act := range rep.Actions {
		if i >= maxActionsGemini {
			break
		}
		app := a.trouverAppareil(act.EntityID)
		if app == nil || app.Domain != act.Domain || ha.DomaineExcluIA(app.Domain) {
			logx.WarnT("gemini.lecture.rejetee", act.EntityID)
			continue
		}
		svc, ok := ha.Lookup(app.Domain)
		if !ok {
			continue
		}

		// Les services attendent un texte normalisé (sans accents), comme celui de Vosk
		texteNorm := text.Normaliser(act.Complement)
		params := svc.ExtraireParams(texteNorm)
		if params == nil {
			params = map[string]interface{}{}
		}
		if app.Domain == "agenda" {
			appliquerPeriodeAgenda(params, act)
		}

		msgs = append(msgs, a.lireEtatMessage(svc, *app, texteNorm, params))
		if premier == nil {
			premier = app
		}
	}

	if len(msgs) == 0 {
		return nil, "", false, false, nil
	}
	msg := fusionnerMessages(msgs)
	return &msg, "", true, false, premier
}

// fusionnerMessages concatène plusieurs messages (patterns + paramètres) en un seul.
func fusionnerMessages(msgs []types.Message) types.Message {
	if len(msgs) == 1 {
		return msgs[0]
	}
	var sms, voix strings.Builder
	var pSMS, pVoix []interface{}
	for i, m := range msgs {
		if i > 0 {
			sms.WriteString("\n")
			voix.WriteString(" ")
		}
		sms.WriteString(i18n.GetPattern(m.SMS.Texte))
		pSMS = append(pSMS, m.SMS.Params...)
		voix.WriteString(i18n.GetPattern(m.Voix.Texte))
		pVoix = append(pVoix, m.Voix.Params...)
	}
	return types.Message{
		SMS:  types.MessageDetails{Texte: sms.String(), Params: pSMS},
		Voix: types.MessageDetails{Texte: voix.String(), Params: pVoix},
	}
}

// appliquerPeriodeAgenda ajoute la période explicite (passé ou futur) demandée par l'IA.
func appliquerPeriodeAgenda(params map[string]interface{}, act gemini.Action) {
	debut, ok := parserDateIA(act.Debut)
	if !ok {
		return
	}
	fin, ok := parserDateIA(act.Fin)
	if !ok || !fin.After(debut) {
		fin = debut.Add(24 * time.Hour)
	}
	if fin.Sub(debut) > maxPlageAgenda {
		fin = debut.Add(maxPlageAgenda)
	}
	params["debut"] = debut
	params["fin"] = fin
}

// parserDateIA accepte RFC 3339, « 2006-01-02T15:04:05 » ou « 2006-01-02 » (heure locale).
func parserDateIA(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// executerHistoriqueGemini répond à une question sur le passé d'une entité en
// lisant l'historique HA sur la période demandée par l'IA. Le résumé est construit
// par le code (cf. ha.ResumerHistorique), l'IA ne relit rien.
func (a *Analyseur) executerHistoriqueGemini(rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil) {
	var textes []string
	var premier *ha.Appareil
	maintenant := time.Now()

	for i, act := range rep.Actions {
		if i >= maxActionsGemini {
			break
		}
		app := a.trouverAppareil(act.EntityID)
		if app == nil || app.Domain != act.Domain || ha.DomaineExcluIA(app.Domain) {
			logx.WarnT("gemini.historique.rejete", act.EntityID)
			continue
		}

		debut, ok := parserDateIA(act.Debut)
		if !ok {
			debut = maintenant.Add(-24 * time.Hour)
		}
		fin, ok := parserDateIA(act.Fin)
		if !ok || fin.After(maintenant) {
			fin = maintenant
		}
		if !fin.After(debut) {
			logx.WarnT("gemini.historique.rejete", act.EntityID)
			continue
		}
		if fin.Sub(debut) > maxPlageHistorique {
			debut = fin.Add(-maxPlageHistorique)
		}

		texte, err := a.haClient.ResumerHistorique(*app, debut, fin)
		if err != nil {
			logx.WarnT("gemini.historique.erreur", app.EntityID, err)
			continue
		}
		textes = append(textes, texte)
		if premier == nil {
			premier = app
		}
	}

	if len(textes) == 0 {
		return nil, "", false, false, nil
	}
	msg := messageTexte(strings.Join(textes, " "))
	return &msg, "", true, false, premier
}
