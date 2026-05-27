package transcribe

import (
	"bytes"
	"encoding/json"
	"time"
)

// Mode de transcription disponibles
type Mode string

const (
	ModeVosk   Mode = "vosk"   // Vosk local temps réel (Linux uniquement, via AcceptWaveform)
	ModeRemote Mode = "remote" // Faster-Whisper distant (Raspberry Pi ou autre endpoint HTTP)
	ModeLocal  Mode = "local"  // whisper.cpp en binaire local
)

// Moteur est l'interface commune à tous les moteurs de transcription.
// Pour ajouter un nouveau moteur (ex: Google STT, Azure), il suffit
// d'implémenter cette interface et de l'enregistrer dans New().
type Moteur interface {
	Transcribe(wavData *bytes.Buffer) (texte string, duree time.Duration, err error)
}

// Config regroupe tous les paramètres possibles selon le moteur choisi
type Config struct {
	Mode Mode

	// ModeRemote
	WhisperURL   string
	SystemPrompt string // hotwords / contexte injecté dans le prompt

	// ModeLocal
	BinPath   string
	ModelPath string
	VadModel  string
}

// Engine est le transcripteur principal : il délègue au bon moteur
type Engine struct {
	moteur Moteur
}

// New construit un Engine avec le moteur correspondant au mode choisi.
// Retourne une erreur si le mode est inconnu.
func New(cfg Config) (*Engine, error) {
	var m Moteur

	switch cfg.Mode {
	case ModeRemote:
		m = &moteurRemote{
			url:          cfg.WhisperURL,
			systemPrompt: cfg.SystemPrompt,
		}
	case ModeLocal:
		m = &moteurLocal{
			binPath:   cfg.BinPath,
			modelPath: cfg.ModelPath,
			vadModel:  cfg.VadModel,
		}
	case ModeVosk:
		// Vosk ne passe pas par Engine.Transcribe() — il utilise AcceptWaveform
		// directement dans la boucle audio. On retourne quand même un Engine valide
		// pour ne pas casser l'API appelante.
		m = &moteurVosk{}
	default:
		return nil, &ErrModeInconnu{Mode: string(cfg.Mode)}
	}

	return &Engine{moteur: m}, nil
}

// Transcribe délègue au moteur sous-jacent
func (e *Engine) Transcribe(wavData *bytes.Buffer) (string, time.Duration, error) {
	return e.moteur.Transcribe(wavData)
}

// SetSystemPrompt met à jour le prompt à chaud (si le moteur le supporte)
func (e *Engine) SetSystemPrompt(prompt string) {
	if r, ok := e.moteur.(*moteurRemote); ok {
		r.systemPrompt = prompt
	}
}

// ---- Vosk helpers (utilisés dans la boucle audio de main.go) ----

// ExtraireTexteVosk parse la réponse JSON de Vosk et retourne le texte reconnu
func ExtraireTexteVosk(jsonBrut string) string {
	var result struct {
		Text         string `json:"text"`
		Alternatives []struct {
			Text string `json:"text"`
		} `json:"alternatives"`
	}
	if err := json.Unmarshal([]byte(jsonBrut), &result); err != nil {
		return ""
	}
	if result.Text != "" {
		return result.Text
	}
	if len(result.Alternatives) > 0 {
		return result.Alternatives[0].Text
	}
	return ""
}

// ---- Moteur Vosk (stub — transcription gérée dans main.go) ----

type moteurVosk struct{}

func (v *moteurVosk) Transcribe(wavData *bytes.Buffer) (string, time.Duration, error) {
	return "", 0, &ErrModeVosk{}
}

// ---- Erreurs ----

type ErrModeInconnu struct{ Mode string }
func (e *ErrModeInconnu) Error() string {
	return "mode de transcription inconnu : " + e.Mode
}

type ErrModeVosk struct{}
func (e *ErrModeVosk) Error() string {
	return "Vosk : la transcription passe par AcceptWaveform dans la boucle audio, pas par Engine.Transcribe()"
}
