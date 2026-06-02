//go:build windows || nvosk

package voice

import (
	"ha-command-gateway/internal/core/adapters/stt"
	"ha-command-gateway/internal/input"
	"ha-command-gateway/internal/logx"
)

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
		logx.WarnT("vosk.windows")
	}
	BoucleDetectionParole(stdout, recorder, engine, etat, canal, EstEnTrainDeParlerFunc)
}
