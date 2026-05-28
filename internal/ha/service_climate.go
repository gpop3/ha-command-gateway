package ha

import (
	"fmt"
	"strings"

	"ha-command-gateway/internal/i18n"
)

type ServiceClimate struct{ serviceBase }

type HvacMode string

const (
	HvacOff      HvacMode = "off"
	HvacHeat     HvacMode = "heat"
	HvacCool     HvacMode = "cool"
	HvacHeatCool HvacMode = "heat_cool"
	HvacAuto     HvacMode = "auto"
	HvacFanOnly  HvacMode = "fan_only"
	HvacDry      HvacMode = "dry"
)

func NewServiceClimate(c *Client) *ServiceClimate {
	return &ServiceClimate{newServiceBase("climate", c, map[string]string{
		// Uniquement des verbes d'action
		"allume":    "turn_on",
		"active":    "turn_on",
		"éteins":    "turn_off",
		"coupe":     "turn_off",
		"règle":     "set_temperature",
		"mets":      "set_temperature",
		"programme": "set_preset_mode",
	})}
}

func (s *ServiceClimate) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
}

func (s *ServiceClimate) ScoreDomaine(estAction bool) int {
	if estAction {
		return 0
	}
	return 10
}

func (s *ServiceClimate) EstActionParDefaut() bool { return false }

func (s *ServiceClimate) ExtraireParams(texte string) map[string]interface{} {
	params := s.serviceBase.ExtraireParams(texte)
	presets := map[string]string{
		"confort": "comfort", "comfort": "comfort", "éco": "eco", "eco": "eco",
		"absent": "away", "absence": "away", "boost": "boost",
		"nuit": "sleep", "sommeil": "sleep", "hors-gel": "frost_protection", "antigel": "frost_protection",
	}
	for mot, preset := range presets {
		if strings.Contains(texte, mot) {
			params["preset_mode"] = preset
			break
		}
	}
	modes := map[string]string{
		"chauffage": string(HvacHeat), "chauffe": string(HvacHeat),
		"froid": string(HvacCool), "climatise": string(HvacCool),
		"auto": string(HvacAuto), "ventilateur": string(HvacFanOnly), "sec": string(HvacDry),
	}
	for mot, mode := range modes {
		if strings.Contains(texte, mot) {
			params["hvac_mode"] = mode
			break
		}
	}
	return params
}

func (s *ServiceClimate) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	if temp, ok := params["temperature"].(float64); ok {
		return s.appeler(app.EntityID, "set_temperature", map[string]interface{}{"temperature": temp})
	}
	if pct, ok := params["pourcentage"].(int); ok {
		return s.appeler(app.EntityID, "set_temperature", map[string]interface{}{"temperature": float64(pct)})
	}
	if preset, ok := params["preset_mode"].(string); ok {
		return s.appeler(app.EntityID, "set_preset_mode", map[string]interface{}{"preset_mode": preset})
	}
	if mode, ok := params["hvac_mode"].(string); ok {
		return s.appeler(app.EntityID, "set_hvac_mode", map[string]interface{}{"hvac_mode": mode})
	}
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "turn_on"
	}
	return s.appeler(app.EntityID, action, nil)
}

func (s *ServiceClimate) MotsReconnus() []string {
	return append(s.Verbes(),
		"confort", "éco", "eco", "absent", "absence", "boost", "nuit", "sommeil", "hors-gel", "antigel",
		"chauffage", "climatise", "ventilateur", "sec", "auto",
		"degrés", "degré", "température", "thermostat", "chauffe", "refroidis",
	)
}

func FormaterEtatClimate(nom string, etat *EtatComplet) string {
	var action string
	switch etat.Attributes.HvacAction {
	case "heating":
		action = i18n.T("climate.chauffe")
	case "cooling":
		action = i18n.T("climate.refroid")
	default:
		action = i18n.T("climate.repos")
	}
	return fmt.Sprintf(i18n.T("climate.format"),
		nom, etat.Attributes.CurrentTemperature, etat.Attributes.Temperature, action, etat.State)
}

func FormaterEtatClimateVoix(nom string, etat *EtatComplet) string {
	var action string
	switch etat.Attributes.HvacAction {
	case "heating":
		action = i18n.T("climate.chauffe")
	case "cooling":
		action = i18n.T("climate.refroid")
	default:
		action = i18n.T("climate.repos")
	}
	return fmt.Sprintf(i18n.T("assistant.retour.climate"),
		etat.Attributes.CurrentTemperature, etat.Attributes.Temperature, action, etat.State)
}
