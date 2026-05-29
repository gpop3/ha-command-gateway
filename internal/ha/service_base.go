package ha

import (
	"fmt"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/pkg/types"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"ha-command-gateway/internal/utils/conversion"
)

// serviceBase fournit l'implémentation commune à tous les services.
type serviceBase struct {
	domaine string
	client  *Client
	verbes  map[string]VerbeConfig // "allume" → {Action: "turn_on", Params: [...]}
}

func newServiceBase(domaine string, client *Client, verbes map[string]VerbeConfig) serviceBase {
	return serviceBase{domaine: domaine, client: client, verbes: verbes}
}

func (b *serviceBase) Domaine() string { return b.domaine }

func (b *serviceBase) Actions() []string {
	seen := map[string]bool{}
	var actions []string
	for _, cfg := range b.verbes {
		if !seen[cfg.Action] {
			seen[cfg.Action] = true
			actions = append(actions, cfg.Action)
		}
	}
	sort.Strings(actions)
	return actions
}

func (b *serviceBase) Verbes() []string {
	verbes := make([]string, 0, len(b.verbes))
	for v := range b.verbes {
		verbes = append(verbes, v)
	}
	sort.Strings(verbes)
	return verbes
}

// MotsReconnus retourne par défaut les verbes français.
// Les services avec des params riches (couleurs, modes...) surchargent cette méthode.
func (b *serviceBase) MotsReconnus() []string {
	return b.Verbes()
}

func (b *serviceBase) Verbe(verbe string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(verbe))
	cfg, ok := b.verbes[v]
	return cfg.Action, ok
}

// VerbsAvecParams retourne les verbes qui ont des paramètres associés.
func (b *serviceBase) VerbsAvecParams() []VerbeConfig {
	var result []VerbeConfig
	for verbe, cfg := range b.verbes {
		if len(cfg.Params) > 0 {
			result = append(result, VerbeConfig{Action: verbe, Params: cfg.Params})
		}
	}
	return result
}

func (b *serviceBase) ScoreDomaine(_ bool) int { return 0 }

// EstActionParDefaut retourne false par défaut — les services HA standard
// ont une notion de lecture d'état. Les services non-HA surchargent à true.
func (b *serviceBase) EstActionParDefaut() bool { return false }

// ExtraireParams extrait les paramètres universels compris par tous les services :
//   - pourcentage : "80%", "quatre-vingts pour cent"
//   - temperature : "20 degrés", "vingt degrés"
//   - heure       : "à 14h30"
//
// Les services surchargent cette méthode pour ajouter leurs params spécifiques,
// en appelant d'abord b.ExtraireParams(texte) pour hériter des universels.
func (b *serviceBase) ExtraireParams(texte string) map[string]interface{} {
	params := map[string]interface{}{}

	// -- Pourcentage chiffré : "80%" ou "80 %"
	if re := regexp.MustCompile(`(\d{1,3})\s*%`); re.MatchString(texte) {
		m := re.FindStringSubmatch(texte)
		var pct int
		_, err := fmt.Sscanf(m[1], "%d", &pct)
		if err != nil {
			return nil
		}
		params["pourcentage"] = pct
		return params
	}

	// -- Pourcentage en lettres : "quatre-vingts pour cent"
	mots := strings.Fields(texte)
	for i, mot := range mots {
		if i+2 < len(mots) && mots[i+1] == "pour" && mots[i+2] == "cent" {
			if v, ok := conversion.LettreVersEntier(mot); ok {
				params["pourcentage"] = v
				return params
			}
		}
		if i+1 < len(mots) && mots[i+1] == "pourcentage" {
			if v, ok := conversion.LettreVersEntier(mot); ok {
				params["pourcentage"] = v
				return params
			}
		}
		if mot == "cent" && i > 0 {
			if v, ok := conversion.LettreVersEntier(mots[i-1]); ok {
				params["pourcentage"] = v
				return params
			}
		}
	}

	// -- Température chiffrée : "à 20 degrés" / "20°"
	if re := regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(?:degrés?|°)`); re.MatchString(texte) {
		m := re.FindStringSubmatch(texte)
		var temp float64
		_, err := fmt.Sscanf(strings.ReplaceAll(m[1], ",", "."), "%f", &temp)
		if err != nil {
			return nil
		}
		params["temperature"] = temp
		return params
	}

	// -- Température en lettres : "vingt degrés"
	for i, mot := range mots {
		if (mot == "degrés" || mot == "degré") && i > 0 {
			if v, ok := conversion.LettreVersEntier(mots[i-1]); ok {
				params["temperature"] = float64(v)
				return params
			}
		}
	}

	return params
}

// ExecuterCommande par défaut : résout le verbe et appelle Executer
func (b *serviceBase) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := b.Verbe(verbe)
	if !ok {
		for _, cfg := range b.verbes {
			action = cfg.Action
			break
		}
	}
	return b.appeler(app.EntityID, action, params)
}

func (b *serviceBase) appeler(entityID, action string, params map[string]interface{}) (string, error) {
	if params == nil {
		params = map[string]interface{}{}
	}

	// Construire target et data séparément pour WebSocket
	target := map[string]interface{}{}
	if entityID != "" {
		target["entity_id"] = entityID
	}

	// Utiliser WebSocket si disponible (plus rapide)
	if b.client.ws != nil {
		if err := b.client.ws.CallService(b.domaine, action, target, params); err != nil {
			log.Printf("⚠️ [WS] CallService échoué, fallback HTTP : %v", err)
			// Fallback HTTP
			goto httpFallback
		}
		return fmt.Sprintf("✅ [WS] [%s] %s → %s", b.domaine, entityID, action), nil
	}

httpFallback:
	if entityID != "" {
		params["entity_id"] = entityID
	}
	_, err := b.client.post(
		fmt.Sprintf("/api/services/%s/%s", b.domaine, action),
		params,
	)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✅ [%s] %s → %s", b.domaine, entityID, action), nil
}

func (b *serviceBase) RecupererEtat(app Appareil, dateCible time.Time, params map[string]interface{}) (*EtatComplet, any, error) {
	if !dateCible.IsZero() {
		etat, err := b.client.RecupererHistorique(app.EntityID, dateCible)
		return etat, nil, err
	}

	etat, err := b.client.RecupererEtatLive(app.EntityID)
	return etat, nil, err
}

func (b *serviceBase) EtatEnMessage(app Appareil, etat *EtatComplet, etatCustom any, dateCible time.Time) types.Message {
	if !dateCible.IsZero() {
		h := dateCible.Hour()
		m := dateCible.Minute()

		var heureVoix string
		if m == 0 {
			heureVoix = fmt.Sprintf("%d heure", h)
		} else {
			heureVoix = fmt.Sprintf("%d heure %d", h, m)
		}

		return types.Message{
			SMS: types.MessageDetails{
				Texte:  i18n.T("message.retour.etat.heure"),
				Params: []interface{}{app.FriendlyNameExact, dateCible.Format("15h04"), etat.State},
			},
			Voix: types.MessageDetails{
				Texte:  i18n.T("assistant.retour.etat.heure"),
				Params: []interface{}{app.FriendlyNameExact, heureVoix, etat.State},
			},
		}
	}
	return types.Message{
		SMS: types.MessageDetails{
			Texte:  i18n.T("message.retour.etat"),
			Params: []interface{}{app.FriendlyNameExact, etat.State},
		},
		Voix: types.MessageDetails{
			Texte:  i18n.T("assistant.retour.etat"),
			Params: []interface{}{app.FriendlyNameExact, etat.State},
		},
	}
}

func (b *serviceBase) AutoriseMotsSansEntites() bool {
	return false
}
