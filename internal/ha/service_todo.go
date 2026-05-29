package ha

// ServiceTodo gère le domaine "todo" (listes de tâches)
type ServiceTodo struct{ serviceBase }

func NewServiceTodo(c *Client) *ServiceTodo {
	return &ServiceTodo{newServiceBase("todo", c, map[string]VerbeConfig{
		"ajoute":   {Action: "add_item"},
		"rajoute":  {Action: "add_item"},
		"supprime": {Action: "remove_item"},
		"retire":   {Action: "remove_item"},
		"complète": {Action: "update_item"},
		"termine":  {Action: "update_item"},
		"liste":    {Action: "get_items"},
		"montre":   {Action: "get_items"},
	})}
}

func (s *ServiceTodo) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "add_item"
	}
	haParams := map[string]interface{}{}
	if item, ok := params["item"].(string); ok {
		haParams["item"] = item
	}
	return s.appeler(app.EntityID, action, haParams)
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceTodo) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
