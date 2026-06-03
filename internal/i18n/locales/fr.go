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
		"meteo.actuelle.sms":       "Météo actuelle : %s, %.0f°C",
		"meteo.actuelle.voix":      "Météo actuelle : %s, %.0f degrés",
		"meteo.humidite":           ", humidité %d%%",
		"meteo.vent.sms":           ", vent %.0f km/h",
		"meteo.vent.voix":          ", vent %.0f kilometres par heure",
		"meteo.previsions":         "Prévisions pour les prochaines heures :\n",
		"meteo.heure.ligne.sms":    "• %s : %s, %.0f°C",
		"meteo.heure.ligne.voix":   " %s : %s, %.0f degrés",
		"meteo.precipitation.sms":  ", %.1f mm",
		"meteo.precipitation.voix": " avec %.1f millimètre de pluie",
		"meteo.demain.sms":         "Demain : %s, %.0f°C",
		"meteo.demain.voix":        "Demain : %s, %.0f degrés",
		"meteo.demain.vent.sms":    ", vent %.0f km/h",
		"meteo.demain.vent.voix":   ", vent %.0f kilometres par heure",
		"meteo.indispo":            "Aucune prévision disponible.",
		"meteo.demain.indispo":     "Prévision de demain non disponible.",
		"meteo.semaine":            "Prévisions de la semaine :\n",
		"meteo.weekend":            "Prévisions du week-end :\n",
		"meteo.jour.ligne.sms":     "• %s : %s, %.0f°C",
		"meteo.jour.ligne.voix":    " %s : %s, %.0f degrés",

		"meteo.erreur.previsions": "météo: échec récupération prévisions %s (%s) : %v",
		"meteo.erreur.actuel":     "météo: échec récupération état actuel %s : %v",
		"meteo.erreur.etatcustom": "météo: type etatCustom inattendu : %v",

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
		"maison.resume":            "Résumé de la maison :\n",
		"maison.temperatures":      "Températures :\n",
		"maison.sms.temp.ligne":    "• %s : %s°C\n",
		"maison.voix.temp.ligne":   "%s : %s degrés. ",
		"maison.sms.temp.climate":  "• %s : %.1f°C (consigne %.1f°C)\n",
		"maison.voix.temp.climate": "%s : %.1f degrés, consigne %.1f degrés. ",
		"maison.temp.aucune":       "Aucun capteur de température trouvé.",
		"maison.lumieres.on":       "Lumières allumées : %s.",
		"maison.lumieres.off":      "Toutes les lumières sont éteintes.",
		"maison.volets.ouverts":    "Volets ouverts : %s.",
		"maison.volets.fermes":     "Tous les volets sont fermés.",
		"maison.resume.concis":     "Maison : %d lumières allumées, %d volets ouverts.",

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
		"audio.vosk.text.compris":                      "[VOSK] texte compris : %s",
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
		// ---- Retours d'action (serviceBase) ----
		"action.ws.ok":   "✅ [WS] [%s] %s → %s",
		"action.http.ok": "✅ [%s] %s → %s",

		// ---- Heure parlée ----
		"voix.heure":         "%d heure",
		"voix.heure.minute":  "%d heure %d",
		"voix.heures":        "%d heures",
		"voix.heures.minute": "%d heures %02d",

		// ---- Agenda (lignes) ----
		"agenda.ligne.simple":  "• %s\n",
		"agenda.ligne.heure":   "• %s %d %s à %s : %s\n",
		"agenda.ligne.journee": "• %s %d %s : %s (toute la journée)\n",

		// ---- Media player / Spotify ----
		"media.spotify.domaine.incoherent": "⚠️ [Spotify] Domaine incohérent",
		"media.spotify.source.introuvable": "⚠️ [Spotify] Impossible de trouver la source",

		// ---- Liste de courses ----
		"shopping.maj": "✅ Liste de courses mise à jour",

		// ---- Erreurs HA / client ----
		"erreur.ha.reponse.get":    "HA a répondu %d sur GET %s",
		"erreur.ha.reponse.post":   "HA a répondu %d sur POST %s",
		"erreur.aucun.historique":  "aucun historique pour %s à %s",
		"erreur.recuperer.entites": "impossible de récupérer les entités",

		// ---- Erreurs WebSocket ----
		"erreur.ws.dial":              "websocket dial",
		"erreur.ws.timeout.connexion": "timeout connexion WS",
		"erreur.ws.envoi":             "envoi WS",
		"erreur.ws.reponse":           "WS %s : %s",
		"erreur.ws.timeout.id":        "timeout WS id=%d",

		// ---- Erreurs service loader / custom ----
		"erreur.loader.lecture":            "lecture %s",
		"erreur.loader.parsing":            "parsing %s",
		"erreur.custom.action.introuvable": "aucune action trouvée pour le verbe '%s' sur %s",

		// ---- Erreurs init (voix) ----
		"erreur.init.tts":           "init TTS",
		"erreur.init.transcripteur": "init transcripteur",
		"erreur.demarrage.micro":    "démarrage micro",
		"erreur.init.service":       "init %s : %v",
		"erreur.demarrage.services": "❌ démarrage services : %v",

		// ---- Erreurs / messages API SMS ----
		"erreur.modem.indispo":      "modem SMS non disponible",
		"erreur.numero.requis":      "numéro de téléphone requis",
		"erreur.message.requis":     "message requis",
		"erreur.message.trop.long":  "message trop long (%d caractères, max 160)",
		"api.acces.refuse":          "accès refusé",
		"api.cle.invalide":          "clé API invalide",
		"api.methode.non.autorisee": "méthode non autorisée",
		"api.body.json.invalide":    "body JSON invalide : %s",
		"api.sms.envoye.a":          "SMS envoyé à %s",

		// ---- Erreurs modem ----
		"erreur.modem.connexion":          "connexion modem TCL",
		"erreur.modem.getdevicest":        "GetDeviceSt",
		"erreur.modem.login":              "Login/ForceLogin",
		"erreur.modem.token.absent":       "token absent de la réponse : %v",
		"erreur.modem.sendsms.tentatives": "SendSMS après %d tentatives",
		"erreur.modem.envoi.echoue":       "envoi SMS échoué",
		"erreur.modem.chiffrement":        "chiffrement",
		"erreur.modem.reponse.invalide":   "réponse invalide",
		"erreur.modem.api":                "erreur API : %v",

		// ---- Erreurs crypto ----
		"erreur.crypto.base64":        "base64 decode",
		"erreur.crypto.header":        "format invalide : header Salted__ manquant",
		"erreur.crypto.ciphertext":    "ciphertext non aligné",
		"erreur.crypto.donnees.vides": "données vides",
		"erreur.crypto.padding":       "padding invalide : %d",

		// ---- Erreurs TTS ----
		"erreur.tts.json":  "erreur json",
		"erreur.tts.piper": "piper HTTP",
		"erreur.tts.wav":   "lecture WAV",

		// ---- Erreurs STT remote ----
		"erreur.stt.remote.file":     "remote: création du champ file",
		"erreur.stt.remote.copie":    "remote: copie audio",
		"erreur.stt.remote.requete":  "remote: création requête",
		"erreur.stt.remote.envoi":    "remote: envoi requête",
		"erreur.stt.remote.lecture":  "remote: lecture réponse",
		"erreur.stt.remote.invalide": "remote: réponse invalide",

		// ---- Erreurs STT local ----
		"erreur.stt.local.ecriture": "local: écriture fichier tmp",
		"erreur.stt.local.whisper":  "local: whisper.cpp a échoué",
		"erreur.stt.local.sortie":   "Sortie: %s",

		// ---- Erreurs STT engine ----
		"stt.mode.inconnu":          "mode de transcription inconnu : %s",
		"stt.mode.vosk.explication": "Vosk : la transcription passe par AcceptWaveform dans la boucle audio, pas par Engine.Transcribe()",

		// ---- Erreurs plugins ----
		"erreur.plugin.ouverture":           "ouverture %s",
		"erreur.plugin.symbole.introuvable": "%s : symbole NewService introuvable",
		"erreur.plugin.type":                "%s : NewService doit être de type func(plugins.Env) core.Service",

		// ---- Erreurs Vosk ----
		"erreur.vosk.chargement.modele": "Vosk: erreur chargement modèle : %v",
		"erreur.vosk.init.recognizer":   "Vosk: erreur init recognizer : %v",

		// ---- Désambiguïsation (choix multiples) ----
		"desambiguisation.invite":       "Plusieurs choix possibles : %s. Dites par exemple « choix un ».",
		"nlp.desambiguisation.propose":  "désambiguïsation : %d choix proposés",
		"nlp.desambiguisation.choix":    "désambiguïsation : choix « %s »",
		"nlp.desambiguisation.candidat": "option %d : %s (score %d, écart %d)",

		// ---- NLP ----
		"nlp.mot.assistant":   "assistant",
		"nlp.mot.pourcentage": "pourcentage",
		"nlp.mot.choix":       "choix",

		// ---- Nombres en lettres (1..100) ----
		"nombre.1":   "un",
		"nombre.2":   "deux",
		"nombre.3":   "trois",
		"nombre.4":   "quatre",
		"nombre.5":   "cinq",
		"nombre.6":   "six",
		"nombre.7":   "sept",
		"nombre.8":   "huit",
		"nombre.9":   "neuf",
		"nombre.10":  "dix",
		"nombre.11":  "onze",
		"nombre.12":  "douze",
		"nombre.13":  "treize",
		"nombre.14":  "quatorze",
		"nombre.15":  "quinze",
		"nombre.16":  "seize",
		"nombre.17":  "dix-sept",
		"nombre.18":  "dix-huit",
		"nombre.19":  "dix-neuf",
		"nombre.20":  "vingt",
		"nombre.21":  "vingt-et-un",
		"nombre.22":  "vingt-deux",
		"nombre.23":  "vingt-trois",
		"nombre.24":  "vingt-quatre",
		"nombre.25":  "vingt-cinq",
		"nombre.26":  "vingt-six",
		"nombre.27":  "vingt-sept",
		"nombre.28":  "vingt-huit",
		"nombre.29":  "vingt-neuf",
		"nombre.30":  "trente",
		"nombre.31":  "trente-et-un",
		"nombre.32":  "trente-deux",
		"nombre.33":  "trente-trois",
		"nombre.34":  "trente-quatre",
		"nombre.35":  "trente-cinq",
		"nombre.36":  "trente-six",
		"nombre.37":  "trente-sept",
		"nombre.38":  "trente-huit",
		"nombre.39":  "trente-neuf",
		"nombre.40":  "quarante",
		"nombre.41":  "quarante-et-un",
		"nombre.42":  "quarante-deux",
		"nombre.43":  "quarante-trois",
		"nombre.44":  "quarante-quatre",
		"nombre.45":  "quarante-cinq",
		"nombre.46":  "quarante-six",
		"nombre.47":  "quarante-sept",
		"nombre.48":  "quarante-huit",
		"nombre.49":  "quarante-neuf",
		"nombre.50":  "cinquante",
		"nombre.51":  "cinquante-et-un",
		"nombre.52":  "cinquante-deux",
		"nombre.53":  "cinquante-trois",
		"nombre.54":  "cinquante-quatre",
		"nombre.55":  "cinquante-cinq",
		"nombre.56":  "cinquante-six",
		"nombre.57":  "cinquante-sept",
		"nombre.58":  "cinquante-huit",
		"nombre.59":  "cinquante-neuf",
		"nombre.60":  "soixante",
		"nombre.61":  "soixante-et-un",
		"nombre.62":  "soixante-deux",
		"nombre.63":  "soixante-trois",
		"nombre.64":  "soixante-quatre",
		"nombre.65":  "soixante-cinq",
		"nombre.66":  "soixante-six",
		"nombre.67":  "soixante-sept",
		"nombre.68":  "soixante-huit",
		"nombre.69":  "soixante-neuf",
		"nombre.70":  "soixante-dix",
		"nombre.71":  "soixante-onze",
		"nombre.72":  "soixante-douze",
		"nombre.73":  "soixante-treize",
		"nombre.74":  "soixante-quatorze",
		"nombre.75":  "soixante-quinze",
		"nombre.76":  "soixante-seize",
		"nombre.77":  "soixante-dix-sept",
		"nombre.78":  "soixante-dix-huit",
		"nombre.79":  "soixante-dix-neuf",
		"nombre.80":  "quatre-vingts",
		"nombre.81":  "quatre-vingt-un",
		"nombre.82":  "quatre-vingt-deux",
		"nombre.83":  "quatre-vingt-trois",
		"nombre.84":  "quatre-vingt-quatre",
		"nombre.85":  "quatre-vingt-cinq",
		"nombre.86":  "quatre-vingt-six",
		"nombre.87":  "quatre-vingt-sept",
		"nombre.88":  "quatre-vingt-huit",
		"nombre.89":  "quatre-vingt-neuf",
		"nombre.90":  "quatre-vingt-dix",
		"nombre.91":  "quatre-vingt-onze",
		"nombre.92":  "quatre-vingt-douze",
		"nombre.93":  "quatre-vingt-treize",
		"nombre.94":  "quatre-vingt-quatorze",
		"nombre.95":  "quatre-vingt-quinze",
		"nombre.96":  "quatre-vingt-seize",
		"nombre.97":  "quatre-vingt-dix-sept",
		"nombre.98":  "quatre-vingt-dix-huit",
		"nombre.99":  "quatre-vingt-dix-neuf",
		"nombre.100": "cent",

		// Mots en vrac
		"mot.heure": "heure",
	})
}
