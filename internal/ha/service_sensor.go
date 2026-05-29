package ha

// ServiceSensor gère le domaine "sensor" (capteurs lecture seule)
// Pas d'actions — uniquement la lecture d'état
type ServiceSensor struct{ serviceBase }

func NewServiceSensor(c *Client) *ServiceSensor {
	return &ServiceSensor{newServiceBase("sensor", c, map[string]string{})}
}

func (s *ServiceSensor) ScoreDomaine(estAction bool) int {
	return 20
}

func (s *ServiceSensor) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
