package ha

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/text"
)

// Recherche de recettes par ingrédients : « j'ai du riz et des œufs, qu'est-ce que je peux
// cuisiner ? ». L'intégration Mealie de Home Assistant ne permet pas de lister les recettes
// (seulement le plan de repas) : on interroge donc directement l'API de Mealie
// (MEALIE_URL + MEALIE_TOKEN). Toutes les recettes et leurs ingrédients sont lus une fois puis
// gardés en mémoire (12 h) ; le code cherche, l'IA présente le résultat.
//
// Les noms de champs de l'API Mealie (recipeIngredient / recipe_ingredient, display…) ne
// sont pas vérifiés : la lecture accepte les deux graphies.

var (
	mealieURL   string
	mealieToken string
)

// DefinirMealieAPI règle l'adresse et le jeton de l'API Mealie (vides = fonction désactivée).
func DefinirMealieAPI(baseURL, token string) {
	mealieURL, mealieToken = strings.TrimRight(strings.TrimSpace(baseURL), "/"), strings.TrimSpace(token)
}

// MealieActif : tous les appels à Mealie (plan de repas, recettes, menu du briefing) sont
// désactivés tant que MEALIE_URL n'est pas renseigné.
func MealieActif() bool { return mealieURL != "" }

// MealieAPIConfiguree indique si la recherche de recettes est utilisable.
func MealieAPIConfiguree() bool { return mealieURL != "" && mealieToken != "" }

type recetteIndex struct {
	ID          string
	Nom         string
	Ingredients []string
}

var cacheRecettes struct {
	sync.Mutex
	liste []recetteIndex
	maj   time.Time
}

const (
	dureeCacheRecettes = 12 * time.Hour
	maxRecettesIndex   = 1000
	parallelismeMealie = 6
)

var clientMealie = &http.Client{Timeout: 10 * time.Second}

// mealieGet lit un chemin de l'API Mealie et décode le JSON.
func mealieGet(chemin string, dest interface{}) error {
	req, err := http.NewRequest("GET", mealieURL+chemin, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+mealieToken)
	req.Header.Set("Accept", "application/json")
	resp, err := clientMealie.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mealie: statut %d pour %s", resp.StatusCode, chemin)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

// texteIngredient : le texte lisible d'un ingrédient Mealie (display, sinon texte d'origine,
// sinon note, sinon nom de l'aliment).
func texteIngredient(m map[string]interface{}) string {
	for _, k := range []string{"display", "originalText", "original_text", "note"} {
		if s := strings.TrimSpace(chaine(m, k)); s != "" {
			return s
		}
	}
	if food, ok := m["food"].(map[string]interface{}); ok {
		return strings.TrimSpace(chaine(food, "name"))
	}
	return ""
}

// chargerRecettes retourne toutes les recettes avec leurs ingrédients (cache 12 h).
func chargerRecettes() ([]recetteIndex, error) {
	cacheRecettes.Lock()
	defer cacheRecettes.Unlock()
	if len(cacheRecettes.liste) > 0 && time.Since(cacheRecettes.maj) < dureeCacheRecettes {
		return cacheRecettes.liste, nil
	}

	// 1. Liste des recettes (pages de 100)
	var slugs, noms, ids []string
	for page := 1; page <= 30; page++ {
		var rep struct {
			Items []map[string]interface{} `json:"items"`
		}
		if err := mealieGet(fmt.Sprintf("/api/recipes?page=%d&perPage=100", page), &rep); err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		if len(rep.Items) == 0 {
			break
		}
		for _, it := range rep.Items {
			if slug := chaine(it, "slug"); slug != "" && len(slugs) < maxRecettesIndex {
				slugs, noms, ids = append(slugs, slug), append(noms, chaine(it, "name")), append(ids, chaine(it, "id"))
			}
		}
		if len(rep.Items) < 100 {
			break
		}
	}
	if len(slugs) == 0 {
		return nil, fmt.Errorf("aucune recette dans Mealie")
	}

	// 2. Détail de chaque recette (ingrédients), en parallèle limité
	resultats := make([]recetteIndex, len(slugs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallelismeMealie)
	for i, slug := range slugs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, slug string) {
			defer wg.Done()
			defer func() { <-sem }()
			var rec map[string]interface{}
			if err := mealieGet("/api/recipes/"+url.PathEscape(slug), &rec); err != nil {
				return
			}
			r := recetteIndex{ID: premierNonVide(chaine(rec, "id"), ids[i]), Nom: premierNonVide(chaine(rec, "name"), noms[i])}
			liste, _ := premiereCle(rec, "recipeIngredient", "recipe_ingredient", "ingredients").([]interface{})
			for _, x := range liste {
				if m, ok := x.(map[string]interface{}); ok {
					if t := texteIngredient(m); t != "" {
						r.Ingredients = append(r.Ingredients, t)
					}
				} else if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
					r.Ingredients = append(r.Ingredients, s)
				}
			}
			resultats[i] = r
		}(i, slug)
	}
	wg.Wait()

	var index []recetteIndex
	for _, r := range resultats {
		if r.Nom != "" && len(r.Ingredients) > 0 {
			index = append(index, r)
		}
	}
	if len(index) == 0 {
		return nil, fmt.Errorf("aucun ingrédient lisible dans les recettes Mealie")
	}
	cacheRecettes.liste, cacheRecettes.maj = index, time.Now()
	logx.InfoT("mealie.recettes.chargees", len(index))
	return index, nil
}

// normIngredient met un texte en minuscules sans accents (œ → oe, æ → ae).
func normIngredient(s string) string {
	s = strings.NewReplacer("œ", "oe", "Œ", "oe", "æ", "ae").Replace(strings.ToLower(s))
	return strings.TrimSpace(text.Normaliser(s))
}

// racineIngredient retire le pluriel d'un mot (« oeufs » → « oeuf »).
func racineIngredient(s string) string {
	if len(s) > 3 && (strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x")) {
		return s[:len(s)-1]
	}
	return s
}

// racines découpe un texte en mots et met chaque mot au singulier : « 3 gros oeufs » →
// « 3 gros oeuf ». La recherche compare des MOTS entiers (« riz » ne matche pas « chorizo »).
func racines(s string) string {
	mots := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for i := range mots {
		mots[i] = racineIngredient(mots[i])
	}
	return strings.Join(mots, " ")
}

// ingredientsDisponibles découpe « du riz, des œufs et de la crème » en éléments normalisés
// (les articles « du », « des », « de la »… sont sans effet : la recherche est par mot contenu).
func ingredientsDisponibles(s string) []string {
	s = strings.NewReplacer(" et ", ",", ";", ",", "\n", ",").Replace(s)
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = normIngredient(p)
		for _, art := range []string{"du ", "de la ", "de l'", "des ", "d'", "un ", "une ", "le ", "la ", "les ", "l'"} {
			p = strings.TrimPrefix(p, art)
		}
		if p = racines(strings.TrimSpace(p)); len(p) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// toujoursDispo : ce qu'on suppose avoir en cuisine (jamais compté comme « manquant »).
var toujoursDispo = []string{"sel", "poivre", "eau", "huile"}

// contientUn : le texte d'ingrédient (mots au singulier, entourés d'espaces) contient-il l'un
// des éléments comme suite de mots entiers ?
func contientUn(motsIngredient string, liste []string) (string, bool) {
	for _, d := range liste {
		if strings.Contains(motsIngredient, " "+d+" ") {
			return d, true
		}
	}
	return "", false
}

// RechercherRecettes cherche dans toutes les recettes de Mealie celles que l'on peut faire avec
// ce que l'utilisateur a : d'abord celles où « tout y est », puis celles qui utilisent le plus
// de ses ingrédients et auxquelles il manque le moins de choses.
func RechercherRecettes(dispo string) (map[string]interface{}, string, error) {
	if !MealieAPIConfiguree() {
		return nil, "", fmt.Errorf("%s", i18n.T("cuisiner.non.configure"))
	}
	possede := ingredientsDisponibles(dispo)
	if len(possede) == 0 {
		return nil, "", fmt.Errorf("aucun ingrédient reconnu")
	}
	recettes, err := chargerRecettes()
	if err != nil {
		return nil, "", err
	}

	type candidat struct {
		nom       string
		utilise   []string
		manque    []string
		nbManque  int
		nbUtilise int
	}
	var cands []candidat
	for _, r := range recettes {
		c := candidat{nom: r.Nom}
		vus := map[string]bool{}
		for _, ing := range r.Ingredients {
			n := " " + racines(normIngredient(ing)) + " "
			if d, ok := contientUn(n, possede); ok {
				if !vus[d] {
					vus[d] = true
					c.utilise = append(c.utilise, d)
				}
				continue
			}
			if _, ok := contientUn(n, toujoursDispo); ok {
				continue
			}
			c.manque = append(c.manque, ing)
		}
		if len(c.utilise) == 0 {
			continue
		}
		c.nbManque, c.nbUtilise = len(c.manque), len(c.utilise)
		cands = append(cands, c)
	}

	data := map[string]interface{}{"ingredients_disponibles": possede, "recettes_examinees": len(recettes)}
	if len(cands) == 0 {
		data["remarque"] = "aucune recette ne contient ces ingrédients"
		return data, i18n.T("cuisiner.rien"), nil
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if (a.nbManque == 0) != (b.nbManque == 0) {
			return a.nbManque == 0
		}
		if a.nbUtilise != b.nbUtilise {
			return a.nbUtilise > b.nbUtilise
		}
		return a.nbManque < b.nbManque
	})
	if len(cands) > 5 {
		cands = cands[:5]
	}

	var trouvees []map[string]interface{}
	var phrases []string
	for _, c := range cands {
		manque := c.manque
		if len(manque) > 8 {
			manque = manque[:8]
		}
		trouvees = append(trouvees, map[string]interface{}{"recette": c.nom, "ingredients_utilises": c.utilise, "ingredients_manquants": manque, "nombre_manquants": c.nbManque})
		if c.nbManque == 0 {
			phrases = append(phrases, i18n.T("cuisiner.tout", c.nom))
		} else {
			phrases = append(phrases, i18n.T("cuisiner.manque", c.nom, strings.Join(manque, ", ")))
		}
	}
	data["recettes_trouvees"] = trouvees
	return data, i18n.T("cuisiner.resume", strings.Join(possede, ", "), strings.Join(phrases, " ; ")), nil
}

// ---- Planifier un repas dans Mealie ----

// RecetteRef : une recette de Mealie (identifiant et nom).
type RecetteRef struct {
	ID  string
	Nom string
}

// TrouverRecettes cherche des recettes par leur NOM : celles dont le nom contient le plus des
// mots demandés (mots entiers, singulier), les mieux classées d'abord (5 au plus).
func TrouverRecettes(nom string) ([]RecetteRef, error) {
	if !MealieAPIConfiguree() {
		return nil, fmt.Errorf("%s", i18n.T("cuisiner.non.configure"))
	}
	recettes, err := chargerRecettes()
	if err != nil {
		return nil, err
	}
	mots := strings.Fields(racines(normIngredient(nom)))
	var utiles []string
	for _, m := range mots {
		if len(m) >= 3 { // « de », « au », « la »… sans intérêt
			utiles = append(utiles, m)
		}
	}
	if len(utiles) == 0 {
		return nil, nil
	}
	type scoree struct {
		r     RecetteRef
		score int
		len   int
	}
	var res []scoree
	for _, r := range recettes {
		nomMots := " " + racines(normIngredient(r.Nom)) + " "
		score := 0
		for _, m := range utiles {
			if strings.Contains(nomMots, " "+m+" ") {
				score++
			}
		}
		if score > 0 {
			res = append(res, scoree{r: RecetteRef{ID: r.ID, Nom: r.Nom}, score: score, len: len(r.Nom)})
		}
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].score != res[j].score {
			return res[i].score > res[j].score
		}
		return res[i].len < res[j].len // à score égal, le nom le plus court (le plus précis)
	})
	var out []RecetteRef
	for i, r := range res {
		if i >= 5 {
			break
		}
		out = append(out, r.r)
	}
	return out, nil
}

// typeRepasMealie traduit « dîner », « soir », « midi »… en type de repas Mealie.
func typeRepasMealie(s string) string {
	n := normIngredient(s)
	switch {
	case strings.Contains(n, "petit") || strings.Contains(n, "matin") || strings.Contains(n, "breakfast"):
		return "breakfast"
	case strings.Contains(n, "dejeuner") || strings.Contains(n, "midi") || strings.Contains(n, "lunch"):
		return "lunch"
	case strings.Contains(n, "accompagnement") || strings.Contains(n, "side"):
		return "side"
	}
	return "dinner"
}

// PlanifierRepas met une recette au plan de repas de Mealie (service mealie.set_mealplan
// de Home Assistant). typeRepas : « dîner », « déjeuner »…
func (c *Client) PlanifierRepas(date time.Time, typeRepas string, recette RecetteRef) error {
	entry := c.mealieEntryID()
	if entry == "" {
		return fmt.Errorf("intégration Mealie de Home Assistant introuvable")
	}
	_, err := c.post("/api/services/mealie/set_mealplan", map[string]interface{}{
		"config_entry_id": entry,
		"date":            date.Format("2006-01-02"),
		"entry_type":      typeRepasMealie(typeRepas),
		"recipe_id":       recette.ID,
	})
	return err
}

// PlanifierAuHasard laisse Mealie choisir une recette au hasard (mealie.set_random_mealplan)
// et retourne le titre de ce qui a été planifié (lu ensuite dans le plan de repas).
func (c *Client) PlanifierAuHasard(date time.Time, typeRepas string) (string, error) {
	entry := c.mealieEntryID()
	if entry == "" {
		return "", fmt.Errorf("intégration Mealie de Home Assistant introuvable")
	}
	typeMealie := typeRepasMealie(typeRepas)
	if _, err := c.post("/api/services/mealie/set_random_mealplan", map[string]interface{}{
		"config_entry_id": entry,
		"date":            date.Format("2006-01-02"),
		"entry_type":      typeMealie,
	}); err != nil {
		return "", err
	}
	plan, err := c.PlanRepas(date, date, false)
	if err != nil {
		return "", nil // planifié, mais on ne sait pas quoi
	}
	for i := len(plan) - 1; i >= 0; i-- { // le dernier ajouté du bon type
		if fmt.Sprint(plan[i]["repas"]) == libelleTypeRepas(typeMealie) {
			return fmt.Sprint(plan[i]["titre"]), nil
		}
	}
	return "", nil
}
