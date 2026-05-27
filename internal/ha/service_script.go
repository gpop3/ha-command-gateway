package ha

import (
	"log"
	"strings"
)

// ServiceScript gère le domaine "script"
// Scripts disponibles sur cette instance :
//   - charger_mealplan_mealie
//   - ajouter_evenement_calendrier
//   - annonce_alerte_intelligente       (param: message_vocal)
//   - annonce_alerte_echo_dot_sans_condition (param: message_vocal)
type ServiceScript struct{ serviceBase }

func NewServiceScript(c *Client) *ServiceScript {
	return &ServiceScript{newServiceBase("script", c, map[string]string{
		"exécute": "turn_on",
		"lance":   "turn_on",
		"démarre": "turn_on",
		"arrête":  "turn_off",
	})}
}

func (s *ServiceScript) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
}

func (s *ServiceScript) ScoreDomaine(estAction bool) int {
	if estAction {
		return 10
	}
	return 0
}

func (s *ServiceScript) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "turn_on"
	}

	haParams := map[string]interface{}{
		"entity_id": app.EntityID,
	}

	// Construire les variables du script
	variables := map[string]interface{}{}
	if msg, ok := params["message"].(string); ok && msg != "" {
		variables["message"] = msg
		variables["message_vocal"] = msg
	}
	if len(variables) > 0 {
		haParams["variables"] = variables
	}

	return s.appeler(app.EntityID, action, haParams)
}

// ExtraireParams params du service
func (s *ServiceScript) ExtraireParams(texte string) map[string]interface{} {
	params := s.serviceBase.ExtraireParams(texte)

	mots := strings.Fields(texte)
	for i, mot := range mots {
		if mot == "dire" || mot == "message" || mot == "annonce" {
			if i+1 < len(mots) {
				params["message"] = strings.Join(mots[i+1:], " ")
				break
			}
		}
	}
	log.Printf("DEBUG ExtraireParams texte: '%s' → params: %v", texte, params)

	return params
}

func (s *ServiceScript) MotsReconnus() []string {
	return []string{
		"dire", "message", "annonce",
	}
}
