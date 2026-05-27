package ha

// ServiceBinarySensor gère le domaine "binary_sensor" (détecteurs on/off)
// Pas d'actions — uniquement la lecture d'état (mouvement, ouverture, présence...)
type ServiceBinarySensor struct{ serviceBase }

func NewServiceBinarySensor(c *Client) *ServiceBinarySensor {
	return &ServiceBinarySensor{newServiceBase("binary_sensor", c, map[string]string{})}
}

func (s *ServiceBinarySensor) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return "", nil // lecture seule
}

func (s *ServiceBinarySensor) ScoreDomaine(estAction bool) int {
	if !estAction {
		return 25
	}
	return 0
}

func (s *ServiceBinarySensor) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
