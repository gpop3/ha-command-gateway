package api

import (
	"net/http"
	"strings"
	"time"

	"ha-command-gateway/internal/core"
)

// StatusController expose deux routes de supervision :
//   - GET /health : sonde de VIE. 200 si la boucle de traitement (bus) répond, 503 sinon.
//     Sans authentification, mais réservée à 127.0.0.1 (healthcheck Docker, Uptime Kuma...).
//   - GET /status : état détaillé (IA, HA, mémoire, version). Local + clé API si définie.
type StatusController struct {
	bus     *core.Bus
	apiKey  string
	statut  func() map[string]interface{}
	demarre time.Time
}

// NewStatusController crée le contrôleur ; statut fournit les détails de /status.
func NewStatusController(bus *core.Bus, apiKey string, statut func() map[string]interface{}) *StatusController {
	return &StatusController{bus: bus, apiKey: apiKey, statut: statut, demarre: time.Now()}
}

func (c *StatusController) Register(mux *http.ServeMux) {
	mux.HandleFunc("/health", c.handleHealth)
	mux.HandleFunc("/status", c.handleStatus)
}

func local(r *http.Request) bool {
	return strings.HasPrefix(r.RemoteAddr, "127.0.0.1") || strings.HasPrefix(r.RemoteAddr, "[::1]")
}

func (c *StatusController) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"status": "interdit"})
		return
	}
	vivant := c.bus == nil || c.bus.Ping(2*time.Second)
	code, etat := http.StatusOK, "ok"
	if !vivant {
		code, etat = http.StatusServiceUnavailable, "bloque"
	}
	writeJSON(w, code, map[string]interface{}{
		"status":    etat,
		"uptime_s":  int(time.Since(c.demarre).Seconds()),
		"bus_actif": vivant,
	})
}

func (c *StatusController) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"status": "interdit"})
		return
	}
	if c.apiKey != "" && r.Header.Get("Authorization") != "Bearer "+c.apiKey {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"status": "cle invalide"})
		return
	}
	res := map[string]interface{}{
		"uptime_s":  int(time.Since(c.demarre).Seconds()),
		"bus_actif": c.bus == nil || c.bus.Ping(2*time.Second),
	}
	if c.statut != nil {
		for k, v := range c.statut() {
			res[k] = v
		}
	}
	writeJSON(w, http.StatusOK, res)
}
