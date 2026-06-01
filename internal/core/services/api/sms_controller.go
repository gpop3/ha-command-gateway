package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// SMSController gère les routes HTTP liées aux SMS
type SMSController struct {
	service *SMSService
	apiKey  string
}

// NewSMSController crée un nouveau contrôleur SMS
func NewSMSController(service *SMSService, apiKey string) *SMSController {
	return &SMSController{service: service, apiKey: apiKey}
}

// envoyerSMSRequest représente le body de la requête POST /sms/send
type envoyerSMSRequest struct {
	Numero  string `json:"numero"`
	Message string `json:"message"`
}

// reponseJSON représente une réponse JSON standard
type reponseJSON struct {
	Succes  bool   `json:"succes"`
	Message string `json:"message,omitempty"`
	Erreur  string `json:"erreur,omitempty"`
}

// Register enregistre les routes sur le mux donné
func (c *SMSController) Register(mux *http.ServeMux) {
	mux.HandleFunc("/sms/send", c.authMiddleware(c.handleEnvoyerSMS))
}

// authMiddleware vérifie l'origine locale et la clé API
func (c *SMSController) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if !strings.HasPrefix(ip, "127.0.0.1") {
			writeJSON(w, http.StatusForbidden, reponseJSON{
				Succes: false,
				Erreur: "accès refusé",
			})
			return
		}

		if c.apiKey != "" {
			key := r.Header.Get("Authorization")
			if key != "Bearer "+c.apiKey {
				writeJSON(w, http.StatusUnauthorized, reponseJSON{
					Succes: false,
					Erreur: "clé API invalide",
				})
				return
			}
		}
		next(w, r)
	}
}

// handleEnvoyerSMS gère POST /sms/send
func (c *SMSController) handleEnvoyerSMS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, reponseJSON{
			Succes: false,
			Erreur: "méthode non autorisée",
		})
		return
	}

	var req envoyerSMSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, reponseJSON{
			Succes: false,
			Erreur: "body JSON invalide : " + err.Error(),
		})
		return
	}

	log.Printf("📤 [API] Envoi SMS → %s : %s", req.Numero, req.Message)

	if err := c.service.EnvoyerSMS(req.Numero, req.Message); err != nil {
		writeJSON(w, http.StatusInternalServerError, reponseJSON{
			Succes: false,
			Erreur: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, reponseJSON{
		Succes:  true,
		Message: "SMS envoyé à " + req.Numero,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}
