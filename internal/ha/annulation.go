package ha

import (
	"fmt"
)

// « Annule ça » / « remets comme avant » : avant chaque action commandée par l'IA, l'état
// précédent des entités concernées est mémorisé (EtatSauve) ; l'annulation le rétablit.
// Fonctionne pour les domaines « d'état » ci-dessous. Un SMS envoyé, un script ou une
// automatisation exécutés ne sont PAS annulables.

var domainesAnnulables = map[string]bool{
	"light": true, "switch": true, "input_boolean": true, "fan": true,
	"cover": true, "climate": true, "media_player": true, "automation": true,
}

// DomaineAnnulable indique si les actions sur ce domaine peuvent être annulées.
func DomaineAnnulable(domaine string) bool { return domainesAnnulables[domaine] }

// EtatSauve est l'état d'une entité avant une action.
type EtatSauve struct {
	EntityID  string
	Domaine   string
	Nom       string
	Etat      string
	Attributs map[string]interface{}
}

// SauvegarderEtat mémorise l'état actuel d'une entité (nil si son domaine n'est pas annulable).
func (c *Client) SauvegarderEtat(app Appareil) (*EtatSauve, error) {
	if !domainesAnnulables[app.Domain] {
		return nil, nil
	}
	e, err := c.etatBrut(app.EntityID)
	if err != nil {
		return nil, err
	}
	nom := app.FriendlyNameExact
	if nom == "" {
		nom = app.FriendlyName
	}
	return &EtatSauve{EntityID: app.EntityID, Domaine: app.Domain, Nom: nom, Etat: e.State, Attributs: e.Attributes}, nil
}

func nombreAttr(v interface{}) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// Restaurer remet une entité dans l'état mémorisé.
func (c *Client) Restaurer(e EtatSauve) error {
	id := e.EntityID
	appel := func(service string, data map[string]interface{}) error {
		return c.AppelerService(e.Domaine, service, id, data)
	}
	if e.Etat == "unavailable" || e.Etat == "unknown" {
		return fmt.Errorf("état d'origine inconnu")
	}

	switch e.Domaine {
	case "switch", "input_boolean", "automation":
		if e.Etat == "on" {
			return appel("turn_on", nil)
		}
		return appel("turn_off", nil)

	case "light":
		if e.Etat != "on" {
			return appel("turn_off", nil)
		}
		data := map[string]interface{}{}
		if b, ok := nombreAttr(e.Attributs["brightness"]); ok {
			data["brightness"] = int(b)
		}
		switch mode, _ := e.Attributs["color_mode"].(string); mode {
		case "color_temp":
			if k, ok := nombreAttr(e.Attributs["color_temp_kelvin"]); ok {
				data["color_temp_kelvin"] = int(k)
			}
		case "hs", "rgb", "rgbw", "rgbww", "xy":
			if rgb, ok := e.Attributs["rgb_color"].([]interface{}); ok && len(rgb) == 3 {
				data["rgb_color"] = rgb
			}
		}
		return appel("turn_on", data)

	case "fan":
		if e.Etat != "on" {
			return appel("turn_off", nil)
		}
		data := map[string]interface{}{}
		if p, ok := nombreAttr(e.Attributs["percentage"]); ok && p > 0 {
			data["percentage"] = int(p)
		}
		return appel("turn_on", data)

	case "cover":
		if pos, ok := nombreAttr(e.Attributs["current_position"]); ok {
			return appel("set_cover_position", map[string]interface{}{"position": int(pos)})
		}
		if e.Etat == "open" || e.Etat == "opening" {
			return appel("open_cover", nil)
		}
		return appel("close_cover", nil)

	case "climate":
		if err := appel("set_hvac_mode", map[string]interface{}{"hvac_mode": e.Etat}); err != nil {
			return err
		}
		if t, ok := nombreAttr(e.Attributs["temperature"]); ok && e.Etat != "off" {
			return appel("set_temperature", map[string]interface{}{"temperature": t})
		}
		return nil

	case "media_player":
		if v, ok := nombreAttr(e.Attributs["volume_level"]); ok {
			if err := appel("volume_set", map[string]interface{}{"volume_level": v}); err != nil {
				return err
			}
		}
		switch e.Etat {
		case "off":
			return appel("turn_off", nil)
		case "paused":
			return appel("media_pause", nil)
		case "playing":
			return appel("media_play", nil)
		}
		return nil
	}
	return fmt.Errorf("domaine %q non annulable", e.Domaine)
}
