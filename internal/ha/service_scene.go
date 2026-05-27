package ha

// ServiceScene gère le domaine "scene"
type ServiceScene struct{ serviceBase }

func NewServiceScene(c *Client) *ServiceScene {
	return &ServiceScene{newServiceBase("scene", c, map[string]string{
		"active":    "turn_on",
		"lance":     "turn_on",
		"applique":  "turn_on",
		"déclenche": "turn_on",
	})}
}

func (s *ServiceScene) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
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
