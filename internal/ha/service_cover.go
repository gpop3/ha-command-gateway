package ha

import (
	"ha-command-gateway/internal/i18n"
)

// ServiceCover gère le domaine "cover"
type ServiceCover struct{ serviceBase }

func NewServiceCover(c *Client) *ServiceCover {
	return &ServiceCover{newServiceBase("cover", c, map[string]string{
		"ouvre":  "open_cover",
		"ferme":  "close_cover",
		"stoppe": "stop_cover",
		"arrête": "stop_cover",
		"stop":   "stop_cover",
		"baisse": "close_cover",
		"monte":  "open_cover",
	})}
}

func (s *ServiceCover) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
}

func (s *ServiceCover) ScoreDomaine(estAction bool) int {
	if estAction {
		return 40
	}
	return 20
}

// ExtraireParams hérite des universels (pourcentage) — pas de params spécifiques supplémentaires
func (s *ServiceCover) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}

func (s *ServiceCover) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	if pct, ok := params["pourcentage"].(int); ok {
		if _, err := s.appeler(app.EntityID, "set_cover_position", map[string]interface{}{"position": pct}); err != nil {
			return i18n.T("cover.position.erreur", app.FriendlyName), err
		}
		return i18n.T("cover.position.ok", app.FriendlyName, pct), nil
	}

	action, ok := s.Verbe(verbe)
	if !ok {
		action = "toggle"
	}
	return s.appeler(app.EntityID, action, nil)
}
