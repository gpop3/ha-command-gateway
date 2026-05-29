package ha

// ServiceSwitch gère le domaine "switch"
type ServiceSwitch struct{ serviceBase }

func NewServiceSwitch(c *Client) *ServiceSwitch {
	return &ServiceSwitch{newServiceBase("switch", c, map[string]string{
		"allume":    "turn_on",
		"active":    "turn_on",
		"éteins":    "turn_off",
		"coupe":     "turn_off",
		"désactive": "turn_off",
		"bascule":   "toggle",
	})}
}

func (s *ServiceSwitch) ScoreDomaine(estAction bool) int {
	return -40
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceSwitch) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
