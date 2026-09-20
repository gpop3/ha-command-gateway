package ha

import (
	"bytes"
	"encoding/json"
	"fmt"
	"ha-command-gateway/config"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/conversion"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client est le client HTTP vers Home Assistant
type Client struct {
	url     string
	token   string
	http    *http.Client
	ws      *wsClient
	timeout time.Duration
}

var pieces []Piece

// NewClient crée un nouveau client HA et enregistre tous les services built-in
func NewClient(url, token string, piecesEnv string, timeoutClient time.Duration, cfg *config.Config) *Client {
	c := &Client{
		url:     url,
		token:   token,
		http:    &http.Client{Timeout: timeoutClient * time.Second},
		timeout: timeoutClient,
	}

	ws, err := newWSClient(url, token, timeoutClient)
	if err != nil {
		logx.WarnT("ha.ws.websocket.indisponible.fallback", err)
	} else {
		c.ws = ws
	}

	Register(NewServiceLight(c))
	Register(NewServiceCover(c))
	Register(NewServiceClimate(c))
	Register(NewServiceSwitch(c))
	Register(NewServiceMediaPlayer(c))
	Register(NewServiceVacuum(c))
	Register(NewServiceAlarm(c))
	Register(NewServiceFan(c))
	Register(NewServiceLock(c))
	Register(NewServiceScene(c))
	Register(NewServiceScript(c))
	Register(NewServiceTodo(c))
	Register(NewServiceShoppingList(c))
	Register(NewServiceAutomation(c))
	Register(NewServiceInputBoolean(c))
	Register(NewServiceCamera(c))
	Register(newServiceDefault(c))
	Register(NewServiceResumeMaison(c))
	Register(NewServiceTime(c))
	Register(NewServiceTimer(c))
	Register(NewServiceBriefing(c))
	Register(NewServiceAgenda(c))
	Register(NewServiceWeather(c))

	if err := LoadServicesFromFile(cfg.ServicesFile, c); err != nil {
		logx.WarnT("ha.services.yaml", err)
	}

	if err := LoadPlugins("plugins/", c); err != nil {
		logx.WarnT("ha.plugins", err)
	}

	pieces = ParserPieces(piecesEnv)

	return c
}

// AttendreWS attente du WS
func (c *Client) AttendreWS() {
	if c.ws == nil {
		return
	}
	if !c.ws.WaitReady(c.timeout * time.Second) {
		logx.WarnT("ha.ws.timeout.attente.cache")
	}
}

// ---- Helpers HTTP ----

func (c *Client) get(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.url+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logx.Error(err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", i18n.T("erreur.ha.reponse.get", resp.StatusCode, path))
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) post(path string, payload interface{}) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.url+path, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", i18n.T("erreur.ha.reponse.post", resp.StatusCode, path))
	}
	return body, nil
}

// ---- API publique ----

// RecupererEntites retourne toutes les entités de Home Assistant
func (c *Client) RecupererEntites() ([]Appareil, error) {
	if c.ws != nil {
		etats := c.ws.GetAllStates()
		if len(etats) > 0 {
			var appareils []Appareil
			for _, e := range etats {
				if e.Attributes.FriendlyName == "" {
					continue
				}
				appareils = append(appareils, Appareil{
					EntityID:          e.EntityID,
					FriendlyName:      normaliserNom(e.Attributes.FriendlyName),
					FriendlyNameExact: e.Attributes.FriendlyName,
					State:             e.State,
					Domain:            strings.Split(e.EntityID, ".")[0],
				})
			}
			return appareils, nil
		}
	}

	body, err := c.get("/api/states")
	if err != nil {
		return nil, err
	}

	var raw []entiteRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	var appareils []Appareil
	for _, e := range raw {
		if e.Attributes.FriendlyName == "" {
			continue
		}
		appareils = append(appareils, Appareil{
			EntityID:          e.EntityID,
			FriendlyName:      normaliserNom(e.Attributes.FriendlyName),
			FriendlyNameExact: e.Attributes.FriendlyName,
			State:             e.State,
			Domain:            strings.Split(e.EntityID, ".")[0],
		})
	}
	return appareils, nil
}

// RecupererEtatLive retourne l'état temps réel d'une entité
func (c *Client) RecupererEtatLive(entityID string) (*EtatComplet, error) {
	if c.ws != nil {
		if etat, ok := c.ws.GetState(entityID); ok {
			return etat, nil
		}
	}

	body, err := c.get("/api/states/" + entityID)
	if err != nil {
		return nil, err
	}

	var etat EtatComplet
	if err := json.Unmarshal(body, &etat); err != nil {
		return nil, err
	}
	return &etat, nil
}

// RecupererHistorique retourne l'état d'une entité à un instant passé
func (c *Client) RecupererHistorique(entityID string, dateCible time.Time) (*EtatComplet, error) {
	// pas de websocket pour ce service
	dateUTC := dateCible.UTC().Format("2006-01-02T15:04:05Z")
	path := fmt.Sprintf("/api/history/period/%s?filter_entity_id=%s&end_time=%s",
		dateUTC, entityID, dateUTC)

	body, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var historique [][]EtatComplet
	if err := json.Unmarshal(body, &historique); err != nil {
		return nil, err
	}

	if len(historique) == 0 || len(historique[0]) == 0 {
		return nil, fmt.Errorf("%s", i18n.T("erreur.aucun.historique", entityID, dateCible.Format("15h04")))
	}

	etat := historique[0][0]
	return &etat, nil
}

// GetPieces recuperation des pieces
func (c *Client) GetPieces() []Piece {
	return pieces
}

// ---- Helpers internes ----

func ParserPieces(input string) []Piece {
	noms := strings.Split(input, ",")
	var result []Piece
	for _, n := range noms {
		n = strings.TrimSpace(n)
		if n != "" {
			result = append(result, Piece{Name: n, ID: n}) // ID et Name identiques pour simplifier
		}
	}
	return result
}

func normaliserNom(input string) string {
	return conversion.ChiffreVersLettre(input)
}

// AppelerService appelle un service HA sur une entité (WebSocket, repli HTTP).
func (c *Client) AppelerService(domaine, service, entityID string, data map[string]interface{}) error {
	if data == nil {
		data = map[string]interface{}{}
	}
	if c.ws != nil {
		target := map[string]interface{}{"entity_id": entityID}
		if err := c.ws.CallService(domaine, service, target, data); err == nil {
			return nil
		} else {
			logx.WarnT("ha.ws.callservice.echoue.fallback", err)
		}
	}
	payload := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		payload[k] = v
	}
	payload["entity_id"] = entityID
	_, err := c.post(fmt.Sprintf("/api/services/%s/%s", domaine, service), payload)
	return err
}

// EtatBrut : un état HA avec tous ses attributs.
type EtatBrut struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	LastChanged string                 `json:"last_changed"`
	Attributes  map[string]interface{} `json:"attributes"`
}

// etatsBruts lit tous les états (REST) avec leurs attributs.
func (c *Client) etatsBruts() ([]EtatBrut, error) {
	body, err := c.get("/api/states")
	if err != nil {
		return nil, err
	}
	var out []EtatBrut
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// etatBrut lit l'état complet d'UNE entité.
func (c *Client) etatBrut(entityID string) (*EtatBrut, error) {
	body, err := c.get("/api/states/" + entityID)
	if err != nil {
		return nil, err
	}
	var e EtatBrut
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// EtatWebsocket décrit la connexion WebSocket : « absent », « connexion » ou « prêt ».
func (c *Client) EtatWebsocket() string {
	if c.ws == nil {
		return "absent"
	}
	c.ws.readyMu.RLock()
	ch := c.ws.ready
	c.ws.readyMu.RUnlock()
	select {
	case <-ch:
		return "prêt"
	default:
		return "connexion"
	}
}
