package main

import (
	"context"
	"ha-command-gateway/internal/core/adapters/gemini"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ha-command-gateway/config"
	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/core/services/api"
	"ha-command-gateway/internal/core/services/console"
	"ha-command-gateway/internal/core/services/sms"
	"ha-command-gateway/internal/core/services/voice"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	_ "ha-command-gateway/internal/i18n/locales"
	"ha-command-gateway/internal/core/services/hapublisher"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/nlp"
	"ha-command-gateway/internal/plugins"
)

// version est renseignée à la compilation : go build -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	cfg := config.Load()
	i18n.SetLocale(cfg.Lang)

	haClient := ha.NewClient(cfg.HAUrl, cfg.HAToken, cfg.HAPieces, time.Duration(cfg.HATimeout), cfg)
	haClient.AttendreWS()

	ha.DefinirBriefingRepas(cfg.BriefingCalendrierRepas)
	ha.DefinirTarifKWh(cfg.TarifKWh)
	ha.DefinirMealieAPI(cfg.MealieURL, cfg.MealieToken)

	analyseur := nlp.New(haClient, cfg.ActivePreselection, nlp.ConfigDesambiguisation{
		Active:   cfg.DesambiguisationActive,
		Seuil:    cfg.DesambiguisationSeuil,
		MaxChoix: cfg.DesambiguisationMaxChoix,
	}, nlp.ConfigScore{
		Minimal:               cfg.ScoreMinimal,
		BonusPiece:            cfg.ScoreBonusPiece,
		BonusMot:              cfg.ScoreBonusMot,
		BonusFuzzy:            cfg.ScoreBonusFuzzy,
		MalusPieceSeule:       cfg.ScoreMalusPieceSeule,
		BonusLieuFonction:     cfg.ScoreBonusLieuFonction,
		BonusCouvertureExacte: cfg.ScoreBonusCouvertureExacte,
		MalusMotSuperflu:      cfg.ScoreMalusMotSuperflu,
		MalusActionSansCible:  cfg.ScoreMalusActionSansCible,
	})

	// Journal des décisions et phrases apprises par l'IA (réutilisées sans elle)
	analyseur.DefinirJournalDecisions(cfg.DecisionsFile)
	if err := analyseur.DefinirFichierAppris(cfg.NLPApprisFile); err != nil {
		logx.WarnT("appris.fichier.erreur", err)
	}

	var geminiClient *gemini.Client // nil si l'IA est désactivée
	if cfg.GeminiActive {
		if cfg.GeminiAPIKey == "" {
			logx.WarnT("gemini.cle.manquante")
		} else {
			geminiClient = gemini.New(cfg.GeminiAPIKey, cfg.GeminiModel)
			geminiClient.ActiverDebug(cfg.GeminiDebug)
			geminiClient.DefinirQuotas(gemini.Quotas{
				RequetesMinute: cfg.GeminiMaxRequetesMinute,
				RequetesJour:   cfg.GeminiMaxRequetesJour,
				TokensMinute:   cfg.GeminiMaxTokensMinute,
				DelaiMin:       time.Duration(cfg.GeminiDelaiMinMs) * time.Millisecond,
			})
			analyseur.DefinirGemini(geminiClient, cfg.GeminiPrimary)

			numeros := cfg.IANumerosAutorises
			if numeros == "" {
				numeros = cfg.Whitelist
			}
			analyseur.DefinirConfigIA(nlp.ConfigIA{
				Preselection:     cfg.GeminiPreselection,
				ContexteMax:      cfg.GeminiContexteMax,
				MemoireTours:     cfg.GeminiMemoireTours,
				MemoireDuree:     time.Duration(cfg.GeminiMemoireSecondes) * time.Second,
				Confirmation:     cfg.IAConfirmation,
				NumerosAutorises: strings.Split(numeros, ","),
				SecondeChance:    cfg.GeminiSecondeChance,
				Analyse:          cfg.GeminiAnalyse,
				SeuilGroupe:      cfg.IAConfirmationGroupe,
				Ombre:            cfg.IAOmbre,
				ServiceNotification: cfg.NotifyService,
			})
			// Pré-charge le registre des pièces HA (évite la latence au premier appel)
			go haClient.ZonesEntites()
			logx.InfoT("gemini.active", cfg.GeminiModel, cfg.GeminiPrimary)
		}
	}

	if err := analyseur.RafraichirCatalogue(); err != nil {
		logx.Fatalf("%s", i18n.T("erreur.ha.connexion", err))
	}
	logx.InfoT("assistant.catalogue")

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

	// SMS (fournit le port SMSSender)
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

	// Fin d'un minuteur HA (timer.*) : annonce vocale, sérialisée par le bus
	ha.DefinirSurMinuteurTermine(func(nom string) {
		bus.Soumettre(func() { speaker.Parler("timer.termine", nom) })
	})

	// Visibilité dans Home Assistant : capteurs d'état + un événement à chaque commande
	if cfg.HAPublish {
		analyseur.DefinirSurDecision(func(d nlp.Decision) {
			evt := map[string]interface{}{
				"canal": d.Canal, "moteur": d.Moteur, "type": d.Type, "sujet": d.Sujet, "actions": d.Actions,
				"resultat": d.Resultat, "reussi": d.Reussi, "duree_ms": d.DureeMs, "tokens": d.Tokens, "faux": d.Faux,
			}
			if cfg.HAPublishPhrase {
				evt["phrase"] = d.Phrase
			}
			go func() {
				if err := haClient.PublierEvenement(cfg.HAEventName, evt); err != nil {
					logx.DebugT("publication.erreur", err)
				}
			}()
		})
		mgr.Register(hapublisher.New(haClient, func() hapublisher.Etat {
			e := hapublisher.Etat{Statut: "ia_desactivee", Version: version, Extra: map[string]interface{}{
				"ha_websocket": haClient.EtatWebsocket(), "entites": analyseur.NbEntites(), "sessions_ia": analyseur.NbSessions(),
			}}
			if geminiClient != nil {
				s := geminiClient.Statut()
				e.Statut, e.TokensJour, e.AppelsJour = "ok", s.TokensAujourdhui, s.AppelsAujourdhui
				e.IASuspendue, e.DerniereErreur = s.DisjoncteurOuvert, s.DerniereErreur
				if s.DisjoncteurOuvert {
					e.Statut = "ia_suspendue"
				}
			}
			return e
		}, time.Duration(cfg.HAPublishIntervalS)*time.Second))
	}

	// API HTTP
	if cfg.ActiveServerHttp {
		statut := func() map[string]interface{} {
			res := map[string]interface{}{
				"version":      version,
				"ha_websocket": haClient.EtatWebsocket(),
				"entites":      analyseur.NbEntites(),
				"sessions_ia":  analyseur.NbSessions(),
				"services": map[string]bool{
					"voix": cfg.ActiveVoice, "sms": cfg.ActiveSms, "console": cfg.ActiveConsole, "api": cfg.ActiveServerHttp,
				},
			}
			if geminiClient != nil {
				res["ia"] = geminiClient.Statut()
			} else {
				res["ia"] = "désactivée"
			}
			return res
		}
		mgr.Register(api.New(cfg.APIPort, cfg.APIKey, apiSender, analyseur, bus, statut))
	}

	// Plugins .so (services tiers)
	env := plugins.Env{Bus: bus, Analyseur: analyseur, Speaker: speaker, Sender: sender}
	if svcs, err := plugins.Charger("plugins/", env); err != nil {
		logx.WarnT("log.plugins", err)
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
		logx.Fatalf("%s", i18n.T("erreur.demarrage.services", err))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	logx.InfoT("assistant.pret")
	<-sig
	logx.InfoT("assistant.arret")
	cancel()
	mgr.Fermer(context.Background())
}
