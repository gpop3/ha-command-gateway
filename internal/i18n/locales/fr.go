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

		// ---- assistant ----
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
		"erreur.action.parler":         "Erreur lors de l'action sur l'entité",

		// --- SMS ----
		"message.retour.etat":       "[%s] État actuel : %s.",
		"message.retour.etat.heure": "[%s] À %s, l'état était : %s.",

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
		"time.heure":   "Il est %s heure %s.",
		"time.date":    "Nous sommes le %s %s %s %s.",
		"time.complet": "Il est %s heure %s, le %s %s %s %s.",

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

		// ---- Logs / système ----
		"tts.pret":          "TTS prêt avec cache par composants.",
		"commande.vocale":   "🎯 Commande vocale : %s",
		"api.demarree":      "🌐 API HTTP démarrée sur %s",
		"assistant.arret":   "🛑 Arrêt en cours…",
		"vosk.pause":        "🔇 Silence détecté — Vosk en pause",
		"vosk.reprend":      "🎤 Son détecté — Vosk reprend",
		"vosk.stream.fin":   "ℹ️ Fin du stream audio (EOF).",
		"sms.modem.indispo": "⚠️ Modem non disponible : %v",

		// ---- Vosk Windows ----
		"vosk.windows": "⚠️  Vosk non disponible sur Windows → mode remote",

		// ---- Logs techniques ----
		"agenda.agenda": "❌ [agenda] %s : %v",
		"agenda.agenda.echec.critique.etatcustom":      "❌ [Agenda] Échec critique: etatCustom n'est pas de type 'Agenda' (type réel reçu: %T)",
		"agenda.agenda.erreur.lors.de":                 "⚠️ [Agenda] Erreur lors de la construction du message: %v",
		"agenda.agenda.unmarshal":                      "❌ [agenda] unmarshal %s : %v",
		"agenda.valeurs.recues":                        "Valeurs reçues : A=%v, B=%v",
		"api.api.envoi.sms":                            "📤 [API] Envoi SMS → %s : %s",
		"audio.erreur.json.vosk":                       "⚠️ Erreur JSON Vosk : %v",
		"audio.fin.du.stream.audio":                    "⚠️ Fin du stream audio avec erreur : %v",
		"audio.sauvegarderaudio.octets":                "SauvegarderAudio: %s (%d octets)",
		"audio.vosk.confiance.trop.faible":             "[VOSK] Confiance trop faible (%d) pour : %q",
		"config.note.aucun.fichier.env":                "Note: aucun fichier .env trouvé, utilisation des variables système",
		"console.reponse":                              "Réponse : %v",
		"core.core.service.deja.enregistre":            "⚠️ [core] service '%s' déjà enregistré — ignoré",
		"core.fermeture":                               "⚠️ fermeture %s : %v",
		"core.service.arrete":                          "⚠️ service %s arrêté : %v",
		"core.service.demarre":                         "▶️ service %s démarré",
		"ha.plugins":                                   "⚠️ plugins : %v",
		"ha.services.yaml":                             "⚠️ services.yaml : %v",
		"ha.ws.callservice.echoue.fallback":            "⚠️ [WS] CallService échoué, fallback HTTP : %v",
		"ha.ws.timeout.attente.cache":                  "⚠️ [WS] timeout attente cache — fallback HTTP",
		"ha.ws.websocket.indisponible.fallback":        "⚠️ [WS] WebSocket indisponible, fallback HTTP : %v",
		"log.plugins":                                  "⚠️ plugins : %v",
		"modem.sms.erreur.contacts.reconnexion":        "⚠️ [SMS] erreur contacts : %v — reconnexion...",
		"modem.sms.erreur.contenu.contactid":           "⚠️ [SMS] erreur contenu contactID=%d : %v — reconnexion...",
		"modem.sms.reconnexion.echouee":                "⚠️ [SMS] reconnexion échouée : %v",
		"modem.sms.reconnexion.echouee.2":              "⚠️ [SMS] reconnexion échouée : %v",
		"modem.sms.recu.un.numero":                     "📱 SMS reçu d'un numéro inconnu %s : %s",
		"modem.sms.suppression.echouee.smsid":          "⚠️ [SMS] suppression échouée SMSId=%d : %v",
		"modem.sms.supprime.smsid":                     "✅ [SMS] supprimé SMSId=%d",
		"nlp.catalogue":                                "DEBUG catalogue[0]: %+v",
		"nlp.domaine":                                  "DEBUG domaine: %+v",
		"nlp.estaction.domaine":                        "DEBUG estAction=%v domaine=%s\n",
		"nlp.estactionpardefaut.true":                  "DEBUG EstActionParDefaut → true\n",
		"nlp.mots":                                     "DEBUG mots: %+v",
		"nlp.preselection.score.domaine":               "DEBUG: Présélection '%s' | Score: %d | Domaine: %s\n",
		"nlp.score.domaine":                            "DEBUG: '%s' | Score: %d | Domaine: %s\n",
		"nlp.selection.du.domaine":                     "DEBUG: 'Selection du domaine %s' pour la recherche\n",
		"nlp.verbes":                                   "DEBUG verbes: %+v",
		"plugin.plugin":                                "⚠️ [plugin] %s : %v",
		"plugin.plugin.charge.domaine":                 "✅ [plugin] %s chargé → domaine '%s'",
		"plugin.plugin.symbole.pluginservice.manquant": "⚠️ [plugin] %s : symbole PluginService manquant",
		"plugin.plugin.type.invalide":                  "⚠️ [plugin] %s : type invalide",
		"plugin.services.domaine.deja.enregistre":      "⚠️ [services] domaine '%s' déjà enregistré — remplacé",
		"plugin.services.service.custom.charge":        "✅ [services] service custom '%s' chargé (%d verbes, %d mots)",
		"plugin.services.service.sans.domaine":         "⚠️ [services] service sans domaine ignoré",
		"script.extraireparams.texte.params":           "DEBUG ExtraireParams texte: '%s' → params: %v",
		"sms.envoi":                                    "Envoi du SMS : %s",
		"sms.envoi.sms.echoue":                         "❌ Envoi SMS échoué : %v",
		"sms.traitement.sms.impossible.analyse":        "traitement SMS impossible : analyse en erreur",
		"stt.remote.reponse.json.invalide":             "remote: réponse JSON invalide. Brut : %s",
		"stt.transcription.locale.terminee.duration":   "⏱️ Transcription locale terminée en %v\n",
		"tts.tts.aplay":                                "⚠️ [TTS] aplay : %v\n",
		"tts.tts.generation":                           "[TTS] Génération [%s] -> %s\n",
		"ws.ws.abonne.aux.changements":                 "✅ [WS] abonné aux changements d'état",
		"ws.ws.authentification.echouee.token":         "❌ [WS] authentification échouée — token invalide",
		"ws.ws.authentifie.chargement.des":             "✅ [WS] authentifié — chargement des états...",
		"ws.ws.connexion.perdue.reconnexion":           "⚠️ [WS] connexion perdue : %v — reconnexion...",
		"ws.ws.erreur.auth":                            "❌ [WS] erreur auth : %v",
		"ws.ws.etats.charges.en":                       "✅ [WS] %d états chargés en cache",
		"ws.ws.get.states.envoi":                       "⚠️ [WS] get_states envoi : %v",
		"ws.ws.message.invalide":                       "⚠️ [WS] message invalide : %v",
		"ws.ws.reconnecte":                             "✅ [WS] reconnecté",
		"ws.ws.reconnexion.echouee":                    "⚠️ [WS] reconnexion échouée : %v",
		"ws.ws.subscribe.events":                       "⚠️ [WS] subscribe_events : %v",
		"ws.ws.tentative.de.reconnexion":               "🔄 [WS] tentative de reconnexion...",
		"ws.ws.timeout.get.states":                     "⚠️ [WS] timeout get_states — cache vide, fallback HTTP",
		"ws.ws.unmarshal.etats":                        "⚠️ [WS] unmarshal états : %v",
	})
}
