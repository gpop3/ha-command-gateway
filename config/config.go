package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config contient tous les paramètres configurables de l'assistant
type Config struct {
	// Langue (ex: "fr", "en")
	Lang string

	// Home Assistant
	HAUrl    string
	HAToken  string
	HAPieces string

	// Raspberry Pi / Whisper
	RaspberryPiIP string
	WhisperURL    string // si vide, utilise RaspberryPiIP

	// Transcription
	TranscriptionMode string // "remote", "local", "vosk"

	// Transcription locale (whisper.cpp)
	WhisperBinPath   string
	WhisperModelPath string
	WhisperVadModel  string

	// vosk
	VoskModelPath string

	// Audio
	AlsaDevice string // ex: "plughw:3,0"
	WindowsMic string // ex: "Microphone (Realtek(R) Audio)"

	// GSM
	GSMPort string // ex: "/dev/ttyUSB0"
	GSMBaud int
	GSMPin  string

	// piper
	PiperBin   string
	PiperModel string

	// notify
	NotifyDevice string

	// Modem TCL LinkKey IK41
	ModemURL      string // ex: http://192.168.1.1
	ModemPassword string // mot de passe interface web
	ModemVerifKey string // _TclRequestVerificationKey header
	ModemXorKey   string // clé de chiffrement XOR
	ModemFreeKey  string // clé AES pour les APIs free (freeApiKey)
	ModemHmacKey  string // clé HMAC-SHA256
	Whitelist     string

	// API HTTP
	APIPort int
	APIKey  string
}

// Load charge la config depuis les variables d'environnement, avec des valeurs par défaut
func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("Note: aucun fichier .env trouvé, utilisation des variables système")
	}

	c := &Config{
		// Langue
		Lang: getEnv("LANG", "fr"),

		// Home Assistant
		HAUrl:    getEnv("HA_URL", "http://localhost:8123"),
		HAToken:  getEnv("HA_TOKEN", ""),
		HAPieces: getEnv("MES_PIECES", ""),

		// Whisper / Raspberry Pi
		RaspberryPiIP: getEnv("RASPBERRY_PI_IP", "localhost"),
		WhisperURL:    getEnv("WHISPER_URL", ""), // si vide, construit depuis RaspberryPiIP

		// Mode de transcription : "remote" | "local" | "vosk"
		// - "vosk"   : Vosk local (Linux uniquement)
		// - "remote" : Whisper sur le Raspberry Pi (endpoint HTTP)
		// - "local"  : whisper.cpp en local (binaire)
		TranscriptionMode: getEnv("TRANSCRIPTION_MODE", "vosk"),

		// Modèle vosk
		VoskModelPath: getEnv("VOSK_MODEL_PATH", ""),

		// Transcription locale whisper.cpp
		WhisperBinPath:   getEnv("WHISPER_BIN", ""),
		WhisperModelPath: getEnv("WHISPER_MODEL", ""),
		WhisperVadModel:  getEnv("WHISPER_VAD_MODEL", ""),

		// Audio
		AlsaDevice: getEnv("ALSA_DEVICE", "plughw:CARD=Bar,DEV=0"),
		WindowsMic: getEnv("WINDOWS_MIC", "Microphone (Realtek(R) Audio)"),

		// GSM
		GSMPort: getEnv("GSM_PORT", "/dev/ttyUSB0"),
		GSMBaud: 9600,
		GSMPin:  getEnv("GSM_PIN", "1234"),

		// Piper
		PiperBin:   getEnv("PIPER_BIN", ""),
		PiperModel: getEnv("PIPER_MODEL", ""),

		// Notify
		NotifyDevice: getEnv("NOTIFY_DEVICE", ""),

		// Model
		ModemURL:      getEnv("MODEM_URL", "http://192.168.1.1"),
		ModemPassword: getEnv("MODEM_PASSWORD", ""),
		ModemVerifKey: getEnv("MODEM_VERIF_KEY", ""),
		ModemXorKey:   getEnv("MODEM_XOR_KEY", ""),
		ModemFreeKey:  getEnv("MODEM_FREE_KEY", ""),
		ModemHmacKey:  getEnv("MODEM_HMAC_KEY", ""),
		Whitelist:     getEnv("WHITELIST", ""),

		// HTTP Client
		APIPort: getEnvInt("API_PORT", 8080),
		APIKey:  getEnv("API_KEY", ""),
	}

	// Construction automatique du whisperURL si non fourni
	if c.WhisperURL == "" {
		c.WhisperURL = "http://" + c.RaspberryPiIP + ":8000/v1/audio/transcriptions"
	}

	return c
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		var i int
		if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
			return i
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
