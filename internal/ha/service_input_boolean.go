package ha

// ServiceInputBoolean gère le domaine "input_boolean" (interrupteurs virtuels)
type ServiceInputBoolean struct{ serviceBase }

func NewServiceInputBoolean(c *Client) *ServiceInputBoolean {
	return &ServiceInputBoolean{newServiceBase("input_boolean", c, map[string]string{
		"active":    "turn_on",
		"allume":    "turn_on",
		"désactive": "turn_off",
		"éteins":    "turn_off",
		"bascule":   "toggle",
	})}
}

func (s *ServiceInputBoolean) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
}

func (s *ServiceInputBoolean) ScoreDomaine(estAction bool) int {
	return -40
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceInputBoolean) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
