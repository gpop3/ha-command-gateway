package ha

// ServiceSwitch gère le domaine "switch"
type ServiceSwitch struct{ serviceBase }

func NewServiceSwitch(c *Client) *ServiceSwitch {
	return &ServiceSwitch{newServiceBase("switch", c, map[string]VerbeConfig{
		"allume":    {Action: "turn_on"},
		"active":    {Action: "turn_on"},
		"éteins":    {Action: "turn_off"},
		"coupe":     {Action: "turn_off"},
		"désactive": {Action: "turn_off"},
		"bascule":   {Action: "toggle"},
	})}
}

func (s *ServiceSwitch) ScoreDomaine(estAction bool) int {
	return -40
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceSwitch) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
