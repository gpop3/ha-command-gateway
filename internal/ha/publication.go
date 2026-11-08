package ha

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ---- Publication dans Home Assistant ----

// PublierEtat crée ou met à jour un état (sensor.*, binary_sensor.*) via l'API REST de HA.
// Ces états ne sont pas des entités de l'interface (pas d'unique_id) : ils disparaissent
// au redémarrage de HA jusqu'à la prochaine publication, d'où la republication périodique.
func (c *Client) PublierEtat(entityID, etat string, attributs map[string]interface{}) error {
	if attributs == nil {
		attributs = map[string]interface{}{}
	}
	_, err := c.post("/api/states/"+entityID, map[string]interface{}{"state": etat, "attributes": attributs})
	return err
}

// PublierEvenement déclenche un événement sur le bus d'événements de HA (utilisable comme
// déclencheur d'automatisation : « event » avec ce type).
func (c *Client) PublierEvenement(typeEvenement string, donnees map[string]interface{}) error {
	if donnees == nil {
		donnees = map[string]interface{}{}
	}
	_, err := c.post("/api/events/"+typeEvenement, donnees)
	return err
}

// ---- Notifications mobiles ----

// ServicesMobiles liste les services notify.mobile_app_* (application mobile HA).
func (c *Client) ServicesMobiles() []string {
	body, err := c.get("/api/services")
	if err != nil {
		return nil
	}
	var domaines []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if err := json.Unmarshal(body, &domaines); err != nil {
		return nil
	}
	var out []string
	for _, d := range domaines {
		if d.Domain != "notify" {
			continue
		}
		for nom := range d.Services {
			if strings.HasPrefix(nom, "mobile_app_") {
				out = append(out, nom)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Notifier envoie une notification via un service notify.<service>.
func (c *Client) Notifier(service, titre, message string) error {
	service = strings.TrimPrefix(strings.TrimSpace(service), "notify.")
	if service == "" {
		return fmt.Errorf("aucun service de notification")
	}
	data := map[string]interface{}{"message": message}
	if titre != "" {
		data["title"] = titre
	}
	if c.ws != nil {
		if err := c.ws.CallService("notify", service, nil, data); err == nil {
			return nil
		}
	}
	_, err := c.post("/api/services/notify/"+service, data)
	return err
}
