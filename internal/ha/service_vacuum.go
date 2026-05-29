package ha

import "strings"

// ServiceVacuum gère le domaine "vacuum"
type ServiceVacuum struct{ serviceBase }

func NewServiceVacuum(c *Client) *ServiceVacuum {
	vitesses := []string{"silencieux", "normal", "turbo", "max", "fort", "doux"}

	return &ServiceVacuum{newServiceBase("vacuum", c, map[string]VerbeConfig{
		"démarre":  {Action: "start", Params: vitesses},
		"lance":    {Action: "start", Params: vitesses},
		"aspire":   {Action: "start", Params: vitesses},
		"pause":    {Action: "pause"},
		"stoppe":   {Action: "stop"},
		"arrête":   {Action: "stop"},
		"stop":     {Action: "stop"},
		"rentre":   {Action: "return_to_base"},
		"base":     {Action: "return_to_base"},
		"recharge": {Action: "return_to_base"},
		"localise": {Action: "locate"},
	})}
}

func (s *ServiceVacuum) ScoreDomaine(estAction bool) int {
	if estAction {
		return 30
	}
	return 0
}

// ExtraireParams hérite des universels + ajoute : fan_speed
func (s *ServiceVacuum) ExtraireParams(texte string) map[string]interface{} {
	params := s.serviceBase.ExtraireParams(texte)

	vitesses := map[string]string{
		"silencieux": "quiet",
		"normal":     "standard",
		"turbo":      "turbo",
		"max":        "max",
		"fort":       "turbo",
		"doux":       "quiet",
	}
	for mot, vitesse := range vitesses {
		if strings.Contains(texte, mot) {
			params["fan_speed"] = vitesse
			break
		}
	}

	return params
}

func (s *ServiceVacuum) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	// Vitesse d'aspiration
	if speed, ok := params["fan_speed"].(string); ok {
		return s.appeler(app.EntityID, "set_fan_speed", map[string]interface{}{"fan_speed": speed})
	}

	action, ok := s.Verbe(verbe)
	if !ok {
		action = "start"
	}
	return s.appeler(app.EntityID, action, nil)
}

func (s *ServiceVacuum) MotsReconnus() []string {
	return s.Verbes()
}
