package ha

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
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
	conn      *websocket.Conn
	mu        sync.Mutex
	counter   atomic.Int32
	pending   map[int]chan wsMessage
	pendingMu sync.Mutex
	url       string
	token     string
	ready     chan struct{}
	closed    bool
	timeout   time.Duration

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
	c.ready = make(chan struct{})

	go c.readLoop()
	return nil
}

// readLoop lit les messages entrants et dispatch
func (c *wsClient) readLoop() {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if !c.closed {
				log.Printf("⚠️ [WS] connexion perdue : %v — reconnexion...", err)
				go c.reconnect()
			}
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("⚠️ [WS] message invalide : %v", err)
			continue
		}

		switch msg.Type {
		case "auth_required":
			c.authenticate()
		case "auth_ok":
			log.Printf("✅ [WS] authentifié — chargement des états...")
			// Charger les états directement sans passer par send()
			go c.chargerEtatsInitiaux()
		case "auth_invalid":
			log.Printf("❌ [WS] authentification échouée")
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

// chargerEtatsInitiaux envoie get_states directement (sans send()) pour éviter le deadlock
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
		log.Printf("⚠️ [WS] get_states envoi : %v", err)
		close(c.ready)
		return
	}

	// 2. Attendre la réponse avec un timeout généreux
	select {
	case resp := <-ch:
		var etats []EtatComplet
		if err := json.Unmarshal(resp.Result, &etats); err == nil {
			c.stateCacheMu.Lock()
			for i := range etats {
				c.stateCache[etats[i].EntityID] = &etats[i]
			}
			c.stateCacheMu.Unlock()
			log.Printf("✅ [WS] %d états chargés en cache", len(etats))
		} else {
			log.Printf("⚠️ [WS] unmarshal états : %v", err)
		}
	case <-time.After(c.timeout * time.Second):
		log.Printf("⚠️ [WS] timeout get_states — cache vide")
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
		log.Printf("⚠️ [WS] subscribe_events : %v", err)
	} else {
		log.Printf("✅ [WS] abonné aux changements d'état")
	}

	// 4. Cache prêt — débloquer les appels en attente
	close(c.ready)
}

// GetState retourne l'état d'une entité depuis le cache
func (c *wsClient) GetState(entityID string) (*EtatComplet, bool) {
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

func (c *wsClient) authenticate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.WriteJSON(wsMessage{
		Type:        "auth",
		AccessToken: c.token,
	}); err != nil {
		log.Printf("❌ [WS] erreur auth : %v", err)
	}
}

func (c *wsClient) reconnect() {
	for {
		time.Sleep(5 * time.Second)
		log.Printf("🔄 [WS] tentative de reconnexion...")
		if err := c.connect(); err != nil {
			log.Printf("⚠️ [WS] reconnexion échouée : %v", err)
			continue
		}
		go c.chargerEtatsInitiaux()
		log.Printf("✅ [WS] reconnecté")
		return
	}
}

// send envoie un message et attend la réponse
func (c *wsClient) send(msg wsMessage) (wsMessage, error) {
	select {
	case <-c.ready:
	case <-time.After(c.timeout * time.Second):
		return wsMessage{}, fmt.Errorf("timeout connexion WS")
	}

	id := int(c.counter.Add(1))
	msg.ID = id

	ch := chanPool.Get().(chan wsMessage)
	defer func() {
		// Vider le canal avant de le remettre dans le pool
		for len(ch) > 0 {
			<-ch
		}
		chanPool.Put(ch)
	}()

	c.pendingMu.Lock()
	c.pending[id] = ch
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
	case resp := <-ch:
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
