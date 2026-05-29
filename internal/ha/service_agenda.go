package ha

import (
	"encoding/json"
	"fmt"
	"ha-command-gateway/pkg/types"
	"log"
	"slices"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
)

type ServiceAgenda struct {
	serviceBase
	catalogue []Appareil
}

func NewServiceAgenda(c *Client) *ServiceAgenda {
	return &ServiceAgenda{serviceBase: newServiceBase("agenda", c, map[string]string{})}
}

// SetCatalogue met à jour le catalogue — appelé depuis l'analyseur après RafraichirCatalogue
func (s *ServiceAgenda) SetCatalogue(catalogue []Appareil) {
	s.catalogue = catalogue
}

func (s *ServiceAgenda) ScoreDomaine(_ bool) int { return 80 }

func (s *ServiceAgenda) EstActionParDefaut() bool { return false }

func (s *ServiceAgenda) ExtraireParams(texte string) map[string]interface{} {
	params := map[string]interface{}{}
	switch {
	case strings.Contains(texte, "demain"):
		params["horizon"] = "demain"
	case strings.Contains(texte, "semaine"):
		params["horizon"] = "semaine"
	case strings.Contains(texte, "mois"):
		params["horizon"] = "mois"
	default:
		params["horizon"] = "aujourd'hui"
	}
	return params
}

func (s *ServiceAgenda) MotsReconnus() []string {
	return []string{
		"agenda", "calendrier", "rendez-vous", "prévu", "programme", "événements",
		"demain", "semaine", "aujourd'hui", "mois",
	}
}

func (s *ServiceAgenda) getEvenements(debut, fin time.Time) []EvenementCalendrier {
	var tousEvenements []EvenementCalendrier

	for _, app := range s.catalogue {
		if app.Domain != "calendar" {
			continue
		}
		path := fmt.Sprintf("/api/calendars/%s?start=%s&end=%s",
			app.EntityID,
			debut.UTC().Format("2006-01-02T15:04:05.000Z"),
			fin.UTC().Format("2006-01-02T15:04:05.000Z"),
		)
		body, err := s.client.get(path)
		if err != nil {
			log.Printf("❌ [agenda] %s : %v", app.EntityID, err)
			continue
		}
		var events []EvenementCalendrier
		if err := json.Unmarshal(body, &events); err != nil {
			log.Printf("❌ [agenda] unmarshal %s : %v", app.EntityID, err)
			continue
		}
		tousEvenements = append(tousEvenements, events...)
	}

	slices.SortFunc(tousEvenements, func(a, b EvenementCalendrier) int {
		timeA, errA := time.Parse(time.RFC3339, a.Start.DateTime)
		timeB, errB := time.Parse(time.RFC3339, b.Start.DateTime)

		if errA != nil || errB != nil {
			log.Printf("⚠️ Erreur de parsing dans le tri : errA=%v, errB=%v", errA, errB)
			log.Printf("Valeurs reçues : A=%q, B=%q", a.Start.DateTime, b.Start.DateTime)
		}

		return timeA.Compare(timeB)
	})

	return tousEvenements
}

func (s *ServiceAgenda) ConstructionMessage(horizon string, tousEvenements []EvenementCalendrier) (string, []interface{}, error) {
	var params []interface{}
	var sb strings.Builder

	if len(tousEvenements) == 0 {
		switch horizon {
		case "demain":
			return i18n.T("agenda.vide.demain"), nil, nil
		case "semaine":
			return i18n.T("agenda.vide.semaine"), nil, nil
		case "mois":
			return i18n.T("agenda.vide.mois"), nil, nil
		default:
			return i18n.T("agenda.vide.jour"), nil, nil
		}
	}

	switch horizon {
	case "demain":
		sb.WriteString(i18n.T("agenda.demain") + "\n")
	case "semaine":
		sb.WriteString(i18n.T("agenda.semaine") + "\n")
	case "mois":
		sb.WriteString(i18n.T("agenda.mois") + "\n")
	default:
		sb.WriteString(i18n.T("agenda.aujourd.hui") + "\n")
	}

	for _, e := range tousEvenements {
		val := e.Start.Value()
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			t, err = time.Parse("2006-01-02", val)
		}

		if err != nil {
			sb.WriteString("• %s\n")
			params = append(params, e.Summary)
			continue
		}

		jour := joursFR[t.Weekday()]
		nomMois := moisFR[t.Month()-1]

		if t.Hour() != 0 || t.Minute() != 0 {
			heureFormatee := fmt.Sprintf("%d heures", t.Hour())
			if t.Minute() > 0 {
				heureFormatee = fmt.Sprintf("%d heures %02d", t.Hour(), t.Minute())
			}

			sb.WriteString("• %s %d %s à %s : %s\n")
			params = append(params, jour, t.Day(), nomMois, heureFormatee, e.Summary)
		} else {
			sb.WriteString("• %s %d %s : %s (toute la journée)\n")
			params = append(params, jour, t.Day(), nomMois, e.Summary)
		}
	}

	return sb.String(), params, nil
}

type Agenda struct {
	Horizon    string                `json:"horizon"`
	Evenements []EvenementCalendrier `json:"evenements"`
}

func (s *ServiceAgenda) RecupererEtat(app Appareil, dateCible time.Time, params map[string]interface{}) (*EtatComplet, any, error) {
	horizon, _ := params["horizon"].(string)
	now := time.Now()

	var reponse Agenda
	reponse.Horizon = horizon
	var debut, fin time.Time
	switch horizon {
	case "demain":
		debut = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.Local)
		fin = debut.Add(24 * time.Hour)
	case "semaine":
		debut = now
		fin = now.Add(7 * 24 * time.Hour)
	case "mois":
		debut = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		fin = time.Date(now.Year(), now.Month()+1, now.Day(), 0, 0, 0, 0, time.Local)
	default:
		debut = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		fin = debut.Add(24 * time.Hour)
	}

	reponse.Evenements = s.getEvenements(debut, fin)

	return nil, reponse, nil

}

func (s *ServiceAgenda) EtatEnMessage(app Appareil, etat *EtatComplet, etatCustom any, dateCible time.Time) types.Message {
	log.Printf("[Agenda] Début de la conversion d'état en message pour l'appareil: %s", app.FriendlyNameExact)

	if calendrier, ok := etatCustom.(Agenda); ok {
		log.Printf("[Agenda] Type 'Agenda' détecté avec succès pour l'horizon: '%s' (%d événements)", calendrier.Horizon, len(calendrier.Evenements))

		message, params, err := s.ConstructionMessage(calendrier.Horizon, calendrier.Evenements)
		if err != nil {
			log.Printf("⚠️ [Agenda] Erreur lors de la construction du message: %v", err)
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

		log.Printf("[Agenda] Message construit avec succès. Template: '%s' | Nombre de params: %d", message, len(params))

		return types.Message{
			SMS: types.MessageDetails{
				Texte:  message,
				Params: params,
			},
			Voix: types.MessageDetails{
				Texte:  message,
				Params: params,
			},
		}
	}

	log.Printf("❌ [Agenda] Échec critique: etatCustom n'est pas de type 'Agenda' (type réel reçu: %T)", etatCustom)

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
