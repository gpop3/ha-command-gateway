package ha

import (
	"ha-command-gateway/pkg/types"
	"strings"
	"time"

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
	return &ServiceClimate{newServiceBase("climate", c, map[string]VerbeConfig{
		"allume":    {Action: "turn_on"},
		"active":    {Action: "turn_on", Params: []string{"chauffage", "climatise", "ventilateur", "sec", "auto"}},
		"éteins":    {Action: "turn_off"},
		"coupe":     {Action: "turn_off"},
		"règle":     {Action: "set_temperature", Params: []string{"degres", "degre", "temperature"}},
		"mets":      {Action: "set_temperature", Params: []string{"confort", "eco", "absent", "absence", "boost", "nuit", "sommeil", "antigel"}},
		"programme": {Action: "set_preset_mode", Params: []string{"confort", "eco", "absent", "absence", "boost", "nuit", "sommeil", "antigel"}},
	})}
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
		"température", "thermostat", "chauffe", "refroidis",
	)
}

func (s *ServiceClimate) EtatEnMessage(app Appareil, etat *EtatComplet, etatCustom any, dateCible time.Time) types.Message {
	var action string
	switch etat.Attributes.HvacAction {
	case "heating":
		action = i18n.T("climate.chauffe")
	case "cooling":
		action = i18n.T("climate.refroid")
	default:
		action = i18n.T("climate.repos")
	}

	return types.Message{
		SMS: types.MessageDetails{
			Texte:  i18n.T("climate.format"),
			Params: []interface{}{app.FriendlyNameExact, etat.Attributes.CurrentTemperature, etat.Attributes.Temperature, action, etat.State},
		},
		Voix: types.MessageDetails{
			Texte:  i18n.T("assistant.retour.climate"),
			Params: []interface{}{etat.Attributes.CurrentTemperature, etat.Attributes.Temperature, action, etat.State},
		},
	}
}

func (s *ServiceClimate) AutoriseMotsSansEntites() bool {
	return true
}
