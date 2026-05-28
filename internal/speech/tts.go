package speech

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"ha-command-gateway/internal/i18n"
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

	fmt.Println("TTS prêt avec cache par composants.")
	return nil
}

// Parler prend la clé i18n, le pattern de traduction brute et ses arguments.
// Exemple : Parler("maison.temp.ligne", "• %s : %s°C\n", "Salon", "22.5")
// Parler s'adapte automatiquement :
// - Si cleEtTexte est une clé i18n (ex: "maison.temp.ligne"), elle utilise le pattern et les args.
// - Si cleEtTexte est une phrase brute (ex: "Attention, la voiture est ouverte"), elle gère le cache par son hash MD5.
func Parler(cleEtTexte string, args ...interface{}) {
	if !started {
		fmt.Println("TTS non initialisé")
		return
	}

	cleEtTexte = strings.ReplaceAll(cleEtTexte, "\n", " ")
	cleEtTexte = strings.TrimSpace(cleEtTexte)
	if cleEtTexte == "" {
		return
	}

	var pcmGlobal []byte

	if len(args) == 0 && (strings.Contains(cleEtTexte, " ") || !estUneCleI18n(cleEtTexte)) {
		// Le hash du texte brut sert d'ID unique de cache
		idCache := fmt.Sprintf("raw_%x", md5.Sum([]byte(cleEtTexte)))
		pcm, err := obtenirOuGenerer(idCache, cleEtTexte)
		if err != nil {
			fmt.Printf("⚠️ [TTS] Erreur texte brut : %v\n", err)
			return
		}
		pcmGlobal = pcm
	} else {
		// 2. CAS AVEC CLÉ I18N (Le code de découpage précédent)
		// Ici, cleEtTexte est considéré comme la CLÉ (ex: "maison.temp.ligne")
		// On va chercher sa traduction brute (le pattern) depuis ton package locales
		pattern := recupererPatternDepuisI18n(cleEtTexte)

		if len(args) == 0 {
			pcm, err := obtenirOuGenerer(cleEtTexte, pattern)
			if err != nil {
				fmt.Printf("⚠️ [TTS] Erreur : %v\n", err)
				return
			}
			pcmGlobal = pcm
		} else {
			format := pattern
			verbes := []string{"%v", "%s", "%d", "%.1f", "%.0f", "%02d"}
			for _, v := range verbes {
				format = strings.ReplaceAll(format, v, "||TAG||")
			}
			partiesStatiques := strings.Split(format, "||TAG||")

			for i, partie := range partiesStatiques {
				partie = strings.TrimSpace(partie)
				if partie != "" {
					idComposant := fmt.Sprintf("%s_part_%d", cleEtTexte, i)
					pcm, err := obtenirOuGenerer(idComposant, partie)
					if err == nil {
						pcmGlobal = append(pcmGlobal, pcm...)
					}
				}

				if i < len(args) {
					valeurStr := fmt.Sprintf("%v", args[i])
					valeurStr = strings.TrimSpace(valeurStr)
					if valeurStr != "" {
						idDyn := fmt.Sprintf("dyn_%x", md5.Sum([]byte(valeurStr)))
						pcm, err := obtenirOuGenerer(idDyn, valeurStr)
						if err == nil {
							pcmGlobal = append(pcmGlobal, pcm...)
						}
					}
				}
			}
		}
	}

	if len(pcmGlobal) > 0 {
		jouerPCM(pcmGlobal)
	}
}

// Permet de valider si la chaîne passée est une clé présente dans ton fichier de langue
func estUneCleI18n(cle string) bool {
	return i18n.Existe(cle)
}

// Récupère la vraie chaîne avec les %s depuis ton fichier de langue
func recupererPatternDepuisI18n(cle string) string {
	return i18n.GetPattern(cle)
}

// obtenirOuGenerer cherche le composant dans le cache via son ID unique, ou appelle Piper
func obtenirOuGenerer(idCache string, texte string) ([]byte, error) {
	path := filepath.Join(cacheDir, idCache+".pcm")

	// 1. Tente de lire depuis le cache disque
	if pcm, err := os.ReadFile(path); err == nil {
		return pcm, nil
	}

	// 2. Absent du cache : génération via Piper
	fmt.Printf("[TTS] Génération [%s] -> %s\n", idCache, texte)
	pcm, err := genererPCM(texte)
	if err != nil {
		return nil, err
	}

	// 3. Sauvegarde dans le cache de manière asynchrone pour ne pas bloquer le flux
	go func() {
		_ = os.WriteFile(path, pcm, 0644)
	}()

	return pcm, nil
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

// genererPCM appelle Piper HTTP et retourne le PCM brut (sans l'entête WAV de 44 octets)
type PiperRequest struct {
	Text string `json:"text"`
}

func genererPCM(texte string) ([]byte, error) {
	payload := PiperRequest{Text: texte}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("erreur json: %w", err)
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

// jouerPCM lance aplay pour jouer la totalité du flux assemblé
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
