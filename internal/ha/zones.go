package ha

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"ha-command-gateway/internal/logx"
)

// Registre des pièces (areas) de Home Assistant : permet de rattacher chaque
// entité à sa pièce (« éteins tout dans le salon »). Nécessite le WebSocket et un
// token d'administrateur (les commandes config/*_registry/list sont réservées aux admins).
// Si le registre est indisponible, l'assistant fonctionne sans information de pièce.

var cacheZones struct {
	sync.Mutex
	parEntite map[string]string // entity_id → nom de la pièce
	maj       time.Time
	echec     time.Time
}

const (
	dureeCacheZones      = 30 * time.Minute
	delaiReessaiZones    = 5 * time.Minute
	commandeAreaRegistry = "config/area_registry/list"
	commandeDeviceRegist = "config/device_registry/list"
	commandeEntityRegist = "config/entity_registry/list"
)

// commandeWS envoie une commande WebSocket HA et décode son résultat dans dest.
func (c *Client) commandeWS(typ string, dest interface{}) error {
	if c.ws == nil {
		return fmt.Errorf("websocket HA indisponible")
	}
	resp, err := c.ws.send(wsMessage{Type: typ})
	if err != nil {
		return err
	}
	return json.Unmarshal(resp.Result, dest)
}

// ZonesEntites retourne, pour chaque entité rattachée à une pièce, le nom de cette
// pièce. Mis en cache 30 minutes ; en cas d'échec, on ne réessaie pas avant 5 minutes.
func (c *Client) ZonesEntites() map[string]string {
	cacheZones.Lock()
	defer cacheZones.Unlock()

	if cacheZones.parEntite != nil && time.Since(cacheZones.maj) < dureeCacheZones {
		return cacheZones.parEntite
	}
	if !cacheZones.echec.IsZero() && time.Since(cacheZones.echec) < delaiReessaiZones {
		return cacheZones.parEntite
	}

	res, err := c.chargerZones()
	if err != nil {
		logx.WarnT("zones.erreur", err)
		cacheZones.echec = time.Now()
		return cacheZones.parEntite
	}
	cacheZones.parEntite = res
	cacheZones.maj = time.Now()
	cacheZones.echec = time.Time{}
	return res
}

func (c *Client) chargerZones() (map[string]string, error) {
	var zones []struct {
		AreaID string `json:"area_id"`
		Name   string `json:"name"`
	}
	if err := c.commandeWS(commandeAreaRegistry, &zones); err != nil {
		return nil, err
	}
	nomsZones := make(map[string]string, len(zones))
	for _, z := range zones {
		nomsZones[z.AreaID] = z.Name
	}

	var appareils []struct {
		ID     string `json:"id"`
		AreaID string `json:"area_id"`
	}
	if err := c.commandeWS(commandeDeviceRegist, &appareils); err != nil {
		return nil, err
	}
	zoneAppareil := make(map[string]string, len(appareils))
	for _, a := range appareils {
		if a.AreaID != "" {
			zoneAppareil[a.ID] = a.AreaID
		}
	}

	var entites []struct {
		EntityID string `json:"entity_id"`
		AreaID   string `json:"area_id"`
		DeviceID string `json:"device_id"`
	}
	if err := c.commandeWS(commandeEntityRegist, &entites); err != nil {
		return nil, err
	}

	res := make(map[string]string, len(entites))
	for _, e := range entites {
		// La pièce de l'entité prime ; sinon celle de son appareil
		area := e.AreaID
		if area == "" && e.DeviceID != "" {
			area = zoneAppareil[e.DeviceID]
		}
		if nom := nomsZones[area]; nom != "" {
			res[e.EntityID] = nom
		}
	}
	return res, nil
}
