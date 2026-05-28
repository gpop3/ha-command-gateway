package speech

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

var (
	mu      sync.Mutex
	started bool
	piperIn io.WriteCloser
	aplayIn io.WriteCloser
)

func Init(piperBin, piperModel, alsaDevice string) error {
	aplay := exec.Command("aplay",
		"-D", alsaDevice,
		"-r", "22050",
		"-f", "S16_LE",
		"-c", "1",
		"--buffer-size=4096",
		"-t", "raw",
		"-",
	)
	aplayInPipe, err := aplay.StdinPipe()
	if err != nil {
		return fmt.Errorf("aplay stdin: %w", err)
	}
	aplay.Stderr = os.Stderr
	if err := aplay.Start(); err != nil {
		return fmt.Errorf("aplay start: %w", err)
	}
	aplayIn = aplayInPipe

	// Démarrer Piper avec stdin/stdout pipes
	piper := exec.Command(piperBin, "--model", piperModel, "--output-raw")
	piperInPipe, err := piper.StdinPipe()
	if err != nil {
		return fmt.Errorf("piper stdin: %w", err)
	}
	piperOut, err := piper.StdoutPipe()
	if err != nil {
		return fmt.Errorf("piper stdout: %w", err)
	}
	piper.Stderr = os.Stderr
	if err := piper.Start(); err != nil {
		return fmt.Errorf("piper start: %w", err)
	}
	piperIn = piperInPipe

	// Goroutine : sortie Piper → aplay stdin
	go func() {
		if _, err := io.Copy(aplayIn, piperOut); err != nil {
			fmt.Printf("⚠️ [TTS] pipe piper→aplay : %v\n", err)
		}
	}()

	started = true
	fmt.Println("TTS pret.")
	return nil
}

// Bip joue un bip sonore court
func Bip() {
	if !started {
		return
	}
	mu.Lock()
	defer mu.Unlock()

	const sampleRate = 22050
	samples := int(math.Round(float64(sampleRate) * 0.15))
	buf := make([]byte, samples*2)
	fadeLen := samples / 10

	for i := 0; i < samples; i++ {
		t := float64(i) / sampleRate
		env := 1.0
		if i < fadeLen {
			env = float64(i) / float64(fadeLen)
		} else if i > samples-fadeLen {
			env = float64(samples-i) / float64(fadeLen)
		}
		val := int16(12000 * env * math.Sin(2*math.Pi*880.0*t))
		buf[i*2] = byte(val)
		buf[i*2+1] = byte(val >> 8)
	}

	if _, err := aplayIn.Write(buf); err != nil {
		return
	}
	silence := make([]byte, sampleRate*2)
	_, _ = aplayIn.Write(silence)
}

// nettoyerPourTTS nettoyage anti debug (provisoire)
func nettoyerPourTTS(texte string) string {
	// Supprimer emojis et caractères spéciaux
	texte = regexp.MustCompile(`[^\p{L}\p{N}\s.,!?;:'-]`).ReplaceAllString(texte, "")
	return strings.TrimSpace(texte)
}

// Parler envoie le texte à Piper via stdin
func Parler(texte string) {
	texte = nettoyerPourTTS(texte)
	if !started {
		fmt.Println("TTS non initialise")
		return
	}
	mu.Lock()
	defer mu.Unlock()

	fmt.Printf("[TTS] message : %v\n", texte)
	if _, err := fmt.Fprintln(piperIn, texte); err != nil {
		fmt.Printf("⚠️ [TTS] erreur écriture piper : %v\n", err)
	}
}
