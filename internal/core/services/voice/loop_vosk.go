//go:build !windows && !nvosk

package voice

import (
	"encoding/json"
	"fmt"
	"io"
	"log"

	"ha-command-gateway/internal/core/adapters/stt"
	"ha-command-gateway/internal/input"

	vosk "github.com/alphacep/vosk-api/go"
)

const (
	SeuilConfianceMin = 70.0
)

type VoskAlternative struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

type VoskResultMultiple struct {
	Alternatives []VoskAlternative `json:"alternatives"`
}

func BoucleAudio(
	stdout interface{ Read([]byte) (int, error) },
	recorder *Recorder,
	mode stt.Mode,
	engine *stt.Engine,
	etat *int,
	canal chan<- input.Commande,
	voskModelPath string,
	grammaireJSON string,
) {
	if mode == stt.ModeVosk {
		initVosk(stdout, recorder, etat, canal, voskModelPath, grammaireJSON)
	} else {
		BoucleDetectionParole(stdout, recorder, engine, etat, canal)
	}
}

func initVosk(
	stdout interface{ Read([]byte) (int, error) },
	recorder *Recorder,
	etat *int,
	canal chan<- input.Commande,
	voskModelPath string,
	grammaireJSON string,
) {
	model, err := vosk.NewModel(voskModelPath)
	if err != nil {
		log.Fatalf("Vosk: erreur chargement modèle : %v", err)
	}
	defer model.Free()

	rec, err := vosk.NewRecognizer(model, 16000.0)
	if err != nil {
		log.Fatalf("Vosk: erreur init recognizer : %v", err)
	}
	defer rec.Free()

	rec.SetMaxAlternatives(5)
	rec.SetWords(1)
	rec.SetGrm(grammaireJSON)

	fmt.Println("🎙️  Vosk prêt.")
	BoucleVosk(stdout, rec, canal, etat)
}

func commandeEstFiable(res VoskResultMultiple) (VoskAlternative, bool) {
	if len(res.Alternatives) == 0 {
		return VoskAlternative{}, false
	}

	meilleur := res.Alternatives[0]

	if meilleur.Text == "" {
		return meilleur, false
	}

	if meilleur.Confidence < SeuilConfianceMin {
		log.Printf("[VOSK] Confiance trop faible (%d) pour : %q", int(meilleur.Confidence), meilleur.Text)
		return meilleur, false
	}

	return meilleur, true
}

func BoucleVosk(
	stdout interface{ Read([]byte) (int, error) },
	rec *vosk.VoskRecognizer,
	canal chan<- input.Commande,
	etat *int,
) {
	buf := make([]byte, 4096)
	framessilence := 0
	const maxFramesSilence = 50

	for {
		n, err := stdout.Read(buf)

		if n > 0 {
			if EstSilence(buf[:n], 50) {
				framessilence++
				if framessilence == maxFramesSilence {
					log.Printf("🔇 Silence détecté — Vosk en pause")
				}
				if framessilence >= maxFramesSilence {
					continue
				}
			} else {
				if framessilence >= maxFramesSilence {
					log.Printf("🎤 Son détecté — Vosk reprend")
				}
				framessilence = 0
			}

			if rec.AcceptWaveform(buf[:n]) == 1 {
				raw := rec.Result()

				var res VoskResultMultiple
				if jsonErr := json.Unmarshal([]byte(raw), &res); jsonErr != nil {
					log.Printf("⚠️ Erreur JSON Vosk : %v", jsonErr)
					continue
				}

				if cmd, ok := commandeEstFiable(res); ok {
					canal <- input.Commande{
						Texte: cmd.Text,
						Etat:  etat,
					}
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				log.Printf("⚠️ Fin du stream audio avec erreur : %v", err)
			} else {
				log.Println("ℹ️ Fin du stream audio (EOF).")
			}
			break
		}
	}
}
