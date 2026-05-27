package ha

// ServiceAutomation gère le domaine "automation"
type ServiceAutomation struct{ serviceBase }

func NewServiceAutomation(c *Client) *ServiceAutomation {
	return &ServiceAutomation{newServiceBase("automation", c, map[string]string{
		"déclenche": "trigger",
		"lance":     "trigger",
		"active":    "turn_on",
		"désactive": "turn_off",
		"bascule":   "toggle",
	})}
}

func (s *ServiceAutomation) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
}

func (s *ServiceAutomation) ScoreDomaine(estAction bool) int {
	if estAction {
		return -30
	}
	return 0
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceAutomation) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
