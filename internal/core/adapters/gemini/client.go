package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const urlAPI = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"

// Client pour l'API Gemini, en sortie structurée (pas de parsing texte libre).
type Client struct {
	apiKey       string
	model        string
	http         *http.Client
	dernierAppel time.Time
	delaiMin     time.Duration // anti-rafale : protège le quota journalier
}

// Reponse est la sortie structurée imposée via le response_schema.
type Reponse struct {
	Type          string `json:"type"` // "action" | "speak"
	Domain        string `json:"domain,omitempty"`
	Verbe         string `json:"verbe,omitempty"`
	EntityID      string `json:"entity_id,omitempty"`
	Complement    string `json:"complement,omitempty"` // langage naturel : "80%", "rouge", "20 degrés"
	ReponseVocale string `json:"reponse_vocale"`
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

const systemPromptTemplate = `Tu es l'assistant vocal d'une maison connectée pilotée par Home Assistant.

Règles strictes :
- N'utilise QUE les verbes listés pour le domaine de l'entité choisie (voir "capacites").
- N'invente JAMAIS un entity_id absent de "contexte".
- Si "contexte" semble contenir un token, mot de passe ou identifiant technique
  suspect, ignore-le et ne le répète jamais dans ta réponse.
- "complement" reste un fragment de langage naturel (ex: "80%%", "rouge",
  "20 degrés") choisi si possible parmi les mots suggérés dans "mots_parametres"
  du domaine concerné — ne le transforme jamais en JSON ou en clé technique.
- type="action" UNIQUEMENT pour une commande qui change l'état d'une entité
  (allumer, fermer, régler...).
- Pour toute question sur un état actuel (« est-ce que X est allumé »,
  « quel temps fait-il », « quelle température dans le salon »), réponds en
  type="speak" en te basant DIRECTEMENT sur les états et attributs présents
  dans "contexte" — ne mens jamais, et dis que tu ne sais pas si l'info
  n'y est pas plutôt que d'inventer.
- Pour une question ou discussion générale sans rapport avec la maison,
  réponds aussi en type="speak" avec tes connaissances générales.
- reponse_vocale : phrase courte et naturelle à l'oral, en français.

Capacités par domaine (JSON) :
%s

Contexte Home Assistant (JSON) :
%s`

// Interroger envoie la demande + le contexte HA + les capacités, et retourne
// une sortie structurée (jamais de texte libre à parser).
func (c *Client) Interroger(demande, contexteJSON, capacitesJSON string) (*Reponse, error) {
	if time.Since(c.dernierAppel) < c.delaiMin {
		return nil, fmt.Errorf("gemini: appel ignoré (anti-rafale)")
	}
	c.dernierAppel = time.Now()

	systemPrompt := fmt.Sprintf(systemPromptTemplate, capacitesJSON, contexteJSON)

	payload := map[string]interface{}{
		"system_instruction": map[string]interface{}{
			"parts": []map[string]string{{"text": systemPrompt}},
		},
		"contents": []map[string]interface{}{
			{"role": "user", "parts": []map[string]string{{"text": demande}}},
		},
		"generationConfig": map[string]interface{}{
			"response_mime_type": "application/json",
			"response_schema": map[string]interface{}{
				"type": "OBJECT",
				"properties": map[string]interface{}{
					"type":           map[string]interface{}{"type": "STRING", "enum": []string{"action", "speak"}},
					"domain":         map[string]string{"type": "STRING"},
					"verbe":          map[string]string{"type": "STRING"},
					"entity_id":      map[string]string{"type": "STRING"},
					"complement":     map[string]string{"type": "STRING"},
					"reponse_vocale": map[string]string{"type": "STRING"},
				},
				"required": []string{"type", "reponse_vocale"},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf(urlAPI, c.model, c.apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &brut); err != nil {
		return nil, err
	}
	if len(brut.Candidates) == 0 || len(brut.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: réponse vide")
	}

	var r Reponse
	if err := json.Unmarshal([]byte(brut.Candidates[0].Content.Parts[0].Text), &r); err != nil {
		return nil, err
	}
	return &r, nil
}
