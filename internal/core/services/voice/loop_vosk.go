//go:build !windows && !nvosk

package voice

import (
	"encoding/json"
	"ha-command-gateway/internal/i18n"
	"io"

	"ha-command-gateway/internal/core/adapters/stt"
	"ha-command-gateway/internal/input"

	"ha-command-gateway/internal/logx"

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
	EstEnTrainDeParlerFunc func() bool,
) {
	if mode == stt.ModeVosk {
		initVosk(stdout, recorder, etat, canal, voskModelPath, grammaireJSON, EstEnTrainDeParlerFunc)
	} else {
		BoucleDetectionParole(stdout, recorder, engine, etat, canal, EstEnTrainDeParlerFunc)
	}
}

func initVosk(
	stdout interface{ Read([]byte) (int, error) },
	recorder *Recorder,
	etat *int,
	canal chan<- input.Commande,
	voskModelPath string,
	grammaireJSON string,
	EstEnTrainDeParlerFunc func() bool,
) {
	model, err := vosk.NewModel(voskModelPath)
	if err != nil {
		logx.Fatalf("%s", i18n.T("erreur.vosk.chargement.modele", err))
	}
	defer model.Free()

	rec, err := vosk.NewRecognizer(model, 16000.0)
	if err != nil {
		logx.Fatalf("%s", i18n.T("erreur.vosk.init.recognizer", err))
	}
	defer rec.Free()

	rec.SetMaxAlternatives(5)
	rec.SetWords(1)
	rec.SetGrm(grammaireJSON)

	logx.InfoT("audio.vosk.pret")
	BoucleVosk(stdout, rec, canal, etat, EstEnTrainDeParlerFunc)
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
		logx.InfoT("audio.vosk.confiance.trop.faible", int(meilleur.Confidence), meilleur.Text)
		return meilleur, false
	}

	return meilleur, true
}

func BoucleVosk(
	stdout interface{ Read([]byte) (int, error) },
	rec *vosk.VoskRecognizer,
	canal chan<- input.Commande,
	etat *int,
	EstEnTrainDeParlerFunc func() bool,
) {
	buf := make([]byte, 2024)
	framessilence := 0
	const maxFramesSilence = 50
	wasTalking := false

	for {
		n, err := stdout.Read(buf)

		if n > 0 {
			if EstEnTrainDeParlerFunc() {
				wasTalking = true
				framessilence = 0
				continue
			}

			if wasTalking {
				rec.Reset()
				wasTalking = false
				continue
			}

			if EstSilence(buf[:n], 50) {
				framessilence++
				if framessilence == maxFramesSilence {
					logx.DebugT("vosk.pause")
				}
				if framessilence >= maxFramesSilence {
					continue
				}
			} else {
				if framessilence >= maxFramesSilence {
					logx.DebugT("vosk.reprend")
				}
				framessilence = 0
			}

			if rec.AcceptWaveform(buf[:n]) == 1 {
				raw := rec.Result()

				var res VoskResultMultiple
				if jsonErr := json.Unmarshal([]byte(raw), &res); jsonErr != nil {
					logx.WarnT("audio.erreur.json.vosk", jsonErr)
					continue
				}

				if cmd, ok := commandeEstFiable(res); ok {
					logx.debug("audio.vosk.text.compris", cmd.Text)

					canal <- input.Commande{
						Texte: cmd.Text,
						Etat:  etat,
					}
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				logx.WarnT("audio.fin.du.stream.audio", err)
			} else {
				logx.InfoT("vosk.stream.fin")
			}
			break
		}
	}
}
