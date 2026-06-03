package voice

import (
	"context"
	"fmt"
	"ha-command-gateway/internal/i18n"
	"strings"
	"time"

	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/core/adapters/stt"
	"ha-command-gateway/internal/core/adapters/tts"
	"ha-command-gateway/internal/input"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/nlp"
	"ha-command-gateway/internal/utils/text"
)

// Modes internes de la machine à états vocale (mot-clé de réveil).
const (
	modeVeille = iota
	modeCommand
	modeAttente
)

// Config regroupe les paramètres audio / transcription du service voix.
type Config struct {
	PiperUrl   string
	AlsaDevice string
	WindowsMic string

	TranscriptionMode string // "vosk" | "remote" | "local"
	WhisperURL        string
	WhisperBinPath    string
	WhisperModelPath  string
	WhisperVadModel   string
	VoskModelPath     string
}

// Service prend en charge le micro, la synthèse vocale (TTS) et la
// transcription (STT). Il POSSÈDE sa propre logique de traitement (réveil par
// mot-clé « assistant »), expose la parole via le port core.Speaker, et soumet
// ses traitements au bus pour qu'ils soient sérialisés avec ceux des autres
// services.
type Service struct {
	cfg       Config
	analyseur *nlp.Analyseur
	bus       *core.Bus

	// machine à états (manipulée uniquement sur la goroutine du bus)
	etat        int
	dernierMode time.Time

	tts      *tts.Client
	engine   *stt.Engine
	mode     stt.Mode
	stdout   interface{ Read([]byte) (int, error) }
	closer   interface{ Close() error }
	rec      *Recorder
	gram     string
	etatLoop int // pointeur passé aux boucles audio (valeur ignorée par le traitement)
}

// New crée le service voix. L'init lourde (TTS, STT, micro) a lieu dans Init.
func New(cfg Config, analyseur *nlp.Analyseur, bus *core.Bus) *Service {
	return &Service{cfg: cfg, analyseur: analyseur, bus: bus}
}

func (s *Service) Nom() string { return "voix" }

// Init monte le TTS, le moteur de transcription et ouvre le flux micro.
// Une erreur ici stoppe le boot (fail-fast).
func (s *Service) Init(ctx context.Context) error {
	client, err := tts.New(s.cfg.PiperUrl, s.cfg.AlsaDevice)
	if err != nil {
		return fmt.Errorf("%s : %w", i18n.T("erreur.init.tts"), err)
	}
	s.tts = client

	s.mode = resolveTranscriptMode(s.cfg.TranscriptionMode)
	engine, err := stt.New(stt.Config{
		Mode:         s.mode,
		WhisperURL:   s.cfg.WhisperURL,
		SystemPrompt: s.analyseur.GenererSystemPrompt(),
		BinPath:      s.cfg.WhisperBinPath,
		ModelPath:    s.cfg.WhisperModelPath,
		VadModel:     s.cfg.WhisperVadModel,
	})
	if err != nil {
		return fmt.Errorf("%s : %w", i18n.T("erreur.init.transcripteur"), err)
	}
	s.engine = engine

	flux, err := DemarrerFluxMicro(s.cfg.AlsaDevice, s.cfg.WindowsMic)
	if err != nil {
		return fmt.Errorf("%s : %w", i18n.T("erreur.demarrage.micro"), err)
	}
	s.stdout = flux
	s.closer = flux
	s.rec = NewRecorder(32000 * 5)
	s.gram = s.analyseur.GenererGrammaire()
	return nil
}

// Démarrer lance la boucle audio et relaie chaque transcription au bus, où elle
// sera traitée par la machine à états vocale.
func (s *Service) Démarrer(ctx context.Context) error {
	interne := make(chan input.Commande, 10)
	go BoucleAudio(s.stdout, s.rec, s.mode, s.engine, &s.etatLoop, interne, s.cfg.VoskModelPath, s.gram, s.tts.EstEnTrainDeParler)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case cmd := <-interne:
			texte := cmd.Texte
			s.bus.Soumettre(func() { s.traiter(texte) })
		case <-ticker.C:
			s.bus.Soumettre(s.verifierTimeout)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Fermer coupe le flux micro, ce qui fait sortir la boucle audio.
func (s *Service) Fermer(ctx context.Context) error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

// ---- Traitement vocal (réveil par mot-clé) ----

func (s *Service) traiter(inputText string) {
	if s.etat == modeAttente {
		return
	}
	etatActuel := s.etat
	s.etat = modeAttente
	texte := strings.ToLower(inputText)
	logx.InfoT("commande.vocale", texte)

	prochainEtat := modeVeille
	defer func() { s.etat = prochainEtat }()

	switch etatActuel {
	case modeVeille:
		mots := strings.Fields(texte)
		cle := i18n.T("nlp.mot.assistant")

		idxAssistant := -1
		for i, m := range mots {
			if text.DistanceLevenshtein(m, cle) <= 2 {
				idxAssistant = i
				break
			}
		}
		if idxAssistant == -1 {
			return
		}
		logx.InfoT("assistant.mot.cle")

		filtres := mots[idxAssistant+1:]

		match := false
		commande := strings.Join(filtres, " ")
		if len(commande) > 3 {
			match = s.executer(commande, true)
		}
		switch {
		case !match:
			s.Bip()
			s.dernierMode = time.Now()
			prochainEtat = modeCommand
		case s.analyseur.AttenteDeChoix("voix"):
			s.dernierMode = time.Now()
			prochainEtat = modeCommand
		}

	case modeCommand:
		if len(texte) <= 3 {
			s.dernierMode = time.Now()
			prochainEtat = modeCommand
			return
		}
		s.executer(inputText, false)
		if s.analyseur.AttenteDeChoix("voix") {
			s.dernierMode = time.Now()
			prochainEtat = modeCommand
		}
	}
}

// executer analyse la commande et restitue la réponse à la voix.
func (s *Service) executer(inputText string, muteEnCasDerreur bool) bool {
	reponse, verbe, match, isAction, appareil := s.analyseur.AnalyserEtExecuter("voix", inputText)
	switch {
	case appareil == nil || reponse == nil:
		if match {
			s.Parler("assistant.retour.erreur")
		} else {
			if !muteEnCasDerreur {
				s.Parler("assistant.retour.pas.compris")
			}
		}
	case isAction:
		s.Parler("assistant.retour.action", verbe, appareil.FriendlyName)
	default:
		s.Parler(reponse.Voix.Texte, reponse.Voix.Params...)
	}
	return match
}

// verifierTimeout repasse en veille si la fenêtre de commande a expiré.
func (s *Service) verifierTimeout() {
	if s.etat == modeCommand && time.Since(s.dernierMode) > 10*time.Second {
		logx.InfoT("assistant.timeout")
		s.etat = modeVeille
	}
}

// ---- core.Speaker ----

func (s *Service) Parler(cle string, args ...any) {
	if s.tts != nil {
		s.tts.Parler(cle, args...)
	}
}

func (s *Service) Bip() {
	if s.tts != nil {
		s.tts.Bip()
	}
}

// resolveTranscriptMode choisit le mode de transcription selon la config.
func resolveTranscriptMode(mode string) stt.Mode {
	switch stt.Mode(mode) {
	case stt.ModeRemote:
		logx.InfoT("transcription.remote")
		return stt.ModeRemote
	case stt.ModeLocal:
		logx.InfoT("transcription.local")
		return stt.ModeLocal
	default:
		logx.InfoT("transcription.vosk")
		return stt.ModeVosk
	}
}
