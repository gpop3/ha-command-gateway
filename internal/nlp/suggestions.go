package nlp

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"ha-command-gateway/internal/core/adapters/gemini"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/pkg/types"
)

const (
	dureeSuite     = 3 * time.Minute
	maxSuggestions = 3
)

// ---- « Et dans la chambre ? » : suite de phrase sans IA ----

// suiteClassique mémorise la dernière commande comprise par le NLP classique.
type suiteClassique struct {
	texte string
	quand time.Time
}

func (a *Analyseur) memoriserClassique(session, texte string) {
	a.muSessions.Lock()
	a.dernierClassique[session] = suiteClassique{texte: texte, quand: time.Now()}
	a.muSessions.Unlock()
}

// motsRemplissage : mots de liaison qui, avec un nom de pièce, forment une simple suite.
var motsRemplissage = map[string]bool{
	"et": true, "dans": true, "la": true, "le": true, "les": true, "l": true, "pour": true, "sur": true,
	"au": true, "aux": true, "a": true, "aussi": true, "pareil": true, "meme": true, "chose": true,
	"idem": true, "de": true, "du": true, "des": true, "en": true, "ici": true,
}

// suiteClassique reconnaît une phrase qui ne contient qu'un lieu (« et dans la chambre ? ») juste
// après une commande : elle reprend la commande précédente en remplaçant sa pièce par la
// nouvelle (« allume la lumière du salon » puis « et dans la chambre ? » → « allume la lumière
// de la chambre »). Sans commande récente, ou si la phrase contient autre chose qu'un lieu,
// rien n'est réécrit.
func (a *Analyseur) suiteClassique(session, nettoye string) (string, bool) {
	a.muSessions.Lock()
	prec, ok := a.dernierClassique[session]
	a.muSessions.Unlock()
	if !ok || time.Since(prec.quand) > dureeSuite {
		return "", false
	}

	phrase := normaliserPourPhrases(nettoye)
	precedent := normaliserPourPhrases(prec.texte)
	pieces := a.GetPieces()

	cible := ""
	for _, p := range pieces {
		np := strings.TrimSpace(normaliserPourPhrases(p.Name))
		if np != "" && strings.Contains(phrase, " "+np+" ") && len(np) > len(cible) {
			cible = np
		}
	}
	if cible == "" {
		return "", false
	}
	// La phrase ne doit contenir QUE le lieu et des mots de liaison
	reste := strings.ReplaceAll(phrase, " "+cible+" ", " ")
	for _, m := range strings.Fields(reste) {
		if !motsRemplissage[m] {
			return "", false
		}
	}

	for _, p := range pieces {
		np := strings.TrimSpace(normaliserPourPhrases(p.Name))
		if np != "" && np != cible && strings.Contains(precedent, " "+np+" ") {
			return strings.TrimSpace(strings.ReplaceAll(precedent, " "+np+" ", " "+cible+" ")), true
		}
	}
	if strings.Contains(precedent, " "+cible+" ") {
		return "", false // même pièce : rien à changer
	}
	return strings.TrimSpace(precedent) + " " + cible, true
}

// ---- « Tu voulais dire… ? » ----

type propositionSuggestion struct {
	app     ha.Appareil
	texte   string
	libelle string
}

type suggestionEnAttente struct {
	propositions []propositionSuggestion
	idx          int
	expire       time.Time
}

func (a *Analyseur) definirSuggestion(session string, s suggestionEnAttente) {
	s.expire = time.Now().Add(dureeAttenteChoix)
	a.muSessions.Lock()
	a.suggestions[session] = s
	a.muSessions.Unlock()
}

func (a *Analyseur) suggestionPour(session string) (suggestionEnAttente, bool) {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	s, ok := a.suggestions[session]
	if !ok {
		return suggestionEnAttente{}, false
	}
	if time.Now().After(s.expire) {
		delete(a.suggestions, session)
		return suggestionEnAttente{}, false
	}
	return s, true
}

func (a *Analyseur) effacerSuggestion(session string) {
	a.muSessions.Lock()
	delete(a.suggestions, session)
	a.muSessions.Unlock()
}

// proposerSuggestions : quand rien n'a été compris, propose l'entité (et le verbe) la plus proche —
// jusqu'à trois, l'une après l'autre : « oui » l'exécute, « non » passe à la suivante. Jamais
// pour les serrures, alarmes et caméras.
func (a *Analyseur) proposerSuggestions(session, nettoye string, estAction bool, classement []Candidat) *types.Message {
	seuil := a.score.Minimal / 2
	if seuil < 1 {
		seuil = 1
	}
	var props []propositionSuggestion
	for _, c := range classement {
		if len(props) >= maxSuggestions || c.Score < seuil {
			break
		}
		app := c.Appareil
		if app.Domain == "lock" || app.Domain == "alarm_control_panel" || app.Domain == "camera" {
			continue
		}
		nom := nomAppareil(app)
		if estAction {
			v, ok := domaineAUnVerbe(nettoye, app.Domain)
			if !ok {
				continue // ce type d'appareil ne connaît pas le verbe dit
			}
			props = append(props, propositionSuggestion{app: app, texte: nettoye, libelle: v + " " + nom})
		} else {
			props = append(props, propositionSuggestion{app: app, texte: nettoye, libelle: i18n.T("suggestion.etat", nom)})
		}
	}
	if len(props) == 0 {
		return nil
	}
	a.definirSuggestion(session, suggestionEnAttente{propositions: props})
	msg := messageTexte(i18n.T("suggestion.demande", props[0].libelle))
	return &msg
}

// ---- Message quand l'IA est suspendue ----

// surErreurIA : quand l'IA devient indisponible (disjoncteur ouvert, quota atteint), l'assistant
// le dit UNE fois, à la prochaine réponse, au lieu de sembler plus bête sans explication.
func (a *Analyseur) surErreurIA(err error) {
	if a.gemini == nil {
		return
	}
	cle := ""
	switch {
	case errors.Is(err, gemini.ErrQuota):
		cle = "ia.quota"
	case errors.Is(err, gemini.ErrIndisponible) || a.gemini.Statut().DisjoncteurOuvert:
		cle = "ia.suspendue"
	}
	if cle == "" {
		return
	}
	a.muSessions.Lock()
	if !a.iaDegradee {
		a.iaDegradee, a.iaNote = true, i18n.T(cle)
	}
	a.muSessions.Unlock()
}

// surSuccesIA : l'IA répond de nouveau ; si la suspension avait été annoncée, on annonce le retour.
func (a *Analyseur) surSuccesIA() {
	a.muSessions.Lock()
	if a.iaDegradee {
		a.iaDegradee, a.iaNote = false, i18n.T("ia.retour")
	}
	a.muSessions.Unlock()
}

func (a *Analyseur) prendreNoteIA() string {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	n := a.iaNote
	a.iaNote = ""
	return n
}

// appliquerNoteIA place la note d'état de l'IA (« je passe en mode simplifié ») devant la réponse.
func (a *Analyseur) appliquerNoteIA(msg *types.Message, verbe string, match, isAction bool, app *ha.Appareil) (*types.Message, bool, bool) {
	note := a.prendreNoteIA()
	if note == "" {
		return msg, match, isAction
	}
	var corps string
	switch {
	case msg == nil:
		corps = i18n.T("assistant.retour.pas.compris")
	case isAction && app != nil:
		corps = i18n.T("assistant.retour.action", verbe, app.FriendlyName)
	default:
		corps = texteComplet(msg)
	}
	m := messageTexte(note + " " + corps)
	return &m, true, false
}

// ---- « Oublie tout » ----

var phrasesOubli = []string{
	"oublie tout", "efface tout", "efface ta memoire", "reinitialise ta memoire", "vide ta memoire",
	"supprime ta memoire", "oublie tout ce que tu as appris", "efface tout ce que tu as appris", "efface le journal",
}

// interpreterOubli reconnaît « oublie tout », « efface ta mémoire »…
func interpreterOubli(texte string) bool {
	t := normaliserPourPhrases(texte)
	if len(strings.Fields(t)) > 9 {
		return false
	}
	for _, p := range phrasesOubli {
		if strings.Contains(t, normaliserPourPhrases(p)) {
			return true
		}
	}
	return false
}

// demanderOubli demande confirmation avant d'effacer (toujours, même si IA_CONFIRMATION=false :
// c'est irréversible).
func (a *Analyseur) demanderOubli(session, texte string) *types.Message {
	rep := &gemini.Reponse{Type: "enquete", Enquete: &gemini.Enquete{Sujet: "oublier"}}
	a.definirConfirmation(session, confirmationEnAttente{rep: rep, texte: texte})
	msg := messageTexte(i18n.T("confirmation.demande", i18n.T("confirmation.oubli")))
	return &msg
}

// vider supprime le journal (mémoire et fichiers, y compris les anciennes versions).
func (j *journalDecisions) vider() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.anneau, j.derniere = nil, map[string]*Decision{}
	if j.fichier == "" {
		return
	}
	_ = os.Remove(j.fichier)
	for i := 1; i <= 10; i++ {
		_ = os.Remove(fmt.Sprintf("%s.%d", j.fichier, i))
	}
}

// vider oublie toutes les phrases apprises (mémoire et fichier).
func (b *baseAppris) vider() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entrees = map[string]*entreeAppris{}
	if b.chemin != "" {
		_ = os.Remove(b.chemin)
	}
}

// oublierTout efface le journal des décisions, les phrases apprises, les mémoires de
// conversation et tout ce qui est en attente ou récent (annulations, dernières réponses…).
func (a *Analyseur) oublierTout() *types.Message {
	a.decisions.vider()
	a.appris.vider()
	a.muSessions.Lock()
	a.memoires = make(map[string]*memoireSession)
	a.confirmations = make(map[string]confirmationEnAttente)
	a.ecoutes = make(map[string]time.Time)
	a.annulations = make(map[string][]groupeAnnulation)
	a.actionsOK = make(map[string][]gemini.Action)
	a.dernieresReponses = make(map[string]string)
	a.dernierClassique = make(map[string]suiteClassique)
	a.suggestions = make(map[string]suggestionEnAttente)
	a.muSessions.Unlock()
	msg := messageTexte(i18n.T("oubli.fait"))
	return &msg
}
