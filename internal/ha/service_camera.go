package ha

// ServiceCamera gère le domaine "camera"
type ServiceCamera struct{ serviceBase }

func NewServiceCamera(c *Client) *ServiceCamera {
	return &ServiceCamera{newServiceBase("camera", c, map[string]string{
		"allume":              "turn_on",
		"active":              "turn_on",
		"éteins":              "turn_off",
		"désactive":           "turn_off",
		"capture":             "snapshot",
		"photo":               "snapshot",
		"active détection":    "enable_motion_detection",
		"désactive détection": "disable_motion_detection",
	})}
}

func (s *ServiceCamera) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
}

func (s *ServiceCamera) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "snapshot"
	}
	haParams := map[string]interface{}{}
	if f, ok := params["filename"].(string); ok {
		haParams["filename"] = f
	}
	return s.appeler(app.EntityID, action, haParams)
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceCamera) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
