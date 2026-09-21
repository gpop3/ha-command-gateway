package nlp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"ha-command-gateway/internal/core/adapters/gemini"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/utils/conversion"
	"ha-command-gateway/internal/utils/text"
)

// ConfigIA regroupe les réglages propres à l'IA (contexte, mémoire, garde-fous).
type ConfigIA struct {
	// Preselection : n'envoyer à l'IA que les entités les plus proches de la
	// phrase (plus les scripts, la météo, les minuteurs, les lecteurs média et les
	// entités virtuelles). false = tout le contexte est envoyé à chaque appel.
	Preselection bool
	ContexteMax  int // nombre d'entités retenues par le scoring (les pièces citées s'y ajoutent)

	// MemoireTours : nombre d'échanges gardés par session (0 = pas de mémoire).
	MemoireTours int
	MemoireDuree time.Duration

	// Confirmation : demander une confirmation orale avant un SMS ou une automatisation.
	Confirmation bool
	// NumerosAutorises : seuls numéros que l'IA peut viser dans une action.
	NumerosAutorises []string

	// SecondeChance : quand le code rejette la réponse de l'IA (entité inconnue,
	// verbe invalide...), la rappeler une fois en lui expliquant l'erreur.
	SecondeChance bool

	// Ombre : mode ombre — l'autre moteur (classique ⇄ IA) dit ce qu'il aurait fait, sans
	// exécuter ; les désaccords sont journalisés. L'IA ombre coûte des tokens.
	Ombre bool

	// ServiceNotification : service notify.* de l'application mobile (ex. mobile_app_pixel_8).
	// Vide = détection automatique.
	ServiceNotification string

	// SeuilGroupe : au-delà de ce nombre d'actions d'un coup (« éteins tout »), demander
	// une confirmation orale. 0 = jamais.
	SeuilGroupe int

	// Analyse : après une lecture d'historique ou un classement, rappeler l'IA pour
	// qu'elle commente les chiffres (« est-ce normal ? »). Un appel en plus.
	Analyse bool
}

func configIAParDefaut() ConfigIA {
	return ConfigIA{
		Preselection: true,
		ContexteMax:  40,
		MemoireTours: 3,
		MemoireDuree: 3 * time.Minute,
		Confirmation: true,
		SecondeChance: true,
		Analyse:       true,
		SeuilGroupe:   5,
	}
}

// DefinirConfigIA applique la configuration IA (à appeler avant de démarrer les services).
func (a *Analyseur) DefinirConfigIA(cfg ConfigIA) {
	a.ia = cfg
	a.numerosAutorises = make(map[string]bool, len(cfg.NumerosAutorises))
	for _, n := range cfg.NumerosAutorises {
		if num, ok := normaliserNumero(n); ok {
			a.numerosAutorises[num] = true
		}
	}
}

// ---- Mémoire de conversation (une par session : jamais partagée entre canaux) ----

// memoireSession garde les derniers échanges d'UNE session (voix, console, un
// numéro de téléphone, une conversation Home Assistant). Les sessions sont
// totalement étanches : la conversation d'un canal n'est jamais envoyée à l'IA
// pour un autre. La mémoire vit en RAM et expire après MemoireDuree.
type memoireSession struct {
	tours []gemini.Tour
	maj   time.Time
}

func (a *Analyseur) historiquePour(session string) []gemini.Tour {
	if a.ia.MemoireTours <= 0 {
		return nil
	}
	a.muSessions.Lock()
	defer a.muSessions.Unlock()

	m, ok := a.memoires[session]
	if !ok {
		return nil
	}
	if time.Since(m.maj) > a.ia.MemoireDuree {
		delete(a.memoires, session)
		return nil
	}
	return append([]gemini.Tour(nil), m.tours...)
}

func (a *Analyseur) memoriser(session, demande string, rep *gemini.Reponse) {
	if a.ia.MemoireTours <= 0 {
		return
	}
	brut, err := json.Marshal(rep)
	if err != nil {
		return
	}

	a.muSessions.Lock()
	defer a.muSessions.Unlock()

	// Nettoyage opportuniste des sessions expirées (les conversations HA sont nombreuses)
	for k, v := range a.memoires {
		if time.Since(v.maj) > a.ia.MemoireDuree {
			delete(a.memoires, k)
		}
	}

	m := a.memoires[session]
	if m == nil {
		m = &memoireSession{}
		a.memoires[session] = m
	}
	m.tours = append(m.tours, gemini.Tour{Demande: demande, Reponse: string(brut)})
	if len(m.tours) > a.ia.MemoireTours {
		m.tours = m.tours[len(m.tours)-a.ia.MemoireTours:]
	}
	m.maj = time.Now()
}

// ---- Écoute prolongée et confirmations ----

// confirmationEnAttente : action sensible proposée par l'IA, en attente d'un « oui ».
type confirmationEnAttente struct {
	rep    *gemini.Reponse
	texte  string // demande d'origine de l'utilisateur
	expire time.Time
}

func (a *Analyseur) definirEcoute(session string) {
	a.muSessions.Lock()
	a.ecoutes[session] = time.Now().Add(dureeAttenteChoix)
	a.muSessions.Unlock()
}

func (a *Analyseur) effacerEcoute(session string) {
	a.muSessions.Lock()
	delete(a.ecoutes, session)
	a.muSessions.Unlock()
}

func (a *Analyseur) definirConfirmation(session string, c confirmationEnAttente) {
	c.expire = time.Now().Add(dureeAttenteChoix)
	a.muSessions.Lock()
	a.confirmations[session] = c
	a.muSessions.Unlock()
}

func (a *Analyseur) effacerConfirmation(session string) {
	a.muSessions.Lock()
	delete(a.confirmations, session)
	a.muSessions.Unlock()
}

func (a *Analyseur) confirmationPour(session string) (confirmationEnAttente, bool) {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	c, ok := a.confirmations[session]
	if !ok {
		return confirmationEnAttente{}, false
	}
	if time.Now().After(c.expire) {
		delete(a.confirmations, session)
		return confirmationEnAttente{}, false
	}
	return c, true
}

// reponseAttendue : l'assistant attend une réponse de l'utilisateur (confirmation
// demandée, ou question posée par l'IA). La voix reste alors à l'écoute et Home
// Assistant garde le micro ouvert (continue_conversation).
func (a *Analyseur) reponseAttendue(session string) bool {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	now := time.Now()
	if c, ok := a.confirmations[session]; ok && now.Before(c.expire) {
		return true
	}
	if t, ok := a.ecoutes[session]; ok && now.Before(t) {
		return true
	}
	return false
}

type reponseConfirmation int

const (
	reponseAutre reponseConfirmation = iota
	reponseOui
	reponseNon
)

var (
	motsOui = map[string]bool{
		"oui": true, "ouais": true, "ok": true, "okay": true, "confirme": true, "confirmer": true,
		"accord": true, "yes": true, "vas": true, "envoie": true, "go": true, "affirmatif": true,
		"exact": true, "exactement": true, "parfait": true,
	}
	motsNon = map[string]bool{
		"non": true, "nan": true, "annule": true, "annuler": true, "stop": true, "arrete": true,
		"negatif": true, "jamais": true, "laisse": true,
	}
)

// interpreterConfirmation lit la réponse à une demande de confirmation. Seules les
// réponses courtes (5 mots max) comptent ; « non » l'emporte sur « oui ».
func interpreterConfirmation(texte string) reponseConfirmation {
	norm := strings.NewReplacer("'", " ", "-", " ").Replace(text.Normaliser(texte))
	mots := strings.Fields(norm)
	if len(mots) == 0 || len(mots) > 5 {
		return reponseAutre
	}
	for i := range mots {
		mots[i] = strings.Trim(mots[i], ".,!?;:«»\"")
	}
	for _, m := range mots {
		if motsNon[m] {
			return reponseNon
		}
	}
	for _, m := range mots {
		if motsOui[m] {
			return reponseOui
		}
	}
	return reponseAutre
}

// ---- Garde-fous : numéros de téléphone et actions sensibles ----

// Un numéro commence par « + » ou « 0 » et compte au moins 9 chiffres (les dates,
// heures et identifiants ordinaires ne correspondent pas).
var reNumero = regexp.MustCompile(`^(\+\d|0\d)[\d .()]{7,}\d$`)

// normaliserNumero met un numéro sous forme canonique (« +33 6 12 34 56 78 » et
// « 0612345678 » donnent la même valeur). Retourne false si ce n'est pas un numéro.
func normaliserNumero(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !reNumero.MatchString(s) {
		return "", false
	}
	var sb strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	n := sb.String()
	switch {
	case strings.HasPrefix(s, "+33"):
		n = "0" + n[2:]
	case strings.HasPrefix(n, "0033"):
		n = "0" + n[4:]
	}
	return n, true
}

// numerosDans retourne les numéros de téléphone présents parmi les variables d'un script.
func numerosDans(vars map[string]interface{}) []string {
	var out []string
	for _, v := range vars {
		if s, ok := v.(string); ok {
			if n, isNum := normaliserNumero(s); isNum {
				out = append(out, n)
			}
		}
	}
	return out
}

// destinatairesAutorises refuse une action de script qui vise un numéro absent de
// la liste autorisée (IA_NUMEROS_AUTORISES, par défaut WHITELIST). Sans liste, tout
// numéro est refusé.
func (a *Analyseur) destinatairesAutorises(domaine string, params map[string]interface{}) bool {
	if domaine != "script" {
		return true
	}
	vars, _ := params["variables"].(map[string]interface{})
	for _, n := range numerosDans(vars) {
		if !a.numerosAutorises[n] {
			return false
		}
	}
	return true
}

// estScriptSMS : script dont le nom (ou l'id) évoque un SMS (« envoi_de_sms_gregory »).
// Un tel script est sensible même si le numéro est écrit en dur dans le script.
func estScriptSMS(app ha.Appareil) bool {
	return strings.Contains(text.Normaliser(app.EntityID+" "+app.FriendlyNameExact), "sms")
}

// contenuFourni : l'appel contient-il un texte à transmettre (autre qu'un simple numéro) ?
func contenuFourni(params map[string]interface{}) bool {
	if m, ok := params["message"].(string); ok && strings.TrimSpace(m) != "" {
		return true
	}
	vars, _ := params["variables"].(map[string]interface{})
	for _, v := range vars {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		if _, isNum := normaliserNumero(s); !isNum {
			return true
		}
	}
	return false
}

// decrireActionSensible indique si une action doit être confirmée (automatisation,
// ou script visant un numéro = SMS) et la décrit pour la question posée à l'utilisateur.
func decrireActionSensible(app ha.Appareil, action string, params map[string]interface{}) (string, bool) {
	nom := app.FriendlyNameExact
	if nom == "" {
		nom = app.FriendlyName
	}
	switch app.Domain {
	case "automation":
		cle := "confirmation.automation." + action
		if !i18n.Existe(cle) {
			cle = "confirmation.automation.autre"
		}
		return i18n.T(cle, nom), true
	case "script":
		vars, _ := params["variables"].(map[string]interface{})
		if len(numerosDans(vars)) == 0 && !estScriptSMS(app) {
			return "", false
		}
		// Ce qui sera transmis au script : variables + message (chemin texte libre)
		contenu := make(map[string]interface{}, len(vars)+1)
		for k, v := range vars {
			contenu[k] = v
		}
		if m, ok := params["message"].(string); ok && m != "" {
			contenu["message"] = m
		}
		if len(contenu) == 0 {
			return i18n.T("confirmation.script.sms.simple", nom), true
		}
		return i18n.T("confirmation.script.sms", nom, valeursTexte(contenu)), true
	}
	return "", false
}

// valeursTexte liste les valeurs des variables d'un script (ordre stable, tronqué).
func valeursTexte(vars map[string]interface{}) string {
	cles := make([]string, 0, len(vars))
	for k := range vars {
		cles = append(cles, k)
	}
	sort.Strings(cles)
	valeurs := make([]string, 0, len(cles))
	for _, k := range cles {
		valeurs = append(valeurs, fmt.Sprintf("%v", vars[k]))
	}
	s := strings.Join(valeurs, ", ")
	if r := []rune(s); len(r) > 100 {
		s = string(r[:100]) + "…"
	}
	return s
}

// ---- Réduction du contexte envoyé à l'IA ----

// preselectionIA choisit les entités à envoyer à l'IA pour cette phrase : les mieux
// classées par le scoring NLP, plus toutes celles des pièces citées (« éteins tout
// dans le salon »). Les scripts, la météo, les minuteurs, les lecteurs média et les
// entités virtuelles sont toujours ajoutés par ha.ContexteJSON.
func (a *Analyseur) preselectionIA(texte string) map[string]bool {
	retenus := make(map[string]bool)
	nettoye := conversion.ChiffreVersLettre(strings.ToLower(texte))

	// 1. Meilleures entités selon le scoring (classement trié par score décroissant)
	for _, c := range a.classerAppareils(nettoye, nil) {
		if len(retenus) >= a.ia.ContexteMax || c.Score <= 0 {
			break
		}
		retenus[c.Appareil.EntityID] = true
	}

	// 2. Toutes les entités des pièces citées dans la phrase
	zones := a.haClient.ZonesEntites()
	if len(zones) == 0 {
		return retenus
	}
	phrase := text.Normaliser(texte)
	citees := make(map[string]bool)
	for _, nom := range zones {
		if !citees[nom] && strings.Contains(phrase, text.Normaliser(nom)) {
			citees[nom] = true
		}
	}
	if len(citees) == 0 {
		return retenus
	}
	ajoutees, plafond := 0, a.ia.ContexteMax*2
	for _, app := range a.catalogue {
		if ajoutees >= plafond {
			break
		}
		if !retenus[app.EntityID] && citees[zones[app.EntityID]] {
			retenus[app.EntityID] = true
			ajoutees++
		}
	}
	return retenus
}
