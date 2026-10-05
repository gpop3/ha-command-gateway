package nlp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ha-command-gateway/internal/core/adapters/gemini"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/conversion"
	"ha-command-gateway/internal/utils/text"
)

// Base « apprise » : quand l'IA a compris une phrase et que ses actions ont TOUTES réussi,
// la phrase et ses actions sont mémorisées dans un fichier. Quand l'IA n'est pas disponible
// (quota, panne, désactivée), la même phrase est comprise sans elle. Un « non, pas ça »
// prononcé juste après une commande fait oublier la phrase.
//
// Seules les commandes d'état simples sont apprises (lumières, prises, volets…) : jamais
// un script, un SMS, une automatisation, ni rien qui contienne un texte libre.

const (
	maxEntreesAppris = 500
	maxMotsAppris    = 12
)

var domainesApprenables = map[string]bool{
	"light": true, "switch": true, "cover": true, "fan": true,
	"climate": true, "media_player": true, "input_boolean": true,
}

type entreeAppris struct {
	Phrase  string          `json:"phrase"`
	Actions []gemini.Action `json:"actions"`
	Succes  int             `json:"succes"`
	Dernier time.Time       `json:"dernier"`
}

type baseAppris struct {
	mu      sync.Mutex
	chemin  string
	entrees map[string]*entreeAppris
}

func nouvelleBaseAppris() *baseAppris { return &baseAppris{entrees: map[string]*entreeAppris{}} }

var reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// clePhrase normalise une phrase (accents, casse, ponctuation, chiffres → lettres) pour la comparer.
func clePhrase(texte string) string {
	t := text.Normaliser(conversion.ChiffreVersLettre(strings.ToLower(texte)))
	return strings.TrimSpace(reNonAlnum.ReplaceAllString(t, " "))
}

// DefinirFichierAppris charge le fichier d'apprentissage (chemin vide = mémoire seulement).
func (a *Analyseur) DefinirFichierAppris(chemin string) error {
	b := a.appris
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chemin = chemin
	if chemin == "" {
		return nil
	}
	data, err := os.ReadFile(chemin)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f struct {
		Entrees []*entreeAppris `json:"entrees"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	for _, e := range f.Entrees {
		if e != nil && e.Phrase != "" {
			b.entrees[clePhrase(e.Phrase)] = e
		}
	}
	return nil
}

// sauver écrit le fichier de façon atomique (à appeler verrou pris).
func (b *baseAppris) sauver() {
	if b.chemin == "" {
		return
	}
	liste := make([]*entreeAppris, 0, len(b.entrees))
	for _, e := range b.entrees {
		liste = append(liste, e)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].Phrase < liste[j].Phrase })
	brut, err := json.MarshalIndent(map[string]interface{}{"version": 1, "entrees": liste}, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(b.chemin), 0o755)
	}
	if err == nil {
		tmp := b.chemin + ".tmp"
		if err = os.WriteFile(tmp, brut, 0o644); err == nil {
			err = os.Rename(tmp, b.chemin)
		}
	}
	if err != nil {
		logx.WarnT("appris.fichier.erreur", err)
	}
}

// chercher retourne une copie de l'entrée apprise pour cette phrase, s'il y en a une.
func (b *baseAppris) chercher(cle string) *entreeAppris {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entrees[cle]
	if e == nil || e.Succes < 1 {
		return nil
	}
	c := *e
	c.Actions = append([]gemini.Action(nil), e.Actions...)
	return &c
}

func (b *baseAppris) apprendre(cle, phrase string, actions []gemini.Action) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := b.entrees[cle]; e != nil {
		e.Actions, e.Dernier = actions, time.Now()
		e.Succes++
	} else {
		b.entrees[cle] = &entreeAppris{Phrase: phrase, Actions: actions, Succes: 1, Dernier: time.Now()}
		logx.DebugT("appris.apprise", phrase, len(actions))
		// Plafond : on retire les entrées les plus anciennes
		for len(b.entrees) > maxEntreesAppris {
			ancienne, ancienneCle := time.Now(), ""
			for k, v := range b.entrees {
				if v.Dernier.Before(ancienne) {
					ancienne, ancienneCle = v.Dernier, k
				}
			}
			delete(b.entrees, ancienneCle)
		}
	}
	b.sauver()
}

func (b *baseAppris) succes(cle string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := b.entrees[cle]; e != nil {
		e.Succes++
		e.Dernier = time.Now()
		b.sauver()
	}
}

func (b *baseAppris) oublier(cle string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := b.entrees[cle]; e != nil {
		logx.InfoT("appris.oubliee", e.Phrase)
		delete(b.entrees, cle)
		b.sauver()
	}
}

// actionsApprenables retourne les actions à mémoriser si TOUTES sont des commandes d'état
// simples (domaines autorisés, aucun texte libre) ; nil sinon.
func actionsApprenables(actions []gemini.Action, prepares []actionPreparee) []gemini.Action {
	if len(actions) == 0 || len(actions) != len(prepares) {
		return nil
	}
	for i, p := range prepares {
		if !domainesApprenables[p.app.Domain] {
			return nil
		}
		if _, msg := p.params["message"]; msg {
			return nil
		}
		if _, vars := p.params["variables"]; vars {
			return nil
		}
		if strings.TrimSpace(actions[i].EntityID) == "" {
			return nil
		}
	}
	return actions
}

// prendreActionsOK retourne (et efface) les actions apprenables de la dernière exécution
// entièrement réussie de la session.
func (a *Analyseur) prendreActionsOK(session string) []gemini.Action {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	acts := a.actionsOK[session]
	delete(a.actionsOK, session)
	return acts
}

func (a *Analyseur) definirActionsOK(session string, acts []gemini.Action) {
	a.muSessions.Lock()
	a.actionsOK[session] = acts
	a.muSessions.Unlock()
}

// apprendreDepuisIA mémorise une phrase comprise par l'IA (si ses actions sont apprenables).
func (a *Analyseur) apprendreDepuisIA(rec *Decision, texte string, acts []gemini.Action) {
	if len(acts) == 0 || len(strings.Fields(texte)) > maxMotsAppris {
		return
	}
	cle := clePhrase(texte)
	if cle == "" {
		return
	}
	a.appris.apprendre(cle, strings.TrimSpace(texte), acts)
	rec.cleAppris = cle
}

