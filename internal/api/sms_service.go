package api

import (
	"fmt"
	"ha-command-gateway/internal/sms"
)

// SMSService gère la logique métier d'envoi de SMS
type SMSService struct {
	client *sms.Client
}

// NewSMSService crée un nouveau service SMS
func NewSMSService(client *sms.Client) *SMSService {
	return &SMSService{client: client}
}

// EnvoyerSMS envoie un SMS après validation
func (s *SMSService) EnvoyerSMS(numero, message string) error {
	if s.client == nil {
		return fmt.Errorf("modem SMS non disponible")
	}
	if numero == "" {
		return fmt.Errorf("numéro de téléphone requis")
	}
	if message == "" {
		return fmt.Errorf("message requis")
	}
	if len(message) > 160 {
		return fmt.Errorf("message trop long (%d caractères, max 160)", len(message))
	}
	return s.client.EnvoyerSMS(numero, message)
}
