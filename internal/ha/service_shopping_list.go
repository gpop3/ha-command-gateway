package ha

import "fmt"

// ServiceShoppingList gère le domaine "shopping_list"
// Note : shopping_list n'utilise pas entity_id dans ses appels API
type ServiceShoppingList struct{ serviceBase }

func NewServiceShoppingList(c *Client) *ServiceShoppingList {
	return &ServiceShoppingList{newServiceBase("shopping_list", c, map[string]string{
		"ajoute":   "add_item",
		"rajoute":  "add_item",
		"mets":     "add_item",
		"supprime": "remove_item",
		"retire":   "remove_item",
		"coche":    "complete_item",
		"complète": "complete_item",
		"décoche":  "incomplete_item",
		"vide":     "clear_completed_items",
	})}
}

// Executer pour shopping_list : pas d'entity_id dans le POST
func (s *ServiceShoppingList) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	// shopping_list n'accepte pas entity_id
	delete(params, "entity_id")
	_, err := s.client.post(fmt.Sprintf("/api/services/shopping_list/%s", action), params)
	if err != nil {
		return "", err
	}
	return "✅ Liste de courses mise à jour", nil
}

func (s *ServiceShoppingList) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "add_item"
	}
	haParams := map[string]interface{}{}
	if nom, ok := params["item"].(string); ok {
		haParams["name"] = nom
	}
	return s.Executer("", action, haParams)
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceShoppingList) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
