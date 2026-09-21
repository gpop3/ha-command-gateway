package nlp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/text"
	"ha-command-gateway/pkg/types"
)

// Journal des décisions : pour chaque échange, la phrase, le moteur qui a répondu, le type
// choisi par l'IA, les actions, le résultat, le coût (appels et tokens) et la durée. Consultable
// par GET /decisions et, si DECISIONS_FILE est défini, écrit en JSONL (une ligne par échange).
// Un « non, pas ça » prononcé juste après marque l'échange comme erroné.

const (
	tailleAnneauDecisions = 300
	fenetreCorrection     = 3 * time.Minute
)

// Ombre : ce qu'aurait fait l'autre moteur (mode ombre), sans exécution.
type Ombre struct {
	Moteur     string   `json:"moteur"`
	Prediction []string `json:"prediction"`
	Accord     *bool    `json:"accord,omitempty"`
}

// Decision décrit un échange complet.
type Decision struct {
	ID            int64     `json:"id"`
	Quand         time.Time `json:"quand"`
	Canal         string    `json:"canal"`
	Phrase        string    `json:"phrase"`
	Moteur        string    `json:"moteur"` // gemini | classique | appris | confirmation | choix
	Type          string    `json:"type,omitempty"`
	Sujet         string    `json:"sujet,omitempty"`
	Actions       []string  `json:"actions,omitempty"`
	Rejets        []string  `json:"rejets,omitempty"`
	SecondeChance bool      `json:"seconde_chance,omitempty"`
	Resultat      string    `json:"resultat"`
	Reussi        bool      `json:"reussi"`
	DureeMs       int64     `json:"duree_ms"`
	AppelsIA      int       `json:"appels_ia,omitempty"`
	Tokens        int       `json:"tokens,omitempty"`
	Ombre         *Ombre    `json:"ombre,omitempty"`
	Faux          bool      `json:"faux,omitempty"`

	session   string   // clé interne de la session (non masquée)
	cleAppris string   // phrase apprise ou rejouée par cet échange
	entites   []string // entités visées (pour la comparaison du mode ombre)
}

type journalDecisions struct {
	mu          sync.Mutex
	anneau      []*Decision
	suivant     int64
	fichier     string
	derniere    map[string]*Decision
	surDecision func(Decision)
	erreurFait  bool
}

func nouveauJournalDecisions() *journalDecisions {
	return &journalDecisions{derniere: map[string]*Decision{}}
}

var reNumeroLibre = regexp.MustCompile(`(\+\d|\b0\d)[\d .()]{7,}\d`)

// masquerNumeros remplace les numéros de téléphone d'un texte (SMS, messages).
func masquerNumeros(s string) string { return reNumeroLibre.ReplaceAllString(s, "[numéro]") }

// masquerCanal : « sms:…678 » pour un numéro de téléphone, le reste tel quel.
func masquerCanal(session string) string {
	if n, ok := normaliserNumero(session); ok {
		return "sms:…" + n[len(n)-3:]
	}
	return session
}

func (j *journalDecisions) ecrire(v interface{}) {
	if j.fichier == "" {
		return
	}
	brut, err := json.Marshal(v)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(j.fichier), 0o755)
	}
	if err == nil {
		var f *os.File
		if f, err = os.OpenFile(j.fichier, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, err = f.Write(append(brut, '\n'))
			_ = f.Close()
		}
	}
	if err != nil && !j.erreurFait {
		j.erreurFait = true // un seul avertissement
		logx.WarnT("decisions.fichier.erreur", err)
	}
}

// ajouter enregistre un échange, l'écrit dans le fichier et prévient l'abonné (HA).
func (j *journalDecisions) ajouter(d *Decision) {
	j.mu.Lock()
	j.suivant++
	d.ID = j.suivant
	j.anneau = append(j.anneau, d)
	if len(j.anneau) > tailleAnneauDecisions {
		j.anneau = j.anneau[len(j.anneau)-tailleAnneauDecisions:]
	}
	if d.session != "" {
		j.derniere[d.session] = d
	}
	j.ecrire(d)
	copie, hook := *d, j.surDecision
	j.mu.Unlock()
	if hook != nil {
		hook(copie)
	}
}

// marquerFaux marque le dernier échange de la session (moins de fenetre) comme erroné.
func (j *journalDecisions) marquerFaux(session string) (*Decision, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	d := j.derniere[session]
	if d == nil || d.Faux || time.Since(d.Quand) > fenetreCorrection {
		return nil, false
	}
	d.Faux = true
	j.ecrire(map[string]interface{}{"id": d.ID, "faux": true, "marque": time.Now()})
	copie := *d
	return &copie, true
}

// derniereDe retourne une copie du dernier échange de la session (nil s'il n'y en a pas).
func (j *journalDecisions) derniereDe(session string) *Decision {
	j.mu.Lock()
	defer j.mu.Unlock()
	d := j.derniere[session]
	if d == nil {
		return nil
	}
	c := *d
	return &c
}

// marquerFauxID marque un échange précis (API).
func (j *journalDecisions) marquerFauxID(id int64) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, d := range j.anneau {
		if d.ID == id {
			d.Faux = true
			j.ecrire(map[string]interface{}{"id": id, "faux": true, "marque": time.Now()})
			return true
		}
	}
	return false
}

func (j *journalDecisions) definirOmbre(id int64, o *Ombre) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, d := range j.anneau {
		if d.ID == id {
			d.Ombre = o
			j.ecrire(map[string]interface{}{"id": id, "ombre": o})
			return
		}
	}
}

// liste retourne les n derniers échanges, du plus récent au plus ancien.
func (j *journalDecisions) liste(n int, fauxSeul bool) []Decision {
	j.mu.Lock()
	defer j.mu.Unlock()
	if n <= 0 || n > tailleAnneauDecisions {
		n = 50
	}
	var out []Decision
	for i := len(j.anneau) - 1; i >= 0 && len(out) < n; i-- {
		if fauxSeul && !j.anneau[i].Faux {
			continue
		}
		out = append(out, *j.anneau[i])
	}
	return out
}

// ---- Analyseur : accès au journal ----

// DefinirJournalDecisions active l'écriture JSONL (chemin vide = mémoire seulement).
func (a *Analyseur) DefinirJournalDecisions(chemin string) {
	a.decisions.mu.Lock()
	a.decisions.fichier = chemin
	a.decisions.mu.Unlock()
}

// DefinirSurDecision enregistre un abonné appelé après chaque échange (publication dans HA).
func (a *Analyseur) DefinirSurDecision(f func(Decision)) {
	a.decisions.mu.Lock()
	a.decisions.surDecision = f
	a.decisions.mu.Unlock()
}

// Decisions retourne les derniers échanges (GET /decisions).
func (a *Analyseur) Decisions(n int, fauxSeul bool) []Decision { return a.decisions.liste(n, fauxSeul) }

// MarquerDecisionFausse marque un échange comme erroné (POST /decisions/faux).
func (a *Analyseur) MarquerDecisionFausse(id int64) bool { return a.decisions.marquerFauxID(id) }

// ---- Correction vocale ----

var phrasesCorrection = []string{
	"pas ca", "pas ce que j'ai demande", "pas ce que je voulais", "tu t'es trompe", "tu te trompes",
	"mauvaise reponse", "c'est faux", "n'importe quoi", "c'est pas bon", "ce n'est pas bon",
}

// interpreterCorrection : « non, pas ça », « c'est pas ce que j'ai demandé », « tu t'es trompé »…
func interpreterCorrection(texte string) bool {
	t := text.Normaliser(texte)
	t = strings.NewReplacer("’", "'", "-", " ", ".", " ", ",", " ", "!", " ", "?", " ", ";", " ", ":", " ").Replace(t)
	mots := strings.Fields(t)
	if len(mots) > 10 {
		return false
	}
	t = " " + strings.Join(mots, " ") + " "
	for _, p := range phrasesCorrection {
		if strings.Contains(t, " "+p+" ") {
			return true
		}
	}
	return false
}

// marquerCorrection : si un échange récent de la session existe, on le marque erroné, on
// oublie la phrase apprise le cas échéant, et on le confirme à l'utilisateur.
func (a *Analyseur) marquerCorrection(session string) (*types.Message, bool) {
	d, ok := a.decisions.marquerFaux(session)
	if !ok {
		return nil, false
	}
	if d.cleAppris != "" {
		a.appris.oublier(d.cleAppris)
	}
	texte := i18n.T("decision.faux.note")
	if len(d.entites) > 0 {
		texte += i18n.T("decision.faux.annuler")
	}
	msg := messageTexte(texte)
	return &msg, true
}

// texteDuMessage : texte final d'un message pour le journal (numéros masqués, tronqué).
func texteDuMessage(m *types.Message) string {
	return tronquerTexte(masquerNumeros(texteComplet(m)), 400)
}

// texteComplet reconstitue le texte final d'un message (clé i18n + paramètres, ou texte libre).
func texteComplet(m *types.Message) string {
	if m == nil {
		return ""
	}
	t := m.Voix.Texte
	if t == "" {
		t = m.SMS.Texte
	}
	switch {
	case i18n.Existe(t):
		return i18n.T(t, m.Voix.Params...)
	case len(m.Voix.Params) > 0:
		return fmt.Sprintf(t, m.Voix.Params...)
	}
	return t
}

func tronquerTexte(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
