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

	// Texte brut sans args
	if len(args) == 0 && (strings.Contains(cleEtTexte, " ") || !estUneCleI18n(cleEtTexte)) {
		id := fmt.Sprintf("raw_%x", md5.Sum([]byte(cleEtTexte)))
		ajouter(id, cleEtTexte)
		return composants
	}

	pattern := recupererPatternDepuisI18n(cleEtTexte)

	// Clé i18n sans args
	if len(args) == 0 {
		ajouter(cleEtTexte, pattern)
		return composants
	}

	// Clé i18n avec args — découpage en parties statiques + dynamiques
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

// Parler prend la clé i18n, le pattern de traduction brute et ses arguments.
// Exemple : Parler("maison.temp.ligne", "• %s : %s°C\n", "Salon", "22.5")
// Parler s'adapte automatiquement :
// - Si cleEtTexte est une clé i18n (ex: "maison.temp.ligne"), elle utilise le pattern et les args.
// - Si cleEtTexte est une phrase brute (ex: "Attention, la voiture est ouverte"), elle gère le cache par son hash MD5.
func Parler(cleEtTexte string, args ...interface{}) {
	if !started {
		return
	}

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

	// Précharge le composant à l'index donné dans un chan
	precharger := func(idx int) chan resultat {
		ch := make(chan resultat, 1)
		go func() {
			pcm, err := obtenirOuGenerer(composants[idx].id, composants[idx].texte)
			ch <- resultat{pcm, err}
		}()
		return ch
	}

	// On démarre le chargement du premier composant
	prochainChan := precharger(0)

	for i := 0; i < len(composants); i++ {
		// Précharge le suivant pendant qu'on attend le courant
		var suivantChan chan resultat
		if i+1 < len(composants) {
			suivantChan = precharger(i + 1)
		}

		// Attend et joue le courant
		res := <-prochainChan
		if res.err == nil && len(res.pcm) > 0 {
			jouerPCM(res.pcm)
		}

		prochainChan = suivantChan
	}
}

// contientDuTexte permet de vérifier si le texte contient quelque chose
func contientDuTexte(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
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

	if pcm, err := os.ReadFile(path); err == nil {
		return pcm, nil
	}

	fmt.Printf("[TTS] Génération [%s] -> %s\n", idCache, texte)
	pcm, err := genererPCM(texte)
	if err != nil {
		return nil, err
	}

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
