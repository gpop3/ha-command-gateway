package ha

import (
	"encoding/json"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
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
	// Map vide — "météo", "pluie", "soleil" sont des sujets, pas des verbes d'action
	return &ServiceWeather{newServiceBase("weather", c, map[string]string{})}
}

func (s *ServiceWeather) ScoreDomaine(estAction bool) int { return 20 }

func (s *ServiceWeather) EstActionParDefaut() bool { return true }

func (s *ServiceWeather) ExtraireParams(texte string) map[string]interface{} {
	params := map[string]interface{}{}
	if strings.Contains(texte, "demain") {
		params["horizon"] = "daily"
	} else if strings.Contains(texte, "heure") || strings.Contains(texte, "après-midi") || strings.Contains(texte, "soir") {
		params["horizon"] = "hourly"
	} else {
		params["horizon"] = "current"
	}
	return params
}

func (s *ServiceWeather) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	horizon, _ := params["horizon"].(string)
	switch horizon {
	case "hourly":
		return s.previsionHoraire(app.EntityID)
	case "daily":
		return s.previsionDemain(app.EntityID)
	default:
		return s.etatActuel(app.EntityID)
	}
}

func (s *ServiceWeather) MotsReconnus() []string {
	return []string{
		"météo", "temps", "pluie", "soleil", "nuage", "vent", "brouillard",
		"demain", "prévisions", "température", "chaud", "froid",
	}
}

func (s *ServiceWeather) etatActuel(entityID string) (string, error) {
	etat, err := s.client.RecupererEtatLive(entityID)

	if err != nil {
		return "", err
	}
	condition := tradCondition(etat.State)
	reponse := i18n.T("meteo.actuelle", condition, etat.Attributes.Temperature)
	if etat.Attributes.Humidity > 0 {
		reponse += i18n.T("meteo.humidite", etat.Attributes.Humidity)
	}
	if etat.Attributes.WindSpeed > 0 {
		reponse += i18n.T("meteo.vent", etat.Attributes.WindSpeed)
	}
	return reponse, nil
}

func (s *ServiceWeather) previsionHoraire(entityID string) (string, error) {
	previsions, err := s.getPrevisions(entityID, "hourly")
	if err != nil {
		return "", err
	}
	if len(previsions) == 0 {
		return i18n.T("meteo.indispo"), nil
	}
	var sb strings.Builder
	sb.WriteString(i18n.T("meteo.previsions"))
	max := 6
	if len(previsions) < max {
		max = len(previsions)
	}
	for _, p := range previsions[:max] {
		t, _ := time.Parse(time.RFC3339, p.DateTime)
		heure := t.Format("15h04")
		sb.WriteString(i18n.T("meteo.heure.ligne", heure, tradCondition(p.Condition), p.Temperature))
		if p.Precipitation > 0 {
			sb.WriteString(i18n.T("meteo.precipitation", p.Precipitation))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func (s *ServiceWeather) previsionDemain(entityID string) (string, error) {
	previsions, err := s.getPrevisions(entityID, "daily")
	if err != nil {
		return "", err
	}
	if len(previsions) < 2 {
		return i18n.T("meteo.demain.indispo"), nil
	}
	p := previsions[1]
	reponse := i18n.T("meteo.demain", tradCondition(p.Condition), p.Temperature)
	if p.Precipitation > 0 {
		reponse += i18n.T("meteo.precipitation", p.Precipitation)
	}
	if p.WindSpeed > 0 {
		reponse += i18n.T("meteo.demain.vent", p.WindSpeed)
	}
	return reponse, nil
}

func (s *ServiceWeather) getPrevisions(entityID, forecastType string) ([]PrevisionHoraire, error) {
	payload := map[string]interface{}{"entity_id": entityID, "type": forecastType}
	body, err := s.client.post("/api/services/weather/get_forecasts", payload)
	if err != nil {
		return nil, err
	}
	var result map[string]struct {
		Forecast []PrevisionHoraire `json:"forecast"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if data, ok := result[entityID]; ok {
		return data.Forecast, nil
	}
	return nil, nil
}

func tradCondition(condition string) string {
	if fr, ok := conditionsFR[condition]; ok {
		return fr
	}
	return condition
}

func (s *ServiceWeather) EstDomaineSansEntites() bool {
	return true
}
