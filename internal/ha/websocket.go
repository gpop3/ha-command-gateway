package ha

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"ha-command-gateway/internal/logx"
)

// wsMessage représente un message JSON-RPC WebSocket HA
type wsMessage struct {
	ID          int             `json:"id,omitempty"`
	Type        string          `json:"type"`
	Domain      string          `json:"domain,omitempty"`
	Service     string          `json:"service,omitempty"`
	Target      interface{}     `json:"target,omitempty"`
	Data        interface{}     `json:"service_data,omitempty"`
	EventType   string          `json:"event_type,omitempty"`
	AccessToken string          `json:"access_token,omitempty"`
	Success     bool            `json:"success,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *wsError        `json:"error,omitempty"`
	Event       *wsEvent        `json:"event,omitempty"`
}

type wsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type wsEvent struct {
	EventType string      `json:"event_type"`
	Data      wsEventData `json:"data"`
}

type wsEventData struct {
	EntityID string       `json:"entity_id"`
	NewState *EtatComplet `json:"new_state"`
}

// wsClient gère la connexion WebSocket vers HA avec cache d'états
type wsClient struct {
	conn         *websocket.Conn
	mu           sync.Mutex
	counter      atomic.Int32
	pending      map[int]chan wsMessage
	pendingMu    sync.Mutex
	url          string
	token        string
	readyMu      sync.RWMutex
	ready        chan struct{} // fermé quand auth + cache prêts
	closed       bool
	timeout      time.Duration
	invalidCache atomic.Bool

	// Cache d'états mis à jour en temps réel
	stateCache   map[string]*EtatComplet
	stateCacheMu sync.RWMutex
}

var chanPool = sync.Pool{
	New: func() interface{} {
		return make(chan wsMessage, 1)
	},
}

// newWSClient crée et connecte un client WebSocket
func newWSClient(haURL, token string, timeoutClient time.Duration) (*wsClient, error) {
	wsURL := haURL
	if len(wsURL) >= 5 && wsURL[:5] == "https" {
		wsURL = "wss" + wsURL[5:] + "/api/websocket"
	} else {
		wsURL = "ws" + wsURL[4:] + "/api/websocket"
	}

	c := &wsClient{
		url:        wsURL,
		token:      token,
		pending:    make(map[int]chan wsMessage),
		ready:      make(chan struct{}),
		stateCache: make(map[string]*EtatComplet),
		timeout:    timeoutClient,
	}

	if err := c.connect(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *wsClient) connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return fmt.Errorf("websocket dial : %w", err)
	}
	c.conn = conn

	go c.readLoop()
	return nil
}

// readLoop lit les messages entrants et dispatch
func (c *wsClient) readLoop() {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if !c.closed {
				c.invalidCache.Store(false)
				logx.WarnT("ws.ws.connexion.perdue.reconnexion", err)
				go c.reconnect()
			}
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			logx.WarnT("ws.ws.message.invalide", err)
			continue
		}

		switch msg.Type {
		case "auth_required":
			c.authenticate()
		case "auth_ok":
			logx.InfoT("ws.ws.authentifie.chargement.des")
			// Réinitialiser le canal ready pour la reconnexion
			c.readyMu.Lock()
			c.ready = make(chan struct{})
			c.readyMu.Unlock()
			go c.chargerEtatsInitiaux()
		case "auth_invalid":
			logx.ErrorT("ws.ws.authentification.echouee.token")
			c.closed = true
			return
		case "result":
			c.pendingMu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.pendingMu.Unlock()
			if ok {
				ch <- msg
			}
		case "event":
			if msg.Event != nil && msg.Event.EventType == "state_changed" {
				if msg.Event.Data.NewState != nil {
					c.stateCacheMu.Lock()
					c.stateCache[msg.Event.Data.EntityID] = msg.Event.Data.NewState
					c.stateCacheMu.Unlock()
				}
			}
		}
	}
}

// chargerEtatsInitiaux envoie get_states directement sans passer par send()
func (c *wsClient) chargerEtatsInitiaux() {
	// 1. Envoyer get_states directement sur le socket
	id := int(c.counter.Add(1))
	ch := make(chan wsMessage, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	c.mu.Lock()
	err := c.conn.WriteJSON(wsMessage{ID: id, Type: "get_states"})
	c.mu.Unlock()

	if err != nil {
		logx.WarnT("ws.ws.get.states.envoi", err)
		c.invalidCache.Store(true)
		c.closeReady()
		return
	}

	// 2. Attendre la réponse
	select {
	case resp := <-ch:
		var etats []EtatComplet
		if err := json.Unmarshal(resp.Result, &etats); err == nil {
			c.stateCacheMu.Lock()
			for i := range etats {
				c.stateCache[etats[i].EntityID] = &etats[i]
			}
			c.stateCacheMu.Unlock()
			logx.InfoT("ws.ws.etats.charges.en", len(etats))
		} else {
			logx.WarnT("ws.ws.unmarshal.etats", err)
		}
	case <-time.After(c.timeout * time.Second):
		logx.WarnT("ws.ws.timeout.get.states")
	}

	// 3. S'abonner aux changements d'état
	subID := int(c.counter.Add(1))
	c.mu.Lock()
	err = c.conn.WriteJSON(wsMessage{
		ID:        subID,
		Type:      "subscribe_events",
		EventType: "state_changed",
	})
	c.mu.Unlock()
	if err != nil {
		logx.WarnT("ws.ws.subscribe.events", err)
	} else {
		logx.InfoT("ws.ws.abonne.aux.changements")
	}

	// 4. Débloquer les appels en attente
	c.closeReady()
}

// closeReady ferme le canal ready de façon sécurisée
func (c *wsClient) closeReady() {
	c.readyMu.RLock()
	ch := c.ready
	c.readyMu.RUnlock()

	select {
	case <-ch:
		// Déjà fermé
	default:
		close(ch)
	}
}

// GetState retourne l'état d'une entité depuis le cache
func (c *wsClient) GetState(entityID string) (*EtatComplet, bool) {
	if !c.invalidCache.Load() {
		return nil, false
	}
	c.stateCacheMu.RLock()
	defer c.stateCacheMu.RUnlock()
	etat, ok := c.stateCache[entityID]
	return etat, ok
}

// GetAllStates retourne tous les états du cache
func (c *wsClient) GetAllStates() []*EtatComplet {
	c.stateCacheMu.RLock()
	defer c.stateCacheMu.RUnlock()
	etats := make([]*EtatComplet, 0, len(c.stateCache))
	for _, e := range c.stateCache {
		etats = append(etats, e)
	}
	return etats
}

// WaitReady attend que le cache soit prêt
func (c *wsClient) WaitReady(timeout time.Duration) bool {
	c.readyMu.RLock()
	ch := c.ready
	c.readyMu.RUnlock()

	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (c *wsClient) authenticate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.WriteJSON(wsMessage{
		Type:        "auth",
		AccessToken: c.token,
	}); err != nil {
		logx.ErrorT("ws.ws.erreur.auth", err)
	}
}

func (c *wsClient) reconnect() {
	for {
		time.Sleep(5 * time.Second)
		logx.InfoT("ws.ws.tentative.de.reconnexion")
		if err := c.connect(); err != nil {
			logx.WarnT("ws.ws.reconnexion.echouee", err)
			continue
		}
		logx.InfoT("ws.ws.reconnecte")
		return
	}
}

// send envoie un message et attend la réponse
func (c *wsClient) send(msg wsMessage) (wsMessage, error) {
	c.readyMu.RLock()
	ch := c.ready
	c.readyMu.RUnlock()

	select {
	case <-ch:
	case <-time.After(15 * time.Second):
		return wsMessage{}, fmt.Errorf("timeout connexion WS")
	}

	id := int(c.counter.Add(1))
	msg.ID = id

	respCh := chanPool.Get().(chan wsMessage)
	defer func() {
		for len(respCh) > 0 {
			<-respCh
		}
		chanPool.Put(respCh)
	}()

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	c.mu.Lock()
	err := c.conn.WriteJSON(msg)
	c.mu.Unlock()

	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return wsMessage{}, fmt.Errorf("envoi WS : %w", err)
	}

	select {
	case resp := <-respCh:
		if !resp.Success && resp.Error != nil {
			return wsMessage{}, fmt.Errorf("WS %s : %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return wsMessage{}, fmt.Errorf("timeout WS id=%d", id)
	}
}

// CallService appelle un service HA via WebSocket
func (c *wsClient) CallService(domain, service string, target, data interface{}) error {
	_, err := c.send(wsMessage{
		Type:    "call_service",
		Domain:  domain,
		Service: service,
		Target:  target,
		Data:    data,
	})
	return err
}

// Close ferme la connexion WebSocket
func (c *wsClient) Close() {
	c.closed = true
	if c.conn != nil {
		_ = c.conn.Close()
	}
}
