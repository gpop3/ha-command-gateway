package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ha-command-gateway/config"
	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/core/plugins"
	"ha-command-gateway/internal/core/services/api"
	"ha-command-gateway/internal/core/services/console"
	"ha-command-gateway/internal/core/services/sms"
	"ha-command-gateway/internal/core/services/voice"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	_ "ha-command-gateway/internal/i18n/locales"
	"ha-command-gateway/internal/nlp"
)

func main() {
	cfg := config.Load()
	i18n.SetLocale(cfg.Lang)

	haClient := ha.NewClient(cfg.HAUrl, cfg.HAToken, cfg.HAPieces, time.Duration(cfg.HATimeout), cfg)
	haClient.AttendreWS()

	analyseur := nlp.New(haClient, cfg.ActivePreselection)
	if err := analyseur.RafraichirCatalogue(); err != nil {
		log.Fatalf("%s", i18n.T("erreur.ha.connexion", err))
	}
	fmt.Println(i18n.T("assistant.catalogue"))

	bus := core.NewBus(20)
	mgr := core.New()

	var speaker core.Speaker = core.NoopSpeaker{}
	var sender core.SMSSender = core.NoopSMS{}
	var apiSender core.SMSSender

	if cfg.ActiveVoice {
		voiceSvc := voice.New(voice.Config{
			PiperUrl:          cfg.PiperUrl,
			AlsaDevice:        cfg.AlsaDevice,
			WindowsMic:        cfg.WindowsMic,
			TranscriptionMode: cfg.TranscriptionMode,
			WhisperURL:        cfg.WhisperURL,
			WhisperBinPath:    cfg.WhisperBinPath,
			WhisperModelPath:  cfg.WhisperModelPath,
			WhisperVadModel:   cfg.WhisperVadModel,
			VoskModelPath:     cfg.VoskModelPath,
		}, analyseur, bus)
		mgr.Register(voiceSvc)
		speaker = voiceSvc
	}

	if cfg.ActiveSms {
		smsSvc := sms.New(sms.Config{
			ModemURL:  cfg.ModemURL,
			Password:  cfg.ModemPassword,
			VerifKey:  cfg.ModemVerifKey,
			XorKey:    cfg.ModemXorKey,
			FreeKey:   cfg.ModemFreeKey,
			HmacKey:   cfg.ModemHmacKey,
			Whitelist: cfg.Whitelist,
		}, analyseur, bus)
		mgr.Register(smsSvc)
		sender = smsSvc
		apiSender = smsSvc
	}

	// Console (utilise le port Speaker)
	if cfg.ActiveConsole {
		mgr.Register(console.New(analyseur, speaker, bus))
	}

	// API HTTP
	if cfg.ActiveServerHttp {
		mgr.Register(api.New(cfg.APIPort, cfg.APIKey, apiSender))
	}

	// Plugins .so (services tiers)
	env := plugins.Env{Bus: bus, Analyseur: analyseur, Speaker: speaker, Sender: sender}
	if svcs, err := plugins.Charger("plugins/", env); err != nil {
		log.Printf("⚠️ plugins : %v", err)
	} else {
		for _, s := range svcs {
			mgr.Register(s)
		}
	}

	// Démarrage
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go bus.Lancer(runCtx)
	if err := mgr.Démarrer(runCtx); err != nil {
		log.Fatalf("❌ démarrage services : %v", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("🚀 Assistant prêt.")
	<-sig
	fmt.Println("\n🛑 Arrêt en cours…")
	cancel()
	mgr.Fermer(context.Background())
}
