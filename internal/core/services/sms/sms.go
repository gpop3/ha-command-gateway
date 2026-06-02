package sms

import (
	"context"
	"fmt"
	"strings"

	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/core/adapters/modem"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/nlp"
)

// Config regroupe les paramètres de connexion au modem TCL.
type Config struct {
	ModemURL  string
	Password  string
	VerifKey  string
	XorKey    string
	FreeKey   string
	HmacKey   string
	Whitelist string
}

// Service écoute les SMS entrants et permet d'en envoyer.
type Service struct {
	cfg       Config
	analyseur *nlp.Analyseur
	bus       *core.Bus

	client *modem.Client
}

// New crée le service SMS. La connexion au modem a lieu dans Init.
func New(cfg Config, analyseur *nlp.Analyseur, bus *core.Bus) *Service {
	return &Service{cfg: cfg, analyseur: analyseur, bus: bus}
}

func (s *Service) Nom() string { return "sms" }

// Init connecte le modem
func (s *Service) Init(ctx context.Context) error {
	client, err := modem.New(
		s.cfg.ModemURL, s.cfg.Password, s.cfg.VerifKey,
		s.cfg.XorKey, s.cfg.FreeKey, s.cfg.HmacKey, s.cfg.Whitelist,
	)
	if err != nil {
		logx.WarnT("sms.modem.indispo", err)
		return nil
	}
	s.client = client
	return nil
}

// Démarrer écoute les SMS entrants et soumet leur traitement au bus.
func (s *Service) Démarrer(ctx context.Context) error {
	if s.client == nil {
		<-ctx.Done()
		return ctx.Err()
	}

	smsChan := make(chan modem.SMS, 10)
	go s.client.EcouterSMS(smsChan)

	for {
		select {
		case m := <-smsChan:
			numero, message := m.Numero, m.Message
			s.bus.Soumettre(func() { s.traiter(numero, message) })
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// traiter analyse le SMS reçu et renvoie la réponse par SMS à l'expéditeur.
func (s *Service) traiter(numero, message string) {
	if len(strings.TrimSpace(message)) <= 3 {
		return
	}

	reponse, _, _, isAction, _ := s.analyseur.AnalyserEtExecuter(message)
	if reponse == nil {
		logx.InfoT("sms.traitement.sms.impossible.analyse")
		return
	}

	var texte string
	switch {
	case isAction:
		texte = reponse.SMS.Texte
	case i18n.Existe(reponse.SMS.Texte):
		texte = i18n.T(reponse.SMS.Texte, reponse.SMS.Params...)
	default:
		texte = fmt.Sprintf(reponse.SMS.Texte, reponse.SMS.Params...)
	}

	logx.InfoT("sms.envoi", texte)
	if err := s.Envoyer(numero, texte); err != nil {
		logx.ErrorT("sms.envoi.sms.echoue", err)
	}
}

// Envoyer implémente core.SMSSender.
func (s *Service) Envoyer(numero, message string) error {
	if s.client == nil {
		return fmt.Errorf("%s", i18n.T("erreur.modem.indispo"))
	}
	return s.client.EnvoyerSMS(numero, message)
}
