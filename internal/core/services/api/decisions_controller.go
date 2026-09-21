package api

import (
	"net/http"
	"strconv"
	"strings"

	"ha-command-gateway/internal/nlp"
)

// DecisionsController expose le journal des décisions (accès local, clé API si définie) :
//   - GET  /decisions?n=50&faux=1 : derniers échanges (phrase, moteur, type, actions, résultat,
//     tokens, durée, mode ombre), du plus récent au plus ancien ; faux=1 = seulement les erreurs ;
//   - POST /decisions/faux?id=12 : marque un échange comme erroné (comme un « non, pas ça »).
type DecisionsController struct {
	analyseur *nlp.Analyseur
	apiKey    string
}

func NewDecisionsController(analyseur *nlp.Analyseur, apiKey string) *DecisionsController {
	return &DecisionsController{analyseur: analyseur, apiKey: apiKey}
}

func (c *DecisionsController) Register(mux *http.ServeMux) {
	mux.HandleFunc("/decisions", c.handleListe)
	mux.HandleFunc("/decisions/faux", c.handleFaux)
}

func (c *DecisionsController) autorise(w http.ResponseWriter, r *http.Request) bool {
	if !local(r) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"status": "interdit"})
		return false
	}
	if c.apiKey != "" && r.Header.Get("Authorization") != "Bearer "+c.apiKey {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"status": "cle invalide"})
		return false
	}
	return true
}

func (c *DecisionsController) handleListe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"status": "GET attendu"})
		return
	}
	if !c.autorise(w, r) {
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	faux := strings.EqualFold(r.URL.Query().Get("faux"), "1") || strings.EqualFold(r.URL.Query().Get("faux"), "true")
	liste := c.analyseur.Decisions(n, faux)
	writeJSON(w, http.StatusOK, map[string]interface{}{"nombre": len(liste), "decisions": liste})
}

func (c *DecisionsController) handleFaux(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"status": "POST attendu"})
		return
	}
	if !c.autorise(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || !c.analyseur.MarquerDecisionFausse(id) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"status": "decision introuvable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "id": id, "faux": true})
}
