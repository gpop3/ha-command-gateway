package ha

// ServiceCamera gère le domaine "camera"
type ServiceCamera struct{ serviceBase }

func NewServiceCamera(c *Client) *ServiceCamera {
	return &ServiceCamera{newServiceBase("camera", c, map[string]VerbeConfig{
		"allume":              {Action: "turn_on"},
		"active":              {Action: "turn_on"},
		"éteins":              {Action: "turn_off"},
		"désactive":           {Action: "turn_off"},
		"capture":             {Action: "snapshot"},
		"photo":               {Action: "snapshot"},
		"active détection":    {Action: "enable_motion_detection"},
		"désactive détection": {Action: "disable_motion_detection"},
	})}
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
