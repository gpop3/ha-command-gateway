# Assistant Domotique Go (`ha-command-gateway`)

Passerelle entre des **commandes en langage naturel** et **Home Assistant**.  
L'assistant écoute des ordres venant de trois sources
- la **voix** (micro + transcription)
- les **SMS** (modem TCL LinkKey IK41)
- la **console** — les analyse pour piloter

Home Assistant ou répondre à une question, puis renvoie la réponse via la **synthèse vocale** (Piper) ou par **SMS**.  
Une petite **API HTTP** permet aussi de déclencher l'envoi d'un SMS depuis l'extérieur.

---

## Sommaire

- [Fonctionnalités](#fonctionnalités)
- [Architecture](#architecture)
- [Prérequis](#prérequis)
- [Installation & lancement](#installation--lancement)
- [Configuration (variables d'environnement)](#configuration-variables-denvironnement)
- [Ajouter un service applicatif](#ajouter-un-service-applicatif)
- [Ajouter un domaine Home Assistant](#ajouter-un-domaine-home-assistant)
- [API HTTP](#api-http)

---

## Fonctionnalités

- **Voix** : capture micro en continu (ffmpeg), transcription par **Vosk**
  (local temps réel), **Whisper distant** (Raspberry Pi) ou **whisper.cpp**
  (local), réveil par mot-clé « assistant ».
- **SMS** : réception et envoi via un modem **TCL LinkKey IK41** (API web
  chiffrée du modem).
- **Console** : saisie clavier pour tester sans micro ni modem.
- **NLP** : analyse de la commande, mise en correspondance avec les entités
  Home Assistant et exécution de l'action (ou récupération d'un état).
- **Désambiguïsation** : quand plusieurs entités obtiennent un score très
  proche, l'assistant propose une liste de choix (« 1 : …, 2 : … ») et attend
  la réponse de l'utilisateur (« choix un »).
- **Réponses** : synthèse vocale **Piper** (avec cache PCM par composant) ou
  réponse par SMS.
- **API HTTP** : `POST /sms/send` pour envoyer un SMS depuis une autre app.
- **Extensible** : on ajoute une source de commandes (un *service*) en
  implémentant une interface, et un nouveau domaine Home Assistant en Go ou
  en YAML — sans toucher au reste.

### Le réveil par mot-clé (voix)

Le service **voix** a sa propre petite machine à états interne pour le mot-clé
« assistant » (les autres services traitent chaque entrée directement) :

| État | Rôle |
|------|------|
| veille | Attend le mot-clé « assistant ». Tant qu'il n'est pas entendu, rien n'est exécuté. |
| commande | Mot-clé entendu : la phrase suivante est exécutée sur Home Assistant. Retour automatique en veille après réponse ou au bout de 10 s. |

Le **SMS** exécute chaque message reçu et répond par SMS à l'expéditeur. La
**console** exécute chaque ligne saisie comme une commande locale.

---

## Architecture

Le projet suit une **architecture hexagonale pragmatique**. L'idée :

1. Un **cœur** (`internal/core`) qui ne connaît aucun détail technique. Il
   définit *le contrat* d'un service et *les ports* (interfaces) pour les
   capacités partagées.
2. Des **services** (`internal/core/services/*`) : chacun est autonome,
   **possède ses propres ressources** (le service SMS possède le modem, le
   service voix possède le micro + le TTS + le STT…), gère son cycle de vie
   (init / démarrage / arrêt) **et son propre traitement des commandes**.
3. Des **adapters** (`internal/core/adapters/*`) : les drivers techniques bas
   niveau (modem, synthèse vocale, transcription) que les services pilotent.

Un **bus de tâches** (`core.Bus`) sérialise les traitements : chaque service
décide quoi faire de sa commande et soumet une *tâche* au bus, exécutée sur une
unique goroutine. Il n'y a donc **pas de machine à états centrale** — la voix
gère son réveil par mot-clé en interne, le SMS répond par SMS, etc. Et comme un
service apporte à la fois sa source et son traitement, on peut en charger de
nouveaux **en plugins `.so`** (comme les domaines Home Assistant).

```
cmd/assistant/
└── main.go                     Câblage : construit le Manager, enregistre les
                                services actifs, lance la boucle de traitement.

internal/core/                  ── LE CŒUR (package feuille, zéro import métier)
├── manager.go                  Contrat Service + Manager (Register/Démarrer/Fermer)
├── bus.go                      Bus de tâches sérialisé (traitements des services)
└── ports.go                    Ports partagés : SMSSender, Speaker (+ no-op)

internal/core/services/         ── LES SERVICES (source + traitement)
├── console/                    Lit stdin ; exécute la commande localement
├── sms/                        Possède le modem ; écoute + répond par SMS ; = SMSSender
├── voice/                      Possède micro + TTS + STT ; réveil mot-clé ; = Speaker
│   ├── service.go              Cycle de vie + machine mot-clé + implémentation de Speaker
│   ├── recorder.go             Capture micro (ffmpeg) + buffer audio
│   ├── loop_vosk.go            Boucle audio Vosk        (build: !windows && !nvosk)
│   ├── loop_whisper.go         Boucle audio Whisper     (commune)
│   ├── loop_stub.go            Boucle audio de repli    (build: windows || nvosk)
│   └── vosk_helpers.go         Helpers Vosk             (build: !windows)
└── api/                        Serveur HTTP (route /sms/send)

internal/core/adapters/         ── LES DRIVERS TECHNIQUES
├── modem/                      Modem TCL IK41 (chiffrement web + envoi/réception)
├── tts/                        Synthèse vocale Piper + lecture audio (aplay)
└── stt/                        Transcription : Vosk / Whisper distant / whisper.cpp

internal/plugins/               Chargeur de services tiers en .so
plugins/                        (dossier des .so chargés au démarrage)

internal/ha/                    Client Home Assistant + registre des domaines
internal/nlp/                   Analyse de la commande → action HA
internal/i18n/                  Traductions (clés → messages)
internal/logx/                  Logger centralisé (niveaux + horodatage + i18n)
internal/input/                 Type Commande partagé entre services
internal/utils/                 Helpers (Levenshtein, conversions…)
config/                         Chargement de la config depuis l'environnement
```

### Le contrat d'un service

Un service implémente au minimum `core.Service`, et **optionnellement** deux
interfaces de cycle de vie :

```go
// Obligatoire
type Service interface {
    Nom() string                        // identifiant unique (logs)
    Démarrer(ctx context.Context) error // BLOQUANT, lancé dans sa goroutine
}

// Optionnel : init faillible avant tout démarrage (connexion modem, ouverture micro…)
type Initialisable interface {
    Init(ctx context.Context) error
}

// Optionnel : libération de ressources à l'arrêt (fermer le micro, le serveur HTTP…)
type Fermable interface {
    Fermer(ctx context.Context) error
}
```

Le `Manager` orchestre tout : il appelle d'abord `Init()` sur tous les services
(**fail-fast** : une erreur stoppe le boot), puis lance chaque `Démarrer()` dans
sa propre goroutine, et appelle `Fermer()` à l'arrêt.

### Le partage par les ports

Certaines capacités appartiennent à un service mais sont utilisées ailleurs.
Plutôt que de se passer des objets concrets, on passe des **interfaces** (ports)
définies dans `core` :

- **`core.SMSSender`** (`Envoyer(numero, message string) error`) — implémenté
  par le service SMS. Il est fourni à l'API HTTP (pour la route `/sms/send`) et
  à la boucle de traitement (pour répondre à un SMS).
- **`core.Speaker`** (`Parler(cle string, args ...any)` + `Bip()`) — implémenté
  par le service voix. Fourni à la boucle de traitement pour les réponses
  vocales.

Si un service est **désactivé**, son port est remplacé par une implémentation
**no-op** (`core.NoopSMS`, `core.NoopSpeaker`) : le reste du programme continue
de fonctionner sans condition spéciale.

### Le bus de tâches et le traitement par service

Il n'y a pas de fonction centrale qui décide quoi faire d'une commande. Chaque
service **possède son traitement** et le soumet au `core.Bus`, qui exécute les
tâches une par une sur une seule goroutine (l'analyse NLP reste donc
mono-thread, sans course) :

```
service voix    ──(mot-clé + analyse + parole)──┐
service sms     ──(analyse + réponse SMS)────────┤
service console ──(analyse + parole)─────────────┤──▶ bus.Soumettre(tâche)
plugin XYZ      ──(son propre traitement)────────┘            │
                                                              ▼
                                            bus.Lancer : exécute les tâches
                                            une à une  ─▶ nlp ─▶ Home Assistant
                                                       ─▶ Speaker / SMSSender
```

Chaque service reçoit le bus à sa création et, quand il reçoit une entrée, fait
`bus.Soumettre(func() { /* son traitement */ })`. C'est ce qui permet d'ajouter
un service (ou un plugin) qui traite les commandes **comme il l'entend**, sans
toucher au reste.

### Entités virtuelles et init des domaines

Certains domaines NLP n'ont pas d'entité réelle dans Home Assistant (heure,
date, météo, agenda, résumé maison). Chaque service de domaine déclare lui-même
ses **entités virtuelles** (interface `ha.ServiceAvecAppareils`), et celles-ci
sont ajoutées au catalogue lors du rafraîchissement — au lieu d'être codées en
dur dans le NLP.

De même, un domaine qui a besoin d'une référence à l'analyseur (par ex.
`media_player` pour retrouver l'enceinte Spotify, `agenda` pour lire le
catalogue) implémente `ha.Init(analyseur)` : l'analyseur lui est injecté **une
seule fois**, après que le catalogue est prêt. L'interface vit côté `ha`, donc
aucune dépendance circulaire avec `nlp`.

---

## Prérequis

- **Go 1.25+** (le module cible `go 1.25.0`).
- **ffmpeg** (capture micro) et **alsa-utils** (`aplay`, lecture audio) pour la
  voix.
- **Piper** (TTS) — lancé en serveur HTTP local.
- **Vosk** : la bibliothèque C `libvosk` + un modèle de langue (mode `vosk`,
  Linux uniquement, nécessite **cgo**). Inutile en mode `remote`/`local`.
- Un **token Home Assistant** (long-lived access token).
- (Optionnel) un modem **TCL LinkKey IK41** pour le SMS.

---

## Installation & lancement

### Avec Docker (recommandé sur Raspberry Pi)

Le `Dockerfile` est multi-étapes : il installe ffmpeg, alsa, Piper, et libvosk (aarch64), puis compile l'assistant en Go avec cgo activé.

Au lieu de figer les modèles à la compilation, l'image est générique et internationale. Le script entrypoint.sh gère dynamiquement le téléchargement des modèles au premier démarrage (par défaut la voix française `fr_FR-siwis-medium` pour Piper et le modèle `vosk-model-small-fr-0.22` pour Vosk), permettant à l'utilisateur de changer de langue simplement via des variables d'environnement.

Au lancement, l'entrypoint démarre le serveur HTTP de Piper (port 5000) puis l'assistant Go.

```bash
docker build -t assistant-domotique .

docker run --rm -it \
  --env-file .env \
  --device /dev/snd \              # audio
  --device /dev/ttyUSB0 \          # modem (si SMS)
  -v /chemin/vers/vosk-model:/opt/vosk-model \
  -p 8080:8080 \
  assistant-domotique
```

> Mettre `NO_PIPER=true` pour ne pas lancer le serveur Piper interne (par
> exemple si Piper tourne déjà ailleurs).

### En local

```bash
cp .env.example .env          # puis adapter les valeurs
go build ./cmd/assistant/     # cgo requis si TRANSCRIPTION_MODE=vosk
./assistant
```

Pour compiler **sans Vosk** (par ex. sur une machine de dev sans `libvosk`) :

```bash
go build -tags nvosk ./cmd/assistant/   # utilise les modes remote/local
```

---

## Configuration (variables d'environnement)

Toutes les options se règlent par variables d'environnement (un fichier `.env`
est chargé automatiquement au démarrage). Voir aussi `.env.example`.

> **Note** : la plupart des variables sont lues par le binaire Go
> (`config/config.go`). Quelques-unes sont lues **directement** ou par
> l'**entrypoint Docker** (téléchargement des modèles au premier démarrage) :
> `LOG_LEVEL`, `NO_PIPER`, `VOSK_MODEL_NAME`, `PIPER_SERVER_LANG`,
> `PIPER_SERVER_VOICE`, `PIPER_SERVER_MODEL_NAME`. C'est précisé dans les tables.

### Général

| Variable | Défaut | Description |
|----------|--------|-------------|
| `LANG` | `fr` | Langue des messages (clé i18n). |
| `LOG_LEVEL` | `info` | Niveau minimal de log : `debug`, `info`, `warn`, `error`. *(lu directement)* |

### Home Assistant

| Variable | Défaut | Description |
|----------|--------|-------------|
| `HA_URL` | `http://localhost:8123` | URL de Home Assistant. |
| `HA_TOKEN` | *(vide)* | Token long-lived. **Requis.** |
| `MES_PIECES` | *(vide)* | Liste des pièces, séparées par des virgules (ex : `Salon,Cuisine,Bureau`). |
| `HA_WEBSOCKET` | `true` | Utiliser le WebSocket HA (sinon HTTP). |
| `HA_TIMEOUT` | `10` | Timeout client HA (secondes). |
| `SERVICES_FILE` | `services.yaml` | Fichier des domaines HA custom (voir plus bas). |

### Transcription (voix)

| Variable | Défaut | Description |
|----------|--------|-------------|
| `TRANSCRIPTION_MODE` | `vosk` | `vosk` (local temps réel, Linux), `remote` (Whisper sur RPi), `local` (whisper.cpp). |
| `RASPBERRY_PI_IP` | `localhost` | IP du RPi, sert à construire `WHISPER_URL` si vide. |
| `WHISPER_URL` | *(auto)* | Endpoint Whisper distant. Si vide : `http://$RASPBERRY_PI_IP:8000/v1/audio/transcriptions`. |
| `VOSK_MODEL_PATH` | *(vide)* | Chemin du modèle Vosk (mode `vosk`). |
| `WHISPER_BIN` | *(vide)* | Binaire whisper.cpp (mode `local`). |
| `WHISPER_MODEL` | *(vide)* | Modèle `.bin` whisper.cpp (mode `local`). |
| `WHISPER_VAD_MODEL` | *(vide)* | Modèle VAD whisper.cpp (mode `local`). |
| `VOSK_MODEL_NAME` | `vosk-model-small-fr-0.22` | Nom du modèle Vosk téléchargé au premier démarrage. *(entrypoint Docker)* |

### Audio

| Variable | Défaut | Description |
|----------|--------|-------------|
| `ALSA_DEVICE` | `plughw:CARD=Bar,DEV=0` | Périphérique ALSA (micro + lecture). |
| `WINDOWS_MIC` | `Microphone (Realtek(R) Audio)` | Nom du micro dshow (Windows). |

### Synthèse vocale (Piper)

| Variable | Défaut | Description |
|----------|---------|--------------------------------------|
| `PIPER_URL` | `http://localhost:5000` | Endpoint HTTP du serveur Piper. |
| `PIPER_BIN` | *(vide)* | Binaire Piper (si lancement direct). |
| `PIPER_MODEL` | *(vide)* | Modèle de voix `.onnx`. |
| `PIPER_SERVER_LANG` | `fr` | Langue du modèle. *(entrypoint Docker)* |
| `PIPER_SERVER_VOICE` | `fr_FR/siwis/medium` | Path de l'API. *(entrypoint Docker)* |
| `PIPER_SERVER_MODEL_NAME` | `fr_FR-siwis-medium` | Modèle de voix `.onnx`. *(entrypoint Docker)* |

### Modem / SMS (TCL LinkKey IK41)

| Variable | Défaut | Description |
|----------|--------|-------------|
| `MODEM_URL` | `http://192.168.1.1` | URL de l'interface web du modem. |
| `MODEM_PASSWORD` | *(vide)* | Mot de passe de l'interface. |
| `MODEM_VERIF_KEY` | *(vide)* | Header `_TclRequestVerificationKey`. |
| `MODEM_XOR_KEY` | *(vide)* | Clé de chiffrement XOR. |
| `MODEM_FREE_KEY` | *(vide)* | Clé AES des APIs « free ». |
| `MODEM_HMAC_KEY` | *(vide)* | Clé HMAC-SHA256. |
| `WHITELIST` | *(vide)* | Numéros autorisés à piloter par SMS. |
| `GSM_PORT` | `/dev/ttyUSB0` | Port série GSM (legacy). |
| `GSM_PIN` | `1234` | Code PIN SIM (legacy). |

### API HTTP

| Variable | Défaut | Description |
|----------|--------|-------------|
| `API_PORT` | `8080` | Port d'écoute de l'API. |
| `API_KEY` | *(vide)* | Si défini, exige `Authorization: Bearer <clé>`. |

### Activation des services

| Variable | Défaut | Description |
|----------|--------|-------------|
| `ACTIVE_CONSOLE` | `true` | Active la saisie clavier. |
| `ACTIVE_VOICE` | `true` | Active la voix (micro + TTS + STT). |
| `ACTIVE_SMS` | `true` | Active le SMS (modem). |
| `ACTIVE_SERVER_HTTP` | `true` | Active l'API HTTP. |
| `ACTIVE_PRESELECTION` | `true` | Présélection NLP des entités candidates. |

### Désambiguïsation NLP

Quand plusieurs entités ont un score proche, l'assistant propose une liste et
attend un choix. L'attente est mémorisée **par session** (canal) : la voix et un
numéro SMS peuvent chacun avoir leur désambiguïsation en cours sans collision,
et une attente expire au bout de **30 s**.

| Variable | Défaut | Description |
|----------|--------|----------------------------------------|
| `DESAMBIGUISATION_ACTIVE` | `true` | Active la désambiguïsation. |
| `DESAMBIGUISATION_SEUIL` | `5` | Écart de score maximal pour grouper deux entités comme « équivalentes ». |
| `DESAMBIGUISATION_MAX_CHOIX` | `3` | Nombre maximum d'options proposées. |

### Pondération du scoring NLP

Permet d'ajuster le matching entité ↔ commande sans recompiler. Les « malus »
sont stockés positifs et soustraits dans le calcul.

| Variable | Défaut | Description |
|----------|--------|-------------|
| `SCORE_MINIMAL` | `30` | Score minimal pour qu'une entité soit retenue (sinon « pas compris »). |
| `SCORE_BONUS_PIECE` | `100` | Bonus quand un mot correspond à une pièce connue. |
| `SCORE_BONUS_MOT` | `20` | Bonus par mot du nom de l'entité reconnu (match exact). |
| `SCORE_BONUS_FUZZY` | `15` | Bonus pour un match approximatif (Levenshtein). |
| `SCORE_MALUS_PIECE_SEULE` | `80` | Malus si seule une pièce est matchée, sans mot spécifique. |
| `SCORE_BONUS_LIEU_FONCTION` | `60` | Bonus si l'entité matche à la fois la pièce ET la fonction. |
| `SCORE_BONUS_COUVERTURE_EXACTE` | `10` | Bonus si un nom multi-mots est entièrement couvert par la commande. |
| `SCORE_MALUS_MOT_SUPERFLU` | `2` | Malus par mot du nom non prononcé. |
| `SCORE_MALUS_ACTION_SANS_CIBLE` | `50` | Malus pour une action réduite à un seul mot (verbe sans cible). |

---

## Logs et internationalisation (i18n)

Toutes les sorties passent par le logger centralisé `internal/logx`. Chaque
ligne est **horodatée** et préfixée par son **niveau** :

```
2026/06/01 16:30:57 INFO  🎤 Son détecté — Vosk reprend
2026/06/01 16:30:58 WARN  ⚠️ [sms] modem non disponible : ...
2026/06/01 16:30:59 ERROR ❌ Envoi SMS échoué : ...
```

Niveaux disponibles : `debug` < `info` < `warn` < `error`. Le niveau minimal se
règle via `LOG_LEVEL` (ou `logx.SetNiveau`). Les nombreuses traces `DEBUG` de
l'analyse NLP et de Home Assistant sont en niveau `debug` : invisibles par
défaut, on les active avec `LOG_LEVEL=debug`.

Deux familles d'API dans `logx` :

```go
// 1) Style fmt — message littéral (ou déjà traduit)
logx.Info("🚀 Assistant prêt.")
logx.Warnf("⚠️ plugins : %v", err)
logx.Errorf("❌ %s", msg)

// 2) Style i18n — la clé est résolue dans le catalogue de la langue courante
logx.InfoT("vosk.reprend")                 // → "🎤 Son détecté — Vosk reprend"
logx.InfoT("audio.entendu", duree, texte)  // clé avec arguments
logx.WarnT("sms.modem.indispo", err)
```

Les messages destinés à l'utilisateur (réponses, états des pièces, prompts…),
les statuts visibles **et les diagnostics techniques** (avertissements, erreurs,
et même les traces de debug) sont désormais définis une seule fois dans
`internal/i18n/locales/fr.go` et appelés par leur clé (`i18n.T` ou `logx.*T`).
Tout le texte affiché est donc traduisible : pour ajouter une langue, créer
`locales/en.go` qui appelle `i18n.Register("en", …)` avec les mêmes clés, puis
lancer avec `LANG=en`.

> Les traces de debug (`logx.DebugT`) sont elles aussi dans le catalogue pour
> rester traduisibles, mais comme elles sont en niveau `debug` elles n'appa-
> raissent qu'avec `LOG_LEVEL=debug`.

---

## Ajouter un service applicatif

Un « service » apporte **une source de commandes et son traitement**. On crée un
package qui implémente `core.Service`, on reçoit le bus + l'analyseur + les
ports dont on a besoin, et on soumet son traitement au bus. Deux façons : en dur
dans le binaire, ou en **plugin `.so`** (sans recompiler l'assistant).

### En dur (intégré au binaire)

Exemple de création d'un service

```go
// internal/core/services/test/test.go
package test

import (
    "context"

    "ha-command-gateway/internal/core"
    "ha-command-gateway/internal/nlp"
)

type Service struct {
    analyseur *nlp.Analyseur
    speaker   core.Speaker
    bus       *core.Bus
    // ... le service possède ici son client
}

func New(analyseur *nlp.Analyseur, speaker core.Speaker, bus *core.Bus) *Service {
    return &Service{analyseur: analyseur, speaker: speaker, bus: bus}
}

func (s *Service) Nom() string { return "test" }

// Init (optionnel) : connexion au broker. Une erreur stoppe le boot.
func (s *Service) Init(ctx context.Context) error { /* s.client = ... */ return nil }

// Démarrer (obligatoire, bloquant) : écoute, et soumet son traitement au bus.
func (s *Service) Démarrer(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        // case msg := <-s.client.Messages():
        //     texte := msg.Payload
        //     s.bus.Soumettre(func() { s.traiter(texte) })
        }
    }
}

// traiter : le service décide quoi faire (ici : analyse + réponse vocale).
func (s *Service) traiter(texte string) {
    reponse, verbe, _, isAction, appareil := s.analyseur.AnalyserEtExecuter("test", texte)
    if appareil != nil && reponse != nil && isAction {
        s.speaker.Parler("assistant.retour.action", verbe, appareil.FriendlyName)
    }
}

// Fermer (optionnel) : libère les ressources.
func (s *Service) Fermer(ctx context.Context) error { return nil }
```

Puis dans `cmd/assistant/main.go`, à côté des autres `Register` :

```go
mgr.Register(test.New(analyseur, speaker, bus))
```

Si le service **fournit** une capacité partagée (parler, envoyer un SMS…), on
lui fait implémenter le port correspondant (`core.Speaker`, `core.SMSSender`, ou
un nouveau port défini dans `internal/core/ports.go`) et on le passe là où la
capacité est consommée — exactement comme la voix fournit `Speaker` et le SMS
fournit `SMSSender`.

### En plugin `.so` (sans recompiler)

Les `.so` déposés dans le dossier `plugins/` sont chargés au démarrage. Un
plugin est un package `main` qui expose une fonction `NewService` recevant un
`plugins.Env` (le bus, l'analyseur, les ports) :

```go
package main

import (
    "context"

    "ha-command-gateway/internal/core"
    "ha-command-gateway/internal/plugins"
)

type service struct{ env plugins.Env }

func NewService(env plugins.Env) core.Service { return &service{env} }

func (s *service) Nom() string { return "mon-plugin" }

func (s *service) Démarrer(ctx context.Context) error {
    // ... reçoit des entrées, puis :
    // s.env.Bus.Soumettre(func() {
    //     reponse, _, _, _, _ := s.env.Analyseur.AnalyserEtExecuter("mon-plugin", texte)
    //     // répondre via s.env.Speaker / s.env.Sender
    // })
    <-ctx.Done()
    return ctx.Err()
}
```

Compilation :

```bash
go build -buildmode=plugin -o plugins/mon-plugin.so ./chemin/du/plugin
```

> Les plugins Go sont **Linux uniquement** et exigent les **mêmes versions** de
> Go et de dépendances que l'assistant hôte. Pour du multiplateforme, préférer
> l'ajout « en dur ».

---

## Ajouter un domaine Home Assistant

À ne pas confondre avec un *service applicatif* ci-dessus : ici on étend la
**compréhension des commandes** vers un nouveau domaine Home Assistant (`light`,
`cover`, `switch`…). Trois façons, par ordre de simplicité :

### 1. En YAML (sans recompiler) — `services.yaml`

Le plus rapide. Le fichier pointé par `SERVICES_FILE` est chargé au démarrage :

```yaml
- domain: "fan"            # domaine Home Assistant
  verbs:                   # verbe reconnu → action HA
    allume:   { action: "turn_on" }
    éteins:   { action: "turn_off" }
    bascule:  { action: "toggle" }
  words: ["ventilateur", "ventilo"]   # mots qui évoquent ce domaine
  default_action: false               # true = est une action par défaut
  score: 30                           # priorité de matching
```

Chaque entrée correspond à la structure `ha.ConfigService` ; chaque `VerbeConfig`
accepte `action` et, optionnellement, `params`.

### 2. En Go (intégré au binaire)

Pour une logique plus fine. Créer `internal/ha/service_xxx.go` sur le modèle des
services existants (ex. `service_switch.go`) :

```go
package ha

type ServiceSwitch struct{ serviceBase }

func NewServiceSwitch(c *Client) *ServiceSwitch {
    return &ServiceSwitch{newServiceBase("switch", c, map[string]VerbeConfig{
        "allume":  {Action: "turn_on"},
        "éteins":  {Action: "turn_off"},
        "bascule": {Action: "toggle"},
    })}
}

func (s *ServiceSwitch) ScoreDomaine(estAction bool) int { return -40 }
```

Puis enregistrer le service dans `internal/ha/client.go`, dans `NewClient`, à
côté des autres :

```go
Register(NewServiceSwitch(c))
```

Un service Go peut aussi, **optionnellement**, implémenter :

- `ScoreDomaine(estAction bool) int` — sa priorité de matching (selon que la
  commande est une action ou une lecture). Le `estAction` est désormais
  déterminé **par domaine** : il est vrai uniquement si *ce* domaine connaît un
  verbe présent dans la phrase.
- `EstActionParDefaut() bool` — `true` si une commande sans verbe explicite doit
  quand même être traitée comme une action.
- `ExtraireParams(texte) map[string]interface{}` — extraction de paramètres
  propres au domaine (horizon météo, mode du résumé, pourcentage…).
- `RecupererEtat(...)` + `EtatEnMessage(...)` — pour un domaine de **lecture**
  qui construit son message en fragments i18n + params (voir météo / agenda).
- `AppareilsVirtuels() []Appareil` — déclarer des entités virtuelles (heure,
  date, météo…).
- `Init(analyseur)` — recevoir l'analyseur une fois au démarrage (callbacks,
  pré-chargements).

### 3. En plugin `.so` (chargement dynamique)

Le client charge aussi les plugins Go compilés déposés dans le dossier
`plugins/`. Un plugin doit exposer un symbole `PluginService` de type
`*ha.Service`. Pratique pour distribuer un domaine sans recompiler l'assistant
(Linux uniquement, contraintes des plugins Go).

---

## API HTTP

### `POST /sms/send`

Envoie un SMS via le modem (nécessite `ACTIVE_SMS=true` ; sinon la route n'est
pas montée).

**Sécurité** : la requête doit provenir de `127.0.0.1`, et si `API_KEY` est
défini, l'en-tête `Authorization: Bearer <API_KEY>` est exigé. Le message est
validé (numéro et message non vides, <= 160 caractères).

```bash
curl -X POST http://127.0.0.1:8080/sms/send \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"numero":"+33600000000","message":"Bonjour depuis l assistant"}'
```

Réponse :

```json
{ "succes": true, "message": "SMS envoyé à +33600000000" }
```