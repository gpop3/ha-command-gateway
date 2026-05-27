//go:build windows || nvosk

package voice

import (
	"fmt"

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
		fmt.Println("⚠️  Vosk non disponible sur Windows → mode remote")
	}
	BoucleDetectionParole(stdout, recorder, engine, etat, canal)
}
