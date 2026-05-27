package ha

import (
	"fmt"
	"ha-command-gateway/internal/i18n"
)

// ServiceNotify gère le domaine "notify".
// cible : n'importe quel service notify HA (string libre ou constante CibleNotify)
type ServiceNotify struct{ serviceBase }

// CibleNotify est un alias string — utilisation optionnelle pour les cibles connues
type CibleNotify = string

var notifyDevice = ""

func NewServiceNotify(c *Client, NotifyDevice string) *ServiceNotify {
	notifyDevice = NotifyDevice
	return &ServiceNotify{newServiceBase("notify", c, map[string]string{
		"envoie":   "send_message",
		"notifie":  "send_message",
		"annonce":  "send_message",
		"préviens": "send_message",
		"dis":      "send_message",
		"alerte":   "send_message",
	})}
}

// Executer implémente Service — entityID = nom de la cible notify
func (s *ServiceNotify) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	msg, _ := params["message"].(string)
	titre, _ := params["title"].(string)
	return s.envoyer(entityID, msg, titre)
}

// ExecuterCommande envoie une notification via le params "message"
func (s *ServiceNotify) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	msg, _ := params["message"].(string)
	titre, _ := params["title"].(string)
	cible := app.EntityID
	if cible == "" {
		cible = notifyDevice
	}
	return s.envoyer(cible, msg, titre)
}

// envoyer est l'implémentation interne commune
func (s *ServiceNotify) envoyer(cible, message, titre string) (string, error) {
	payload := map[string]interface{}{"message": message}
	if titre != "" {
		payload["title"] = titre
	}
	_, err := s.client.post(fmt.Sprintf("/api/services/notify/%s", cible), payload)
	if err != nil {
		return "", fmt.Errorf("%s", i18n.T("notify.erreur", cible, err))
	}
	return i18n.T("notify.ok", cible), nil
}

// EnvoyerPlusieurs envoie le même message à plusieurs cibles
func (s *ServiceNotify) EnvoyerPlusieurs(cibles []string, message, titre string) error {
	for _, cible := range cibles {
		if _, err := s.envoyer(cible, message, titre); err != nil {
			return err
		}
	}
	return nil
}

// ExtraireParams délègue aux paramètres universels (pourcentage, température)
func (s *ServiceNotify) ExtraireParams(texte string) map[string]interface{} {
	return s.serviceBase.ExtraireParams(texte)
}
