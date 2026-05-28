package locales

import "ha-command-gateway/internal/i18n"

func init() {
	i18n.Register("fr", i18n.Locale{

		// ---- Général ----
		"erreur.ha.connexion":   "Impossible de joindre Home Assistant : %v",
		"erreur.action":         "❌ Échec de l'action sur %s : %v",
		"erreur.domaine":        "❌ Domaine '%s' non supporté.",
		"erreur.lecture.live":   "⚠️ Erreur lecture live de [%s].",
		"erreur.historique":     "⚠️ Erreur historique pour [%s] à %s.",
		"traitement":            "Traitement en cours.",
		"ok.action":             "✅ L'ordre a été exécuté sur : %s.",
		"ok.message":            "✅ %s",
		"erreur.lecture.parler": "Erreur lecture",

		// ---- Voix / assistant ----
		"assistant.pret":      "🚀 Assistant prêt (Voix + SMS + Console).",
		"assistant.catalogue": "🚀 Catalogue HA chargé.",
		"assistant.ecoute":    "Je vous écoute.",
		"assistant.timeout":   "⏱️ Timeout → retour veille",
		"assistant.mot.cle":   "👉 Mot clé détecté !",

		// --- Voix ----
		"assistant.attente":            "--- En attente d'un nouvel ordre ---",
		"assistant.retour.etat":        "L'état actuel de %s est : %s",
		"assistant.retour.etat.heure":  "L'état de %s à %s était %s",
		"assistant.retour.action":      "Action \"%s\" exécuté sur %s",
		"assistant.retour.erreur":      "Une erreur est survenue",
		"assistant.retour.pas.compris": "Je n'ai pas compris.",
		"assistant.retour.climate":     "Température actuelle : %.1f degrés. Consigne : %.1f degrés. Le statut est %s avec un mode %s.",

		// ---- Audio / transcription ----
		"audio.micro":          "🎤 Micro activé en continu.",
		"audio.vosk.pret":      "🎙️  Vosk prêt.",
		"audio.silence":        "🔇 Silence détecté → envoi",
		"audio.parole":         "🎤 Parole détectée",
		"audio.timeout":        "⏱️  Timeout 5s → envoi forcé",
		"audio.entendu":        "🎧 Entendu (%v) : %s",
		"audio.erreur":         "❌ Erreur transcription : %v",
		"transcription.remote": "🌐 Mode transcription : remote (Whisper)",
		"transcription.local":  "💻 Mode transcription : local (whisper.cpp)",
		"transcription.vosk":   "🎙️  Mode transcription : Vosk local",

		// ---- GSM / SMS ----
		"sms.recu":            "📱 SMS reçu de %s : %s",
		"sms.ecoute":          "📱 Écoute SMS activée.",
		"sms.envoye":          "📤 SMS envoyé à %s : %s",
		"sms.indispo.windows": "ℹ️  SMS non disponible sur Windows.",
		"sms.reponse":         "Réponse SMS :",
		"gsm.erreur":          "⚠️ GSM non disponible : %v",

		// ---- Console ----
		"console.prete": "⌨️ Console prête. Tapez une commande :",

		// ---- Heure / Date ----
		"time.heure":   "🕐 Il est %dh%02d.",
		"time.date":    "📅 Nous sommes le %s %d %s %d.",
		"time.complet": "🕐 Il est %dh%02d, le %s %d %s %d.",

		// ---- Météo ----
		"meteo.actuelle":       "🌤️ Météo actuelle : %s, %.0f°C",
		"meteo.humidite":       ", humidité %d%%",
		"meteo.vent":           ", vent %.0f km/h",
		"meteo.previsions":     "🕐 Prévisions pour les prochaines heures :\n",
		"meteo.heure.ligne":    "• %s : %s, %.0f°C",
		"meteo.precipitation":  ", %.1f mm",
		"meteo.demain":         "📅 Demain : %s, %.0f°C",
		"meteo.demain.vent":    ", vent %.0f km/h",
		"meteo.indispo":        "Aucune prévision disponible.",
		"meteo.demain.indispo": "Prévision de demain non disponible.",

		// ---- Agenda ----
		"agenda.aujourd.hui":  "Aujourd'hui :\n",
		"agenda.demain":       "Demain :\n",
		"agenda.semaine":      "Cette semaine :\n",
		"agenda.mois":         "Ce mois :\n",
		"agenda.ligne":        "• %dh%02d — %s\n",
		"agenda.vide.jour":    "Rien de prévu aujourd'hui.",
		"agenda.vide.demain":  "Rien de prévu demain.",
		"agenda.vide.semaine": "Rien de prévu cette semaine.",
		"agenda.vide.mois":    "Rien de prévu ce mois.",

		// ---- Résumé maison ----
		"maison.resume":         "🏠 Résumé de la maison :\n",
		"maison.temperatures":   "🌡️ Températures :\n",
		"maison.temp.ligne":     "• %s : %s°C\n",
		"maison.temp.climate":   "• %s : %.1f°C (consigne %.1f°C)\n",
		"maison.temp.aucune":    "Aucun capteur de température trouvé.",
		"maison.lumieres.on":    "💡 Lumières allumées : %s.",
		"maison.lumieres.off":   "💡 Toutes les lumières sont éteintes.",
		"maison.volets.ouverts": "🪟 Volets ouverts : %s.",
		"maison.volets.fermes":  "🪟 Tous les volets sont fermés.",

		// ---- Cover ----
		"cover.position.ok":     "✅ %s positionné à %d%%.",
		"cover.position.erreur": "❌ Échec du positionnement sur %s.",

		// ---- Notify ----
		"notify.ok":     "✅ Message envoyé → %s",
		"notify.erreur": "notify/%s : %v",

		// ---- Lecture d'état ----
		"etat.live":       "📊 [%s] État actuel : %s.",
		"etat.historique": "⏳ [%s] À %s, l'état était : %s.",

		// ---- Climate ----
		"climate.repos":   "au repos",
		"climate.chauffe": "🔥 en chauffe",
		"climate.refroid": "❄️ en refroidissement",
		"climate.format":  "🌡️ [%s]\n• Temp. actuelle : %.1f°C\n• Consigne : %.1f°C\n• Statut : %s (mode %s)",

		// ---- Vosk Windows ----
		"vosk.windows": "⚠️  Vosk non disponible sur Windows → mode remote",
	})
}
