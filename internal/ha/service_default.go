package ha

// ServiceDefault gère les états si pas de domaine trouvé
type ServiceDefault struct{ serviceBase }

func newServiceDefault(c *Client) *ServiceDefault {
	return &ServiceDefault{serviceBase: newServiceBase("service_default", c, map[string]VerbeConfig{})}

}

func (s *ServiceDefault) ScoreDomaine(estAction bool) int {
	return -100
}
