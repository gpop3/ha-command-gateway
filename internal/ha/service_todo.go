package ha

// ServiceTodo gère le domaine "todo" (listes de tâches)
type ServiceTodo struct{ serviceBase }

func NewServiceTodo(c *Client) *ServiceTodo {
	return &ServiceTodo{newServiceBase("todo", c, map[string]string{
		"ajoute":   "add_item",
		"rajoute":  "add_item",
		"supprime": "remove_item",
		"retire":   "remove_item",
		"complète": "update_item",
		"termine":  "update_item",
		"liste":    "get_items",
		"montre":   "get_items",
	})}
}

func (s *ServiceTodo) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
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
