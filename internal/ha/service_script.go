package ha

import (
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/text"
	"sort"
	"strings"
)

// ErreurParametreManquant : un script appelé par l'IA a des champs obligatoires
// (`fields` avec required: true) qui n'ont pas été fournis.
type ErreurParametreManquant struct {
	Champs []string
}

func (e *ErreurParametreManquant) Error() string {
	return i18n.T("script.parametre.manquant", strings.Join(e.Champs, ", "))
}

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
	vars, _ := params["variables"].(map[string]interface{})
	for k, v := range vars {
		variables[k] = v
	}
	// params["ia"] : appel proposé par l'IA (contrôle des champs obligatoires)
	depuisIA, _ := params["ia"].(bool)

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
		var manquants []string
		for nom, champ := range s.client.champsScript(app.EntityID) {
			if _, present := variables[nom]; champ.Requis && !present {
				manquants = append(manquants, nom)
			}
		}
		if len(manquants) > 0 {
			sort.Strings(manquants)
			return "", &ErreurParametreManquant{Champs: manquants}
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
		if estMotCleMessage(mot) {
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

// estMotCleMessage : « dire », « message », « annonce » — avec une faute de
// transcription tolérée (« annoce », « anonce »...).
func estMotCleMessage(mot string) bool {
	if mot == "dire" {
		return true
	}
	for _, kw := range []string{"message", "annonce"} {
		if text.DistanceLevenshtein(mot, kw) <= 1 {
			return true
		}
	}
	return false
}
