# Fonctionnalités — ha-command-gateway

Passerelle entre des **commandes en langage naturel** (voix, SMS, console, Home Assistant Assist) et
**Home Assistant**, avec un moteur de compréhension classique (scoring) et un moteur **IA (Gemini)**.

Légende : **[base]** = présent dans le projet d'origine · **[IA]** = ajouté avec l'intégration Gemini.

> ⚠️ **Statut** : toutes les fonctions **[IA]** ont été écrites sans compilateur ni exécution.
> Elles sont livrées mais **non validées** tant que `go vet -tags nvosk ./...` et
> `go test -tags nvosk ./internal/nlp/` n'ont pas tourné. Les points marqués *(à vérifier)*
> dépendent de détails de l'API Home Assistant non contrôlés.

---

## Sommaire

1. [Sources de commandes et réponses](#1-sources-de-commandes-et-réponses)
2. [Compréhension classique (sans IA)](#2-compréhension-classique-sans-ia)
3. [Domaines Home Assistant gérés](#3-domaines-home-assistant-gérés)
4. [Moteur IA (Gemini)](#4-moteur-ia-gemini)
5. [Commander la maison avec l'IA](#5-commander-la-maison-avec-lia)
6. [Lire des informations](#6-lire-des-informations)
7. [Historique, journal et analyse](#7-historique-journal-et-analyse)
8. [Enquêtes et conseils](#8-enquêtes-et-conseils)
9. [Sécurité et garde-fous](#9-sécurité-et-garde-fous)
10. [Robustesse et supervision](#10-robustesse-et-supervision)
11. [Extensibilité](#11-extensibilité)
12. [Déploiement](#12-déploiement)
13. [Variables d'environnement principales](#13-variables-denvironnement-principales)
14. [Limites connues et non fait](#14-limites-connues-et-non-fait)

---

## 1. Sources de commandes et réponses

| Fonction | Origine | Détail |
|---|---|---|
| **Voix** | base | Micro en continu (ffmpeg), mot de réveil « assistant », transcription **Vosk** (local temps réel), **Whisper distant** ou **whisper.cpp**. Après une réponse, retour en veille (10 s) |
| **SMS** | base | Réception et envoi via un modem **TCL LinkKey IK41** (API chiffrée), `WHITELIST` des numéros autorisés à commander |
| **Console** | base | Saisie clavier pour tester sans micro ni modem |
| **API HTTP** | base | `POST /sms/send` (envoi de SMS depuis une autre application) |
| **Agent conversationnel HA** | base | `POST /conversation` + intégration `ha-integration` : la passerelle devient un agent Assist de Home Assistant |
| **Réponse vocale** | base | Synthèse **Piper** avec cache PCM par composant de phrase |
| **Réponse par SMS** | base | Réponse renvoyée à l'expéditeur |
| **Micro ouvert après une question** | IA | Quand l'assistant attend une réponse (question de l'IA, confirmation, choix), la voix reste à l'écoute et HA garde le micro ouvert (`continue_conversation`) |
| **Retour parlé naturel** | IA | « J'ai éteint Salon et Cuisine. », « Volet salon réglé à 50 pour cent. », échecs partiels signalés — formulé par le code, seulement **après** exécution |
| **Réponse ⇄ SMS/API sans `%!(MISSING)`** | IA | Les `%` du texte libre de l'IA sont échappés |
| **Notification sur le téléphone** | IA | « envoie-moi ça sur mon téléphone » : notification de l'app mobile HA (`NOTIFY_SERVICE`, sinon détection `notify.mobile_app_*`), dernière réponse par défaut, **avec confirmation orale comme un SMS** |

---

## 2. Compréhension classique (sans IA)

- **NLP par scoring** [base] : mise en correspondance phrase ↔ entités HA (mots, pièces, fuzzy Levenshtein
  insensible aux accents, bonus lieu + fonction, malus mots superflus…), pondérations réglables sans recompiler
  (`SCORE_*`).
- **Présélection par domaine** [base] (`ACTIVE_PRESELECTION`) : restreint le scoring aux domaines dont un verbe
  est présent dans la phrase.
- **Désambiguïsation** [base] : si plusieurs entités ont un score proche, proposition « 1 : …, 2 : … » ; attente
  de réponse **par session** (voix, chaque numéro SMS…), expiration à 30 s.
- **Paramètres universels** [base] : pourcentage (« 80 % », « quatre-vingts pour cent »), température
  (« 20 degrés »), heure.
- **Lecture d'un état à une heure passée** [base] : « quel était l'état de X à 14h » (aujourd'hui).
- **Grammaire Vosk et prompt Whisper générés** [base] à partir des verbes, mots et noms d'entités.
- **Météo « après-demain », jours de la semaine, « dans 3 jours »** [IA] : correction du libellé et nouveaux horizons.
- **Mots-clés tolérants aux fautes de transcription** [IA] : « annoce » reconnu comme « annonce ».

---

## 3. Domaines Home Assistant gérés

| Domaine | Origine | Verbes / capacités |
|---|---|---|
| `light` | base | allume, éteins, luminosité, couleurs… |
| `cover` | base | ouvre, ferme, stoppe, position en % |
| `climate` | base | consigne, mode |
| `switch`, `input_boolean` | base | allume/éteins/active/désactive/bascule |
| `fan` | base | on/off, vitesse |
| `media_player` | base | play/pause/stop/suivant/précédent, volume, source, mode sonore, aléatoire, **Spotify sur un appareil** |
| `vacuum` | base | commandes aspirateur |
| `scene`, `script` | base | activer / exécuter (avec paramètres pour les scripts) |
| `automation` | base | déclenche, active, désactive, bascule |
| `todo`, `shopping_list` | base | gestion de listes |
| `alarm_control_panel`, `lock`, `camera` | base | (jamais exposés à l'IA) |
| `sensor`, `binary_sensor` | base | lecture d'état |
| `weather` | base | actuelle, heures, demain, semaine, week-end + **après-demain / jours / dans N jours** [IA] |
| `time` | base | heure, date |
| `agenda` | base | aujourd'hui, demain, semaine, mois + **période libre passée/future** et **filtre par calendrier** [IA] |
| `resume_maison` | base | résumé de la maison, températures |
| `timer` | IA | minuteurs (helpers HA) : lance, pause, annule, temps restant, **annonce à la fin** |
| `briefing` | IA | briefing à la demande |
| Domaines **custom** | base | `services.yaml` (voir §11) |

---

## 4. Moteur IA (Gemini)

**Principe** : *Gemini propose, le code décide.* Chaque réponse est un JSON structuré, validée avant toute
exécution (entité existante, verbe connu, action autorisée).

### 4.1 Modes et types de réponse [IA]

- **Primaire** (`GEMINI_PRIMARY=true`) ou **secours** quand le NLP classique ne comprend pas.
- **8 types de réponse** :

| Type | Sert à |
|---|---|
| `action` | commander une ou plusieurs entités |
| `read` | lire météo, agenda, heure, résumé maison, briefing |
| `history` | état d'une entité dans le passé |
| `classement` | comparer des capteurs (« quelle pièce est la plus humide ? ») |
| `journal` | qui / quoi a provoqué un changement |
| `recherche` | trouver un événement dans la courbe d'un capteur |
| `enquete` | diagnostic, résumé, énergie, conseil, annulation, pourquoi une automatisation |
| `speak` | réponse parlée (état lu dans le contexte, discussion, saint du jour…) |

### 4.2 Contexte, mémoire, pièces [IA]

- **Contexte réduit** : seules les entités les plus proches de la phrase (scoring NLP) + toutes celles des pièces
  citées + toujours scripts, météo, minuteurs, lecteurs média, calendriers et entités virtuelles.
  **Désactivable** : `GEMINI_PRESELECTION=false` (tout est alors envoyé).
- **Pièces** lues dans le registre des zones de HA et jointes à chaque entité *(token administrateur requis)*.
- **Date, heure et « depuis quand »** : chaque entité (hors capteurs numériques) porte l'heure de son dernier
  changement ; les automatisations et scripts leur `last_triggered`.
- **Mémoire de conversation par session** : 3 échanges / 3 min par défaut, **jamais partagée** entre voix, console,
  numéros SMS et conversations HA (`conversation_id`). En RAM, jamais écrite sur disque. Désactivable.
- **Paramètres des scripts** : les `fields` déclarés dans HA (nom, description, obligatoire) sont fournis à l'IA ;
  un champ obligatoire manquant déclenche une question à l'utilisateur.
- **Cache de contexte implicite** : la date est en fin de prompt pour garder un préfixe identique d'un appel à l'autre.

### 4.3 Second appel léger [IA]

Après une lecture faite par le code (historique, classement, journal, recherche, enquête), l'IA **reformule à
l'oral** et donne son avis si on le demande. Repli automatique sur le résumé du code en cas d'échec.
Désactivable : `GEMINI_ANALYSE=false`.

### 4.4 Robustesse de l'IA [IA]

- **Seconde chance** : quand le code rejette la réponse (entité inconnue, verbe invalide, action interdite), l'IA
  est rappelée **une fois** avec le motif exact.
- **Quotas locaux** (requêtes/min, requêtes/jour, tokens/min) et **anti-rafale**.
- **Disjoncteur** : après un `429` (durée conseillée par l'API) ou 3 échecs consécutifs, l'IA est suspendue et le
  NLP classique prend le relais, sans spam de logs.
- **Debug** : `GEMINI_DEBUG=true` (ou `LOG_LEVEL=debug`) journalise le prompt, le contexte, la réponse brute, les
  tokens et l'usage du jour ; la clé API passe dans un header (jamais dans l'URL/logs).
- **Entités hors catalogue** validées directement auprès de HA ; le domaine est lu dans l'`entity_id`.

### 4.5 Journal des décisions, mode ombre, apprentissage [IA]

- **Journal des décisions** : chaque échange consigné (phrase avec numéros masqués, canal, moteur, type choisi par
  l'IA, actions, rejets, seconde chance, résultat, appels IA, tokens, durée). `GET /decisions?n=50&faux=1`
  et fichier JSONL (`DECISIONS_FILE`).
- **« Non, pas ça »** : marque le dernier échange (moins de 3 min) comme **faux**, fait oublier la phrase apprise ;
  `POST /decisions/faux?id=N` équivalent.
- **Mode ombre** (`IA_OMBRE`) : l'autre moteur (classique ⇄ IA) dit ce qu'il aurait choisi sans exécuter ; les
  désaccords sont journalisés — repère les erreurs de scoring et de prompt.
- **Base apprise** (`NLP_APPRIS_FILE`) : une commande d'état simple comprise par l'IA et entièrement réussie est
  mémorisée ; réutilisée quand l'IA n'est pas disponible. Jamais de script, SMS, automatisation ni texte libre.

---

## 5. Commander la maison avec l'IA

- **Multi-actions** [IA] : « ouvre salon 1 et 2 » = 2 actions ; 12 au maximum par demande ; « éteins tout dans le
  salon » grâce aux pièces.
- **Scripts avec paramètres** [IA] : SMS, annonces sur Echo… l'IA choisit le script d'après son nom et sa
  description, et remplit ses paramètres. Script sans champ déclaré : le texte est transmis en `message`.
- **Spotify sur une enceinte / barre de son** [IA] : choix de l'appareil dans la `source_list`, puis lecture ;
  l'appareil peut être retrouvé dans la phrase si l'IA l'oublie.
- **Minuteurs** [IA] : sur un **Echo** (Alexa Media Player ≥ 3.4.0, commande vocale « mets un minuteur de… ») ou
  via les helpers `timer` de HA (avec annonce vocale à la fin).
- **Automatisations** [IA] : exécuter, activer, désactiver — **jamais** basculer, recharger ni modifier.
- **Annulation** [IA] : « annule ça » / « annule l'action » / « remets comme avant » — état d'avant mémorisé (15 min,
  5 commandes par session) pour les commandes de l'IA **et du NLP classique** ; la demande est reconnue **directement
  par le code** (sans passer par l'IA). Lumières, prises, ventilateurs, volets (open/close_cover ou position),
  thermostats, lecteurs média, automatisations. Un **SMS, un script ou une automatisation exécutés ne sont pas
  annulables** (l'assistant le dit).
- **Confirmation orale** [IA] avant un SMS, une action sur une automatisation ou une **grosse action groupée**
  (5 actions ou plus par défaut). Réponse « oui / non » interprétée par le code, 30 s.
- **Questions de l'IA** [IA] : elle peut demander une information manquante (« quel message ? ») ; la réponse est
  rattachée grâce à la mémoire de session.

---

## 6. Lire des informations

| Fonction | Origine | Détail |
|---|---|---|
| **Météo future** | IA | prévisions `weather.get_forecasts` (bug de lecture corrigé) : ce soir, demain, après-demain, jour de la semaine, semaine, week-end |
| **Agenda** | IA | période passée ou future libre, tous les calendriers ou un seul (ex. **Mealie** pour les repas / recettes) |
| **Heure, date, résumé de la maison** | base | |
| **Briefing à la demande** | IA | météo du jour, agenda (hors calendriers de repas), **repas du jour (Mealie)**, alertes (portes/fenêtres ouvertes, batteries < 15 %), formulé à l'oral par l'IA avec le **saint du jour** (connu de l'IA, aucun calendrier embarqué). **Ne démarre jamais à heure fixe.** Sans IA, le code assemble un briefing sans saint du jour |
| **Menu de demain et ingrédients** | IA | « qu'est-ce que je prépare demain soir ? » : plan de repas Mealie + détail des recettes (ingrédients, étapes à anticiper) *(à vérifier)*, repli sur les calendriers de repas ; le briefing du soir l'inclut |
| **Trouver une recette avec ce que tu as** | IA | « j'ai du riz et des œufs, qu'est-ce que je peux cuisiner ? » : recherche dans **toutes** les recettes de Mealie (API directe : `MEALIE_URL` + `MEALIE_TOKEN`), classement « tout y est » puis moins de manquants, correspondance par mots entiers, sel/poivre/eau/huile supposés disponibles ; l'IA propose 1 à 3 recettes et dit ce qui manque *(à vérifier : champs de l'API Mealie)* |
| **Saint du jour** | IA | simple question à l'IA (« c'est quel saint aujourd'hui / demain ? ») |
| **Question sur un état actuel** | IA | répondue à partir du contexte (« depuis quand la porte est ouverte ? ») |

---

## 7. Historique, journal et analyse

Toutes ces lectures sont calculées **par le code** ; l'IA ne fait ni arithmétique ni recherche, elle formule.

- **Historique d'une entité** [IA] : capteur numérique → minimum / maximum (avec leur heure) / moyenne
  pondérée par le temps ; état on/off… → temps passé dans chaque état, **nombre de fois**, changements.
  Périodes en langage courant (« hier soir », « aujourd'hui, entre 7h et 15h30 »), 31 jours maximum.
- **Comparer deux périodes** [IA] (« comme hier à la même heure ») : deux lectures, commentées par le second appel.
- **Journal (« qui l'a fait ? »)** [IA] : entrées de `/api/logbook` avec la **cause** — automatisation, script,
  utilisateur HA (nom avec token administrateur), ou « sans auteur identifié » (bouton physique, appareil).
  « Dernière fois que… », « pourquoi la lumière s'est allumée ? ». *(noms de champs à vérifier)*
- **Recherche dans une courbe** [IA] : plus forte chute / hausse, chute ou hausse d'**au moins X** (éventuellement
  « en moins de N minutes »), premier passage sous / au-dessus d'un seuil (et nombre de passages).
- **Classement de capteurs** [IA] : humidité, température, etc. par `device_class`, valeurs actuelles ou
  max / min / moyenne sur 7 jours ; compare les pièces entre elles ou les capteurs d'une pièce.
- **Analyse en deux appels** [IA] : « est-ce normal ? » → l'IA commente les chiffres, en signalant quand elle
  s'appuie sur un ordre de grandeur général plutôt que sur un seuil donné.
- **Consommation d'énergie** [IA] : somme des hausses des compteurs `device_class: energy` (kWh, Wh), remises à
  zéro comprises ; coût estimé avec `TARIF_KWH`.

> L'historique HA est purgé au bout de `purge_keep_days` (10 jours par défaut) : au-delà, rien à lire.

---

## 8. Enquêtes et conseils

Type `enquete` : le code rassemble les faits, l'IA les explique. [IA]

| Sujet | Exemple | Données rassemblées |
|---|---|---|
| `pourquoi_automatisation` | « pourquoi l'automatisation de la serre ne s'est pas déclenchée ? » | activée/désactivée, dernier déclenchement, 5 dernières exécutions (traces HA) avec la **condition bloquante**, déclencheurs et conditions *(à vérifier ; automatisations avec `id` seulement)* |
| `diagnostic_piece` | « pourquoi il fait froid dans la chambre ? » | entités de la pièce (fenêtres ouvertes depuis quand, chauffage, températures…), météo extérieure, événements des 6 dernières heures |
| `diagnostic_maison` | « y a-t-il un problème ? » | capteurs indisponibles > 24 h, batteries faibles, ouvertures ouvertes > 30 min, lumières allumées > 8 h, automatisations désactivées, mises à jour |
| `resume` | « que s'est-il passé cette nuit ? » | journal global filtré (portes, lumières, volets, automatisations, scripts) ; mouvements comptés |
| `energie` | « combien j'ai consommé aujourd'hui ? » | voir §7 |
| `conseil` | « faut-il arroser aujourd'hui ? », « je peux étendre le linge ? » | météo (actuelle, 3 jours, pluie des 12 h) + mesures demandées ; décision motivée |
| `annuler` | « annule ça », « annule l'action » | voir §5 |
| `expliquer` | « pourquoi tu as fait ça ? » | dernier échange du **journal des décisions** (phrase dite, moteur, entités choisies, rejets, phrase apprise) ; l'IA l'explique et propose de corriger |
| `planifier` | « mets les pâtes au pesto vendredi soir », « propose-moi un dîner au hasard » | écrit dans le plan de repas Mealie (`set_mealplan` / `set_random_mealplan`, *à vérifier*) ; recette retrouvée par son nom, choix demandé si plusieurs |
| `bilan` | « fais-moi le bilan de la semaine » | énergie, extrêmes de température et d'humidité, automatisations les plus déclenchées, ouvertures fréquentes, anomalies actuelles |
| `notifier` | « envoie-moi ça sur mon téléphone » | notification mobile avec confirmation (voir §1) |
| `repas` | « qu'est-ce que je prépare demain soir ? » | voir §6 |
| `cuisiner` | « j'ai du riz et des œufs, je cuisine quoi ? » | voir §6 |
| `aide` | « que sais-tu faire ? » | types d'appareils réels + verbes + grandes fonctions ; l'IA en tire des exemples de phrases |
| `inventaire` | « que puis-je contrôler dans le salon ? » | appareils contrôlables de la pièce (registre des zones), par type, avec leurs verbes |

---

## 9. Sécurité et garde-fous

- **Domaines interdits à l'IA** : `person`, `device_tracker`, `camera`, `lock`, `alarm_control_panel`, `update`
  (commande, lecture, historique, journal).
- **Liste d'actions autorisées par domaine**, vérifiée **par le code** (pas par le prompt) : ex. automatisations →
  `trigger`, `turn_on`, `turn_off` seulement.
- **Numéros autorisés** (`IA_NUMEROS_AUTORISES`, à défaut `WHITELIST`) : une action de script visant un autre
  numéro est refusée ; formats `+33 6…` et `06…` reconnus comme identiques.
- **Confirmation orale** avant SMS (script dont le nom contient « sms » ou qui vise un numéro), action sur une
  automatisation, ou action groupée. `IA_CONFIRMATION=false` la désactive.
- **Paramètres validés** : nom de paramètre de script inconnu rejeté, champ obligatoire manquant → question.
- **Format d'`entity_id` contrôlé** avant tout appel HA ; le domaine est déduit de l'identifiant.
- **Étanchéité des sessions** : mémoire, confirmations, annulations et attentes propres à chaque canal.
- **Accès HTTP** : `/conversation`, `/sms/send`, `/health`, `/status` réservés à `127.0.0.1` ; clé API
  (`Authorization: Bearer`) si définie.
- **Clé Gemini** jamais dans l'URL ni dans les logs d'erreur.
- **Données non fiables** : les titres d'agenda / de recettes ne sont transmis à l'IA que pour formuler un texte
  lu à voix haute (aucune action n'en découle).

---

## 10. Robustesse et supervision

- **`GET /health`** [IA] : sonde de **vie** — la boucle de traitement (bus) répond-elle ? 200 ou 503.
  Branchée en `HEALTHCHECK` Docker. *Docker ne redémarre pas seul un conteneur « unhealthy » : prévoir `autoheal`
  ou un watchdog.*
- **Visibilité dans HA** [IA] : capteurs `sensor.assistant_statut`, `sensor.assistant_tokens_jour`,
  `binary_sensor.assistant_ia_suspendue`, `sensor.assistant_derniere_erreur_ia` (republiés toutes les 60 s) et un
  **événement** `ha_command_gateway_command` à chaque commande — pour des automatisations et un tableau de bord.
- **`GET /decisions`** [IA] : journal des décisions (voir §4.5), accès local + clé API.
- **`GET /status`** [IA] : version, WebSocket HA, taille du catalogue, sessions IA, disjoncteur Gemini (ouvert ?
  reprise dans N s), appels et tokens du jour, dernière erreur, services actifs.
- **Reconnexion WebSocket HA** automatique [base] avec cache d'états temps réel.
- **Logs** [base] : niveaux `debug` / `info` / `warn` / `error`, horodatés, tous traduisibles (i18n).
- **Corrections de fond** [IA] : lecture des réponses POST de HA (les prévisions météo étaient toujours vides),
  plantage de l'API `/conversation` quand rien n'était compris, réponses de l'IA prises pour des erreurs par la
  voix et la console.

---

## 11. Extensibilité

- **Nouveau domaine HA en YAML** [base] : `services.yaml` (`SERVICES_FILE`) — verbes, mots, score.
- **Nouveau domaine en Go** [base] : service `internal/ha/service_xxx.go`.
- **Plugins `.so`** [base] : domaines HA ou services applicatifs chargés sans recompiler (Linux).
- **Services applicatifs** [base] : ajouter une source de commandes en implémentant l'interface `core.Service`
  (bus de tâches sérialisé, ports `SMSSender` / `Speaker`).
- **i18n** [base] : messages et logs dans `internal/i18n/locales/` ; ajouter une langue = un fichier.
- **Architecture hexagonale** [base] : cœur, services (source + traitement), adapters (modem, TTS, STT, Gemini).

---

## 12. Déploiement

- **Docker** (Raspberry Pi, aarch64) [base] : image multi-étapes avec ffmpeg, alsa, Piper, Vosk ;
  modèles téléchargés au premier démarrage ; `docker-compose.yml` avec `/dev/snd` et le modem USB.
- **Sonde de vie** [IA] : `HEALTHCHECK` sur `/health`.
- **Modes de transcription** [base] : `vosk` (local temps réel), `remote` (Whisper), `local` (whisper.cpp).
- **Compilation sans Vosk** [base] : `go build -tags nvosk`.
- **Version** [IA] : `-ldflags "-X main.version=…"` (affichée par `/status`).

---

## 13. Variables d'environnement principales

Voir `.env.example` et le `README.md` pour la liste complète. Réglages ajoutés avec l'IA :

| Variable | Défaut | Rôle |
|---|---|---|
| `GEMINI_ACTIVE` / `GEMINI_PRIMARY` / `GEMINI_MODEL` / `GEMINI_API_KEY` | — | activation, mode, modèle, clé |
| `GEMINI_DEBUG` | `false` | journaliser prompt, contexte, réponse, tokens |
| `GEMINI_PRESELECTION` / `GEMINI_CONTEXT_MAX` | `true` / `40` | réduction du contexte |
| `GEMINI_MEMOIRE_TOURS` / `GEMINI_MEMOIRE_SECONDES` | `3` / `180` | mémoire de conversation (0 = aucune) |
| `GEMINI_SECONDE_CHANCE` | `true` | rappel de l'IA après un rejet |
| `GEMINI_ANALYSE` | `true` | second appel (formulation naturelle, avis) |
| `GEMINI_MAX_REQUETES_MINUTE` / `_JOUR`, `GEMINI_MAX_TOKENS_MINUTE` | `0` | quotas locaux (0 = illimité) |
| `GEMINI_DELAI_MIN_MS` | `2000` | délai minimal entre deux appels |
| `IA_CONFIRMATION` | `true` | confirmation orale des actions sensibles |
| `IA_CONFIRMATION_GROUPE` | `5` | confirmation au-delà de N actions (0 = jamais) |
| `IA_NUMEROS_AUTORISES` | *(WHITELIST)* | numéros que l'IA peut viser |
| `BRIEFING_CALENDRIER_REPAS` | `mealie` | mot-clé des calendriers de repas |
| `TARIF_KWH` | `0` | prix du kWh pour estimer un coût |
| `IA_OMBRE` | `false` | mode ombre (désaccords NLP ⇄ IA journalisés) |
| `DECISIONS_FILE` / `NLP_APPRIS_FILE` | `data/…` | journal JSONL des décisions / phrases apprises (volume `./data`) |
| `MEALIE_URL` / `MEALIE_TOKEN` | *(vide = **tous les appels à Mealie désactivés**)* | interrupteur Mealie (menu du briefing, plan de repas, recettes) ; le jeton sert à chercher / planifier une recette par son nom |
| `NOTIFY_SERVICE` | *(auto)* | service `notify.*` de l'application mobile |
| `HA_PUBLISH` / `HA_PUBLISH_INTERVAL_S` / `HA_EVENT_NAME` / `HA_PUBLISH_PHRASE` | `true` / `60` / `ha_command_gateway_command` / `true` | publication de capteurs et d'événements dans HA |

---

## 14. Limites connues et non fait

**Limites connues**

- Aucune compilation ni exécution de test : le code **[IA]** est à valider.
- Le **saint du jour** vient de la mémoire de Gemini : il peut se tromper ou suivre un autre calendrier que le tien.
- L'historique HA est purgé (10 jours par défaut) ; le classement de capteurs est limité à 7 jours, l'historique
  à 31 jours. Pas d'usage des statistiques à long terme de HA.
- Un script est reconnu comme « SMS » d'après son **nom** (contient « sms »).
- Un service dont le retour commence par « ⚠️ » est compté comme un échec.
- Les entités de présence (`person`, `device_tracker`) restent interdites à l'IA.
- L'annulation ne couvre pas les scripts / SMS / automatisations exécutés.
- Le prompt compte 8 types de réponse : à surveiller si le modèle se trompe de type.
- Pièces, journal (noms d'utilisateurs) et traces d'automatisations demandent un **token administrateur**.

**Idées proposées et non réalisées**

- Tests unitaires (durées de minuteur, numéros, oui/non, recherche de source Spotify, chutes et seuils…).
- Mode « à blanc » (`DRY_RUN`), permissions par canal, mémos persistants (seuils personnalisés), routines nommées.
- Découpage des longues réponses par phrases pour démarrer la voix plus tôt.
- Génération d'automatisation en brouillon (YAML relu à la main).