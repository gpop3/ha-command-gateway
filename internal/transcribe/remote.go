package transcribe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"
)

// moteurRemote envoie l'audio à un endpoint Whisper HTTP (ex: faster-whisper sur Raspberry Pi)
type moteurRemote struct {
	url          string
	systemPrompt string
}

func (m *moteurRemote) Transcribe(wavData *bytes.Buffer) (string, time.Duration, error) {
	start := time.Now()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", 0, fmt.Errorf("remote: création du champ file : %w", err)
	}
	if _, err = io.Copy(part, wavData); err != nil {
		return "", 0, fmt.Errorf("remote: copie audio : %w", err)
	}

	_ = writer.WriteField("model", "Systran/faster-whisper-base")
	_ = writer.WriteField("language", "fr")
	_ = writer.WriteField("temperature", "0.0")
	_ = writer.WriteField("stream", "false")
	_ = writer.WriteField("vad_filter", "false")

	if m.systemPrompt != "" {
		_ = writer.WriteField("prompt", m.systemPrompt)
	}

	// Hotwords statiques pour la domotique — à externaliser dans Config si besoin
	motsCles := "Assistant,Allume,Eteins,Lumiere,Aspirateur,Volets,Temperature," +
		"Cuisine,Salon,Bureau,Chambre,Alarme,Rappel,Poubelle,Pergola,Machine,LaveVaisselle,Musique,Stop"
	_ = writer.WriteField("hotwords", motsCles)

	err = writer.Close()
	if err != nil {
		return "", 0, err
	}

	req, err := http.NewRequest("POST", m.url, body)
	if err != nil {
		return "", 0, fmt.Errorf("remote: création requête : %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("remote: envoi requête : %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("remote: lecture réponse : %w", err)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Printf("remote: réponse JSON invalide. Brut : %s", string(respBody))
		return "", 0, fmt.Errorf("remote: réponse invalide : %w", err)
	}

	return result.Text, time.Since(start), nil
}
