package ha

import "encoding/json"

// domainesExclus : jamais envoyés à l'IA (présence, sécurité, accès)
var domainesExclus = map[string]bool{
	"person":              true,
	"device_tracker":      true,
	"camera":              true,
	"lock":                true,
	"alarm_control_panel": true,
	"update":              true,
	"automation":          true,
}

// attributsWhitelist : par domaine, les seuls attributs (au-delà de l'état
// brut) transmis à l'IA. Tout ce qui n'est pas listé ici est systématiquement
// exclu — jamais de dump brut des attributs HA (pas de risque de token/URL/MAC).
var attributsWhitelist = map[string][]string{
	"weather": {"temperature", "humidity", "wind_speed", "pressure"},
	"climate": {"current_temperature", "temperature", "hvac_action"},
	"sensor":  {"unit_of_measurement", "device_class"},
	"cover":   {"current_position"},
	"fan":     {"percentage"},
	"light":   {"brightness"},
}

type etatRawPourContexte struct {
	EntityID   string                 `json:"entity_id"`
	State      string                 `json:"state"`
	Attributes map[string]interface{} `json:"attributes"`
}

// EntiteContexte est la forme whitelistée envoyée à l'IA.
type EntiteContexte struct {
	EntityID     string                 `json:"entity_id"`
	FriendlyName string                 `json:"nom"`
	Domain       string                 `json:"domaine"`
	State        string                 `json:"etat"`
	Attributs    map[string]interface{} `json:"attributs,omitempty"`
}

func (c *Client) ContexteJSON(pieces []Piece) (string, error) {
	raw, err := c.get("/api/states")
	if err != nil {
		return "", err
	}

	var etats []etatRawPourContexte
	if err := json.Unmarshal(raw, &etats); err != nil {
		return "", err
	}

	var out []EntiteContexte
	for _, e := range etats {
		domaine := domaineDepuisEntityID(e.EntityID)
		if domainesExclus[domaine] {
			continue
		}

		nom, _ := e.Attributes["friendly_name"].(string)

		var attrsFiltres map[string]interface{}
		if cles, ok := attributsWhitelist[domaine]; ok {
			attrsFiltres = map[string]interface{}{}
			for _, cle := range cles {
				if v, ok := e.Attributes[cle]; ok {
					attrsFiltres[cle] = v
				}
			}
		}

		out = append(out, EntiteContexte{
			EntityID:     e.EntityID,
			FriendlyName: nom,
			Domain:       domaine,
			State:        e.State,
			Attributs:    attrsFiltres,
		})
	}

	payload := struct {
		Pieces  []Piece          `json:"pieces"`
		Entites []EntiteContexte `json:"entites"`
	}{Pieces: pieces, Entites: out}

	b, err := json.Marshal(payload)
	return string(b), err
}

func domaineDepuisEntityID(entityID string) string {
	for i, r := range entityID {
		if r == '.' {
			return entityID[:i]
		}
	}
	return ""
}

// CapacitesDomaine décrit ce que l'IA peut faire sur un domaine : ses verbes
// français, et le vocabulaire de paramètres reconnu (couleurs, presets...).
type CapacitesDomaine struct {
	Verbes     []string `json:"verbes"`
	MotsParams []string `json:"mots_parametres,omitempty"`
}

// CapacitesIA construit, pour chaque domaine enregistré, ses verbes et son
// vocabulaire de paramètres — dérivé automatiquement de chaque service, donc
// toujours à jour si tu ajoutes un domaine ou un verbe.
func CapacitesIA() map[string]CapacitesDomaine {
	out := make(map[string]CapacitesDomaine)
	for _, domaine := range ListDomaines() {
		if domainesExclus[domaine] {
			continue
		}
		svc, ok := Lookup(domaine)
		if !ok {
			continue
		}
		unique := map[string]bool{}
		var mots []string
		for _, vp := range svc.VerbsAvecParams() {
			for _, p := range vp.Params {
				if !unique[p] {
					unique[p] = true
					mots = append(mots, p)
				}
			}
		}
		out[domaine] = CapacitesDomaine{Verbes: svc.Verbes(), MotsParams: mots}
	}
	return out
}
