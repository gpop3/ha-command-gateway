package stt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"io"
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
		return "", 0, fmt.Errorf("%s : %w", i18n.T("erreur.stt.remote.file"), err)
	}
	if _, err = io.Copy(part, wavData); err != nil {
		return "", 0, fmt.Errorf("%s : %w", i18n.T("erreur.stt.remote.copie"), err)
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
		return "", 0, fmt.Errorf("%s : %w", i18n.T("erreur.stt.remote.requete"), err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("%s : %w", i18n.T("erreur.stt.remote.envoi"), err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logx.Error(err)
		}
	}(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("%s : %w", i18n.T("erreur.stt.remote.lecture"), err)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		logx.InfoT("stt.remote.reponse.json.invalide", string(respBody))
		return "", 0, fmt.Errorf("%s : %w", i18n.T("erreur.stt.remote.invalide"), err)
	}

	return result.Text, time.Since(start), nil
}
