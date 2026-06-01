//go:build windows || nvosk

package voice

import (
	"fmt"

	"ha-command-gateway/internal/core/adapters/stt"
	"ha-command-gateway/internal/input"
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
) {
	if mode == stt.ModeVosk {
		fmt.Println("⚠️  Vosk non disponible sur Windows → mode remote")
	}
	BoucleDetectionParole(stdout, recorder, engine, etat, canal)
}
