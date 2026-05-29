package ha

// ServiceNumber gère le domaine "number" (valeurs numériques ajustables)
// Rarement pertinent en vocal — malus pour éviter les faux positifs
type ServiceNumber struct{ serviceBase }

func NewServiceNumber(c *Client) *ServiceNumber {
	return &ServiceNumber{newServiceBase("number", c, map[string]string{
		"règle":    "set_value",
		"mets":     "set_value",
		"augmente": "set_value",
		"diminue":  "set_value",
	})}
}

func (s *ServiceNumber) ScoreDomaine(_ bool) int {
	// Malus systématique — trop générique, source de faux positifs
	return -40
}

func (s *ServiceNumber) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}

func (s *ServiceNumber) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	if temp, ok := params["temperature"].(float64); ok {
		return s.appeler(app.EntityID, "set_value", map[string]interface{}{"value": temp})
	}
	if pct, ok := params["pourcentage"].(int); ok {
		return s.appeler(app.EntityID, "set_value", map[string]interface{}{"value": float64(pct)})
	}
	return s.appeler(app.EntityID, "set_value", params)
}
