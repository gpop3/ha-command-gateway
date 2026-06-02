package console

import (
	"bufio"
	"context"
	"os"
	"strings"

	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/nlp"
)

// Service lit les lignes tapées au clavier et soumet leur traitement au bus.
type Service struct {
	analyseur *nlp.Analyseur
	speaker   core.Speaker
	bus       *core.Bus
}

// New crée le service console.
func New(analyseur *nlp.Analyseur, speaker core.Speaker, bus *core.Bus) *Service {
	return &Service{analyseur: analyseur, speaker: speaker, bus: bus}
}

func (s *Service) Nom() string { return "console" }

func (s *Service) Démarrer(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	logx.InfoT("console.prete")

	for {
		texte, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		texte = strings.TrimSpace(texte)
		if texte == "" {
			continue
		}

		cmd := texte
		s.bus.Soumettre(func() { s.traiter(cmd) })
	}
}

// traiter analyse la commande et restitue la réponse.
func (s *Service) traiter(inputText string) {
	reponse, verbe, match, isAction, appareil := s.analyseur.AnalyserEtExecuter("console", inputText)
	logx.InfoT("console.reponse", reponse)

	if appareil == nil || reponse == nil {
		if match {
			s.speaker.Parler("assistant.retour.erreur")
		} else {
			s.speaker.Parler("assistant.retour.pas.compris")
		}
	} else if isAction {
		s.speaker.Parler("assistant.retour.action", verbe, appareil.FriendlyName)
	} else {
		s.speaker.Parler(reponse.Voix.Texte, reponse.Voix.Params...)
	}
}
