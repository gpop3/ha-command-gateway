package ha

import "strings"

type ServiceLight struct{ serviceBase }

func NewServiceLight(c *Client) *ServiceLight {
	couleurs := []string{"rouge", "vert", "bleu", "jaune", "orange", "violet", "rose", "blanc", "cyan"}
	temperatures := []string{"chaud", "chaleureux", "froid", "neutre", "daylight"}
	params := append(couleurs, temperatures...)

	return &ServiceLight{newServiceBase("light", c, map[string]VerbeConfig{
		"allume":  {Action: "turn_on", Params: params},
		"éclaire": {Action: "turn_on", Params: params},
		"active":  {Action: "turn_on", Params: params},
		"éteins":  {Action: "turn_off"},
		"coupe":   {Action: "turn_off"},
		"bascule": {Action: "toggle", Params: params},
	})}
}

func (s *ServiceLight) ScoreDomaine(estAction bool) int {
	if estAction {
		return 20
	}
	return 0
}

func (s *ServiceLight) EstActionParDefaut() bool { return false }

func (s *ServiceLight) ExtraireParams(texte string) map[string]interface{} {
	params := s.serviceBase.ExtraireParams(texte)
	couleurs := map[string][]int{
		"rouge": {255, 0, 0}, "vert": {0, 255, 0}, "bleu": {0, 0, 255},
		"jaune": {255, 255, 0}, "orange": {255, 165, 0}, "violet": {128, 0, 128},
		"rose": {255, 105, 180}, "blanc": {255, 255, 255}, "cyan": {0, 255, 255},
	}
	for nom, rgb := range couleurs {
		if strings.Contains(texte, nom) {
			params["rgb"] = rgb
			break
		}
	}
	if strings.Contains(texte, "chaud") || strings.Contains(texte, "chaleureux") {
		params["kelvin"] = 2700
	} else if strings.Contains(texte, "froid") || strings.Contains(texte, "neutre") {
		params["kelvin"] = 4000
	} else if strings.Contains(texte, "daylight") {
		params["kelvin"] = 6500
	}
	return params
}

func (s *ServiceLight) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "turn_on"
	}
	haParams := map[string]interface{}{}
	if pct, ok := params["pourcentage"].(int); ok {
		haParams["brightness_pct"] = pct
	}
	if rgb, ok := params["rgb"].([]int); ok && len(rgb) == 3 {
		haParams["rgb_color"] = rgb
	}
	if kelvin, ok := params["kelvin"].(int); ok {
		haParams["color_temp_kelvin"] = kelvin
	}
	return s.appeler(app.EntityID, action, haParams)
}

func (s *ServiceLight) MotsReconnus() []string {
	return s.Verbes()
}
