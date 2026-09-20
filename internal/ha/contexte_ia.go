package ha

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"ha-command-gateway/internal/logx"
)

// domainesExclus : jamais envoyés à l'IA, ni commandables ni lisibles par elle
// (présence, sécurité, accès)
var domainesExclus = map[string]bool{
	"person":              true,
	"device_tracker":      true,
	"camera":              true,
	"lock":                true,
	"alarm_control_panel": true,
	"update":              true,
}

// actionsIAAutorisees restreint, par domaine, les actions HA que l'IA peut
// déclencher (vérifié par le code, pas par le prompt). Un domaine absent de
// cette table n'a pas de restriction supplémentaire.
//
// Automatisations : l'IA peut les exécuter (trigger), les activer (turn_on) ou
// les désactiver (turn_off) — jamais les basculer (toggle), recharger, etc.
var actionsIAAutorisees = map[string]map[string]bool{
	"automation": {"trigger": true, "turn_on": true, "turn_off": true},
}

// ActionIAAutorisee indique si l'IA a le droit d'appeler cette action HA.
func ActionIAAutorisee(domaine, action string) bool {
	allow, restreint := actionsIAAutorisees[domaine]
	if !restreint {
		return true
	}
	return allow[action]
}

// domainesToujoursDansContexte : domaines toujours transmis à l'IA, même quand le
// contexte est réduit à la demande (peu d'entités, et l'IA a besoin de les connaître
// pour les scripts, la météo, les minuteurs, la musique).
var domainesToujoursDansContexte = map[string]bool{
	"script":       true,
	"weather":      true,
	"timer":        true,
	"media_player": true,
	"calendar":     true, // peu nombreux ; l'IA doit savoir quels calendriers existent (dont Mealie)
}

// DomaineToujoursDansContexte indique si un domaine échappe à la réduction du contexte.
func DomaineToujoursDansContexte(domaine string) bool { return domainesToujoursDansContexte[domaine] }

// DomaineExcluIA indique si un domaine est interdit à l'IA.
func DomaineExcluIA(domaine string) bool { return domainesExclus[domaine] }

// attributsWhitelist : par domaine, les seuls attributs (au-delà de l'état
// brut) transmis à l'IA. Tout ce qui n'est pas listé ici est systématiquement
// exclu — jamais de dump brut des attributs HA (pas de risque de token/URL/MAC).
var attributsWhitelist = map[string][]string{
	"weather":      {"temperature", "humidity", "wind_speed", "pressure"},
	"climate":      {"current_temperature", "temperature", "hvac_action"},
	"sensor":       {"unit_of_measurement", "device_class"},
	"cover":        {"current_position"},
	"fan":          {"percentage"},
	"light":        {"brightness"},
	"media_player": {"source_list", "source", "volume_level"},
	"timer":        {"duration", "finishes_at", "remaining"},
	"automation":   {"last_triggered"},
	"script":       {"last_triggered"},
}

// ChampScript décrit un paramètre déclaré par un script HA (section `fields`).
type ChampScript struct {
	Nom         string      `json:"nom,omitempty"`
	Description string      `json:"description,omitempty"`
	Requis      bool        `json:"requis,omitempty"`
	Exemple     interface{} `json:"exemple,omitempty"`
}

type etatRawPourContexte struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	LastChanged string                 `json:"last_changed"`
	Attributes map[string]interface{} `json:"attributes"`
}

// EntiteContexte est la forme whitelistée envoyée à l'IA.
type EntiteContexte struct {
	EntityID     string                 `json:"entity_id"`
	FriendlyName string                 `json:"nom"`
	Domain       string                 `json:"domaine"`
	State        string                 `json:"etat"`
	Attributs    map[string]interface{} `json:"attributs,omitempty"`
	Description  string                 `json:"description,omitempty"` // scripts : ce que fait le script
	Parametres   map[string]ChampScript `json:"parametres,omitempty"` // scripts : paramètres acceptés
	Piece        string                 `json:"piece,omitempty"`      // pièce HA de l'entité (registre des zones)
	Depuis       string                 `json:"depuis,omitempty"`     // depuis quand l'état actuel dure (heure locale)
}

// ---- Paramètres des scripts ----

var cacheScripts struct {
	sync.Mutex
	champs       map[string]map[string]ChampScript
	descriptions map[string]string
	maj          time.Time
}

// champsScripts retourne, par script (object_id), les champs qu'il déclare.
// Source : GET /api/services (domaine "script"), mis en cache 10 minutes.
func (c *Client) champsScripts() map[string]map[string]ChampScript {
	cacheScripts.Lock()
	defer cacheScripts.Unlock()

	if cacheScripts.champs != nil && time.Since(cacheScripts.maj) < 10*time.Minute {
		return cacheScripts.champs
	}

	body, err := c.get("/api/services")
	if err != nil {
		logx.WarnT("gemini.scripts.erreur", err)
		return cacheScripts.champs
	}

	var domaines []struct {
		Domain   string `json:"domain"`
		Services map[string]struct {
			Description string `json:"description"`
			Fields      map[string]struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Required    bool        `json:"required"`
				Example     interface{} `json:"example"`
			} `json:"fields"`
		} `json:"services"`
	}
	if err := json.Unmarshal(body, &domaines); err != nil {
		logx.WarnT("gemini.scripts.erreur", err)
		return cacheScripts.champs
	}

	res := make(map[string]map[string]ChampScript)
	descriptions := make(map[string]string)
	for _, d := range domaines {
		if d.Domain != "script" {
			continue
		}
		for id, svc := range d.Services {
			if id == "reload" || id == "turn_on" || id == "turn_off" || id == "toggle" {
				continue
			}
			champs := make(map[string]ChampScript, len(svc.Fields))
			for nom, f := range svc.Fields {
				champs[nom] = ChampScript{Nom: f.Name, Description: f.Description, Requis: f.Required, Exemple: f.Example}
			}
			res[id] = champs
			descriptions[id] = svc.Description
		}
	}

	cacheScripts.champs = res
	cacheScripts.descriptions = descriptions
	cacheScripts.maj = time.Now()
	return res
}

// descriptionScript retourne la description d'un script (ce qu'il fait), pour
// aider l'IA à choisir entre plusieurs scripts proches.
func (c *Client) descriptionScript(entityID string) string {
	c.champsScripts() // s'assure que le cache est chargé
	cacheScripts.Lock()
	defer cacheScripts.Unlock()
	return cacheScripts.descriptions[strings.TrimPrefix(entityID, "script.")]
}

// champsScript retourne les champs déclarés par un script (entity_id « script.xxx »).
func (c *Client) champsScript(entityID string) map[string]ChampScript {
	return c.champsScripts()[strings.TrimPrefix(entityID, "script.")]
}

// ChampsScript retourne les paramètres (`fields`) déclarés par un script (entity_id « script.xxx »).
func (c *Client) ChampsScript(entityID string) map[string]ChampScript {
	return c.champsScript(entityID)
}

// ParametresIAValides filtre les paramètres structurés proposés par l'IA : seuls
// ceux que le domaine sait réellement utiliser sont conservés (les noms de
// paramètres inventés sont rejetés). Le résultat est fusionné dans les params
// passés à Service.ExecuterCommande.
func (c *Client) ParametresIAValides(app Appareil, brut map[string]string) map[string]interface{} {
	out := map[string]interface{}{}
	if len(brut) == 0 {
		return out
	}

	switch app.Domain {
	case "script":
		champs := c.champsScript(app.EntityID)
		variables := map[string]interface{}{}
		for nom, val := range brut {
			if _, ok := champs[nom]; ok {
				variables[nom] = val
			} else {
				logx.WarnT("gemini.parametre.rejete", nom, app.EntityID)
			}
		}
		if len(variables) > 0 {
			out["variables"] = variables
		}
	case "media_player":
		for _, nom := range []string{"source", "cible", "duree"} {
			if v := strings.TrimSpace(brut[nom]); v != "" {
				out[nom] = v
			}
		}
	case "agenda":
		// Restreint la lecture à un calendrier (mot présent dans son nom : « mealie »...)
		if v := strings.TrimSpace(brut["calendrier"]); v != "" {
			out["calendrier"] = v
		}
	case "timer":
		if d, ok := analyserDuree(brut["duree"]); ok {
			out["duration"] = formaterDuree(d)
		}
	}
	return out
}

// ---- Contexte envoyé à l'IA ----

// entitesVirtuellesContexte expose à l'IA les entités « virtuelles » (agenda,
// heure, résumé maison...) qui n'existent pas dans /api/states, pour qu'elle
// sache qu'elle peut les lire (type="read"). Un domaine déjà présent avec de
// vraies entités (ex : weather) n'est pas doublé par sa version virtuelle.
func entitesVirtuellesContexte(idsVus, domainesReels map[string]bool) []EntiteContexte {
	domaines := ListDomaines()
	sort.Strings(domaines)

	var out []EntiteContexte
	for _, domaine := range domaines {
		if domainesExclus[domaine] || domainesReels[domaine] {
			continue
		}
		svc, ok := Lookup(domaine)
		if !ok {
			continue
		}
		av, ok := svc.(ServiceAvecAppareils)
		if !ok {
			continue
		}
		for _, app := range av.AppareilsVirtuels() {
			if idsVus[app.EntityID] {
				continue
			}
			nom := app.FriendlyNameExact
			if nom == "" {
				nom = app.FriendlyName
			}
			out = append(out, EntiteContexte{
				EntityID:     app.EntityID,
				FriendlyName: nom,
				Domain:       app.Domain,
				State:        "virtuelle",
				Attributs:    map[string]interface{}{"lecture_seule": true},
			})
		}
	}
	return out
}

// ContexteJSON construit le contexte envoyé à l'IA. retenus == nil : toutes les
// entités (réduction désactivée) ; sinon seules les entités listées y sont — plus
// celles des domaines toujours transmis (cf. domainesToujoursDansContexte) et les
// entités virtuelles.
func (c *Client) ContexteJSON(pieces []Piece, retenus map[string]bool) (string, error) {
	raw, err := c.get("/api/states")
	if err != nil {
		return "", err
	}

	var etats []etatRawPourContexte
	if err := json.Unmarshal(raw, &etats); err != nil {
		return "", err
	}

	var out []EntiteContexte
	idsVus := map[string]bool{}
	domainesReels := map[string]bool{}
	var champs map[string]map[string]ChampScript
	zones := c.ZonesEntites()
	total := 0

	for _, e := range etats {
		domaine := domaineDepuisEntityID(e.EntityID)
		if domainesExclus[domaine] {
			continue
		}
		total++
		if retenus != nil && !retenus[e.EntityID] && !domainesToujoursDansContexte[domaine] {
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

		ent := EntiteContexte{
			EntityID:     e.EntityID,
			FriendlyName: nom,
			Domain:       domaine,
			State:        e.State,
			Attributs:    attrsFiltres,
			Piece:        zones[e.EntityID],
		}
		// « Depuis quand ? » : utile pour les questions « la porte est ouverte depuis
		// longtemps ? » ou « le chauffage tourne depuis 6h, c'est normal ? ». Inutile pour
		// les capteurs numériques, dont l'état change sans cesse.
		if domaine != "sensor" && domaine != "weather" {
			if t, err := time.Parse(time.RFC3339, e.LastChanged); err == nil {
				ent.Depuis = t.Local().Format("2006-01-02 15:04")
			}
		}
		if domaine == "script" {
			if champs == nil {
				champs = c.champsScripts()
			}
			ent.Parametres = champs[strings.TrimPrefix(e.EntityID, "script.")]
			ent.Description = c.descriptionScript(e.EntityID)
		}

		out = append(out, ent)
		idsVus[e.EntityID] = true
		domainesReels[domaine] = true
	}

	logx.DebugT("gemini.contexte.taille", len(out), total)
	out = append(out, entitesVirtuellesContexte(idsVus, domainesReels)...)

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
// français, le vocabulaire de paramètres reconnu (couleurs, presets...) et, pour
// les domaines en lecture seule, les mots d'horizon/période compris.
type CapacitesDomaine struct {
	Verbes      []string `json:"verbes"`
	MotsParams  []string `json:"mots_parametres,omitempty"`
	MotsLecture []string `json:"mots_lecture,omitempty"`
}

// CapacitesIA construit, pour chaque domaine enregistré, ses verbes autorisés et
// son vocabulaire — dérivé automatiquement de chaque service, donc toujours à
// jour si tu ajoutes un domaine ou un verbe. Les actions interdites à l'IA
// (cf. actionsIAAutorisees) sont filtrées ici.
func CapacitesIA() map[string]CapacitesDomaine {
	out := make(map[string]CapacitesDomaine)
	for _, domaine := range ListDomaines() {
		if domainesExclus[domaine] || domaine == "service_default" {
			continue
		}
		svc, ok := Lookup(domaine)
		if !ok {
			continue
		}

		verbes := []string{}
		for _, v := range svc.Verbes() {
			action, vok := svc.Verbe(v)
			if !vok || !ActionIAAutorisee(domaine, action) {
				continue
			}
			verbes = append(verbes, v)
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

		capa := CapacitesDomaine{Verbes: verbes, MotsParams: mots}
		if len(verbes) == 0 {
			seen := map[string]bool{}
			for _, m := range svc.MotsReconnus() {
				if !seen[m] {
					seen[m] = true
					capa.MotsLecture = append(capa.MotsLecture, m)
				}
			}
		}
		out[domaine] = capa
	}
	return out
}
