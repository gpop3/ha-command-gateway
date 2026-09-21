package ha

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Accès à Mealie via l'intégration officielle de Home Assistant : services
// mealie.get_mealplan (plan de repas) et mealie.get_recipe (ingrédients, étapes).
// Les noms de champs de leurs réponses n'ont pas pu être vérifiés : la lecture est tolérante et,
// si elle échoue, on se replie sur les calendriers de repas (titres seulement).

var cacheMealie struct {
	sync.Mutex
	entryID string
	maj     time.Time
	echec   time.Time
}

// mealieEntryID retrouve l'identifiant de l'intégration Mealie (config_entry_id).
// Lecture réservée aux administrateurs. Mis en cache 1 h ; pas de nouvel essai avant 5 min après un échec.
func (c *Client) mealieEntryID() string {
	if !MealieActif() {
		return ""
	}
	cacheMealie.Lock()
	defer cacheMealie.Unlock()
	now := time.Now()
	if cacheMealie.entryID != "" && now.Sub(cacheMealie.maj) < time.Hour {
		return cacheMealie.entryID
	}
	if !cacheMealie.echec.IsZero() && now.Sub(cacheMealie.echec) < 5*time.Minute {
		return ""
	}
	body, err := c.get("/api/config/config_entries/entry")
	if err == nil {
		var entrees []struct {
			EntryID string `json:"entry_id"`
			Domain  string `json:"domain"`
		}
		if json.Unmarshal(body, &entrees) == nil {
			for _, e := range entrees {
				if e.Domain == "mealie" {
					cacheMealie.entryID, cacheMealie.maj = e.EntryID, now
					return e.EntryID
				}
			}
		}
	}
	cacheMealie.echec = now
	return ""
}

// reponseService appelle un service HA qui renvoie une réponse (return_response).
func (c *Client) reponseService(domaine, service string, data map[string]interface{}) (map[string]interface{}, error) {
	body, err := c.post(fmt.Sprintf("/api/services/%s/%s?return_response", domaine, service), data)
	if err != nil {
		return nil, err
	}
	var res struct {
		ServiceResponse map[string]interface{} `json:"service_response"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return res.ServiceResponse, nil
}

func libelleTypeRepas(t string) string {
	switch strings.ToLower(t) {
	case "breakfast":
		return "petit-déjeuner"
	case "lunch":
		return "déjeuner"
	case "dinner":
		return "dîner"
	case "side":
		return "accompagnement"
	}
	return t
}

func listeTexte(v interface{}, cles ...string) []string {
	liste, _ := v.([]interface{})
	var out []string
	for _, x := range liste {
		switch e := x.(type) {
		case string:
			out = append(out, e)
		case map[string]interface{}:
			for _, k := range cles {
				if s := strings.TrimSpace(chaine(e, k)); s != "" {
					out = append(out, s)
					break
				}
			}
		}
	}
	return out
}

// detailRecette : ingrédients, étapes et temps d'une recette (nil si illisible).
func (c *Client) detailRecette(entryID, recetteID string) map[string]interface{} {
	rep, err := c.reponseService("mealie", "get_recipe", map[string]interface{}{"config_entry_id": entryID, "recipe_id": recetteID})
	if err != nil {
		return nil
	}
	rec, _ := rep["recipe"].(map[string]interface{})
	if rec == nil {
		return nil
	}
	d := map[string]interface{}{}
	if n := chaine(rec, "name"); n != "" {
		d["nom"] = n
	}
	if ing := listeTexte(premiereCle(rec, "recipe_ingredient", "ingredients"), "display", "original_text", "note", "title"); len(ing) > 0 {
		d["ingredients"] = ing
	}
	if etapes := listeTexte(premiereCle(rec, "recipe_instructions", "instructions"), "text", "title"); len(etapes) > 0 {
		if len(etapes) > 12 {
			etapes = etapes[:12]
		}
		for i := range etapes {
			etapes[i] = tronquer(etapes[i], 250)
		}
		d["etapes"] = etapes
	}
	for _, k := range []string{"prep_time", "cook_time", "total_time"} {
		if t := chaine(rec, k); t != "" {
			d[k] = t
		}
	}
	if len(d) == 0 {
		return nil
	}
	return d
}

// PlanRepas lit le plan de repas Mealie sur [debut, fin] (dates comprises) avec, si
// demandé, le détail des 4 premières recettes. Erreur si l'intégration est absente.
func (c *Client) PlanRepas(debut, fin time.Time, avecDetails bool) ([]map[string]interface{}, error) {
	if !MealieActif() {
		return nil, fmt.Errorf("Mealie désactivé (MEALIE_URL vide)")
	}
	entry := c.mealieEntryID()
	if entry == "" {
		return nil, fmt.Errorf("intégration Mealie introuvable")
	}
	rep, err := c.reponseService("mealie", "get_mealplan", map[string]interface{}{
		"config_entry_id": entry,
		"start_date":      debut.Format("2006-01-02"),
		"end_date":        fin.Format("2006-01-02"),
	})
	if err != nil {
		return nil, err
	}
	liste, _ := rep["mealplan"].([]interface{})
	var out []map[string]interface{}
	for i, x := range liste {
		m, _ := x.(map[string]interface{})
		if m == nil {
			continue
		}
		rec, _ := m["recipe"].(map[string]interface{})
		titre := premierNonVide(chaine(m, "title"), chaine(rec, "name"))
		item := map[string]interface{}{"repas": libelleTypeRepas(chaine(m, "entry_type")), "titre": titre}
		if d := chaine(m, "description"); d != "" {
			item["description"] = tronquer(d, 300)
		}
		id := premierNonVide(chaine(rec, "recipe_id"), chaine(rec, "id"), chaine(m, "recipe_id"))
		if avecDetails && id != "" && i < 4 {
			if det := c.detailRecette(entry, id); det != nil {
				item["recette"] = det
			}
		}
		out = append(out, item)
	}
	return out, nil
}
