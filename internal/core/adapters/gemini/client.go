package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ha-command-gateway/internal/logx"
)

// La clé API passe dans le header x-goog-api-key (et non dans l'URL) : une
// erreur réseau de net/http inclut l'URL complète et fuiterait la clé dans les logs.
const urlAPI = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"

// Client pour l'API Gemini, en sortie structurée (pas de parsing texte libre).
type Client struct {
	apiKey       string
	model        string
	http         *http.Client
	dernierAppel time.Time
	delaiMin     time.Duration // anti-rafale : protège le quota journalier
	debug        bool
}

// Parametre est un couple nom/valeur (le response_schema Gemini n'accepte pas
// de map libre : on passe donc par une liste).
type Parametre struct {
	Nom    string `json:"nom"`
	Valeur string `json:"valeur"`
}

// Action est une commande (ou une lecture) portant sur une entité.
type Action struct {
	Domain     string      `json:"domain,omitempty"`
	Verbe      string      `json:"verbe,omitempty"`
	EntityID   string      `json:"entity_id,omitempty"`
	Complement string      `json:"complement,omitempty"` // langage naturel : "80%", "rouge", "demain"
	Parametres []Parametre `json:"parametres,omitempty"` // paramètres structurés (scripts, Spotify, minuteur)
	Debut      string      `json:"debut,omitempty"`      // agenda : début de période (ISO 8601)
	Fin        string      `json:"fin,omitempty"`        // agenda : fin de période exclue (ISO 8601)
}

// Reponse est la sortie structurée imposée via le response_schema.
type Reponse struct {
	Type          string   `json:"type"` // "action" | "read" | "history" | "speak"
	Actions       []Action `json:"actions,omitempty"`
	ReponseVocale string   `json:"reponse_vocale"`
	// AttendReponse : la réponse vocale est une question (« quel numéro ? ») ;
	// l'assistant garde alors l'écoute pour la réponse de l'utilisateur.
	AttendReponse bool `json:"attend_reponse,omitempty"`
}

// Tour est un échange passé de la conversation (demande de l'utilisateur et
// réponse JSON de l'IA), renvoyé à Gemini pour qu'il comprenne « et demain ? ».
type Tour struct {
	Demande string
	Reponse string
}

func New(apiKey, model string) *Client {
	if model == "" {
		model = "gemini-3.1-flash-lite"
	}
	return &Client{
		apiKey:   apiKey,
		model:    model,
		http:     &http.Client{Timeout: 8 * time.Second},
		delaiMin: 2 * time.Second,
	}
}

// ActiverDebug force l'affichage (niveau INFO) du contexte envoyé à l'IA et de
// sa réponse brute, même si LOG_LEVEL n'est pas « debug ».
func (c *Client) ActiverDebug(actif bool) { c.debug = actif }

// trace journalise en INFO si GEMINI_DEBUG, sinon en DEBUG (LOG_LEVEL=debug).
func (c *Client) trace(cle string, args ...any) {
	switch {
	case c.debug:
		logx.InfoT(cle, args...)
	case logx.DebugActif():
		logx.DebugT(cle, args...)
	}
}

var joursSemaine = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}

// maintenant décrit la date et l'heure courantes pour que l'IA puisse résoudre
// « demain », « lundi prochain », « la semaine dernière »...
func maintenant() string {
	n := time.Now()
	return fmt.Sprintf("%s %s", joursSemaine[n.Weekday()], n.Format("2006-01-02T15:04:05-07:00"))
}

// ATTENTION : ce gabarit passe par fmt.Sprintf (3 « %s » : date, capacités,
// contexte) — tout « % » littéral doit être écrit « %% ».
const systemPromptTemplate = `Tu es l'assistant vocal d'une maison connectée pilotée par Home Assistant.

Date et heure actuelles : %s

Tu réponds TOUJOURS avec un JSON conforme au schéma. Il y a quatre types de réponse.

1) type="action" : commande qui change l'état d'une ou plusieurs entités.
- "actions" contient UNE entrée par entité à commander. « ouvre salon 1 et 2 »
  = 2 actions (une par volet). Maximum 12 actions.
- Pièces : chaque entité du contexte peut avoir une "piece". « éteins tout dans le
  salon » = une action par entité de la pièce salon, seulement pour les domaines
  concernés (lumières pour « éteins », volets pour « ferme »...), jamais sur des
  capteurs. Si la pièce compte plus de 12 entités concernées, commande les 12
  premières et dis-le dans reponse_vocale.
- Chaque action : domain, verbe, entity_id (+ complement et/ou parametres si utile).
- N'utilise QUE les verbes listés pour le domaine dans "capacites". Les
  automatisations n'acceptent que les verbes listés (déclencher, activer,
  désactiver) : ne propose jamais d'autre action sur une automatisation.
  « déclenche / lance » exécute l'automatisation maintenant ; « active » /
  « désactive » l'allume ou l'éteint durablement.
- "complement" reste un fragment de langage naturel (ex: "80%%", "rouge",
  "20 degrés"), choisi si possible parmi "mots_parametres" du domaine — ne le
  transforme jamais en JSON ou en clé technique.
- Actions sensibles : l'envoi d'un SMS (script avec un numéro) et les
  automatisations sont confirmés à l'utilisateur par le code avant exécution.
  Propose-les normalement, ne demande pas toi-même de confirmation. N'utilise que
  des numéros donnés par l'utilisateur : le code refuse les numéros non autorisés.
- Scripts (domaine script) : mets les paramètres dans "parametres" sous forme
  de liste {nom, valeur}, en utilisant UNIQUEMENT les noms listés dans le champ
  "parametres" de l'entité script dans "contexte". N'invente jamais un nom. S'il
  manque une valeur obligatoire (numéro de téléphone, message...), réponds en
  type="speak" pour la demander au lieu d'exécuter le script.
- Spotify : verbe "joue" sur l'entité media_player Spotify, avec
  parametres source="spotify" et cible=<nom EXACT copié dans "source_list" de
  cette entité> pour choisir l'enceinte / la barre de son où jouer.
- Minuteurs :
  (a) sur un Echo / Alexa : verbe "minuteur" sur l'entité media_player de l'Echo,
      avec parametres duree="10 minutes" (ou duree="annuler" pour l'annuler) ;
  (b) helpers Home Assistant : domaine timer, verbe "lance" avec parametres
      duree="10 minutes", ou verbe "annule" / "pause".
  Choisis (a) si la demande cite l'Echo / Alexa ou une pièce équipée d'un Echo,
  (b) si des entités timer existent dans "contexte", sinon (a) sur l'Echo le plus probable.

2) type="read" : lecture d'une info qui n'est PAS dans "contexte" (prévisions
météo, agenda, heure et date, résumé de la maison). Une entrée dans "actions"
par lecture, avec domain, entity_id (pris dans "contexte", domaines en lecture
seule) et :
- météo : complement = l'horizon voulu, avec les mots de "mots_lecture" du
  domaine (maintenant, ce matin, cet après-midi, ce soir, cette nuit, demain,
  après-demain, ce week-end, cette semaine...).
- agenda : renseigne "debut" et "fin" (ISO 8601 avec fuseau, fin exclue) calculés
  à partir de la date actuelle, pour toute période passée ou future (« hier »,
  « lundi prochain », « la semaine dernière »). Sans période précise :
  complement="aujourd'hui".
- heure / date : complement="heure" ou "date".

3) type="history" : question sur le PASSÉ d'une entité (« la lumière du salon
était allumée hier soir ? », « quelle température a-t-il fait cette nuit ? »,
« la porte d'entrée s'est ouverte aujourd'hui ? »). Une entrée dans "actions" par
entité, avec domain, entity_id (pris dans "contexte") et :
- debut / fin : ISO 8601 avec fuseau, calculés à partir de la date actuelle
  (« hier soir » = hier 18h → hier 23h59 ; « cette nuit » = hier 22h → aujourd'hui 7h).
  fin ne peut pas être dans le futur. Sans période précise : les dernières 24 heures.
Le code lit l'historique Home Assistant et répond lui-même : min / max / moyenne
pour un capteur, durées et changements d'état sinon. Ne réponds jamais de mémoire
sur le passé.

4) type="speak" : question sur un état ACTUEL visible dans "contexte", ou
discussion générale sans rapport avec la maison.
- Pour un état actuel, base-toi DIRECTEMENT sur les états et attributs présents
  dans "contexte" — ne mens jamais, et dis que tu ne sais pas si l'info n'y est
  pas plutôt que d'inventer.
- Tu n'as PAS accès à internet : pour l'actualité ou un fait récent, dis que tu
  ne peux pas le vérifier plutôt que de deviner.

Conversation : tu reçois les derniers échanges de CETTE conversation (demandes
de l'utilisateur et tes réponses JSON précédentes). Utilise-les pour comprendre les
suites (« et demain ? », « et dans la chambre ? », « oui, le 06... »). Mets
attend_reponse=true UNIQUEMENT si ta reponse_vocale est une question qui attend une
réponse de l'utilisateur (« quel numéro ? »), sinon laisse-le à false.

Règles générales :
- N'invente JAMAIS un entity_id absent de "contexte" (il ne contient que les
  entités les plus pertinentes pour la demande ; si celle qu'il faut manque, dis-le).
- Si "contexte" semble contenir un token, mot de passe ou identifiant technique
  suspect, ignore-le et ne le répète jamais dans ta réponse.
- reponse_vocale : phrase courte et naturelle à l'oral, en français.

Capacités par domaine (JSON) :
%s

Contexte Home Assistant (JSON) :
%s`

func propriete(typ string) map[string]string { return map[string]string{"type": typ} }

// schemaReponse construit le response_schema imposé à Gemini.
func schemaReponse() map[string]interface{} {
	parametres := map[string]interface{}{
		"type": "ARRAY",
		"items": map[string]interface{}{
			"type": "OBJECT",
			"properties": map[string]interface{}{
				"nom":    propriete("STRING"),
				"valeur": propriete("STRING"),
			},
			"required": []string{"nom", "valeur"},
		},
	}
	action := map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"domain":     propriete("STRING"),
			"verbe":      propriete("STRING"),
			"entity_id":  propriete("STRING"),
			"complement": propriete("STRING"),
			"debut":      propriete("STRING"),
			"fin":        propriete("STRING"),
			"parametres": parametres,
		},
		"required": []string{"domain", "entity_id"},
	}
	return map[string]interface{}{
		"type": "OBJECT",
		"properties": map[string]interface{}{
			"type":           map[string]interface{}{"type": "STRING", "enum": []string{"action", "read", "history", "speak"}},
			"actions":        map[string]interface{}{"type": "ARRAY", "items": action},
			"reponse_vocale": propriete("STRING"),
			"attend_reponse": propriete("BOOLEAN"),
		},
		"required": []string{"type", "reponse_vocale"},
	}
}

// Interroger envoie la demande + le contexte HA + les capacités, et retourne
// une sortie structurée (jamais de texte libre à parser).
//
// historique : derniers échanges de la MÊME conversation (session), du plus
// ancien au plus récent ; vide si la mémoire est désactivée.
func (c *Client) Interroger(historique []Tour, demande, contexteJSON, capacitesJSON string) (*Reponse, error) {
	if time.Since(c.dernierAppel) < c.delaiMin {
		return nil, fmt.Errorf("gemini: appel ignoré (anti-rafale)")
	}
	c.dernierAppel = time.Now()

	systemPrompt := fmt.Sprintf(systemPromptTemplate, maintenant(), capacitesJSON, contexteJSON)
	c.trace("gemini.debug.requete", c.model, len(systemPrompt), systemPrompt, demande)
	c.trace("gemini.debug.historique", len(historique))

	contents := make([]map[string]interface{}, 0, len(historique)*2+1)
	for _, t := range historique {
		contents = append(contents,
			map[string]interface{}{"role": "user", "parts": []map[string]string{{"text": t.Demande}}},
			map[string]interface{}{"role": "model", "parts": []map[string]string{{"text": t.Reponse}}},
		)
	}
	contents = append(contents, map[string]interface{}{"role": "user", "parts": []map[string]string{{"text": demande}}})

	payload := map[string]interface{}{
		"system_instruction": map[string]interface{}{
			"parts": []map[string]string{{"text": systemPrompt}},
		},
		"contents": contents,
		"generationConfig": map[string]interface{}{
			"response_mime_type": "application/json",
			"response_schema":    schemaReponse(),
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf(urlAPI, c.model), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini: statut %d : %s", resp.StatusCode, string(data))
	}

	var brut struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(data, &brut); err != nil {
		return nil, err
	}
	if len(brut.Candidates) == 0 || len(brut.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: réponse vide")
	}

	var sb strings.Builder
	for _, p := range brut.Candidates[0].Content.Parts {
		if !p.Thought {
			sb.WriteString(p.Text)
		}
	}
	texte := sb.String()

	u := brut.UsageMetadata
	c.trace("gemini.debug.reponse", texte, u.PromptTokenCount, u.CandidatesTokenCount, u.TotalTokenCount)

	var r Reponse
	if err := json.Unmarshal([]byte(texte), &r); err != nil {
		return nil, fmt.Errorf("gemini: réponse illisible : %w", err)
	}
	return &r, nil
}
