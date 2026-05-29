package ha

// ServiceSelect gère le domaine "select" (listes déroulantes)
// Malus — trop générique, source de faux positifs
type ServiceSelect struct{ serviceBase }

func NewServiceSelect(c *Client) *ServiceSelect {
	return &ServiceSelect{newServiceBase("select", c, map[string]string{
		"sélectionne": "select_option",
		"choisis":     "select_option",
		"mets":        "select_option",
		"suivant":     "select_next",
		"précédent":   "select_previous",
		"premier":     "select_first",
		"dernier":     "select_last",
	})}
}

func (s *ServiceSelect) ScoreDomaine(_ bool) int {
	return -40
}

func (s *ServiceSelect) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}

func (s *ServiceSelect) EstDomaineSansEntites() bool {
	return true
}
