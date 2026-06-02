package api

import (
	"fmt"
	"ha-command-gateway/internal/i18n"

	"ha-command-gateway/internal/core"
)

// SMSService applique la validation métier avant de déléguer l'envoi au port
type SMSService struct {
	sender core.SMSSender
}

// NewSMSService crée le service de validation d'envoi SMS.
func NewSMSService(sender core.SMSSender) *SMSService {
	return &SMSService{sender: sender}
}

// EnvoyerSMS valide puis envoie.
func (s *SMSService) EnvoyerSMS(numero, message string) error {
	if s.sender == nil {
		return fmt.Errorf("%s", i18n.T("erreur.modem.indispo"))
	}
	if numero == "" {
		return fmt.Errorf("%s", i18n.T("erreur.numero.requis"))
	}
	if message == "" {
		return fmt.Errorf("%s", i18n.T("erreur.message.requis"))
	}
	if len(message) > 160 {
		return fmt.Errorf("%s", i18n.T("erreur.message.trop.long", len(message)))
	}
	return s.sender.Envoyer(numero, message)
}
