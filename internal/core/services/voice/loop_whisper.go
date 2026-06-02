package voice

import (
	"time"

	"ha-command-gateway/internal/core/adapters/stt"
	"ha-command-gateway/internal/input"
	"ha-command-gateway/internal/logx"
)

func BoucleDetectionParole(
	stdout interface{ Read([]byte) (int, error) },
	recorder *Recorder,
	engine *stt.Engine,
	etat *int,
	canal chan<- input.Commande,
	EstEnTrainDeParlerFunc func() bool,
) {
	const (
		SeuilSilenceMax = 5
		SeuilParoleMax  = 25
		tailleFenetre   = 6400
	)

	compteurSilence := 0
	compteurParole := 0

	go func() {
		buf := make([]byte, 4096)
		for {
			n, _ := stdout.Read(buf)
			if n > 0 {
				recorder.Write(buf[:n])
			}
		}
	}()

	for {
		time.Sleep(200 * time.Millisecond)
		data := recorder.GetRawBytes()
		doitEnvoyer := false

		if EstEnTrainDeParlerFunc() {
			recorder.Clear()
			continue
		}

		if len(data) >= tailleFenetre {
			fenetre := data[len(data)-tailleFenetre:]

			if EstSilence(fenetre, 15) {
				if compteurParole > 0 {
					compteurSilence++
					if compteurSilence >= SeuilSilenceMax {
						logx.InfoT("audio.silence")
						doitEnvoyer = true
					}
				}
			} else {
				compteurParole++
				if compteurParole == 1 {
					recorder.ClearNotAll(12800)
					logx.InfoT("audio.parole")
				}
				if compteurParole >= SeuilParoleMax {
					logx.InfoT("audio.timeout")
					doitEnvoyer = true
				}
			}

			if (compteurSilence+compteurParole) >= SeuilParoleMax && !doitEnvoyer {
				doitEnvoyer = true
			}
		}

		if doitEnvoyer && len(data) > 0 {
			wavData := recorder.GetWavBytes()
			recorder.Clear()
			compteurSilence = 0
			compteurParole = 0

			texte, dur, err := engine.Transcribe(wavData)
			if err != nil {
				logx.ErrorT("audio.erreur", err)
				continue
			}
			logx.InfoT("audio.entendu", dur.Round(time.Millisecond), texte)
			if texte != "" {
				canal <- input.Commande{Texte: texte, Etat: etat}
			}
		}
	}
}
