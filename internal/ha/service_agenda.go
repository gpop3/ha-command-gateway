package ha

import (
	"encoding/json"
	"fmt"
	"log"
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
func (s *ServiceAgenda) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return "", nil
}

func (s *ServiceAgenda) ScoreDomaine(_ bool) int { return 80 }

func (s *ServiceAgenda) EstActionParDefaut() bool { return true }

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

func (s *ServiceAgenda) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	horizon, _ := params["horizon"].(string)
	now := time.Now()
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
	return s.getEvenements(debut, fin, horizon)
}

func (s *ServiceAgenda) MotsReconnus() []string {
	return []string{
		"agenda", "calendrier", "rendez-vous", "prévu", "programme", "événements",
		"demain", "semaine", "aujourd'hui", "mois",
	}
}

func (s *ServiceAgenda) getEvenements(debut, fin time.Time, horizon string) (string, error) {
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

	if len(tousEvenements) == 0 {
		switch horizon {
		case "demain":
			return i18n.T("agenda.vide.demain"), nil
		case "semaine":
			return i18n.T("agenda.vide.semaine"), nil
		case "mois":
			return i18n.T("agenda.vide.mois"), nil
		default:
			return i18n.T("agenda.vide.jour"), nil
		}
	}

	var sb strings.Builder
	switch horizon {
	case "demain":
		sb.WriteString(i18n.T("agenda.demain"))
	case "semaine":
		sb.WriteString(i18n.T("agenda.semaine"))
	case "mois":
		sb.WriteString(i18n.T("agenda.mois"))
	default:
		sb.WriteString(i18n.T("agenda.aujourd.hui"))
	}

	for _, e := range tousEvenements {
		val := e.Start.Value()
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			t, err = time.Parse("2006-01-02", val)
		}
		if err != nil {
			_, err := fmt.Fprintf(&sb, "• %s\n", e.Summary)
			if err != nil {
				return "", err
			}
			continue
		}

		jour := joursFR[t.Weekday()]
		date := fmt.Sprintf("%s %d %s", jour, t.Day(), moisFR[t.Month()-1])

		if t.Hour() != 0 || t.Minute() != 0 {
			_, err := fmt.Fprintf(&sb, "• %s %dh%02d — %s\n", date, t.Hour(), t.Minute(), e.Summary)
			if err != nil {
				return "", err
			}
		} else {
			_, err := fmt.Fprintf(&sb, "• %s — %s\n", date, e.Summary)
			if err != nil {
				return "", err
			}
		}
	}

	return sb.String(), nil
}
