//go:build !windows && !nvosk

package voice

import (
	"encoding/json"
	"fmt"
	"io"
	"log"

	"strings"

	vosk "github.com/alphacep/vosk-api/go"
	"golang.org/x/text/unicode/norm"

	"ha-command-gateway/internal/input"
	"ha-command-gateway/internal/transcribe"
)

const (
	SeuilConfianceMin = 100.0
	EcartMinSecurite  = 5.0
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
	mode transcribe.Mode,
	engine *transcribe.Engine,
	etat *int,
	canal chan<- input.Commande,
	voskModelPath string,
	grammaireJSON string,
) {
	if mode == transcribe.ModeVosk {
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
	log.Printf("DEBUG confidence brute : %f", meilleur.Confidence)
	if meilleur.Text == "" || meilleur.Confidence < SeuilConfianceMin {
		log.Printf("🚫 [Rejeté] Confiance trop faible (%d) pour : %q",
			int(meilleur.Confidence), meilleur.Text)
		return meilleur, false
	}

	for i := 1; i < len(res.Alternatives); i++ {
		autre := res.Alternatives[i]
		ecart := meilleur.Confidence - autre.Confidence

		if ecart < EcartMinSecurite && normaliser(meilleur.Text) != normaliser(autre.Text) {
			log.Printf("🧠 [Rejeté] Hésitation trop forte entre le choix principal %q (%d) et l'alternative #%d %q (%d)",
				meilleur.Text, int(meilleur.Confidence),
				i+1, autre.Text, int(autre.Confidence))
			return meilleur, false
		}
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

	for {
		n, err := stdout.Read(buf)

		if n > 0 {
			if rec.AcceptWaveform(buf[:n]) == 1 {
				raw := rec.Result()
				log.Printf("DEBUG résultat complet : %s", raw)

				var res VoskResultMultiple
				if jsonErr := json.Unmarshal([]byte(raw), &res); jsonErr != nil {
					log.Printf("⚠️ Erreur JSON Vosk : %v", jsonErr)
					continue
				}

				if cmd, ok := commandeEstFiable(res); ok {
					log.Printf("✅ [Validé] Commande envoyée : %q (%d)",
						cmd.Text, int(cmd.Confidence))

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

func normaliser(s string) string {
	t := norm.NFD.String(s)
	result := make([]rune, 0, len(t))
	for _, r := range t {
		if r < 0x0300 || r > 0x036f {
			result = append(result, r)
		}
	}
	return strings.ToLower(string(result))
}
