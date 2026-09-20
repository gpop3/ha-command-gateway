package ha

import (
	"encoding/json"
	"fmt"
	"ha-command-gateway/pkg/types"
	"slices"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/text"
)

type ServiceAgenda struct {
	serviceBase
	analyseur Analyseur
}

func NewServiceAgenda(c *Client) *ServiceAgenda {
	return &ServiceAgenda{serviceBase: newServiceBase("agenda", c, map[string]VerbeConfig{})}
}

func (s *ServiceAgenda) Init(a Analyseur) { s.analyseur = a }

func (s *ServiceAgenda) AppareilsVirtuels() []Appareil {
	return []Appareil{{
		EntityID: "agenda.home", FriendlyName: "agenda", FriendlyNameExact: "agenda", Domain: "agenda",
	}}
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

// getEvenements lit les événements de tous les calendriers HA ; `filtre` (facultatif)
// restreint aux calendriers dont l'id ou le nom contient ce mot (« mealie »).
func (s *ServiceAgenda) getEvenements(debut, fin time.Time, filtre string) []EvenementCalendrier {
	var garde func(Appareil) bool
	if filtre != "" {
		mot := text.Normaliser(filtre)
		garde = func(app Appareil) bool {
			return strings.Contains(text.Normaliser(app.EntityID+" "+app.FriendlyNameExact), mot)
		}
	}
	return s.evenementsFiltres(debut, fin, garde)
}

// evenementsCalendrier lit les événements d'UN calendrier sur [debut, fin].
func (s *ServiceAgenda) evenementsCalendrier(app Appareil, debut, fin time.Time) []EvenementCalendrier {
	path := fmt.Sprintf("/api/calendars/%s?start=%s&end=%s",
		app.EntityID,
		debut.UTC().Format("2006-01-02T15:04:05.000Z"),
		fin.UTC().Format("2006-01-02T15:04:05.000Z"),
	)
	body, err := s.client.get(path)
	if err != nil {
		logx.ErrorT("agenda.agenda", app.EntityID, err)
		return nil
	}
	var events []EvenementCalendrier
	if err := json.Unmarshal(body, &events); err != nil {
		logx.ErrorT("agenda.agenda.unmarshal", app.EntityID, err)
		return nil
	}
	return events
}

// debutEvenement : instant de début d'un événement (date seule = minuit local).
func debutEvenement(e EvenementCalendrier) time.Time {
	if e.Start.DateTime != "" {
		t, _ := time.Parse(time.RFC3339, e.Start.DateTime)
		return t
	}
	if e.Start.Date != "" {
		t, _ := time.ParseInLocation("2006-01-02", e.Start.Date, time.Local)
		return t
	}
	return time.Time{}
}

// evenementsFiltres lit les événements des calendriers acceptés par `garde`
// (nil = tous), triés par début.
func (s *ServiceAgenda) evenementsFiltres(debut, fin time.Time, garde func(Appareil) bool) []EvenementCalendrier {
	var tousEvenements []EvenementCalendrier
	if s.analyseur == nil {
		return tousEvenements
	}

	for _, app := range s.analyseur.GetCatalogue() {
		if app.Domain != "calendar" {
			continue
		}
		if garde != nil && !garde(app) {
			continue
		}
		tousEvenements = append(tousEvenements, s.evenementsCalendrier(app, debut, fin)...)
	}

	slices.SortFunc(tousEvenements, func(a, b EvenementCalendrier) int {
		return debutEvenement(a).Compare(debutEvenement(b))
	})

	return tousEvenements
}

func (s *ServiceAgenda) ConstructionMessage(horizon string, tousEvenements []EvenementCalendrier) (string, []interface{}, error) {
	var params []interface{}
	var sb strings.Builder

	if len(tousEvenements) == 0 {
		switch horizon {
		case "periode":
			return i18n.T("agenda.vide.periode"), nil, nil
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
	case "periode":
		sb.WriteString(i18n.T("agenda.periode") + "\n")
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
			sb.WriteString(i18n.GetPattern("agenda.ligne.simple"))
			params = append(params, e.Summary)
			continue
		}

		jour := joursFR[t.Weekday()]
		nomMois := moisFR[t.Month()-1]

		if t.Hour() != 0 || t.Minute() != 0 {
			heureFormatee := i18n.T("voix.heures", t.Hour())
			if t.Minute() > 0 {
				heureFormatee = i18n.T("voix.heures.minute", t.Hour(), t.Minute())
			}

			sb.WriteString(i18n.GetPattern("agenda.ligne.heure"))
			params = append(params, jour, t.Day(), nomMois, heureFormatee, e.Summary)
		} else {
			sb.WriteString(i18n.GetPattern("agenda.ligne.journee"))
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
	filtre, _ := params["calendrier"].(string)
	filtre = strings.TrimSpace(filtre)
	now := time.Now()

	var reponse Agenda
	reponse.Horizon = horizon

	// Période explicite (passé ou futur), fournie par l'IA
	if d, ok := params["debut"].(time.Time); ok {
		if f, ok := params["fin"].(time.Time); ok && f.After(d) {
			reponse.Horizon = "periode"
			reponse.Evenements = s.getEvenements(d, f, filtre)
			return nil, reponse, nil
		}
	}

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

	reponse.Evenements = s.getEvenements(debut, fin, filtre)

	return nil, reponse, nil

}

func (s *ServiceAgenda) EtatEnMessage(app Appareil, etat *EtatComplet, etatCustom any, dateCible time.Time) types.Message {
	if calendrier, ok := etatCustom.(Agenda); ok {
		message, params, err := s.ConstructionMessage(calendrier.Horizon, calendrier.Evenements)
		if err != nil {
			logx.WarnT("agenda.agenda.erreur.lors.de", err)
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

	logx.ErrorT("agenda.agenda.echec.critique.etatcustom", etatCustom)

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

func (s *ServiceAgenda) AutoriseMotsSansEntites() bool {
	return true
}
