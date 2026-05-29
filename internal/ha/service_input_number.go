package ha

// ServiceInputNumber gère le domaine "input_number" (curseurs virtuels)
// Malus — trop générique, source de faux positifs
type ServiceInputNumber struct{ serviceBase }

func NewServiceInputNumber(c *Client) *ServiceInputNumber {
	return &ServiceInputNumber{newServiceBase("input_number", c, map[string]string{
		"règle":    "set_value",
		"mets":     "set_value",
		"augmente": "increment",
		"monte":    "increment",
		"diminue":  "decrement",
		"baisse":   "decrement",
	})}
}

func (s *ServiceInputNumber) ScoreDomaine(_ bool) int {
	return -40
}

func (s *ServiceInputNumber) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}

func (s *ServiceInputNumber) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	if temp, ok := params["temperature"].(float64); ok {
		return s.appeler(app.EntityID, "set_value", map[string]interface{}{"value": temp})
	}
	if pct, ok := params["pourcentage"].(int); ok {
		return s.appeler(app.EntityID, "set_value", map[string]interface{}{"value": float64(pct)})
	}
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "set_value"
	}
	return s.appeler(app.EntityID, action, params)
}
