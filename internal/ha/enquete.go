package ha

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"ha-command-gateway/internal/utils/text"
)

// Collecteurs de données pour les « enquêtes » : le code rassemble des faits dans Home
// Assistant (traces d'une automatisation, état d'une pièce ou de la maison, journal d'une
// période, consommation, météo et mesures), puis l'IA les commente dans un second appel.
// Chaque collecteur retourne (données structurées pour l'IA, résumé prononçable de repli).

// ---- utilitaires ----

func chaine(m map[string]interface{}, cle string) string {
	v, _ := m[cle].(string)
	return v
}

func tronquer(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func nomEtat(e EtatBrut) string {
	if n := chaine(e.Attributes, "friendly_name"); n != "" {
		return n
	}
	return e.EntityID
}

func tempsChange(e EtatBrut) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, e.LastChanged)
	return t, err == nil
}

// formaterDepuis : « depuis 6h05 » (aujourd'hui) ou « depuis hier 22h ».
func formaterDepuis(t time.Time) string {
	return "depuis " + formaterInstant(t, !memeJour(t, time.Now()))
}

func toJSON(v interface{}, max int) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return tronquer(string(b), max)
}

// premiereCle retourne la première clé présente d'une configuration HA (nouvelle ou
// ancienne syntaxe : « conditions » / « condition »).
func premiereCle(cfg map[string]interface{}, cles ...string) interface{} {
	for _, c := range cles {
		if v, ok := cfg[c]; ok {
			return v
		}
	}
	return nil
}

// ---- Pourquoi une automatisation s'est (ou non) déclenchée ----

func traduireExecution(s string) string {
	switch s {
	case "finished":
		return "exécutée jusqu'au bout"
	case "failed_conditions":
		return "arrêtée : une condition n'était pas remplie"
	case "failed_single":
		return "arrêtée : déjà en cours d'exécution (mode single)"
	case "failed_max_runs":
		return "arrêtée : trop d'exécutions simultanées"
	case "failed_unknown_error", "error":
		return "en erreur"
	case "aborted":
		return "interrompue"
	case "cancelled":
		return "annulée"
	case "":
		return "résultat inconnu"
	}
	return s
}

// DonneesAutomatisation rassemble de quoi expliquer le comportement d'une automatisation :
// activée ou non, dernier déclenchement, dernières exécutions (traces HA, avec la condition
// qui a bloqué le cas échéant) et configuration (déclencheurs et conditions).
func (c *Client) DonneesAutomatisation(app Appareil) (map[string]interface{}, string, error) {
	etat, err := c.etatBrut(app.EntityID)
	if err != nil {
		return nil, "", err
	}
	nom := nomEtat(*etat)
	data := map[string]interface{}{"nom": nom, "mode": chaine(etat.Attributes, "mode")}
	if etat.State == "on" {
		data["etat"] = "activée"
	} else {
		data["etat"] = "désactivée : elle ne se déclenchera pas tant qu'elle ne sera pas réactivée"
	}
	if t, err := time.Parse(time.RFC3339, chaine(etat.Attributes, "last_triggered")); err == nil {
		data["dernier_declenchement"] = jourRelatif(t) + " à " + heureCourte(t)
	} else {
		data["dernier_declenchement"] = "aucun depuis le dernier démarrage de Home Assistant"
	}

	idConfig := chaine(etat.Attributes, "id")
	if idConfig == "" {
		data["remarque"] = "automatisation définie en YAML sans identifiant : traces et configuration indisponibles"
		return data, fmt.Sprintf("%s est %s.", nom, data["etat"]), nil
	}

	// Configuration (déclencheurs et conditions), pour expliquer ce qui a bloqué
	var config map[string]interface{}
	if body, err := c.get("/api/config/automation/config/" + idConfig); err == nil {
		if json.Unmarshal(body, &config) == nil {
			data["declencheurs"] = toJSON(premiereCle(config, "triggers", "trigger"), 1500)
			data["conditions"] = toJSON(premiereCle(config, "conditions", "condition"), 1500)
		}
	}

	// Dernières exécutions
	var traces []map[string]interface{}
	if err := c.commandeWSAvec(wsMessage{Type: "trace/list", Domain: "automation", ItemID: idConfig}, &traces); err != nil {
		data["remarque"] = "traces d'exécution illisibles : " + err.Error()
		return data, fmt.Sprintf("%s est %s.", nom, data["etat"]), nil
	}
	debutTrace := func(m map[string]interface{}) string {
		ts, _ := m["timestamp"].(map[string]interface{})
		return chaine(ts, "start")
	}
	sort.SliceStable(traces, func(i, j int) bool { return debutTrace(traces[i]) < debutTrace(traces[j]) })
	if len(traces) > 5 {
		traces = traces[len(traces)-5:]
	}

	var executions []map[string]interface{}
	var lignes []string
	for i, m := range traces {
		quand := ""
		if t, err := time.Parse(time.RFC3339, debutTrace(m)); err == nil {
			quand = jourRelatif(t) + " à " + heureCourte(t)
		}
		resultat := traduireExecution(chaine(m, "script_execution"))
		ex := map[string]interface{}{"quand": quand, "declencheur": chaine(m, "trigger"), "resultat": resultat, "derniere_etape": chaine(m, "last_step")}
		if msg := chaine(m, "error"); msg != "" {
			ex["erreur"] = msg
		}

		// Pour la dernière exécution bloquée par une condition : détail de la condition
		if i == len(traces)-1 && chaine(m, "script_execution") == "failed_conditions" {
			etape := chaine(m, "last_step")
			var detail map[string]interface{}
			if err := c.commandeWSAvec(wsMessage{Type: "trace/get", Domain: "automation", ItemID: idConfig, RunID: chaine(m, "run_id")}, &detail); err == nil {
				if trace, ok := detail["trace"].(map[string]interface{}); ok {
					if pas, ok := trace[etape].([]interface{}); ok && len(pas) > 0 {
						if premier, ok := pas[0].(map[string]interface{}); ok {
							ex["detail_condition"] = toJSON(premier["result"], 600)
						}
					}
				}
			}
			// Configuration de cette condition (« condition/0 » = première condition)
			if idx, err := strconv.Atoi(strings.TrimPrefix(etape, "condition/")); err == nil {
				if liste, ok := premiereCle(config, "conditions", "condition").([]interface{}); ok && idx >= 0 && idx < len(liste) {
					ex["condition_bloquante"] = toJSON(liste[idx], 600)
				}
			}
		}
		executions = append(executions, ex)
		lignes = append(lignes, fmt.Sprintf("%s : %s", quand, resultat))
	}
	data["executions"] = executions
	if len(executions) == 0 {
		data["remarque"] = "aucune exécution récente : le déclencheur ne s'est pas produit (ou les traces ont été purgées)"
	}
	resume := fmt.Sprintf("%s est %s. ", nom, data["etat"])
	if len(lignes) > 0 {
		resume += "Dernières exécutions : " + strings.Join(lignes, " ; ") + "."
	}
	return data, resume, nil
}

// ---- Diagnostic d'une pièce ----

var prioriteDomaine = map[string]int{"climate": 0, "binary_sensor": 1, "sensor": 2, "cover": 3, "light": 4, "switch": 5, "fan": 6, "media_player": 7}

var classesCapteursPiece = map[string]bool{
	"temperature": true, "humidity": true, "carbon_dioxide": true, "illuminance": true, "pm25": true,
	"door": true, "window": true, "opening": true, "garage_door": true, "motion": true, "occupancy": true,
	"moisture": true, "smoke": true, "gas": true,
}

// descriptionEntite : une entité en quelques champs lisibles par l'IA.
func descriptionEntite(e EtatBrut) map[string]interface{} {
	d := domaineDepuisEntityID(e.EntityID)
	unite := chaine(e.Attributes, "unit_of_measurement")
	m := map[string]interface{}{"nom": nomEtat(e), "domaine": d}
	if classe := chaine(e.Attributes, "device_class"); classe != "" {
		m["type"] = classe
	}
	if v, err := strconv.ParseFloat(e.State, 64); err == nil && d == "sensor" {
		m["valeur"] = formaterValeur(v, unite)
	} else {
		m["etat"] = traduireEtat(d, e.State)
	}
	if d != "sensor" && d != "weather" {
		if t, ok := tempsChange(e); ok {
			m["depuis"] = formaterDepuis(t)
		}
	}
	switch d {
	case "climate":
		if v, ok := nombreAttr(e.Attributes["current_temperature"]); ok {
			m["temperature_actuelle"] = formaterValeur(v, "°C")
		}
		if v, ok := nombreAttr(e.Attributes["temperature"]); ok {
			m["consigne"] = formaterValeur(v, "°C")
		}
		if a := chaine(e.Attributes, "hvac_action"); a != "" {
			m["action"] = a
		}
	case "cover":
		if v, ok := nombreAttr(e.Attributes["current_position"]); ok {
			m["position"] = fmt.Sprintf("%d %%", int(v))
		}
	case "light":
		if v, ok := nombreAttr(e.Attributes["brightness"]); ok {
			m["luminosite"] = fmt.Sprintf("%d %%", int(math.Round(v*100/255)))
		}
	}
	return m
}

// entreeJournal convertit une entrée du journal global en événement lisible.
func (c *Client) entreeJournal(m map[string]interface{}, domaine string) (time.Time, EvenementJournal, bool) {
	t, err := time.Parse(time.RFC3339, champ(m, "when"))
	if err != nil {
		return time.Time{}, EvenementJournal{}, false
	}
	e := EvenementJournal{Quand: formaterQuandAuto(t), Message: champ(m, "message"), Cause: c.causeJournal(m, domaine)}
	if s := strings.ToLower(champ(m, "state")); s != "" && domaine != "automation" && domaine != "script" {
		e.Etat = traduireEtat(domaine, s)
	}
	return t, e, true
}

func (c *Client) journalGlobal(debut, fin time.Time) ([]map[string]interface{}, error) {
	const layout = "2006-01-02T15:04:05Z"
	body, err := c.get(fmt.Sprintf("/api/logbook/%s?end_time=%s", debut.UTC().Format(layout), fin.UTC().Format(layout)))
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// meteoExterieure : conditions extérieures actuelles (première entité weather).
func meteoExterieure(etats []EtatBrut) map[string]interface{} {
	for _, e := range etats {
		if domaineDepuisEntityID(e.EntityID) != "weather" {
			continue
		}
		m := map[string]interface{}{"condition": tradCondition(e.State)}
		if v, ok := nombreAttr(e.Attributes["temperature"]); ok {
			m["temperature"] = formaterValeur(v, "°C")
		}
		if v, ok := nombreAttr(e.Attributes["humidity"]); ok {
			m["humidite"] = fmt.Sprintf("%d %%", int(v))
		}
		return m
	}
	return nil
}

// DiagnosticPiece rassemble l'état d'une pièce (entités rattachées dans HA), ce qui se
// passe dehors et les derniers événements de la pièce, pour expliquer « pourquoi il fait
// froid dans la chambre ? ».
func (c *Client) DiagnosticPiece(piece string) (map[string]interface{}, string, error) {
	etats, err := c.etatsBruts()
	if err != nil {
		return nil, "", err
	}
	zones := c.ZonesEntites()
	pieceNorm := text.Normaliser(strings.TrimSpace(piece))

	type candidat struct {
		e    EtatBrut
		prio int
	}
	var cands []candidat
	ids := map[string]EtatBrut{}
	for _, e := range etats {
		d := domaineDepuisEntityID(e.EntityID)
		prio, ok := prioriteDomaine[d]
		if !ok || domainesExclus[d] {
			continue
		}
		if (d == "sensor" || d == "binary_sensor") && !classesCapteursPiece[chaine(e.Attributes, "device_class")] {
			continue
		}
		if pieceNorm == "" || !strings.Contains(text.Normaliser(zones[e.EntityID]), pieceNorm) {
			continue
		}
		cands = append(cands, candidat{e: e, prio: prio})
		ids[e.EntityID] = e
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].prio < cands[j].prio })
	if len(cands) > 40 {
		cands = cands[:40]
	}

	data := map[string]interface{}{"piece": piece}
	if len(cands) == 0 {
		data["remarque"] = "aucune entité n'est rattachée à cette pièce dans Home Assistant (ou le registre des pièces est inaccessible)"
		return data, fmt.Sprintf("Je ne trouve aucune entité rattachée à la pièce %s.", piece), nil
	}
	var items []map[string]interface{}
	var noms []string
	for _, cd := range cands {
		items = append(items, descriptionEntite(cd.e))
		noms = append(noms, nomEtat(cd.e))
	}
	data["entites"] = items
	if dehors := meteoExterieure(etats); dehors != nil {
		data["dehors"] = dehors
	}

	// Derniers événements de la pièce (6 dernières heures)
	fin := time.Now()
	if brut, err := c.journalGlobal(fin.Add(-6*time.Hour), fin); err == nil {
		var evts []map[string]interface{}
		for _, m := range brut {
			id := champ(m, "entity_id")
			if _, dans := ids[id]; !dans {
				continue
			}
			d := domaineDepuisEntityID(id)
			if _, e, ok := c.entreeJournal(m, d); ok {
				evts = append(evts, map[string]interface{}{"quand": e.Quand, "nom": nomEtat(ids[id]), "etat": e.Etat, "message": e.Message, "cause": e.Cause})
			}
		}
		if len(evts) > 12 {
			evts = evts[len(evts)-12:]
		}
		if len(evts) > 0 {
			data["derniers_evenements"] = evts
		}
	}
	return data, fmt.Sprintf("Dans %s : %s.", piece, strings.Join(noms, ", ")), nil
}

// ---- Diagnostic de la maison ----

// DiagnosticMaison relève ce qui cloche : capteurs indisponibles depuis longtemps,
// batteries faibles, ouvertures restées ouvertes, lumières allumées depuis des heures,
// automatisations désactivées, mises à jour en attente.
func (c *Client) DiagnosticMaison() (map[string]interface{}, string, error) {
	etats, err := c.etatsBruts()
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	var indispos, batteries, ouverts, lumieres, autoOff []string
	majs := 0
	limite := func(l *[]string, nom string, n int) {
		if len(*l) < n {
			*l = append(*l, nom)
		}
	}

	for _, e := range etats {
		d := domaineDepuisEntityID(e.EntityID)
		nom := nomEtat(e)
		classe := chaine(e.Attributes, "device_class")
		t, okT := tempsChange(e)
		age := time.Duration(0)
		if okT {
			age = now.Sub(t)
		}
		switch {
		case e.State == "unavailable" || e.State == "unknown":
			if okT && age > 24*time.Hour && d != "update" && d != "person" && d != "device_tracker" {
				limite(&indispos, fmt.Sprintf("%s (%s)", nom, formaterDepuis(t)), 15)
			}
		case d == "sensor" && classe == "battery":
			if v, err := strconv.ParseFloat(e.State, 64); err == nil && v < 20 {
				limite(&batteries, fmt.Sprintf("%s (%.0f %%)", nom, v), 12)
			}
		case d == "binary_sensor" && classe == "battery" && e.State == "on":
			limite(&batteries, nom, 12)
		case d == "binary_sensor" && e.State == "on" && (classe == "door" || classe == "window" || classe == "opening" || classe == "garage_door"):
			if okT && age > 30*time.Minute {
				limite(&ouverts, fmt.Sprintf("%s (%s)", nom, formaterDepuis(t)), 10)
			}
		case d == "light" && e.State == "on":
			if okT && age > 8*time.Hour {
				limite(&lumieres, fmt.Sprintf("%s (%s)", nom, formaterDepuis(t)), 10)
			}
		case d == "automation" && e.State == "off":
			limite(&autoOff, nom, 12)
		case d == "update" && e.State == "on":
			majs++
		}
	}

	data := map[string]interface{}{}
	var morceaux []string
	ajouter := func(cle, libelle string, l []string) {
		if len(l) > 0 {
			data[cle] = l
			morceaux = append(morceaux, libelle+" : "+strings.Join(l, ", "))
		}
	}
	ajouter("indisponibles_depuis_plus_de_24h", "Indisponibles", indispos)
	ajouter("batteries_faibles", "Batteries faibles", batteries)
	ajouter("ouvertures_ouvertes_depuis_plus_de_30_min", "Ouvert", ouverts)
	ajouter("lumieres_allumees_depuis_plus_de_8h", "Lumières allumées longtemps", lumieres)
	ajouter("automatisations_desactivees", "Automatisations désactivées", autoOff)
	if majs > 0 {
		data["mises_a_jour_en_attente"] = majs
	}
	if len(morceaux) == 0 {
		data["remarque"] = "rien d'anormal détecté"
		return data, "Rien d'anormal.", nil
	}
	return data, strings.Join(morceaux, ". ") + ".", nil
}

// ---- Résumé d'une période ----

var domainesRacontables = map[string]bool{
	"light": true, "switch": true, "cover": true, "fan": true, "climate": true,
	"media_player": true, "automation": true, "script": true, "binary_sensor": true, "input_boolean": true,
}

const maxEvenementsResume = 60

// ResumePeriode lit le journal de tout Home Assistant sur une période et en garde ce qui
// compte (portes, lumières, volets, automatisations, scripts...) ; les détecteurs de
// mouvement sont comptés, pas listés.
func (c *Client) ResumePeriode(debut, fin time.Time) (map[string]interface{}, string, error) {
	brut, err := c.journalGlobal(debut, fin)
	if err != nil {
		return nil, "", err
	}
	etats, _ := c.etatsBruts()
	classes := map[string]string{}
	for _, e := range etats {
		classes[e.EntityID] = chaine(e.Attributes, "device_class")
	}

	mouvements := map[string]int{}
	declenchees := map[string]int{}
	var evts []map[string]interface{}
	for _, m := range brut {
		id := champ(m, "entity_id")
		d := domaineDepuisEntityID(id)
		if !domainesRacontables[d] || domainesExclus[d] {
			continue
		}
		classe := classes[id]
		nom := premierNonVide(champ(m, "name"), id)
		if d == "binary_sensor" {
			switch classe {
			case "motion", "occupancy", "presence":
				mouvements[nom]++
				continue
			case "door", "window", "opening", "garage_door":
			default:
				continue // les autres capteurs binaires sont du bruit
			}
		}
		_, e, ok := c.entreeJournal(m, d)
		if !ok {
			continue
		}
		if d == "automation" {
			declenchees[nom]++
		}
		evts = append(evts, map[string]interface{}{"quand": e.Quand, "nom": nom, "domaine": d, "etat": e.Etat, "message": e.Message, "cause": e.Cause})
	}

	data := map[string]interface{}{"periode": decrirePeriode(debut, fin), "nombre_evenements": len(evts)}
	if len(evts) > maxEvenementsResume {
		data["remarque"] = fmt.Sprintf("liste tronquée aux %d premiers événements sur %d", maxEvenementsResume, len(evts))
		evts = evts[:maxEvenementsResume]
	}
	if len(evts) > 0 {
		data["evenements"] = evts
	}
	if len(mouvements) > 0 {
		data["mouvements_detectes"] = mouvements
	}
	if len(declenchees) > 0 {
		data["automatisations_declenchees"] = declenchees
	}
	if len(evts) == 0 && len(mouvements) == 0 {
		return data, "Rien de notable dans le journal sur cette période.", nil
	}
	return data, fmt.Sprintf("%d événements notables sur cette période.", data["nombre_evenements"]), nil
}

// ---- Consommation d'énergie ----

var tarifKWh float64

// DefinirTarifKWh règle le prix du kWh (€) pour estimer le coût ; 0 = pas de coût.
func DefinirTarifKWh(t float64) { tarifKWh = t }

// facteurKWh convertit l'unité d'un compteur d'énergie en kWh (0 = unité non gérée).
func facteurKWh(unite string) float64 {
	switch strings.ToLower(strings.TrimSpace(unite)) {
	case "kwh":
		return 1
	case "wh":
		return 0.001
	case "mwh":
		return 1000
	}
	return 0
}

// Consommation calcule l'énergie consommée sur une période par les compteurs d'énergie
// (device_class « energy ») : somme des hausses du compteur, en tenant compte des remises à
// zéro. entityID facultatif restreint à un compteur. C'est l'IA qui dira quel compteur est
// le total de la maison (noms du type « consommation totale »).
func (c *Client) Consommation(debut, fin time.Time, entityID string) (map[string]interface{}, string, error) {
	etats, err := c.etatsBruts()
	if err != nil {
		return nil, "", err
	}
	type compteur struct {
		id, nom string
		f       float64
	}
	var compteurs []compteur
	for _, e := range etats {
		if domaineDepuisEntityID(e.EntityID) != "sensor" || chaine(e.Attributes, "device_class") != "energy" {
			continue
		}
		if entityID != "" && e.EntityID != entityID {
			continue
		}
		f := facteurKWh(chaine(e.Attributes, "unit_of_measurement"))
		if f == 0 {
			continue
		}
		compteurs = append(compteurs, compteur{id: e.EntityID, nom: nomEtat(e), f: f})
	}
	if len(compteurs) > 25 {
		compteurs = compteurs[:25]
	}
	data := map[string]interface{}{"periode": decrirePeriode(debut, fin)}
	if len(compteurs) == 0 {
		data["remarque"] = "aucun compteur d'énergie (device_class energy, en kWh ou Wh) trouvé"
		return data, "Je ne trouve aucun compteur d'énergie.", nil
	}
	ids := make([]string, len(compteurs))
	for i, k := range compteurs {
		ids[i] = k.id
	}
	segs, err := c.recupererSegmentsMulti(ids, debut, fin)
	if err != nil {
		return nil, "", err
	}

	type conso struct {
		nom string
		kwh float64
	}
	var res []conso
	for _, k := range compteurs {
		pts := pointsNumeriques(segs[k.id])
		if len(pts) < 2 {
			continue
		}
		total := 0.0
		for i := 1; i < len(pts); i++ {
			delta := pts[i].v - pts[i-1].v
			if delta < 0 { // remise à zéro du compteur : on repart de la nouvelle valeur
				delta = pts[i].v
			}
			total += delta
		}
		res = append(res, conso{nom: k.nom, kwh: total * k.f})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].kwh > res[j].kwh })
	if len(res) > 8 {
		res = res[:8]
	}
	var lignes []map[string]interface{}
	var textes []string
	for _, r := range res {
		l := map[string]interface{}{"compteur": r.nom, "kwh": math.Round(r.kwh*100) / 100}
		if tarifKWh > 0 {
			l["cout_estime_euros"] = math.Round(r.kwh*tarifKWh*100) / 100
		}
		lignes = append(lignes, l)
		textes = append(textes, fmt.Sprintf("%s : %s kWh", r.nom, strings.ReplaceAll(strconv.FormatFloat(math.Round(r.kwh*100)/100, 'f', -1, 64), ".", ",")))
	}
	data["compteurs"] = lignes
	if len(lignes) == 0 {
		data["remarque"] = "pas assez de mesures sur cette période"
		return data, "Pas assez de mesures sur cette période.", nil
	}
	return data, "Consommation : " + strings.Join(textes, " ; ") + ".", nil
}

// ---- Conseil : météo + mesures ----

// DonneesConseil réunit la météo (actuelle, 3 jours, pluie des 12 prochaines heures) et
// l'état actuel des entités demandées, pour répondre à « faut-il arroser aujourd'hui ? »,
// « je peux étendre le linge ? ».
func (c *Client) DonneesConseil(apps []Appareil) (map[string]interface{}, string, error) {
	etats, err := c.etatsBruts()
	if err != nil {
		return nil, "", err
	}
	parID := make(map[string]EtatBrut, len(etats))
	for _, e := range etats {
		parID[e.EntityID] = e
	}
	zones := c.ZonesEntites()

	data := map[string]interface{}{}
	var mesures []map[string]interface{}
	var textes []string
	meteoID := ""
	for _, a := range apps {
		e, ok := parID[a.EntityID]
		if !ok {
			continue
		}
		if a.Domain == "weather" {
			meteoID = a.EntityID
			continue
		}
		m := descriptionEntite(e)
		if z := zones[a.EntityID]; z != "" {
			m["piece"] = z
		}
		mesures = append(mesures, m)
		textes = append(textes, fmt.Sprintf("%s : %v", nomEtat(e), firstNonNil(m["valeur"], m["etat"])))
	}
	if len(mesures) > 0 {
		data["mesures"] = mesures
	}

	// Météo : l'entité demandée, sinon la première du catalogue
	if meteoID == "" {
		for _, e := range etats {
			if domaineDepuisEntityID(e.EntityID) == "weather" {
				meteoID = e.EntityID
				break
			}
		}
	}
	if meteoID != "" {
		meteo := map[string]interface{}{}
		if e, ok := parID[meteoID]; ok {
			meteo["maintenant"] = meteoExterieure([]EtatBrut{e})
		}
		if svc, ok := Lookup("weather"); ok {
			if sw, ok := svc.(*ServiceWeather); ok {
				if jours, err := sw.getPrevisions(meteoID, "daily"); err == nil {
					var out []map[string]interface{}
					for i, p := range jours {
						if i >= 3 {
							break
						}
						j := map[string]interface{}{"jour": jourRelatif(parseJour(p.DateTime)), "condition": tradCondition(p.Condition), "temperature_max": formaterValeur(p.Temperature, "°C")}
						if p.TempLow != nil {
							j["temperature_min"] = formaterValeur(*p.TempLow, "°C")
						}
						if p.Precipitation > 0 {
							j["pluie_mm"] = p.Precipitation
						}
						out = append(out, j)
					}
					meteo["prochains_jours"] = out
				}
				if heures, err := sw.getPrevisions(meteoID, "hourly"); err == nil {
					pluie, proba, n := 0.0, 0.0, 0
					for _, p := range heures {
						t := parseJour(p.DateTime)
						if t.IsZero() || t.Before(time.Now().Add(-time.Hour)) {
							continue
						}
						if n >= 12 {
							break
						}
						n++
						pluie += p.Precipitation
						if p.PrecipProb != nil && *p.PrecipProb > proba {
							proba = *p.PrecipProb
						}
					}
					meteo["pluie_prochaines_12h_mm"] = math.Round(pluie*10) / 10
					if proba > 0 {
						meteo["probabilite_pluie_max_12h_pourcent"] = proba
					}
				}
			}
		}
		data["meteo"] = meteo
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("aucune donnée disponible")
	}
	resume := strings.Join(textes, " ; ")
	if resume == "" {
		resume = "Voici la météo."
	}
	return data, resume, nil
}

func firstNonNil(vals ...interface{}) interface{} {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return ""
}


// ---- Inventaire d'une pièce (« que puis-je contrôler dans le salon ? ») ----

var libellesDomaines = map[string]string{
	"light": "lumières", "switch": "prises et interrupteurs", "cover": "volets et ouvrants", "fan": "ventilateurs",
	"climate": "thermostats", "media_player": "lecteurs multimédia", "vacuum": "aspirateurs",
	"input_boolean": "interrupteurs virtuels", "scene": "scènes",
}

// InventairePiece liste, par type, ce qu'on peut contrôler dans une pièce (entités rattachées
// dans HA) avec les verbes utilisables, pour que l'IA donne des exemples de phrases.
func (c *Client) InventairePiece(piece string) (map[string]interface{}, string, error) {
	etats, err := c.etatsBruts()
	if err != nil {
		return nil, "", err
	}
	zones := c.ZonesEntites()
	pieceNorm := text.Normaliser(strings.TrimSpace(piece))
	capacites := CapacitesIA()

	noms := map[string][]string{}
	for _, e := range etats {
		d := domaineDepuisEntityID(e.EntityID)
		if _, ok := libellesDomaines[d]; !ok || domainesExclus[d] {
			continue
		}
		if pieceNorm == "" || !strings.Contains(text.Normaliser(zones[e.EntityID]), pieceNorm) {
			continue
		}
		if e.State == "unavailable" {
			continue
		}
		noms[d] = append(noms[d], nomEtat(e))
	}

	data := map[string]interface{}{"piece": piece}
	if len(noms) == 0 {
		data["remarque"] = "aucun appareil contrôlable n'est rattaché à cette pièce dans Home Assistant (ou le registre des pièces est inaccessible)"
		return data, fmt.Sprintf("Je ne trouve aucun appareil contrôlable dans %s.", piece), nil
	}
	domaines := make([]string, 0, len(noms))
	for d := range noms {
		domaines = append(domaines, d)
	}
	sort.Strings(domaines)

	var groupes []map[string]interface{}
	var resume []string
	for _, d := range domaines {
		liste := noms[d]
		sort.Strings(liste)
		if len(liste) > 8 {
			liste = liste[:8]
		}
		verbes := capacites[d].Verbes
		if len(verbes) > 5 {
			verbes = verbes[:5]
		}
		groupes = append(groupes, map[string]interface{}{"type": libellesDomaines[d], "appareils": liste, "verbes": verbes})
		resume = append(resume, fmt.Sprintf("%s : %s", libellesDomaines[d], strings.Join(liste, ", ")))
	}
	data["appareils"] = groupes
	return data, fmt.Sprintf("Dans %s, tu peux contrôler — %s.", piece, strings.Join(resume, " ; ")), nil
}

// ---- Bilan de la semaine ----

// extremesClasse : le capteur le plus haut et le plus bas d'une classe (température,
// humidité…) sur la période, avec le moment où chaque extrême a été atteint.
func (c *Client) extremesClasse(classe string, debut, fin time.Time) map[string]interface{} {
	capteurs := c.capteursParClasse(classe, "")
	if len(capteurs) == 0 {
		return nil
	}
	if len(capteurs) > maxCapteursClassement {
		capteurs = capteurs[:maxCapteursClassement]
	}
	ids := make([]string, 0, len(capteurs))
	for _, cp := range capteurs {
		ids = append(ids, cp.EntityID)
	}
	segs, err := c.recupererSegmentsMulti(ids, debut, fin)
	if err != nil {
		return nil
	}
	type ext struct {
		nom, piece, unite string
		v                 float64
		t                 time.Time
	}
	var haut, bas *ext
	for _, cp := range capteurs {
		st, ok := statsNumeriques(segs[cp.EntityID])
		if !ok {
			continue
		}
		if haut == nil || st.Max > haut.v {
			haut = &ext{nom: cp.Nom, piece: cp.Piece, unite: cp.Unite, v: st.Max, t: st.TMax}
		}
		if bas == nil || st.Min < bas.v {
			bas = &ext{nom: cp.Nom, piece: cp.Piece, unite: cp.Unite, v: st.Min, t: st.TMin}
		}
	}
	if haut == nil {
		return nil
	}
	decrire := func(e *ext) map[string]interface{} {
		return map[string]interface{}{"capteur": e.nom, "piece": e.piece, "valeur": formaterValeur(e.v, e.unite), "quand": jourRelatif(e.t) + " à " + heureCourte(e.t)}
	}
	return map[string]interface{}{"le_plus_haut": decrire(haut), "le_plus_bas": decrire(bas)}
}

func topCompteurs(m map[string]int, n int) []map[string]interface{} {
	type kv struct {
		k string
		v int
	}
	var l []kv
	for k, v := range m {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool {
		if l[i].v != l[j].v {
			return l[i].v > l[j].v
		}
		return l[i].k < l[j].k
	})
	if len(l) > n {
		l = l[:n]
	}
	var out []map[string]interface{}
	for _, x := range l {
		out = append(out, map[string]interface{}{"nom": x.k, "fois": x.v})
	}
	return out
}

// BilanSemaine synthétise une période (une semaine par défaut) : consommation d'énergie,
// extrêmes de température et d'humidité, automatisations les plus déclenchées, ouvertures les
// plus fréquentes, et anomalies actuelles. Chaque source indisponible est simplement omise.
func (c *Client) BilanSemaine(debut, fin time.Time) (map[string]interface{}, string, error) {
	data := map[string]interface{}{"periode": decrirePeriode(debut, fin)}
	var morceaux []string

	if conso, resume, err := c.Consommation(debut, fin, ""); err == nil {
		if l, ok := conso["compteurs"]; ok {
			data["energie"] = l
			morceaux = append(morceaux, resume)
		}
	}
	for _, classe := range []string{"temperature", "humidity"} {
		if ext := c.extremesClasse(classe, debut, fin); ext != nil {
			data["extremes_"+classe] = ext
			morceaux = append(morceaux, fmt.Sprintf("%s : %v", nomMesure(classe), ext))
		}
	}

	if brut, err := c.journalGlobal(debut, fin); err == nil {
		etats, _ := c.etatsBruts()
		classes := map[string]string{}
		for _, e := range etats {
			classes[e.EntityID] = chaine(e.Attributes, "device_class")
		}
		auto, ouvertures := map[string]int{}, map[string]int{}
		for _, m := range brut {
			id := champ(m, "entity_id")
			nom := premierNonVide(champ(m, "name"), id)
			switch domaineDepuisEntityID(id) {
			case "automation":
				auto[nom]++
			case "binary_sensor":
				switch classes[id] {
				case "door", "window", "opening", "garage_door":
					if strings.EqualFold(champ(m, "state"), "on") {
						ouvertures[nom]++
					}
				}
			}
		}
		if len(auto) > 0 {
			data["automatisations_les_plus_declenchees"] = topCompteurs(auto, 5)
		}
		if len(ouvertures) > 0 {
			data["ouvertures_les_plus_frequentes"] = topCompteurs(ouvertures, 4)
		}
	}

	if diag, _, err := c.DiagnosticMaison(); err == nil {
		if _, rien := diag["remarque"]; !rien {
			data["anomalies_actuelles"] = diag
		}
	}
	if len(data) <= 1 {
		return data, "Je n'ai rien pu rassembler pour ce bilan.", nil
	}
	return data, "Voici le bilan : " + strings.Join(morceaux, ". ") + ".", nil
}
