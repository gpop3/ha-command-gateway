package ha

import (
	"fmt"

	"ha-command-gateway/internal/i18n"
)

// ServiceShoppingList gère le domaine "shopping_list"
// Note : shopping_list n'utilise pas entity_id dans ses appels API
type ServiceShoppingList struct{ serviceBase }

func NewServiceShoppingList(c *Client) *ServiceShoppingList {
	return &ServiceShoppingList{newServiceBase("shopping_list", c, map[string]VerbeConfig{
		"ajoute":   {Action: "add_item"},
		"rajoute":  {Action: "add_item"},
		"mets":     {Action: "add_item"},
		"supprime": {Action: "remove_item"},
		"retire":   {Action: "remove_item"},
		"coche":    {Action: "complete_item"},
		"complète": {Action: "complete_item"},
		"décoche":  {Action: "incomplete_item"},
		"vide":     {Action: "clear_completed_items"},
	})}
}

// Executer pour shopping_list
func (s *ServiceShoppingList) appeler(entityID, action string, params map[string]interface{}) (string, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	// shopping_list n'accepte pas entity_id
	delete(params, "entity_id")
	_, err := s.client.post(fmt.Sprintf("/api/services/shopping_list/%s", action), params)
	if err != nil {
		return "", err
	}
	return i18n.T("shopping.maj"), nil
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
	return s.appeler("", action, haParams)
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceShoppingList) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
