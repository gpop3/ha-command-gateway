//go:build !windows

package voice

import "encoding/json"

// ExtraireTexteVosk parse la réponse JSON de Vosk et retourne le texte reconnu
func ExtraireTexteVosk(jsonBrut string) string {
	var result struct {
		Text         string `json:"text"`
		Alternatives []struct {
			Text string `json:"text"`
		} `json:"alternatives"`
	}
	if err := json.Unmarshal([]byte(jsonBrut), &result); err != nil {
		return ""
	}
	if result.Text != "" {
		return result.Text
	}
	if len(result.Alternatives) > 0 {
		return result.Alternatives[0].Text
	}
	return ""
}
