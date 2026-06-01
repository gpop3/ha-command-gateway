package stt

import (
	"bytes"
	"fmt"
	"ha-command-gateway/internal/logx"
	"os"
	"os/exec"
	"strings"
	"time"
)

// moteurLocal appelle whisper.cpp via un sous-processus
type moteurLocal struct {
	binPath   string
	modelPath string
	vadModel  string
}

func (m *moteurLocal) Transcribe(wavData *bytes.Buffer) (string, time.Duration, error) {
	start := time.Now()

	// Écriture du buffer WAV dans un fichier temporaire
	tmpFile := "/tmp/assistant_audio.wav"
	if err := os.WriteFile(tmpFile, wavData.Bytes(), 0644); err != nil {
		return "", 0, fmt.Errorf("local: écriture fichier tmp : %w", err)
	}

	domoPrompt := "Assistant, allume, éteins, lumière, cuisine, salon, chambre, garage, température, chauffage, stop, musique, ok."

	args := []string{
		"-m", m.modelPath,
		"-f", tmpFile,
		"-t", "2", // threads
		"-bs", "1", // beam size
		"-l", "fr", // langue
		"-tp", "0", // temperature
		"-nt", // no timestamps
		"-np", // no progress
		"--vad",
		"-vm", m.vadModel,
		"-vt", "0.3", // vad threshold
		"-vp", "500", // vad padding
		"--prompt", domoPrompt,
	}

	cmd := exec.Command(m.binPath, args...)
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		return "", 0, fmt.Errorf("local: whisper.cpp a échoué : %w\nSortie: %s", err, string(output))
	}

	logx.InfoT("stt.transcription.locale.terminee.duration", duration)
	return strings.TrimSpace(string(output)), duration, nil
}
