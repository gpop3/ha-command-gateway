package ha

// ServiceAutomation gère le domaine "automation"
type ServiceAutomation struct{ serviceBase }

func NewServiceAutomation(c *Client) *ServiceAutomation {
	return &ServiceAutomation{newServiceBase("automation", c, map[string]VerbeConfig{
		"déclenche": {Action: "trigger"},
		"lance":     {Action: "trigger"},
		"active":    {Action: "turn_on"},
		"désactive": {Action: "turn_off"},
		"bascule":   {Action: "toggle"},
	})}
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
