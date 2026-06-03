package ha

import (
	"encoding/json"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/pkg/types"
)

type ServiceWeather struct{ serviceBase }

var conditionsFR = map[string]string{
	"clear-night": "nuit dégagée", "cloudy": "nuageux", "fog": "brouillard",
	"hail": "grêle", "lightning": "orage", "lightning-rainy": "orage pluvieux",
	"partlycloudy": "partiellement nuageux", "pouring": "pluie forte", "rainy": "pluvieux",
	"snowy": "neigeux", "snowy-rainy": "neige et pluie", "sunny": "ensoleillé",
	"windy": "venteux", "windy-variant": "très venteux", "exceptional": "conditions exceptionnelles",
}

func NewServiceWeather(c *Client) *ServiceWeather {
	return &ServiceWeather{newServiceBase("weather", c, map[string]VerbeConfig{})}
}

func (s *ServiceWeather) ScoreDomaine(estAction bool) int { return 20 }

func (s *ServiceWeather) EstActionParDefaut() bool { return false }

func (s *ServiceWeather) ExtraireParams(texte string) map[string]interface{} {
	switch {
	case strings.Contains(texte, "après-demain"):
		return map[string]interface{}{"horizon": "daily", "jour": 2}
	case strings.Contains(texte, "demain"):
		return map[string]interface{}{"horizon": "daily", "jour": 1}
	case strings.Contains(texte, "week-end") || strings.Contains(texte, "weekend"):
		return map[string]interface{}{"horizon": "weekend"}
	case strings.Contains(texte, "semaine"):
		return map[string]interface{}{"horizon": "semaine"}
	case strings.Contains(texte, "heure") || strings.Contains(texte, "après-midi") ||
		strings.Contains(texte, "soir") || strings.Contains(texte, "nuit") ||
		strings.Contains(texte, "matin"):
		return map[string]interface{}{"horizon": "hourly"}
	default:
		return map[string]interface{}{"horizon": "current"}
	}
}

func (s *ServiceWeather) MotsReconnus() []string {
	return []string{
		"maintenant", "demain", "après-demain", "aujourd'hui",
		"ce matin", "cet après-midi", "ce soir", "cette nuit",
		"prochaine heure", "heure", "semaine", "week-end", "weekend",
	}
}

// MeteoData transporte l'état météo récupéré jusqu'à la construction du message.
type MeteoData struct {
	Horizon    string
	Condition  string
	Temp       float64
	Humidite   int
	Vent       float64
	Jour       int
	Previsions []PrevisionHoraire
}

func (s *ServiceWeather) RecupererEtat(app Appareil, dateCible time.Time, params map[string]interface{}) (*EtatComplet, any, error) {
	horizon, _ := params["horizon"].(string)
	data := MeteoData{Horizon: horizon}

	switch horizon {
	case "hourly":
		prev, err := s.getPrevisions(app.EntityID, "hourly")
		if err != nil {
			logx.ErrorT("meteo.erreur.previsions", app.EntityID, "hourly", err)
			return nil, nil, err
		}
		data.Previsions = prev

	case "daily", "semaine", "weekend":
		if horizon == "daily" {
			jour, _ := params["jour"].(int)
			if jour < 1 {
				jour = 1
			}
			data.Jour = jour
		}
		prev, err := s.getPrevisions(app.EntityID, "daily")
		if err != nil {
			logx.ErrorT("meteo.erreur.previsions", app.EntityID, "daily", err)
			return nil, nil, err
		}
		data.Previsions = prev

	default:
		etat, err := s.client.RecupererEtatLive(app.EntityID)
		if err != nil {
			logx.ErrorT("meteo.erreur.actuel", app.EntityID, err)
			return nil, nil, err
		}
		data.Condition = etat.State
		data.Temp = etat.Attributes.Temperature
		data.Humidite = etat.Attributes.Humidity
		data.Vent = etat.Attributes.WindSpeed
	}
	return nil, data, nil
}

func (s *ServiceWeather) EtatEnMessage(app Appareil, etat *EtatComplet, etatCustom any, dateCible time.Time) types.Message {
	data, ok := etatCustom.(MeteoData)
	if !ok {
		logx.ErrorT("meteo.erreur.etatcustom", etatCustom)
		return messageErreurMeteo()
	}
	message, params := s.construireMessage(data)
	return types.Message{
		SMS:  types.MessageDetails{Texte: message, Params: params},
		Voix: types.MessageDetails{Texte: message, Params: params},
	}
}

func (s *ServiceWeather) construireMessage(d MeteoData) (string, []interface{}) {
	var sb strings.Builder
	var params []interface{}

	switch d.Horizon {
	case "hourly":
		if len(d.Previsions) == 0 {
			return i18n.GetPattern("meteo.indispo"), nil
		}
		sb.WriteString(i18n.GetPattern("meteo.previsions"))
		maxP := 6
		if len(d.Previsions) < maxP {
			maxP = len(d.Previsions)
		}
		for _, p := range d.Previsions[:maxP] {
			t, _ := time.Parse(time.RFC3339, p.DateTime)
			sb.WriteString(i18n.GetPattern("meteo.heure.ligne"))
			params = append(params, t.Format("15h04"), tradCondition(p.Condition), p.Temperature)
			if p.Precipitation > 0 {
				sb.WriteString(i18n.GetPattern("meteo.precipitation"))
				params = append(params, p.Precipitation)
			}
			sb.WriteString("\n")
		}

	case "semaine", "weekend":
		if len(d.Previsions) == 0 {
			return i18n.GetPattern("meteo.demain.indispo"), nil
		}
		if d.Horizon == "weekend" {
			sb.WriteString(i18n.GetPattern("meteo.weekend"))
		} else {
			sb.WriteString(i18n.GetPattern("meteo.semaine"))
		}
		nb := 0
		for _, p := range d.Previsions {
			t := parseJour(p.DateTime)
			if t.IsZero() {
				continue
			}
			if d.Horizon == "weekend" && t.Weekday() != time.Saturday && t.Weekday() != time.Sunday {
				continue
			}
			sb.WriteString(i18n.GetPattern("meteo.jour.ligne"))
			params = append(params, joursFR[t.Weekday()], tradCondition(p.Condition), p.Temperature)
			if p.Precipitation > 0 {
				sb.WriteString(i18n.GetPattern("meteo.precipitation"))
				params = append(params, p.Precipitation)
			}
			sb.WriteString("\n")
			nb++
			if d.Horizon == "semaine" && nb >= 7 {
				break
			}
		}

	case "daily":
		if len(d.Previsions) <= d.Jour {
			return i18n.GetPattern("meteo.demain.indispo"), nil
		}
		p := d.Previsions[d.Jour]
		sb.WriteString(i18n.GetPattern("meteo.demain"))
		params = append(params, tradCondition(p.Condition), p.Temperature)
		if p.Precipitation > 0 {
			sb.WriteString(i18n.GetPattern("meteo.precipitation"))
			params = append(params, p.Precipitation)
		}
		if p.WindSpeed > 0 {
			sb.WriteString(i18n.GetPattern("meteo.demain.vent"))
			params = append(params, p.WindSpeed)
		}

	default:
		sb.WriteString(i18n.GetPattern("meteo.actuelle"))
		params = append(params, tradCondition(d.Condition), d.Temp)
		if d.Humidite > 0 {
			sb.WriteString(i18n.GetPattern("meteo.humidite"))
			params = append(params, d.Humidite)
		}
		if d.Vent > 0 {
			sb.WriteString(i18n.GetPattern("meteo.vent"))
			params = append(params, d.Vent)
		}
	}
	return sb.String(), params
}

func (s *ServiceWeather) getPrevisions(entityID, forecastType string) ([]PrevisionHoraire, error) {
	payload := map[string]interface{}{"entity_id": entityID, "type": forecastType}
	body, err := s.client.post("/api/services/weather/get_forecasts?return_response", payload)
	if err != nil {
		return nil, err
	}
	var result struct {
		ServiceResponse map[string]struct {
			Forecast []PrevisionHoraire `json:"forecast"`
		} `json:"service_response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if data, ok := result.ServiceResponse[entityID]; ok {
		return data.Forecast, nil
	}
	return nil, nil
}

func parseJour(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

func messageErreurMeteo() types.Message {
	return types.Message{
		SMS:  types.MessageDetails{Texte: i18n.T("erreur.lecture.parler"), Params: []interface{}{}},
		Voix: types.MessageDetails{Texte: i18n.T("erreur.lecture.parler"), Params: []interface{}{}},
	}
}

func tradCondition(condition string) string {
	if fr, ok := conditionsFR[condition]; ok {
		return fr
	}
	return condition
}
