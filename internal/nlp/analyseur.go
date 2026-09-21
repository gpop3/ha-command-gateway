package nlp

import (
	"errors"
	"encoding/json"
	"ha-command-gateway/internal/core/adapters/gemini"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/utils/text"
	"ha-command-gateway/pkg/types"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
	annulations      map[string][]groupeAnnulation

	// Journal des décisions, base apprise, actions réussies à apprendre, dernières réponses
	decisions         *journalDecisions
	appris            *baseAppris
	actionsOK         map[string][]gemini.Action
	dernieresReponses map[string]string

	// Suites de phrase sans IA, propositions « tu voulais dire ? », état de l'IA (annoncé une fois)
	dernierClassique map[string]suiteClassique
	suggestions      map[string]suggestionEnAttente
	iaDegradee       bool
	iaNote           string
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
		annulations:        make(map[string][]groupeAnnulation),
		decisions:          nouveauJournalDecisions(),
		appris:             nouvelleBaseAppris(),
		actionsOK:          make(map[string][]gemini.Action),
		dernieresReponses:  make(map[string]string),
		dernierClassique:   make(map[string]suiteClassique),
		suggestions:        make(map[string]suggestionEnAttente),
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
//
// Chaque échange est consigné dans le journal des décisions (moteur, type choisi par l'IA,
// actions, résultat, coût, durée) ; « non, pas ça » juste après marque le dernier échange
// comme erroné ; une commande comprise par l'IA et entièrement réussie est apprise.
func (a *Analyseur) AnalyserEtExecuter(session, texte string) (*types.Message, string, bool, bool, *ha.Appareil) {
	debut := time.Now()

	// « Non, pas ça » : on marque le dernier échange comme erroné (sauf si on attend une réponse)
	if !a.reponseAttendue(session) && interpreterCorrection(texte) {
		if msg, ok := a.marquerCorrection(session); ok {
			return msg, "", true, false, nil
		}
	}

	rec := &Decision{Quand: debut, Canal: masquerCanal(session), Phrase: masquerNumeros(texte), Moteur: "classique", session: session}
	appels0, tokens0 := a.compteursIA()

	msg, verbe, match, isAction, app := a.analyserEtExecuterInterne(session, texte, rec)

	appels1, tokens1 := a.compteursIA()
	rec.DureeMs = time.Since(debut).Milliseconds()
	rec.AppelsIA, rec.Tokens = appels1-appels0, tokens1-tokens0
	rec.Reussi = match && msg != nil
	rec.Resultat = texteDuMessage(msg)
	if rec.Moteur == "classique" && app != nil && match {
		rec.entites = []string{app.EntityID}
		rec.Actions = []string{strings.TrimSpace(app.EntityID + " " + verbe)}
	}

	// Apprentissage : commande comprise par l'IA (ou rejouée) et entièrement réussie
	if acts := a.prendreActionsOK(session); len(acts) > 0 {
		switch rec.Moteur {
		case "gemini":
			a.apprendreDepuisIA(rec, texte, acts)
		case "appris":
			a.appris.succes(rec.cleAppris)
		}
	}

	// « Envoie-moi ça » enverra cette réponse — sauf si on attend une réponse (question, confirmation)
	if match && msg != nil && rec.Moteur != "confirmation" && !a.reponseAttendue(session) && (rec.Type != "enquete" || rec.Sujet != "notifier") {
		a.definirDerniereReponse(session, texteComplet(msg))
	}

	a.decisions.ajouter(rec)

	// Mode ombre : l'autre moteur dit ce qu'il aurait fait (sans exécuter)
	if a.ia.Ombre && rec.Reussi && (rec.Moteur == "gemini" || rec.Moteur == "classique") {
		go a.executerOmbre(rec.ID, texte, rec.Moteur, rec.Type, rec.entites)
	}

	// L'IA vient d'être suspendue (ou de revenir) : on le dit une fois
	msg, match, isAction = a.appliquerNoteIA(msg, verbe, match, isAction, app)
	return msg, verbe, match, isAction, app
}

func (a *Analyseur) compteursIA() (appels, tokens int) {
	if a.gemini == nil {
		return 0, 0
	}
	return a.gemini.Compteurs()
}

func (a *Analyseur) definirDerniereReponse(session, texte string) {
	a.muSessions.Lock()
	a.dernieresReponses[session] = texte
	a.muSessions.Unlock()
}

func (a *Analyseur) derniereReponse(session string) string {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	return a.dernieresReponses[session]
}

// rejouerConfirmation exécute ce qui attendait un « oui » : une notification, ou des actions.
func (a *Analyseur) rejouerConfirmation(session string, conf confirmationEnAttente) (*types.Message, string, bool, bool, *ha.Appareil) {
	if conf.rep.Enquete != nil && conf.rep.Enquete.Sujet == "oublier" {
		return a.oublierTout(), "", true, false, nil
	}
	if conf.rep.Type == "enquete" {
		msg, verbe, match, isAction, app, _ := a.executerNotification(session, conf.texte, conf.rep, true)
		return msg, verbe, match, isAction, app
	}
	msg, verbe, match, isAction, app, _ := a.executerActionsGemini(session, conf.texte, conf.rep, true)
	return msg, verbe, match, isAction, app
}

func (a *Analyseur) analyserEtExecuterInterne(session, texte string, rec *Decision) (*types.Message, string, bool, bool, *ha.Appareil) {
	nettoye := strings.ToLower(texte)

	// Confirmation en attente (action sensible proposée par l'IA)
	if conf, ok := a.confirmationPour(session); ok {
		a.effacerConfirmation(session)
		switch interpreterConfirmation(nettoye) {
		case reponseOui:
			rec.Moteur = "confirmation"
			return a.rejouerConfirmation(session, conf)
		case reponseNon:
			rec.Moteur = "confirmation"
			msg := messageTexte(i18n.T("confirmation.annule"))
			return &msg, "", true, false, nil
		}
		// Autre réponse : la confirmation est abandonnée, la phrase est traitée normalement
	}
	a.effacerEcoute(session)

	// « Tu voulais dire… ? » : réponse à une proposition
	if sug, ok := a.suggestionPour(session); ok {
		a.effacerSuggestion(session)
		switch interpreterConfirmation(nettoye) {
		case reponseOui:
			rec.Moteur = "suggestion"
			p := sug.propositions[sug.idx]
			return a.executerMatch(session, p.app, p.texte)
		case reponseNon:
			rec.Moteur = "suggestion"
			if sug.idx+1 < len(sug.propositions) {
				sug.idx++
				a.definirSuggestion(session, sug)
				msg := messageTexte(i18n.T("suggestion.autre", sug.propositions[sug.idx].libelle))
				return &msg, "", true, false, nil
			}
			msg := messageTexte(i18n.T("suggestion.abandon"))
			return &msg, "", true, false, nil
		}
		// Autre réponse : la proposition est abandonnée, la phrase est traitée normalement
	}

	if att, ok := a.attentePour(session); ok {
		if idx, ok := interpreterChoix(nettoye, len(att.candidats)); ok {
			a.effacerAttente(session)
			rec.Moteur = "choix"
			choisi := att.candidats[idx]
			logx.DebugT("nlp.desambiguisation.choix", choisi.FriendlyName)
			return a.executerMatch(session, choisi, att.texte)
		}
		a.effacerAttente(session)
	}

	// « Oublie tout » : efface journal, phrases apprises et mémoires — après confirmation
	if interpreterOubli(nettoye) {
		rec.Moteur, rec.Sujet = "oubli", "oublier"
		return a.demanderOubli(session, texte), "", true, false, nil
	}

	// « Annule ça » : détecté directement (sans passer par l'IA, qui répond parfois « c'est
	// annulé » sans rien faire) dès qu'une commande récente peut être annulée.
	if interpreterAnnulation(nettoye) && a.aUneAnnulation(session) {
		rec.Moteur, rec.Type, rec.Sujet = "annulation", "enquete", "annuler"
		msg, verbe, match, isAction, app, _ := a.executerAnnulation(session)
		return msg, verbe, match, isAction, app
	}

	if a.gemini != nil && a.geminiPrimary {
		if msg, verbe, match, isAction, app := a.tenterGemini(session, texte, rec); match {
			return msg, verbe, match, isAction, app
		}
	}

	// Phrase déjà comprise par l'IA (et réussie) : rejouée sans elle
	if cle := clePhrase(texte); cle != "" {
		if e := a.appris.chercher(cle); e != nil {
			rec.Moteur, rec.cleAppris = "appris", cle
			rep := &gemini.Reponse{Type: "action", Actions: e.Actions}
			msg, verbe, match, isAction, app, _ := a.executerActionsGemini(session, texte, rep, false)
			if match {
				for _, act := range e.Actions {
					rec.Actions = append(rec.Actions, strings.TrimSpace(act.EntityID+" "+act.Verbe))
				}
				return msg, verbe, match, isAction, app
			}
			rec.Moteur, rec.cleAppris = "classique", ""
		}
	}

	// « Et dans la chambre ? » : suite de la commande précédente, sans IA
	if suite, ok := a.suiteClassique(session, nettoye); ok {
		logx.DebugT("nlp.suite.classique", nettoye, suite)
		nettoye = suite
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
			if msg, verbe2, match, isAction, app := a.tenterGemini(session, texte, rec); match {
				return msg, verbe2, match, isAction, app
			}
		}
		// Rien de compris : on propose l'appareil le plus proche (« oui » l'exécute)
		if msg := a.proposerSuggestions(session, nettoye, estAction, classement); msg != nil {
			rec.Moteur = "suggestion"
			return msg, verbe, true, false, nil
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

	return a.executerMatch(session, classement[0].Appareil, nettoye)
}

// executerMatch applique le verbe (action) ou lit l'état de l'entité choisie
func (a *Analyseur) executerMatch(session string, app ha.Appareil, texteNettoye string) (*types.Message, string, bool, bool, *ha.Appareil) {
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
		snap, _ := a.haClient.SauvegarderEtat(app) // état d'avant, pour « annule ça »
		etat, reussi := a.executerActionMessageOK(svc, app, verbe, params)
		if reussi {
			a.memoriserClassique(session, texteNettoye)
			g := groupeAnnulation{quand: time.Now()}
			if snap != nil {
				g.etats = []ha.EtatSauve{*snap}
			} else {
				g.nonAnnulables = []string{nomAppareil(app)}
			}
			a.empilerAnnulation(session, g)
		}
		return &etat, verbe, true, estAction && !estActionParDefaut, &app
	}

	if !ok {
		svc, _ = ha.Lookup("service_default")
	}

	etat := a.lireEtatMessage(svc, app, texteNettoye, params)
	a.memoriserClassique(session, texteNettoye)
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

// executerActionMessageOK : comme executerActionMessage, et dit si la commande a réussi.
func (a *Analyseur) executerActionMessageOK(svc ha.Service, app ha.Appareil, verbe string, params map[string]interface{}) (types.Message, bool) {
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
		}, false
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
	}, true
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
	// maxPlageClassement borne la période d'un classement de capteurs (lecture groupée)
	maxPlageClassement = 7 * 24 * time.Hour
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

// reEntityID : forme valide d'un entity_id (évite d'injecter n'importe quoi dans une URL HA).
var reEntityID = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`)

// domaineDeEntite : le domaine fait foi dans l'entity_id (« media_player.xxx »), pas
// dans le champ « domain » que l'IA remplit parfois de travers.
func domaineDeEntite(entityID, parDefaut string) string {
	if i := strings.Index(entityID, "."); i > 0 {
		return entityID[:i]
	}
	return parDefaut
}

// trouverAppareil cherche une entité par son entity_id : d'abord dans le catalogue
// local, puis — si elle en est absente (catalogue périmé, entité sans friendly_name...)
// — directement auprès de HA, qui fait foi. Une entité qui n'existe pas dans HA
// n'est jamais retournée.
func (a *Analyseur) trouverAppareil(entityID string) *ha.Appareil {
	for _, cand := range a.catalogue {
		if cand.EntityID == entityID {
			c := cand
			return &c
		}
	}

	if !reEntityID.MatchString(entityID) {
		return nil
	}
	etat, err := a.haClient.RecupererEtatLive(entityID)
	if err != nil || etat == nil || etat.EntityID != entityID {
		return nil
	}
	logx.DebugT("gemini.entite.hors.catalogue", entityID)
	nom := etat.Attributes.FriendlyName
	if nom == "" {
		nom = entityID
	}
	return &ha.Appareil{
		EntityID:          entityID,
		FriendlyName:      nom,
		FriendlyNameExact: nom,
		State:             etat.State,
		Domain:            strings.SplitN(entityID, ".", 2)[0],
	}
}

// tenterGemini interroge l'IA et valide strictement sa réponse avant toute
// exécution : Gemini propose, le code décide. Quatre types de réponse :
//   - speak  : réponse parlée (état lu dans le contexte, discussion) ;
//   - read   : lecture via les services HA (météo future, agenda passé/futur, heure...) ;
//   - history : état d'une entité dans le passé (historique HA sur une période) ;
//   - action : une ou plusieurs commandes (« ouvre salon 1 et 2 »).
//
// Si le code rejette la réponse (entité inconnue, verbe invalide...), Gemini a une
// seconde chance : on lui renvoie les motifs précis du rejet pour qu'il se corrige.
func (a *Analyseur) tenterGemini(session, texte string, rec *Decision) (*types.Message, string, bool, bool, *ha.Appareil) {
	contexte, capacites, err := a.contexteIA(texte)
	if err != nil {
		logx.WarnT("gemini.contexte.erreur", err)
		return nil, "", false, false, nil
	}

	historique := a.historiquePour(session)
	rep, err := a.gemini.Interroger(historique, texte, contexte, capacites)
	if err != nil {
		a.journaliserErreurIA(err)
		return nil, "", false, false, nil
	}

	a.surSuccesIA()
	msg, verbe, match, isAction, app, rejets := a.traiterReponseGemini(session, texte, rep)

	// Seconde chance : on explique à Gemini pourquoi sa réponse a été rejetée
	if !match && len(rejets) > 0 && a.ia.SecondeChance {
		motifs := strings.Join(rejets, " ; ")
		logx.InfoT("gemini.seconde.chance", motifs)
		rec.SecondeChance = true
		brut, _ := json.Marshal(rep)
		hist2 := append(append([]gemini.Tour(nil), historique...), gemini.Tour{Demande: texte, Reponse: string(brut)})
		rep2, err2 := a.gemini.Reinterroger(hist2, i18n.T("gemini.correction", motifs), contexte, capacites)
		if err2 != nil {
			a.journaliserErreurIA(err2)
		} else {
			rep = rep2
			msg, verbe, match, isAction, app, rejets = a.traiterReponseGemini(session, texte, rep)
		}
	}

	a.memoriser(session, texte, rep)
	rec.Rejets = rejets
	rec.Type = rep.Type
	if rep.Enquete != nil {
		rec.Sujet = rep.Enquete.Sujet
	}
	for _, act := range rep.Actions {
		rec.Actions = append(rec.Actions, strings.TrimSpace(act.EntityID+" "+act.Verbe))
		if rep.Type == "action" {
			rec.entites = append(rec.entites, act.EntityID)
		}
	}
	if match {
		rec.Moteur = "gemini"
	}
	return msg, verbe, match, isAction, app
}

// journaliserErreurIA : les erreurs « attendues » (quota local, disjoncteur ouvert,
// anti-rafale) restent discrètes pour ne pas inonder les logs pendant une panne.
func (a *Analyseur) journaliserErreurIA(err error) {
	a.surErreurIA(err)
	if errors.Is(err, gemini.ErrQuota) || errors.Is(err, gemini.ErrIndisponible) || errors.Is(err, gemini.ErrAntiRafale) {
		logx.DebugT("gemini.appel.ignore", err)
		return
	}
	logx.WarnT("gemini.appel.erreur", err)
}

// traiterReponseGemini exécute une réponse de l'IA. Le dernier retour liste les
// motifs de rejet (s'il y en a) : il alimente la seconde chance.
func (a *Analyseur) traiterReponseGemini(session, texte string, rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	switch rep.Type {
	case "speak":
		if rep.AttendReponse {
			a.definirEcoute(session)
		}
		msg := messageTexte(rep.ReponseVocale)
		return &msg, "", true, false, nil, nil
	case "read":
		return a.executerLecturesGemini(texte, rep)
	case "history":
		return a.executerHistoriqueGemini(texte, rep)
	case "classement":
		return a.executerClassementGemini(texte, rep)
	case "journal":
		return a.executerJournalGemini(texte, rep)
	case "recherche":
		return a.executerRechercheGemini(texte, rep)
	case "enquete":
		return a.executerEnqueteGemini(session, texte, rep)
	case "action":
		return a.executerActionsGemini(session, texte, rep, false)
	}
	return nil, "", false, false, nil, nil
}

// raisonEntite explique pourquoi une entité proposée par l'IA est refusée (motif
// renvoyé à Gemini pour sa seconde chance). Chaîne vide si elle est acceptable.
func raisonEntite(act gemini.Action, app *ha.Appareil) string {
	switch {
	case app == nil:
		return i18n.T("gemini.rejet.entite", act.EntityID)
	case app.Domain != act.Domain:
		return i18n.T("gemini.rejet.domaine", act.EntityID, app.Domain, act.Domain)
	case ha.DomaineExcluIA(app.Domain):
		return i18n.T("gemini.rejet.exclu", app.Domain)
	}
	return ""
}

// validerActionIA vérifie qu'une action proposée par l'IA est légitime : entité
// existante du bon domaine, domaine non interdit, verbe connu, action autorisée.
// En cas de refus, l'erreur porte le motif (renvoyé à Gemini).
func (a *Analyseur) validerActionIA(act gemini.Action) (*ha.Appareil, ha.Service, string, error) {
	act.Domain = domaineDeEntite(act.EntityID, act.Domain)
	app := a.trouverAppareil(act.EntityID)
	if raison := raisonEntite(act, app); raison != "" {
		switch {
		case app == nil:
			logx.WarnT("gemini.entite.introuvable", act.EntityID)
		case app.Domain != act.Domain:
			logx.WarnT("gemini.entite.domaine", act.EntityID, act.Domain, app.Domain)
		default:
			logx.WarnT("gemini.domaine.exclu", app.Domain, act.EntityID)
		}
		return nil, nil, "", errors.New(raison)
	}
	svc, ok := ha.Lookup(act.Domain)
	if !ok {
		return nil, nil, "", errors.New(i18n.T("gemini.rejet.domaine.inconnu", act.Domain))
	}
	verbe := strings.TrimSpace(act.Verbe)
	if verbe == "" && act.Domain == "script" {
		verbe = "exécute" // un script n'a qu'une action possible : l'IA peut omettre le verbe
	}
	action, vok := svc.Verbe(verbe)
	if !vok {
		logx.WarnT("gemini.verbe.rejete", act.Verbe, act.Domain)
		return nil, nil, "", errors.New(i18n.T("gemini.rejet.verbe", act.Verbe, act.Domain, strings.Join(ha.CapacitesIA()[act.Domain].Verbes, ", ")))
	}
	if !ha.ActionIAAutorisee(act.Domain, action) {
		logx.WarnT("gemini.action.interdite", action, act.Domain)
		return nil, nil, "", errors.New(i18n.T("gemini.rejet.action", action, act.Domain))
	}
	return app, svc, action, nil
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

// succesAction : une action exécutée avec succès (sert à formuler le retour parlé).
type succesAction struct {
	verbe  string
	nom    string
	params map[string]interface{}
}

func nomAppareil(app ha.Appareil) string {
	if app.FriendlyNameExact != "" {
		return app.FriendlyNameExact
	}
	return app.FriendlyName
}

// listeNoms : « A », « A et B », « A, B et C ».
func listeNoms(noms []string) string {
	switch len(noms) {
	case 0:
		return ""
	case 1:
		return noms[0]
	}
	return strings.Join(noms[:len(noms)-1], ", ") + " " + i18n.T("mot.et") + " " + noms[len(noms)-1]
}

// phraseSucces formule ce qui a été fait, en français naturel (voix comme SMS) :
// « J'ai éteint Salon et Cuisine. J'ai fermé Volet salon. »
func phraseSucces(succes []succesAction) string {
	var ordre []string
	noms := map[string][]string{}
	var phrases []string

	for _, s := range succes {
		// Réglage chiffré : « Volet salon réglé à 50 pour cent. »
		if pct, ok := s.params["pourcentage"].(int); ok {
			phrases = append(phrases, i18n.T("retour.pourcentage", s.nom, pct))
			continue
		}
		if t, ok := s.params["temperature"].(float64); ok {
			phrases = append(phrases, i18n.T("retour.temperature", s.nom, strings.ReplaceAll(strconv.FormatFloat(t, 'f', -1, 64), ".", ",")))
			continue
		}
		v := strings.ToLower(strings.TrimSpace(s.verbe))
		if _, vu := noms[v]; !vu {
			ordre = append(ordre, v)
		}
		dejaLa := false
		for _, n := range noms[v] {
			if n == s.nom {
				dejaLa = true
			}
		}
		if !dejaLa {
			noms[v] = append(noms[v], s.nom)
		}
	}

	groupes := make([]string, 0, len(ordre))
	for _, v := range ordre {
		cle := "retour.verbe." + v
		if i18n.Existe(cle) {
			groupes = append(groupes, i18n.T(cle, listeNoms(noms[v])))
		} else {
			groupes = append(groupes, i18n.T("retour.action.generique", listeNoms(noms[v])))
		}
	}
	return strings.Join(append(groupes, phrases...), " ")
}

// executerActionsGemini valide puis exécute une ou plusieurs actions proposées par
// l'IA. Garde-fous : numéros de téléphone autorisés seulement, et confirmation orale
// avant un SMS ou une automatisation (sauf si `confirme`, c'est-à-dire déjà confirmée).
// Le dernier retour liste les motifs de rejet (seconde chance de l'IA).
func (a *Analyseur) executerActionsGemini(session, texte string, rep *gemini.Reponse, confirme bool) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	actions := rep.Actions
	if len(actions) > maxActionsGemini {
		actions = actions[:maxActionsGemini]
	}

	var prepares []actionPreparee
	var rejets []string
	numeroRefuse := false
	for _, act := range actions {
		app, svc, action, err := a.validerActionIA(act)
		if err != nil {
			rejets = append(rejets, err.Error())
			continue
		}

		params := svc.ExtraireParams(act.Complement)
		if params == nil {
			params = map[string]interface{}{}
		}
		for k, v := range a.haClient.ParametresIAValides(*app, parametresBruts(act)) {
			params[k] = v
		}
		if app.Domain == "media_player" {
			// Permet de retrouver l'enceinte cible dans la phrase si l'IA ne l'a pas précisée
			params["texte"] = texte
		}
		if app.Domain == "script" {
			params["ia"] = true // contrôle des champs obligatoires côté service
			// Script sans champ déclaré : le texte à transmettre est repris du complément de l'IA
			if _, ok := params["message"]; !ok && len(a.haClient.ChampsScript(app.EntityID)) == 0 {
				if c := strings.TrimSpace(act.Complement); c != "" {
					params["message"] = c
				}
			}
			// Un SMS sans message : on le demande au lieu de lancer le script à vide
			if estScriptSMS(*app) && !contenuFourni(params) {
				a.definirEcoute(session)
				msg := messageTexte(i18n.T("gemini.sms.message.manquant"))
				return &msg, "", true, false, app, nil
			}
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
			return &msg, "", true, false, nil, nil
		}
		// Rien de valide : on laisse la main au NLP classique (après seconde chance de l'IA)
		return nil, "", false, false, nil, rejets
	}

	// Confirmation orale des actions sensibles
	if a.ia.Confirmation && !confirme {
		var descriptions []string
		for _, p := range prepares {
			if d, sensible := decrireActionSensible(*p.app, p.action, p.params); sensible {
				descriptions = append(descriptions, d)
			}
		}
		// Grosse action groupée (« éteins tout ») : on demande aussi confirmation
		if a.ia.SeuilGroupe > 0 && len(prepares) >= a.ia.SeuilGroupe {
			noms := make([]string, 0, 5)
			for i, p := range prepares {
				if i >= 5 {
					break
				}
				noms = append(noms, nomAppareil(*p.app))
			}
			liste := strings.Join(noms, ", ")
			if len(prepares) > 5 {
				liste += " " + i18n.T("briefing.agenda.autres", len(prepares)-5)
			}
			descriptions = append(descriptions, i18n.T("confirmation.groupe", prepares[0].verbe, len(prepares), liste))
		}
		if len(descriptions) > 0 {
			a.definirConfirmation(session, confirmationEnAttente{rep: rep, texte: texte})
			msg := messageTexte(i18n.T("confirmation.demande", strings.Join(descriptions, " ; ")))
			return &msg, "", true, false, prepares[0].app, nil
		}
	}

	premier := prepares[0].app
	var succes []succesAction
	var echecs, avertissements []string
	question := ""
	var sauvegardes []ha.EtatSauve
	var nonAnnulables []string
	for _, p := range prepares {
		snap, _ := a.haClient.SauvegarderEtat(*p.app) // état d'avant, pour « annule ça »
		retour, err := p.svc.ExecuterCommande(*p.app, p.verbe, p.params)
		nom := nomAppareil(*p.app)
		if err != nil {
			logx.WarnT("gemini.action.erreur", p.app.EntityID, err)
			// Champ obligatoire d'un script manquant : on pose la question à l'utilisateur
			var manque *ha.ErreurParametreManquant
			if errors.As(err, &manque) && question == "" {
				question = i18n.T("gemini.parametre.manquant.question", strings.Join(manque.Champs, ", "))
			}
			echecs = append(echecs, nom)
			continue
		}
		// Convention des services : un retour « ⚠️ … » est un échec expliqué à l'utilisateur
		if strings.HasPrefix(retour, "⚠️") {
			avertissements = append(avertissements, retour)
			echecs = append(echecs, nom)
			continue
		}
		succes = append(succes, succesAction{verbe: p.verbe, nom: nom, params: p.params})
		if snap != nil {
			sauvegardes = append(sauvegardes, *snap)
		} else {
			nonAnnulables = append(nonAnnulables, nom)
		}
	}
	if len(succes) > 0 {
		a.empilerAnnulation(session, groupeAnnulation{quand: time.Now(), etats: sauvegardes, nonAnnulables: nonAnnulables})
	}

	nonExecutees := len(actions) - len(prepares) // refusées par la validation
	switch {
	case len(succes) == 0 && question != "":
		a.definirEcoute(session)
		msg := messageTexte(question)
		return &msg, "", true, false, premier, nil
	case len(succes) == 0 && len(avertissements) > 0:
		msg := messageTexte(avertissements[0])
		return &msg, "", true, false, premier, nil
	case len(succes) == 0:
		msg := messageTexte(i18n.T("retour.echec", listeNoms(echecs)))
		return &msg, "", true, false, premier, nil
	}

	// Toutes les actions ont réussi : la commande pourra être apprise (cf. appris.go)
	if len(echecs) == 0 && nonExecutees == 0 && len(succes) == len(actions) {
		if acts := actionsApprenables(actions, prepares); acts != nil {
			a.definirActionsOK(session, acts)
		}
	}

	texteFinal := phraseSucces(succes)
	if len(echecs) > 0 {
		texteFinal += " " + i18n.T("retour.echec.partiel", listeNoms(echecs))
	}
	if nonExecutees > 0 {
		texteFinal += " " + i18n.T("retour.rejet.partiel", nonExecutees)
	}
	msg := messageTexte(texteFinal)
	return &msg, succes[0].verbe, true, false, premier, nil
}

// executerLecturesGemini exécute les lectures demandées par l'IA en réutilisant
// les services HA (météo, agenda, heure...) : le message est construit et prononcé
// par le code, l'IA ne relit jamais le contenu (pas d'injection via un titre d'agenda).
func (a *Analyseur) executerLecturesGemini(texte string, rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	var msgs []types.Message
	var premier *ha.Appareil
	var rejets []string

	for i, act := range rep.Actions {
		if i >= maxActionsGemini {
			break
		}
		act.Domain = domaineDeEntite(act.EntityID, act.Domain)
		app := a.trouverAppareil(act.EntityID)
		if raison := raisonEntite(act, app); raison != "" {
			logx.WarnT("gemini.lecture.rejetee", act.EntityID)
			rejets = append(rejets, raison)
			continue
		}
		// Un calendrier (calendar.xxx) se lit par l'agenda : l'IA propose souvent
		// directement l'entité du calendrier. Sans filtre explicite de sa part, on
		// restreint la lecture à ce calendrier.
		if app.Domain == "calendar" {
			if ag := a.trouverAppareil("agenda.home"); ag != nil {
				if _, filtre := parametresBruts(act)["calendrier"]; !filtre {
					act.Parametres = append(act.Parametres, gemini.Parametre{Nom: "calendrier", Valeur: app.EntityID})
				}
				app = ag
			}
		}
		svc, ok := ha.Lookup(app.Domain)
		if !ok {
			continue
		}

		// Briefing : le code lit les données, l'IA en fait un texte oral (avec le saint du jour)
		if app.Domain == "briefing" {
			if msg := a.briefingIA(texte, svc); msg != nil {
				msgs = append(msgs, *msg)
				if premier == nil {
					premier = app
				}
				continue
			}
		}

		// Les services attendent un texte normalisé (sans accents), comme celui de Vosk
		texteNorm := text.Normaliser(act.Complement)
		params := svc.ExtraireParams(texteNorm)
		if params == nil {
			params = map[string]interface{}{}
		}
		for k, v := range a.haClient.ParametresIAValides(*app, parametresBruts(act)) {
			params[k] = v
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
		return nil, "", false, false, nil, rejets
	}
	msg := fusionnerMessages(msgs)
	return &msg, "", true, false, premier, nil
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
// par le code (cf. ha.DonneesHistorique). Si l'utilisateur veut un avis (« est-ce
// normal ? »), un second appel léger demande à l'IA de commenter ces chiffres.
func (a *Analyseur) executerHistoriqueGemini(texte string, rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	var textes []string
	var donnees []*ha.DonneesHist
	var premier *ha.Appareil
	var rejets []string
	maintenant := time.Now()

	for i, act := range rep.Actions {
		if i >= maxActionsGemini {
			break
		}
		act.Domain = domaineDeEntite(act.EntityID, act.Domain)
		app := a.trouverAppareil(act.EntityID)
		if raison := raisonEntite(act, app); raison != "" {
			logx.WarnT("gemini.historique.rejete", act.EntityID)
			rejets = append(rejets, raison)
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

		d, err := a.haClient.DonneesHistorique(*app, debut, fin)
		if err != nil {
			logx.WarnT("gemini.historique.erreur", app.EntityID, err)
			continue
		}
		textes = append(textes, d.Resume)
		donnees = append(donnees, d)
		if premier == nil {
			premier = app
		}
	}

	if len(textes) == 0 {
		return nil, "", false, false, nil, rejets
	}

	// Deuxième appel : l'IA formule la réponse en langage naturel (et donne son avis si
	// demandé). En cas d'échec, on garde le résumé du code.
	if analyse := a.analyserDonneesIA(texte, map[string]interface{}{"avis_demande": rep.Analyser, "mesures": donnees}); analyse != "" {
		msg := messageTexte(analyse)
		return &msg, "", true, false, premier, nil
	}

	msg := messageTexte(strings.Join(textes, " "))
	return &msg, "", true, false, premier, nil
}

// analyserDonneesIA envoie à l'IA des données déjà calculées par le code pour qu'elle
// les commente. Retourne "" si l'analyse est désactivée ou échoue (l'appelant se
// rabat alors sur le résumé chiffré du code).
func (a *Analyseur) analyserDonneesIA(question string, donnees interface{}) string {
	if !a.ia.Analyse || a.gemini == nil {
		return ""
	}
	brut, err := json.Marshal(donnees)
	if err != nil {
		return ""
	}
	analyse, err := a.gemini.Analyser(question, string(brut))
	if err != nil {
		a.journaliserErreurIA(err)
		return ""
	}
	return analyse
}

// executerClassementGemini compare plusieurs capteurs (« quelle pièce est la plus
// humide ? ») : le classement est calculé par le code, sur les valeurs actuelles ou sur
// une période (7 jours maximum).
func (a *Analyseur) executerClassementGemini(texte string, rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	cl := rep.Classement
	if cl == nil || strings.TrimSpace(cl.TypeMesure) == "" {
		return nil, "", false, false, nil, []string{i18n.T("gemini.rejet.classement")}
	}

	var debut, fin time.Time
	maintenant := time.Now()
	if d, ok := parserDateIA(cl.Debut); ok {
		debut, fin = d, maintenant
		if f, ok := parserDateIA(cl.Fin); ok && f.Before(maintenant) {
			fin = f
		}
		if !fin.After(debut) {
			debut, fin = time.Time{}, time.Time{}
		} else if fin.Sub(debut) > maxPlageClassement {
			debut = fin.Add(-maxPlageClassement)
		}
	}
	top, _ := strconv.Atoi(strings.TrimSpace(cl.Top))

	res, err := a.haClient.ClasserCapteurs(cl.TypeMesure, cl.Critere, cl.Piece, debut, fin, top)
	if err != nil {
		logx.WarnT("gemini.classement.erreur", err)
		return nil, "", false, false, nil, nil
	}
	if analyse := a.analyserDonneesIA(texte, map[string]interface{}{"avis_demande": rep.Analyser, "classement": res}); analyse != "" {
		res = analyse
	}
	msg := messageTexte(res)
	return &msg, "", true, false, nil, nil
}

// briefingIA lit les données du briefing (code) puis demande à l'IA de les formuler
// oralement, saint du jour compris. Retourne nil si l'IA est désactivée ou échoue :
// l'appelant se rabat alors sur le briefing complet assemblé par le code.
func (a *Analyseur) briefingIA(question string, svc ha.Service) *types.Message {
	sb, ok := svc.(*ha.ServiceBriefing)
	if !ok || !a.ia.Analyse || a.gemini == nil {
		return nil
	}
	brut, err := json.Marshal(sb.Sections(time.Now()))
	if err != nil {
		return nil
	}
	texte, err := a.gemini.Resumer(question, string(brut))
	if err != nil {
		a.journaliserErreurIA(err)
		return nil
	}
	msg := messageTexte(texte)
	return &msg
}

// executerJournalGemini répond à « qui a allumé… ? », « pourquoi… ? », « la dernière fois
// que… ? » en lisant le journal HA (avec la cause de chaque changement). Le texte est
// ensuite reformulé oralement par l'IA (repli : le résumé du code).
func (a *Analyseur) executerJournalGemini(texte string, rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	var textes []string
	var donnees []*ha.DonneesJournal
	var premier *ha.Appareil
	var rejets []string
	maintenant := time.Now()

	for i, act := range rep.Actions {
		if i >= maxActionsGemini {
			break
		}
		act.Domain = domaineDeEntite(act.EntityID, act.Domain)
		app := a.trouverAppareil(act.EntityID)
		if raison := raisonEntite(act, app); raison != "" {
			logx.WarnT("gemini.historique.rejete", act.EntityID)
			rejets = append(rejets, raison)
			continue
		}

		debut, ok := parserDateIA(act.Debut)
		if !ok {
			debut = maintenant.Add(-7 * 24 * time.Hour)
		}
		fin, ok := parserDateIA(act.Fin)
		if !ok || fin.After(maintenant) {
			fin = maintenant
		}
		if !fin.After(debut) {
			continue
		}
		if fin.Sub(debut) > maxPlageHistorique {
			debut = fin.Add(-maxPlageHistorique)
		}

		opts := parametresBruts(act)
		dernier := strings.EqualFold(opts["dernier"], "true")
		d, err := a.haClient.JournalEntite(*app, debut, fin, opts["etat"], dernier)
		if err != nil {
			logx.WarnT("gemini.journal.erreur", app.EntityID, err)
			continue
		}
		textes = append(textes, d.Resume)
		donnees = append(donnees, d)
		if premier == nil {
			premier = app
		}
	}

	if len(textes) == 0 {
		return nil, "", false, false, nil, rejets
	}
	if analyse := a.analyserDonneesIA(texte, map[string]interface{}{"avis_demande": rep.Analyser, "journal": donnees}); analyse != "" {
		msg := messageTexte(analyse)
		return &msg, "", true, false, premier, nil
	}
	msg := messageTexte(strings.Join(textes, " "))
	return &msg, "", true, false, premier, nil
}

// executerRechercheGemini cherche un événement dans la courbe d'un capteur numérique
// (plus forte chute, baisse d'au moins X, passage sous un seuil...). C'est le code qui
// cherche dans les points de l'historique ; l'IA fournit le capteur, le seuil, la période.
func (a *Analyseur) executerRechercheGemini(texte string, rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	r := rep.Recherche
	if r == nil || strings.TrimSpace(r.EntityID) == "" || strings.TrimSpace(r.Mode) == "" {
		return nil, "", false, false, nil, []string{i18n.T("gemini.rejet.recherche")}
	}

	act := gemini.Action{EntityID: r.EntityID}
	act.Domain = domaineDeEntite(act.EntityID, "")
	app := a.trouverAppareil(r.EntityID)
	if raison := raisonEntite(act, app); raison != "" {
		logx.WarnT("gemini.historique.rejete", r.EntityID)
		return nil, "", false, false, nil, []string{raison}
	}

	maintenant := time.Now()
	debut, ok := parserDateIA(r.Debut)
	if !ok {
		debut = maintenant.Add(-24 * time.Hour)
	}
	fin, ok := parserDateIA(r.Fin)
	if !ok || fin.After(maintenant) {
		fin = maintenant
	}
	if !fin.After(debut) {
		return nil, "", false, false, nil, nil
	}
	if fin.Sub(debut) > maxPlageHistorique {
		debut = fin.Add(-maxPlageHistorique)
	}

	valeur, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.Valeur), ",", "."), 64)
	minutes, _ := strconv.Atoi(strings.TrimSpace(r.FenetreMinutes))
	mode := strings.ToLower(strings.TrimSpace(r.Mode))

	res, err := a.haClient.RechercheEvenement(*app, mode, valeur, time.Duration(minutes)*time.Minute, debut, fin)
	if err != nil {
		logx.WarnT("gemini.recherche.erreur", r.EntityID, err)
		return nil, "", false, false, nil, nil
	}
	if analyse := a.analyserDonneesIA(texte, map[string]interface{}{"avis_demande": rep.Analyser, "recherche": res}); analyse != "" {
		msg := messageTexte(analyse)
		return &msg, "", true, false, app, nil
	}
	msg := messageTexte(res.Resume)
	return &msg, "", true, false, app, nil
}

// NbEntites : taille du catalogue d'entités en mémoire (pour /status).
func (a *Analyseur) NbEntites() int { return len(a.catalogue) }

// NbSessions : conversations en mémoire (pour /status).
func (a *Analyseur) NbSessions() int {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	return len(a.memoires)
}
