package sms

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/core/adapters/modem"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/nlp"
)

// Service écoute les SMS entrants et permet d'en envoyer.
type Service struct {
	cfg       modem.Config
	analyseur *nlp.Analyseur
	bus       *core.Bus

	client atomic.Pointer[modem.Client]
}

// delaiReessai : intervalle entre deux tentatives quand le modem est absent au démarrage.
const delaiReessai = 30 * time.Second

// New crée le service SMS. La connexion au modem a lieu dans Init.
func New(cfg modem.Config, analyseur *nlp.Analyseur, bus *core.Bus) *Service {
	return &Service{cfg: cfg, analyseur: analyseur, bus: bus}
}

func (s *Service) Nom() string { return "sms" }

// Init connecte le modem. Un échec n'est pas bloquant : Démarrer réessaiera.
func (s *Service) Init(ctx context.Context) error {
	client, err := modem.New(s.cfg)
	if err != nil {
		logx.WarnT("sms.modem.indispo", err)
		return nil
	}
	s.client.Store(client)
	return nil
}

// Démarrer écoute les SMS entrants et soumet leur traitement au bus.
// Si le modem n'était pas prêt (HAT encore en démarrage), il réessaie régulièrement.
func (s *Service) Démarrer(ctx context.Context) error {
	for s.client.Load() == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delaiReessai):
		}
		client, err := modem.New(s.cfg)
		if err != nil {
			logx.DebugT("sms.modem.indispo", err)
			continue
		}
		s.client.Store(client)
		logx.InfoT("modem.reconnecte")
	}

	smsChan := make(chan modem.SMS, 10)
	go s.client.Load().EcouterSMS(ctx, smsChan)

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

	reponse, _, _, isAction, _ := s.analyseur.AnalyserEtExecuter(numero, message)
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

// Fermer libère le port série du modem (core.Fermable).
func (s *Service) Fermer(ctx context.Context) error {
	if c := s.client.Load(); c != nil {
		c.Fermer()
	}
	return nil
}

// Envoyer implémente core.SMSSender.
func (s *Service) Envoyer(numero, message string) error {
	c := s.client.Load()
	if c == nil {
		return fmt.Errorf("%s", i18n.T("erreur.modem.indispo"))
	}
	return c.EnvoyerSMS(numero, message)
}
