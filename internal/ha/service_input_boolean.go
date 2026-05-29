package ha

// ServiceInputBoolean gère le domaine "input_boolean" (interrupteurs virtuels)
type ServiceInputBoolean struct{ serviceBase }

func NewServiceInputBoolean(c *Client) *ServiceInputBoolean {
	return &ServiceInputBoolean{newServiceBase("input_boolean", c, map[string]VerbeConfig{
		"active":    {Action: "turn_on"},
		"allume":    {Action: "turn_on"},
		"désactive": {Action: "turn_off"},
		"éteins":    {Action: "turn_off"},
		"bascule":   {Action: "toggle"},
	})}
}

func (s *ServiceInputBoolean) ScoreDomaine(estAction bool) int {
	return -40
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceInputBoolean) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
