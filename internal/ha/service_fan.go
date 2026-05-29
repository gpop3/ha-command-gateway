package ha

import "strings"

// ServiceFan gère le domaine "fan"
type ServiceFan struct{ serviceBase }

func NewServiceFan(c *Client) *ServiceFan {
	return &ServiceFan{newServiceBase("fan", c, map[string]string{
		"allume":     "turn_on",
		"active":     "turn_on",
		"éteins":     "turn_off",
		"coupe":      "turn_off",
		"accélère":   "increase_speed",
		"plus vite":  "increase_speed",
		"ralentis":   "decrease_speed",
		"moins vite": "decrease_speed",
		"oscille":    "oscillate",
		"pivote":     "oscillate",
	})}
}

func (s *ServiceFan) ScoreDomaine(estAction bool) int {
	if estAction {
		return 40
	}
	return 0
}

// ExtraireParams hérite des universels + ajoute : oscillation, direction, preset
func (s *ServiceFan) ExtraireParams(texte string) map[string]interface{} {
	params := s.serviceBase.ExtraireParams(texte)

	if strings.Contains(texte, "oscillat") || strings.Contains(texte, "pivote") {
		params["oscillating"] = true
	}

	directions := map[string]string{
		"avant":   "forward",
		"arrière": "reverse",
		"inverse": "reverse",
	}
	for mot, dir := range directions {
		if strings.Contains(texte, mot) {
			params["direction"] = dir
			break
		}
	}

	presets := map[string]string{
		"silencieux": "quiet",
		"turbo":      "turbo",
		"auto":       "auto",
		"normal":     "normal",
		"nuit":       "sleep",
	}
	for mot, preset := range presets {
		if strings.Contains(texte, mot) {
			params["preset_mode"] = preset
			break
		}
	}

	return params
}

func (s *ServiceFan) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	if pct, ok := params["pourcentage"].(int); ok {
		return s.appeler(app.EntityID, "set_percentage", map[string]interface{}{"percentage": pct})
	}
	if preset, ok := params["preset_mode"].(string); ok {
		return s.appeler(app.EntityID, "set_preset_mode", map[string]interface{}{"preset_mode": preset})
	}
	if dir, ok := params["direction"].(string); ok {
		return s.appeler(app.EntityID, "set_direction", map[string]interface{}{"direction": dir})
	}
	if osc, ok := params["oscillating"].(bool); ok {
		return s.appeler(app.EntityID, "oscillate", map[string]interface{}{"oscillating": osc})
	}

	action, ok := s.Verbe(verbe)
	if !ok {
		action = "turn_on"
	}
	return s.appeler(app.EntityID, action, nil)
}

func (s *ServiceFan) MotsReconnus() []string {
	return append(s.Verbes(),
		"silencieux", "turbo", "auto", "normal", "nuit",
		"avant", "arrière", "inverse",
		"oscillat", "pivote",
	)
}
