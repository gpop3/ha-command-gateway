package speech

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const cacheDir = "/tmp/tts-cache"

var (
	mu         sync.Mutex
	started    bool
	alsaDev    string
	piperURL   string
	httpClient = &http.Client{Timeout: 30 * time.Second}
)

// Init configure le TTS
func Init(piperURL_, alsaDevice string) error {
	piperURL = piperURL_
	alsaDev = alsaDevice
	started = true

	_ = os.MkdirAll(cacheDir, 0755)

	fmt.Println("TTS prêt.")
	return nil
}

// Parler synthétise le texte et le joue
func Parler(texte string) {
	if !started {
		fmt.Println("TTS non initialisé")
		return
	}

	texte = strings.ReplaceAll(texte, "\n", " ")
	texte = strings.TrimSpace(texte)
	if texte == "" {
		return
	}

	fmt.Printf("[TTS] message : %s\n", texte)

	path := cheminCache(texte)
	pcm, err := os.ReadFile(path)
	if err != nil {
		pcm, err = genererPCM(texte)
		if err != nil {
			fmt.Printf("⚠️ [TTS] erreur Piper : %v\n", err)
			return
		}
		go func() { _ = os.WriteFile(path, pcm, 0644) }()
	}

	jouerPCM(pcm)
}

// Bip joue un bip sonore court
func Bip() {
	if !started {
		return
	}
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

	silence := make([]byte, sampleRate/5*2)
	jouerPCM(append(buf, silence...))
}

// ---- Fonctions internes ----

// cheminCache retourne le chemin du fichier cache pour un texte donné
func cheminCache(texte string) string {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(texte)))
	return filepath.Join(cacheDir, hash+".pcm")
}

type PiperRequest struct {
	Text string `json:"text"`
}

// genererPCM appelle Piper HTTP et retourne le PCM brut
func genererPCM(texte string) ([]byte, error) {
	payload := PiperRequest{Text: texte}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("erreur lors de l'envoi : %w", err)
	}

	resp, err := httpClient.Post(piperURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("piper HTTP : %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	wav, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("lecture WAV : %w", err)
	}

	if len(wav) > 44 && string(wav[:4]) == "RIFF" {
		return wav[44:], nil
	}
	return wav, nil
}

// jouerPCM lance aplay, joue le PCM et attend la fin
func jouerPCM(pcm []byte) {
	mu.Lock()
	defer mu.Unlock()

	aplay := exec.Command("aplay",
		"-D", alsaDev,
		"-r", "22050",
		"-f", "S16_LE",
		"-c", "1",
		"-t", "raw",
		"-",
	)
	aplay.Stdin = bytes.NewReader(pcm)
	aplay.Stderr = os.Stderr

	if err := aplay.Run(); err != nil {
		fmt.Printf("⚠️ [TTS] aplay : %v\n", err)
	}
}
