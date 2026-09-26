package ha

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
)

// Journal de Home Assistant (/api/logbook) : quand une entité a changé d'état, et CE
// QUI l'a provoqué — automatisation, script, utilisateur, ou action directe sur
// l'appareil (bouton physique...), qui n'a pas d'auteur dans HA.
//
// Le nom d'un utilisateur se lit avec un token administrateur (config/auth/list) ; sans
// cela on dit « un utilisateur ». Astuce : créer un utilisateur HA dédié à l'assistant
// permet de reconnaître ce qui vient de la voix, d'Alexa ou de la passerelle.

const (
	maxEvenementsJournal   = 8
	dureeCacheUtilisateurs = 30 * time.Minute
	delaiReessaiUtilisat   = 5 * time.Minute
)

var cacheUtilisateurs struct {
	sync.Mutex
	parID map[string]string
	maj   time.Time
	echec time.Time
}

// nomUtilisateur retourne le nom d'un utilisateur HA (vide s'il est inconnu).
func (c *Client) nomUtilisateur(id string) string {
	if id == "" {
		return ""
	}
	cacheUtilisateurs.Lock()
	defer cacheUtilisateurs.Unlock()

	now := time.Now()
	if (cacheUtilisateurs.parID == nil || now.Sub(cacheUtilisateurs.maj) > dureeCacheUtilisateurs) &&
		now.Sub(cacheUtilisateurs.echec) > delaiReessaiUtilisat {
		var users []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Username string `json:"username"`
		}
		if err := c.commandeWS("config/auth/list", &users); err != nil {
			logx.WarnT("journal.utilisateurs.erreur", err)
			cacheUtilisateurs.echec = now
		} else {
			m := make(map[string]string, len(users))
			for _, u := range users {
				nom := u.Name
				if nom == "" {
					nom = u.Username
				}
				m[u.ID] = nom
			}
			cacheUtilisateurs.parID, cacheUtilisateurs.maj = m, now
		}
	}
	return cacheUtilisateurs.parID[id]
}

// EvenementJournal : un changement d'état (ou une exécution) et sa cause.
type EvenementJournal struct {
	Quand   string `json:"quand"`
	Etat    string `json:"etat,omitempty"`
	Message string `json:"message,omitempty"` // automatisations / scripts : texte du journal HA
	Cause   string `json:"cause,omitempty"`
}

// DonneesJournal : les événements d'une entité sur une période.
type DonneesJournal struct {
	Nom        string             `json:"nom"`
	Periode    string             `json:"periode"`
	Evenements []EvenementJournal `json:"evenements"`
	Resume     string             `json:"-"`
}

func champ(m map[string]interface{}, cle string) string {
	v, _ := m[cle].(string)
	return strings.TrimSpace(v)
}

func premierNonVide(valeurs ...string) string {
	for _, v := range valeurs {
		if v != "" {
			return v
		}
	}
	return ""
}

// formaterQuandAuto : « à 22h10 » si c'est aujourd'hui, sinon « hier à 22h10 »...
func formaterQuandAuto(t time.Time) string {
	return formaterQuand(t, !memeJour(t, time.Now()))
}

// causeJournal décrit ce qui a provoqué une entrée du journal.
func (c *Client) causeJournal(m map[string]interface{}, domaine string) string {
	idUser := champ(m, "context_user_id")
	user := c.nomUtilisateur(idUser)
	nomCtx := premierNonVide(champ(m, "context_entity_id_name"), champ(m, "context_name"), champ(m, "context_entity_id"))

	// Pour une automatisation ou un script, le message du journal EST la cause : on ne
	// reporte ici que l'éventuel utilisateur qui l'a lancé à la main.
	if domaine == "automation" || domaine == "script" {
		switch {
		case user != "":
			return i18n.T("journal.cause.utilisateur", user)
		case idUser != "":
			return i18n.T("journal.cause.utilisateur.inconnu")
		}
		return ""
	}

	switch champ(m, "context_event_type") {
	case "automation_triggered":
		if nomCtx != "" {
			return i18n.T("journal.cause.automatisation", nomCtx)
		}
	case "script_started":
		if nomCtx != "" {
			return i18n.T("journal.cause.script", nomCtx)
		}
	}
	switch {
	case user != "":
		if service := champ(m, "context_service"); service != "" {
			return i18n.T("journal.cause.utilisateur.service", user, service)
		}
		return i18n.T("journal.cause.utilisateur", user)
	case idUser != "":
		return i18n.T("journal.cause.utilisateur.inconnu")
	case nomCtx != "":
		return i18n.T("journal.cause.entite", nomCtx)
	}
	return i18n.T("journal.cause.directe")
}

// JournalEntite lit le journal HA d'une entité sur [debut, fin] :
//   - etat : ne garde que les passages à cet état (« on », « off ») ; sans effet pour
//     les automatisations et scripts, dont chaque exécution est une entrée ;
//   - dernier : ne garde que l'événement le plus récent.
func (c *Client) JournalEntite(app Appareil, debut, fin time.Time, etat string, dernier bool) (*DonneesJournal, error) {
	const layout = "2006-01-02T15:04:05Z"
	path := fmt.Sprintf("/api/logbook/%s?entity=%s&end_time=%s", debut.UTC().Format(layout), app.EntityID, fin.UTC().Format(layout))
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var brut []map[string]interface{}
	if err := json.Unmarshal(body, &brut); err != nil {
		return nil, err
	}

	nom := app.FriendlyNameExact
	if nom == "" {
		nom = app.FriendlyName
	}
	d := &DonneesJournal{Nom: nom, Periode: decrirePeriode(debut, fin)}
	execution := app.Domain == "automation" || app.Domain == "script"
	etat = strings.ToLower(strings.TrimSpace(etat))

	type evt struct {
		t time.Time
		e EvenementJournal
	}
	var evts []evt
	for _, m := range brut {
		quand, err := time.Parse(time.RFC3339, champ(m, "when"))
		if err != nil {
			continue
		}
		etatBrut := strings.ToLower(champ(m, "state"))
		if etat != "" && !execution && etatBrut != etat {
			continue
		}
		e := EvenementJournal{
			Quand:   formaterQuandAuto(quand),
			Message: champ(m, "message"),
			Cause:   c.causeJournal(m, app.Domain),
		}
		if etatBrut != "" && !execution {
			e.Etat = traduireEtat(app.Domain, etatBrut)
		}
		evts = append(evts, evt{t: quand, e: e})
	}

	// Du plus ancien au plus récent ; on garde la fin de la liste
	if len(evts) > 1 && evts[0].t.After(evts[len(evts)-1].t) {
		for i, j := 0, len(evts)-1; i < j; i, j = i+1, j-1 {
			evts[i], evts[j] = evts[j], evts[i]
		}
	}
	garde := maxEvenementsJournal
	if dernier {
		garde = 1
	}
	tronque := len(evts) > garde
	if tronque {
		evts = evts[len(evts)-garde:]
	}
	for _, x := range evts {
		d.Evenements = append(d.Evenements, x.e)
	}

	d.Resume = resumeJournal(d, dernier, tronque)
	return d, nil
}

// ligneJournal : « allumé à 22h10, par l'automatisation Arrosage soir ».
func ligneJournal(e EvenementJournal) string {
	var quoi string
	switch {
	case e.Etat != "":
		quoi = e.Etat + " " + e.Quand
	case e.Message != "":
		quoi = e.Quand + " : " + e.Message
	default:
		quoi = e.Quand
	}
	if e.Cause != "" {
		return quoi + ", " + e.Cause
	}
	return quoi
}

func resumeJournal(d *DonneesJournal, dernier, tronque bool) string {
	if len(d.Evenements) == 0 {
		return i18n.T("journal.vide", d.Nom, d.Periode)
	}
	if dernier {
		return i18n.T("journal.dernier", d.Nom, ligneJournal(d.Evenements[0]))
	}
	lignes := make([]string, 0, len(d.Evenements))
	for _, e := range d.Evenements {
		lignes = append(lignes, ligneJournal(e))
	}
	cle := "journal.liste"
	if tronque {
		cle = "journal.liste.derniers"
	}
	return i18n.T(cle, d.Nom, d.Periode, strings.Join(lignes, " ; "))
}
