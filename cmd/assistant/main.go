package main

import (
	"fmt"
	"ha-command-gateway/internal/api"
	"ha-command-gateway/internal/console"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/voice"
	"log"
	"strings"
	"time"

	"ha-command-gateway/config"
	"ha-command-gateway/internal/ha"
	_ "ha-command-gateway/internal/i18n/locales"
	"ha-command-gateway/internal/input"
	"ha-command-gateway/internal/nlp"
	"ha-command-gateway/internal/sms"
	"ha-command-gateway/internal/speech"
	"ha-command-gateway/internal/transcribe"
	"ha-command-gateway/internal/utils/text"
)

const (
	ModeVeille = iota
	ModeCommand
	ModeSmsCommand
)

var dernierModeCommand time.Time

func main() {
	var gsmClientPtr *sms.Client

	cfg := config.Load()
	i18n.SetLocale(cfg.Lang)

	haClient := ha.NewClient(cfg.HAUrl, cfg.HAToken, cfg.HAPieces, time.Duration(cfg.HATimeout), cfg)
	haClient.AttendreWS()

	analyseur := nlp.New(haClient, cfg.ActivePreselection)

	if err := analyseur.RafraichirCatalogue(); err != nil {
		log.Fatalf("%s", i18n.T("erreur.ha.connexion", err))
	}
	fmt.Println(i18n.T("assistant.catalogue"))

	etatSms := ModeSmsCommand
	etat := ModeVeille
	canalCommandes := make(chan input.Commande, 10)

	if cfg.ActiveVoice {
		// TTS
		if err := speech.Init(cfg.PiperUrl, cfg.AlsaDevice); err != nil {
			log.Fatalf("Erreur init TTS : %v", err)
		}

		// Moteur de transcription
		transcriptMode := resolveTranscriptMode(cfg)
		engine, err := transcribe.New(transcribe.Config{
			Mode:         transcriptMode,
			WhisperURL:   cfg.WhisperURL,
			SystemPrompt: analyseur.GenererSystemPrompt(),
			BinPath:      cfg.WhisperBinPath,
			ModelPath:    cfg.WhisperModelPath,
			VadModel:     cfg.WhisperVadModel,
		})
		if err != nil {
			log.Fatalf("Erreur init transcripteur : %v", err)
		}

		// Flux audio
		stdout, err := voice.DemarrerFluxMicro(cfg.AlsaDevice, cfg.WindowsMic)
		if err != nil {
			log.Fatalf("Erreur démarrage micro : %v", err)
		}
		recorder := voice.NewRecorder(32000 * 5)

		// Boucle audio
		grammaireJSON := analyseur.GenererGrammaire()
		go voice.BoucleAudio(stdout, recorder, transcriptMode, engine, &etat, canalCommandes, cfg.VoskModelPath, grammaireJSON)
	}

	if cfg.ActiveSms {
		gsmClient, err := sms.New(cfg.ModemURL, cfg.ModemPassword, cfg.ModemVerifKey, cfg.ModemXorKey, cfg.ModemFreeKey, cfg.ModemHmacKey, cfg.Whitelist)
		if err != nil {
			log.Printf("⚠️ Modem non disponible : %v", err)
		} else {
			gsmClientPtr = gsmClient
			smsChan := make(chan sms.SMS, 10)
			go gsmClient.EcouterSMS(smsChan)
			go func() {
				for s := range smsChan {
					canalCommandes <- input.Commande{
						Texte:     s.Message,
						Etat:      &etatSms,
						NumeroTel: s.Numero,
					}
				}
			}()
		}
	}

	if cfg.ActiveServerHttp {
		server := api.NewServer(cfg.APIPort)

		if cfg.ActiveSms {
			smsService := api.NewSMSService(gsmClientPtr)
			server.Register(api.NewSMSController(smsService, cfg.APIKey))
		}
		server.Start()
	}

	if cfg.ActiveConsole {
		go func() {
			for {
				text := console.EcouterConsole()
				canalCommandes <- input.Commande{
					Texte:     text,
					Etat:      &etatSms,
					NumeroTel: "",
				}
			}
		}()
	}

	fmt.Println("🚀 Assistant prêt (Voix + SMS + Console).")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case commande := <-canalCommandes:
			if commande.Texte != "" {
				traiterEtat(commande.Texte, commande.Etat, commande.NumeroTel, analyseur, gsmClientPtr)
			}
		case <-ticker.C:
			if etat == ModeCommand && time.Since(dernierModeCommand) > 10*time.Second {
				fmt.Println("⏱️ Timeout → retour veille")
				etat = ModeVeille
			}
		}
	}
}

func resolveTranscriptMode(cfg *config.Config) transcribe.Mode {
	switch transcribe.Mode(cfg.TranscriptionMode) {
	case transcribe.ModeRemote:
		fmt.Println("🌐 Mode transcription : remote (Whisper)")
		return transcribe.ModeRemote
	case transcribe.ModeLocal:
		fmt.Println("💻 Mode transcription : local (whisper.cpp)")
		return transcribe.ModeLocal
	default:
		fmt.Println("🎙️  Mode transcription : Vosk local")
		return transcribe.ModeVosk
	}
}

func modeCommand(inputText string, etat *int, analyseur *nlp.Analyseur) bool {
	reponse, verbe, match, isAction, appareil := analyseur.AnalyserEtExecuter(inputText)
	fmt.Println("Réponse : ", reponse)
	if appareil == nil || reponse == nil {
		if match {
			speech.Parler("assistant.retour.erreur")
		} else {
			speech.Parler("assistant.retour.pas.compris")
		}
	} else {
		if isAction {
			speech.Parler("assistant.retour.action", verbe, appareil.FriendlyName)
		} else {
			speech.Parler(reponse.Voix.Texte, reponse.Voix.Params...)
		}
	}
	fmt.Println("--- En attente d'un nouvel ordre ---")
	*etat = ModeVeille
	return match
}

func traiterEtat(inputText string, etat *int, numeroTel string, analyseur *nlp.Analyseur, gsmClient *sms.Client) {
	texte := strings.ToLower(inputText)
	fmt.Printf("🎯 Commande reçue : %s\n", texte)

	switch *etat {
	case ModeVeille:
		mots := strings.Fields(texte)
		motAssistant := false
		for _, mot := range mots {
			if text.DistanceLevenshtein(mot, "assistant") <= 2 {
				motAssistant = true
				break
			}
		}

		if motAssistant {
			fmt.Println("👉 Mot clé détecté !")

			var motsFiltres []string
			for _, m := range mots {
				if text.DistanceLevenshtein(m, "assistant") > 2 {
					motsFiltres = append(motsFiltres, m)
				}
			}

			match := false
			if len(motsFiltres) > 0 {
				match = modeCommand(strings.Join(motsFiltres, " "), etat, analyseur)
			}

			if len(motsFiltres) == 0 || !match {
				speech.Bip()
				dernierModeCommand = time.Now()
				*etat = ModeCommand
			}
		}

	case ModeCommand:
		if len(texte) > 3 {
			modeCommand(inputText, etat, analyseur)
		}

	case ModeSmsCommand:
		if len(texte) > 3 {
			reponse, _, _, isAction, _ := analyseur.AnalyserEtExecuter(inputText)
			textMessage := ""
			if reponse != nil {
				if isAction {
					textMessage = reponse.SMS.Texte
				} else {
					if i18n.Existe(reponse.SMS.Texte) {
						textMessage = i18n.T(reponse.SMS.Texte, reponse.SMS.Params...)
					} else {
						textMessage = fmt.Sprintf(reponse.SMS.Texte, reponse.SMS.Params...)
					}
				}

				fmt.Println("Envoie du SMS :", textMessage)
				if err := gsmClient.EnvoyerSMS(numeroTel, textMessage); err != nil {
					log.Printf("❌ Envoi SMS échoué : %v", err)
				}
				fmt.Println("--- En attente ---")
			} else {
				fmt.Println("Traitement impossible car une erreur est survenue")
				fmt.Println("--- En attente ---")
			}
		}
	}
}
