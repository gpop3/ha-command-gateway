package ha

// ServiceScene gère le domaine "scene"
type ServiceScene struct{ serviceBase }

func NewServiceScene(c *Client) *ServiceScene {
	return &ServiceScene{newServiceBase("scene", c, map[string]VerbeConfig{
		"active":    {Action: "turn_on"},
		"lance":     {Action: "turn_on"},
		"applique":  {Action: "turn_on"},
		"déclenche": {Action: "turn_on"},
	})}
}

func (s *ServiceScene) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	haParams := map[string]interface{}{}
	if t, ok := params["transition"].(int); ok {
		haParams["transition"] = t
	}
	return s.appeler(app.EntityID, "turn_on", haParams)
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceScene) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
