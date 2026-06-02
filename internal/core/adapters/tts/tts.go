package tts

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
	"sync/atomic"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
)

const cacheDir = "/tmp/tts-cache"

// Client encapsule la connexion à Piper, le périphérique ALSA de sortie et le
// cache PCM par composant.
type Client struct {
	mu           sync.Mutex
	alsaDev      string
	piperURL     string
	http         *http.Client
	estEnLecture atomic.Bool
}

// New construit un client TTS prêt à l'emploi.
func New(piperURL, alsaDevice string) (*Client, error) {
	c := &Client{
		alsaDev:      alsaDevice,
		piperURL:     piperURL,
		http:         &http.Client{Timeout: 30 * time.Second},
		estEnLecture: atomic.Bool{},
	}
	_ = os.MkdirAll(cacheDir, 0755)
	logx.InfoT("tts.pret")
	return c, nil
}

func (c *Client) EstEnTrainDeParler() bool {
	return c.estEnLecture.Load()
}

type composant struct {
	id    string
	texte string
}

func construireComposants(cleEtTexte string, args ...interface{}) []composant {
	var composants []composant

	ajouter := func(id, texte string) {
		texte = strings.TrimSpace(texte)
		if texte != "" && contientDuTexte(texte) {
			composants = append(composants, composant{id: id, texte: texte})
		}
	}

	if len(args) == 0 && (strings.Contains(cleEtTexte, " ") || !estUneCleI18n(cleEtTexte)) {
		id := fmt.Sprintf("raw_%x", md5.Sum([]byte(cleEtTexte)))
		ajouter(id, cleEtTexte)
		return composants
	}

	pattern := recupererPatternDepuisI18n(cleEtTexte)

	if len(args) == 0 {
		ajouter(cleEtTexte, pattern)
		return composants
	}

	format := pattern
	verbes := []string{"%v", "%s", "%d", "%.1f", "%.0f", "%02d"}
	for _, v := range verbes {
		format = strings.ReplaceAll(format, v, "||TAG||")
	}
	partiesStatiques := strings.Split(format, "||TAG||")

	for i, partie := range partiesStatiques {
		partie = strings.TrimSpace(partie)
		if partie != "" && contientDuTexte(partie) {
			ajouter(fmt.Sprintf("%s_part_%d", cleEtTexte, i), partie)
		}
		if i < len(args) {
			valeurStr := strings.TrimSpace(fmt.Sprintf("%v", args[i]))
			if valeurStr != "" && contientDuTexte(valeurStr) {
				ajouter(fmt.Sprintf("dyn_%x", md5.Sum([]byte(valeurStr))), valeurStr)
			}
		}
	}

	return composants
}

// Parler prend la clé i18n (ou une phrase brute) et ses arguments, puis lit le
// résultat audio. Exemple : Parler("maison.temp.ligne", "Salon", "22.5").
func (c *Client) Parler(cleEtTexte string, args ...interface{}) {
	cleEtTexte = strings.ReplaceAll(cleEtTexte, "\n", " ")
	cleEtTexte = strings.TrimSpace(cleEtTexte)
	if cleEtTexte == "" {
		return
	}

	composants := construireComposants(cleEtTexte, args...)
	if len(composants) == 0 {
		return
	}

	type resultat struct {
		pcm []byte
		err error
	}

	c.estEnLecture.Store(true)
	// Passera à false à la fin de la méthode
	defer c.estEnLecture.Store(false)

	precharger := func(idx int) chan resultat {
		ch := make(chan resultat, 1)
		go func() {
			pcm, err := c.obtenirOuGenerer(composants[idx].id, composants[idx].texte)
			ch <- resultat{pcm, err}
		}()
		return ch
	}

	prochainChan := precharger(0)
	for i := 0; i < len(composants); i++ {
		var suivantChan chan resultat
		if i+1 < len(composants) {
			suivantChan = precharger(i + 1)
		}
		res := <-prochainChan
		if res.err == nil && len(res.pcm) > 0 {
			c.jouerPCM(res.pcm)
		}
		prochainChan = suivantChan
	}
}

func contientDuTexte(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}

func estUneCleI18n(cle string) bool { return i18n.Existe(cle) }

func recupererPatternDepuisI18n(cle string) string { return i18n.GetPattern(cle) }

func (c *Client) obtenirOuGenerer(idCache string, texte string) ([]byte, error) {
	path := filepath.Join(cacheDir, idCache+".pcm")
	if pcm, err := os.ReadFile(path); err == nil {
		return pcm, nil
	}

	logx.InfoT("tts.tts.generation", idCache, texte)
	pcm, err := c.genererPCM(texte)
	if err != nil {
		return nil, err
	}

	go func() {
		_ = os.WriteFile(path, pcm, 0644)
	}()

	return pcm, nil
}

// Bip joue un bip sonore court.
func (c *Client) Bip() {
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
	c.jouerPCM(append(buf, silence...))
}

// PiperRequest est le corps JSON envoyé à Piper.
type PiperRequest struct {
	Text string `json:"text"`
}

func (c *Client) genererPCM(texte string) ([]byte, error) {
	payload := PiperRequest{Text: texte}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("erreur.tts.json"), err)
	}

	resp, err := c.http.Post(c.piperURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("%s : %w", i18n.T("erreur.tts.piper"), err)
	}
	defer func(Body io.ReadCloser) {
		if err := Body.Close(); err != nil {
			logx.Error(err)
		}
	}(resp.Body)

	wav, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", i18n.T("erreur.tts.wav"), err)
	}

	if len(wav) > 44 && string(wav[:4]) == "RIFF" {
		return wav[44:], nil
	}
	return wav, nil
}

func (c *Client) jouerPCM(pcm []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	aplay := exec.Command("aplay",
		"-D", c.alsaDev,
		"-r", "22050",
		"-f", "S16_LE",
		"-c", "1",
		"-t", "raw",
		"-",
	)
	aplay.Stdin = bytes.NewReader(pcm)
	aplay.Stderr = os.Stderr

	if err := aplay.Run(); err != nil {
		logx.WarnT("tts.tts.aplay", err)
	}
}
