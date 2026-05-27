//go:build !windows && !nvosk

package voice

import (
	"fmt"
	"log"

	vosk "github.com/alphacep/vosk-api/go"

	"ha-command-gateway/internal/input"
	"ha-command-gateway/internal/transcribe"
)

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
		boucleVosk(stdout, recorder, etat, canal, voskModelPath, grammaireJSON)
	} else {
		BoucleDetectionParole(stdout, recorder, engine, etat, canal)
	}
}

func boucleVosk(
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

	rec.SetPartialWords(70)
	rec.SetWords(1)
	rec.SetMaxAlternatives(1)
	rec.SetGrm(grammaireJSON)

	fmt.Println("🎙️  Vosk prêt.")

	buf := make([]byte, 4096)
	for {
		n, _ := stdout.Read(buf)
		if n > 0 {
			recorder.Write(buf[:n])
			if rec.AcceptWaveform(buf[:n]) == 1 {
				texte := ExtraireTexteVosk(rec.Result())
				canal <- input.Commande{Texte: texte, Etat: etat}
			}
		}
	}
}
