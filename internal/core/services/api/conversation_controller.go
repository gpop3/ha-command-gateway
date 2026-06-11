package api

import (
	"encoding/json"
	"ha-command-gateway/internal/i18n"
	"net/http"
	"strings"

	"ha-command-gateway/internal/logx"
)

// ConversationController expose POST /conversation pour l'agent conversationnel
type ConversationController struct {
	service *ConversationService
	apiKey  string
}

// NewConversationController crée le contrôleur de conversation.
func NewConversationController(service *ConversationService, apiKey string) *ConversationController {
	return &ConversationController{service: service, apiKey: apiKey}
}

// Register enregistre la route sur le mux donné.
func (c *ConversationController) Register(mux *http.ServeMux) {
	mux.HandleFunc("/conversation", c.authMiddleware(c.handleConversation))
}

// authMiddleware
func (c *ConversationController) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if !strings.HasPrefix(ip, "127.0.0.1") {
			writeJSON(w, http.StatusForbidden, reponseJSON{
				Succes: false,
				Erreur: i18n.T("api.acces.refuse"),
			})
			return
		}

		if c.apiKey != "" {
			key := r.Header.Get("Authorization")
			if key != "Bearer "+c.apiKey {
				writeJSON(w, http.StatusUnauthorized, reponseJSON{
					Succes: false,
					Erreur: i18n.T("api.cle.invalide"),
				})
				return
			}
		}
		next(w, r)
	}
}

// conversationRequest est le body envoyé par l'agent HA.
type conversationRequest struct {
	Text           string `json:"text"`
	Language       string `json:"language,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
}

// conversationResponse est ce que l'agent HA attend en retour.
type conversationResponse struct {
	Speech               string `json:"speech"`
	Handled              bool   `json:"handled"`
	Verbe                string `json:"verbe,omitempty"`
	Appareil             string `json:"appareil,omitempty"`
	ContinueConversation bool   `json:"continue_conversation"`
}

// handleConversation gère POST /conversation.
func (c *ConversationController) handleConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, reponseJSON{Succes: false, Erreur: "méthode non autorisée"})
		return
	}

	var req conversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeJSON(w, http.StatusBadRequest, reponseJSON{Succes: false, Erreur: "texte manquant"})
		return
	}

	logx.InfoT("conversation.recue", req.Text)

	res := c.service.Traiter(req.Text)

	resp := conversationResponse{
		Speech:   res.Speech,
		Handled:  res.Handled,
		Verbe:    res.Verbe,
		Appareil: res.Appareil,
	}

	writeJSON(w, http.StatusOK, resp)
}
