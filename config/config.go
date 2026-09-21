package config

import (
	"fmt"
	"os"
	"strings"

	"ha-command-gateway/internal/logx"

	"github.com/joho/godotenv"
)

// Config contient tous les paramètres configurables de l'assistant
type Config struct {
	// Langue (ex: "fr", "en")
	Lang string

	// Home Assistant
	HAUrl       string
	HAToken     string
	HAPieces    string
	HAWebsocket bool
	HATimeout   int

	// services
	ServicesFile string

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
	PiperUrl   string
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

	// Natural Language Processing (NLP)
	ActivePreselection bool

	// Désambiguïsation
	DesambiguisationActive   bool
	DesambiguisationSeuil    int
	DesambiguisationMaxChoix int

	// Pondération du scoring NLP
	ScoreMinimal               int
	ScoreBonusPiece            int
	ScoreBonusMot              int
	ScoreBonusFuzzy            int
	ScoreMalusPieceSeule       int
	ScoreBonusLieuFonction     int
	ScoreBonusCouvertureExacte int
	ScoreMalusMotSuperflu      int
	ScoreMalusActionSansCible  int

	// Features
	ActiveSms        bool
	ActiveVoice      bool
	ActiveServerHttp bool
	ActiveConsole    bool

	// Gemini IA
	GeminiActive  bool
	GeminiPrimary bool
	GeminiModel   string
	GeminiAPIKey  string
	GeminiDebug   bool // journalise le contexte envoyé à l'IA et sa réponse brute (niveau INFO)

	// Réduction du contexte envoyé à l'IA
	GeminiPreselection bool // false = tout le contexte HA est envoyé à chaque appel
	GeminiContexteMax  int  // nombre d'entités retenues par le scoring

	// Mémoire de conversation (par session)
	GeminiMemoireTours    int // 0 = pas de mémoire
	GeminiMemoireSecondes int

	// Garde-fous
	IAConfirmation     bool   // confirmation orale avant un SMS ou une automatisation
	IANumerosAutorises string // numéros que l'IA peut viser (défaut : WHITELIST)

	// Quotas locaux et robustesse (0 = illimité)
	GeminiMaxRequetesMinute int
	GeminiMaxRequetesJour   int
	GeminiMaxTokensMinute   int
	GeminiDelaiMinMs        int  // délai minimal entre deux appels
	GeminiSecondeChance     bool // rappeler l'IA une fois quand le code rejette sa réponse
	GeminiAnalyse           bool // analyse en deux appels (l'IA commente les données lues par le code)
	IAConfirmationGroupe    int  // confirmation au-delà de ce nombre d'actions d'un coup (0 = jamais)
	TarifKWh                float64 // prix du kWh (€) pour estimer un coût ; 0 = pas de coût

	// Journal des décisions, apprentissage, mode ombre
	DecisionsFile string // JSONL des échanges (vide = mémoire seulement)
	DecisionsMaxMo   int // taille maximale du journal en Mo avant rotation (0 = jamais)
	DecisionsAnciens int // nombre d'anciennes versions conservées
	NLPApprisFile string // base des phrases apprises par l'IA (vide = mémoire seulement)
	IAOmbre       bool   // mode ombre : l'autre moteur dit ce qu'il aurait fait

	// Notification sur le téléphone et publication dans Home Assistant
	NotifyService      string // service notify.* de l'application mobile (vide = détection auto)
	HAPublish          bool   // publier capteurs et événements dans HA
	HAPublishIntervalS int    // republication des capteurs (secondes)
	HAEventName        string // type de l'événement publié à chaque commande
	HAPublishPhrase    bool   // inclure la phrase dite dans l'événement

	// Recherche de recettes par ingrédients (API de Mealie)
	MealieURL   string // ex. http://maison.local:9925
	MealieToken string // jeton d'API Mealie (profil utilisateur)

	// Briefing à la demande
	BriefingCalendrierRepas string // mot-clé des calendriers de repas (défaut : mealie ; vide = pas de section repas)
}

// Load charge la config depuis les variables d'environnement, avec des valeurs par défaut
func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		logx.InfoT("config.note.aucun.fichier.env")
	}

	c := &Config{
		// Langue
		Lang: getEnv("LANG", "fr"),

		// Home Assistant
		HAUrl:       getEnv("HA_URL", "http://localhost:8123"),
		HAToken:     getEnv("HA_TOKEN", ""),
		HAPieces:    getEnv("MES_PIECES", ""),
		HAWebsocket: getEnv("HA_WEBSOCKET", "true") == "true",
		HATimeout:   getEnvInt("HA_TIMEOUT", 10),

		// Gestion service
		ServicesFile: getEnv("SERVICES_FILE", "services.yaml"),

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
		PiperUrl:   getEnv("PIPER_URL", "http://localhost:5000"),

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

		// NLP
		ActivePreselection: getEnv("ACTIVE_PRESELECTION", "true") == "true",

		// Désambiguïsation
		DesambiguisationActive:   getEnv("DESAMBIGUISATION_ACTIVE", "true") == "true",
		DesambiguisationSeuil:    getEnvInt("DESAMBIGUISATION_SEUIL", 5),
		DesambiguisationMaxChoix: getEnvInt("DESAMBIGUISATION_MAX_CHOIX", 3),

		// Scoring NLP
		ScoreMinimal:               getEnvInt("SCORE_MINIMAL", 30),
		ScoreBonusPiece:            getEnvInt("SCORE_BONUS_PIECE", 100),
		ScoreBonusMot:              getEnvInt("SCORE_BONUS_MOT", 20),
		ScoreBonusFuzzy:            getEnvInt("SCORE_BONUS_FUZZY", 15),
		ScoreMalusPieceSeule:       getEnvInt("SCORE_MALUS_PIECE_SEULE", 80),
		ScoreBonusLieuFonction:     getEnvInt("SCORE_BONUS_LIEU_FONCTION", 60),
		ScoreBonusCouvertureExacte: getEnvInt("SCORE_BONUS_COUVERTURE_EXACTE", 10),
		ScoreMalusMotSuperflu:      getEnvInt("SCORE_MALUS_MOT_SUPERFLU", 2),
		ScoreMalusActionSansCible:  getEnvInt("SCORE_MALUS_ACTION_SANS_CIBLE", 50),

		// Features
		ActiveSms:        getEnv("ACTIVE_SMS", "true") == "true",
		ActiveVoice:      getEnv("ACTIVE_VOICE", "true") == "true",
		ActiveServerHttp: getEnv("ACTIVE_SERVER_HTTP", "true") == "true",
		ActiveConsole:    getEnv("ACTIVE_CONSOLE", "true") == "true",

		// Gemini
		GeminiActive:  getEnv("GEMINI_ACTIVE", "false") == "true",
		GeminiPrimary: getEnv("GEMINI_PRIMARY", "false") == "true",
		GeminiModel:   getEnv("GEMINI_MODEL", "gemini-3.1-flash-lite"),
		GeminiAPIKey:  getEnv("GEMINI_API_KEY", ""),
		GeminiDebug:   getEnv("GEMINI_DEBUG", "false") == "true",

		GeminiPreselection:    getEnv("GEMINI_PRESELECTION", "true") == "true",
		GeminiContexteMax:     getEnvInt("GEMINI_CONTEXT_MAX", 40),
		GeminiMemoireTours:    getEnvInt("GEMINI_MEMOIRE_TOURS", 3),
		GeminiMemoireSecondes: getEnvInt("GEMINI_MEMOIRE_SECONDES", 180),
		IAConfirmation:        getEnv("IA_CONFIRMATION", "true") == "true",
		IANumerosAutorises:    getEnv("IA_NUMEROS_AUTORISES", ""),

		GeminiMaxRequetesMinute: getEnvInt("GEMINI_MAX_REQUETES_MINUTE", 0),
		GeminiMaxRequetesJour:   getEnvInt("GEMINI_MAX_REQUETES_JOUR", 0),
		GeminiMaxTokensMinute:   getEnvInt("GEMINI_MAX_TOKENS_MINUTE", 0),
		GeminiDelaiMinMs:        getEnvInt("GEMINI_DELAI_MIN_MS", 2000),
		GeminiSecondeChance:     getEnv("GEMINI_SECONDE_CHANCE", "true") == "true",
		GeminiAnalyse:           getEnv("GEMINI_ANALYSE", "true") == "true",
		IAConfirmationGroupe:    getEnvInt("IA_CONFIRMATION_GROUPE", 5),
		TarifKWh:                getEnvFloat("TARIF_KWH", 0),

		DecisionsFile: getEnv("DECISIONS_FILE", "data/decisions.jsonl"),
		DecisionsMaxMo:   getEnvInt("DECISIONS_MAX_MO", 5),
		DecisionsAnciens: getEnvInt("DECISIONS_ANCIENS", 3),
		NLPApprisFile: getEnv("NLP_APPRIS_FILE", "data/nlp_appris.json"),
		IAOmbre:       getEnv("IA_OMBRE", "false") == "true",

		NotifyService:      getEnv("NOTIFY_SERVICE", ""),
		HAPublish:          getEnv("HA_PUBLISH", "true") == "true",
		HAPublishIntervalS: getEnvInt("HA_PUBLISH_INTERVAL_S", 60),
		HAEventName:        getEnv("HA_EVENT_NAME", "ha_command_gateway_command"),
		HAPublishPhrase:    getEnv("HA_PUBLISH_PHRASE", "true") == "true",
		MealieURL:          getEnv("MEALIE_URL", ""),
		MealieToken:        getEnv("MEALIE_TOKEN", ""),
		BriefingCalendrierRepas: getEnv("BRIEFING_CALENDRIER_REPAS", "mealie"),
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

func getEnvFloat(key string, fallback float64) float64 {
	if val := os.Getenv(key); val != "" {
		var f float64
		if _, err := fmt.Sscanf(strings.ReplaceAll(val, ",", "."), "%f", &f); err == nil {
			return f
		}
	}
	return fallback
}
