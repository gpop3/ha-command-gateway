package ha

// ServiceLock gère le domaine "lock"
type ServiceLock struct{ serviceBase }

func NewServiceLock(c *Client) *ServiceLock {
	return &ServiceLock{newServiceBase("lock", c, map[string]VerbeConfig{
		"verrouille":   {Action: "lock"},
		"ferme à clé":  {Action: "lock"},
		"déverrouille": {Action: "unlock"},
		"ouvre":        {Action: "open"},
	})}
}

func (s *ServiceLock) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "lock"
	}
	haParams := map[string]interface{}{}
	if code, ok := params["code"].(string); ok && code != "" {
		haParams["code"] = code
	}
	return s.appeler(app.EntityID, action, haParams)
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceLock) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
