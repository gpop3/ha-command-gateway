package ha

import (
	"fmt"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
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
	return &ServiceScript{newServiceBase("script", c, map[string]VerbeConfig{
		"exécute": {Action: "turn_on"},
		"lance":   {Action: "turn_on"},
		"démarre": {Action: "turn_on"},
		"arrête":  {Action: "turn_off"},
	})}
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

	// Paramètres structurés proposés par l'IA (déjà filtrés sur les `fields`
	// déclarés par le script, cf. Client.ParametresIAValides)
	vars, depuisIA := params["variables"].(map[string]interface{})
	if depuisIA {
		for k, v := range vars {
			variables[k] = v
		}
	}

	// Chemin classique (texte libre « dire ... ») : message / message_vocal
	if msg, ok := params["message"].(string); ok && msg != "" {
		if _, existe := variables["message"]; !existe {
			variables["message"] = msg
		}
		if _, existe := variables["message_vocal"]; !existe {
			variables["message_vocal"] = msg
		}
	}

	// Pour l'IA : refuser d'exécuter un script dont un champ obligatoire manque
	if depuisIA {
		for nom, champ := range s.client.champsScript(app.EntityID) {
			if _, present := variables[nom]; champ.Requis && !present {
				return "", fmt.Errorf("%s", i18n.T("script.parametre.manquant", nom))
			}
		}
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
	logx.DebugT("script.extraireparams.texte.params", texte, params)

	return params
}

func (s *ServiceScript) MotsReconnus() []string {
	return []string{
		"dire", "message", "annonce",
	}
}
