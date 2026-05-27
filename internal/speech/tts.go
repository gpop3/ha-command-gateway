package speech

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sync"
)

const fifoPath = "/tmp/piper-input"

var (
	mu       sync.Mutex
	started  bool
	aplayInG io.WriteCloser
)

func Init(piperBin, piperModel, alsaDevice string) error {
	_ = os.Remove(fifoPath)
	if err := exec.Command("mkfifo", fifoPath).Run(); err != nil {
		return fmt.Errorf("mkfifo: %w", err)
	}

	// Demarrer aplay UNE SEULE FOIS
	aplay := exec.Command("aplay",
		"-D", alsaDevice,
		"-r", "22050",
		"-f", "S16_LE",
		"-c", "1",
		"-t", "raw",
		"-",
	)
	aplayIn, err := aplay.StdinPipe()
	if err != nil {
		return fmt.Errorf("aplay stdin: %w", err)
	}
	aplay.Stderr = os.Stderr
	if err := aplay.Start(); err != nil {
		return fmt.Errorf("aplay start: %w", err)
	}
	aplayInG = aplayIn

	// Demarrer Piper qui lit le FIFO
	piper := exec.Command("sh", "-c",
		fmt.Sprintf("tail -f %s | %s --model %s --output-raw", fifoPath, piperBin, piperModel),
	)
	piperOut, err := piper.StdoutPipe()
	if err != nil {
		return fmt.Errorf("piper stdout: %w", err)
	}
	piper.Stderr = os.Stderr
	if err := piper.Start(); err != nil {
		return fmt.Errorf("piper start: %w", err)
	}

	// Goroutine : sortie Piper -> aplayInG
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := piperOut.Read(buf)
			if n > 0 {
				_, err := aplayInG.Write(buf[:n])
				if err != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
	}()

	started = true
	fmt.Println("TTS pret.")
	return nil
}

func Bip() {
	if !started {
		return
	}
	mu.Lock()
	defer mu.Unlock()

	const sampleRate = 22050
	samples := int(math.Round(float64(sampleRate) * 0.15))
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		t := float64(i) / sampleRate
		env := 1.0
		fadeLen := samples / 10
		if i < fadeLen {
			env = float64(i) / float64(fadeLen)
		} else if i > samples-fadeLen {
			env = float64(samples-i) / float64(fadeLen)
		}
		val := int16(12000 * env * math.Sin(2*math.Pi*880.0*t))
		buf[i*2] = byte(val)
		buf[i*2+1] = byte(val >> 8)
	}
	_, errBuf := aplayInG.Write(buf)
	if errBuf != nil {
		return
	}

	silence := make([]byte, 22050*2) // 1 seconde de silence
	_, errSilence := aplayInG.Write(silence)
	if errSilence != nil {
		return
	}
}

func Parler(texte string) {
	if !started {
		fmt.Println("TTS non initialise")
		return
	}
	mu.Lock()
	defer mu.Unlock()

	f, err := os.OpenFile(fifoPath, os.O_WRONLY, os.ModeNamedPipe)
	if err != nil {
		fmt.Printf("Erreur ouverture FIFO : %v\n", err)
		return
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			fmt.Printf("Erreur fermeture FIFO : %v\n", err)
		}
	}(f)

	_, _ = fmt.Fprintln(f, texte)
}
