package ha

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
)

// maxPointsHistorique borne le nombre de changements d'état lus pour une entité.
const maxPointsHistorique = 20000

// nbChangementsAffiches : au-delà, seuls les derniers changements sont énumérés.
const nbChangementsAffiches = 6

type pointHistorique struct {
	State       string `json:"state"`
	LastChanged string `json:"last_changed"`
}

// segmentHistorique est une durée pendant laquelle l'entité est restée dans un même état.
type segmentHistorique struct {
	Etat  string
	Debut time.Time
	Fin   time.Time
}

// recupererSegments lit l'historique HA d'une entité sur [debut, fin] et le
// découpe en segments d'état constant. Le premier segment porte l'état que
// l'entité avait au début de la période.
func (c *Client) recupererSegments(entityID string, debut, fin time.Time) ([]segmentHistorique, error) {
	const layout = "2006-01-02T15:04:05Z"
	path := fmt.Sprintf("/api/history/period/%s?filter_entity_id=%s&end_time=%s&no_attributes&minimal_response",
		debut.UTC().Format(layout), entityID, fin.UTC().Format(layout))

	body, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var hist [][]pointHistorique
	if err := json.Unmarshal(body, &hist); err != nil {
		return nil, err
	}
	if len(hist) == 0 || len(hist[0]) == 0 {
		return nil, nil
	}

	points := hist[0]
	if len(points) > maxPointsHistorique {
		points = points[:maxPointsHistorique]
	}

	var segs []segmentHistorique
	for i, p := range points {
		t := debut
		if i > 0 {
			if lt, err := time.Parse(time.RFC3339, p.LastChanged); err == nil && lt.After(debut) {
				t = lt
			}
		}
		if t.After(fin) {
			break
		}
		segs = append(segs, segmentHistorique{Etat: p.State, Debut: t})
	}
	for i := range segs {
		if i+1 < len(segs) {
			segs[i].Fin = segs[i+1].Debut
		} else {
			segs[i].Fin = fin
		}
	}
	return segs, nil
}

// uniteEntite retourne l'unité de mesure actuelle de l'entité (« °C », « % »...).
func (c *Client) uniteEntite(entityID string) string {
	etat, err := c.RecupererEtatLive(entityID)
	if err != nil || etat == nil {
		return ""
	}
	return etat.Attributes.Unit
}

// ResumerHistorique lit l'historique d'une entité sur une période (passée) et le
// résume en une phrase : minimum / maximum / moyenne pour un capteur numérique,
// durées passées dans chaque état et changements pour le reste.
// Le texte est construit ici (pas par l'IA).
func (c *Client) ResumerHistorique(app Appareil, debut, fin time.Time) (string, error) {
	segs, err := c.recupererSegments(app.EntityID, debut, fin)
	if err != nil {
		return "", err
	}

	nom := app.FriendlyNameExact
	if nom == "" {
		nom = app.FriendlyName
	}
	periode := i18n.T("historique.periode", formaterInstant(debut, true), formaterInstant(fin, true))

	if len(segs) == 0 {
		return i18n.T("historique.vide", nom, periode), nil
	}
	if texte, ok := resumeNumerique(nom, periode, segs, c.uniteEntite(app.EntityID)); ok {
		return texte, nil
	}
	return resumeEtats(app.Domain, nom, periode, segs), nil
}

func memeJour(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// formaterInstant : « 22h », « 6h05 », avec le jour (« samedi 19 septembre 22h ») si demandé.
func formaterInstant(t time.Time, avecJour bool) string {
	t = t.Local()
	heure := fmt.Sprintf("%dh", t.Hour())
	if t.Minute() > 0 {
		heure = fmt.Sprintf("%dh%02d", t.Hour(), t.Minute())
	}
	if !avecJour {
		return heure
	}
	return fmt.Sprintf("%s %d %s %s", joursFR[t.Weekday()], t.Day(), moisFR[t.Month()-1], heure)
}

// uniteParlee remplace les unités courantes par leur lecture à voix haute.
func uniteParlee(unite string) string {
	switch unite {
	case "°C":
		return "degrés"
	case "°F":
		return "degrés Fahrenheit"
	case "%":
		return "pour cent"
	case "W":
		return "watts"
	case "kW":
		return "kilowatts"
	case "kWh":
		return "kilowattheures"
	case "V":
		return "volts"
	case "A":
		return "ampères"
	case "lx":
		return "lux"
	case "hPa":
		return "hectopascals"
	case "km/h":
		return "kilomètres heure"
	}
	return unite
}

func formaterValeur(v float64, unite string) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	s = strings.ReplaceAll(s, ".", ",")
	if u := uniteParlee(unite); u != "" {
		s += " " + u
	}
	return s
}

// resumeNumerique : min / max / moyenne pondérée par le temps pour un capteur
// numérique. Retourne false si l'entité n'est pas majoritairement numérique.
func resumeNumerique(nom, periode string, segs []segmentHistorique, unite string) (string, bool) {
	var somme, duree, vmin, vmax float64
	var tMin, tMax time.Time
	n := 0
	for _, s := range segs {
		v, err := strconv.ParseFloat(strings.ReplaceAll(s.Etat, ",", "."), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		d := s.Fin.Sub(s.Debut).Seconds()
		somme += v * d
		duree += d
		if n == 0 || v < vmin {
			vmin, tMin = v, s.Debut
		}
		if n == 0 || v > vmax {
			vmax, tMax = v, s.Debut
		}
		n++
	}
	if n == 0 || n*2 < len(segs) {
		return "", false
	}

	if vmin == vmax {
		return i18n.T("historique.constant", nom, periode, formaterValeur(vmin, unite)), true
	}

	moy := vmin
	if duree > 0 {
		moy = somme / duree
	}
	avecJour := !memeJour(segs[0].Debut, segs[len(segs)-1].Fin)
	return i18n.T("historique.numerique", nom, periode,
		formaterValeur(vmin, unite), formaterInstant(tMin, avecJour),
		formaterValeur(vmax, unite), formaterInstant(tMax, avecJour),
		formaterValeur(moy, unite)), true
}

// traduireEtat traduit un état HA (« on », « closed »...) selon le domaine.
// Clés cherchées dans l'ordre : etat.<domaine>.<état>, etat.<état>, sinon l'état brut.
func traduireEtat(domaine, etat string) string {
	for _, cle := range []string{"etat." + domaine + "." + etat, "etat." + etat} {
		if i18n.Existe(cle) {
			return i18n.T(cle)
		}
	}
	return etat
}

// resumeEtats : durée passée dans chaque état + liste des changements.
func resumeEtats(domaine, nom, periode string, segs []segmentHistorique) string {
	durees := map[string]time.Duration{}
	var ordre []string
	for _, s := range segs {
		if _, ok := durees[s.Etat]; !ok {
			ordre = append(ordre, s.Etat)
		}
		durees[s.Etat] += s.Fin.Sub(s.Debut)
	}

	if len(ordre) == 1 {
		return i18n.T("historique.constant", nom, periode, traduireEtat(domaine, ordre[0]))
	}

	sort.SliceStable(ordre, func(i, j int) bool { return durees[ordre[i]] > durees[ordre[j]] })
	var parts []string
	for _, e := range ordre {
		if durees[e] < time.Minute {
			continue
		}
		parts = append(parts, i18n.T("historique.etat.duree", traduireEtat(domaine, e), decrireDuree(durees[e])))
	}
	if len(parts) == 0 {
		parts = append(parts, traduireEtat(domaine, ordre[0]))
	}
	texte := i18n.T("historique.etats", nom, periode, strings.Join(parts, ", "))

	// Changements d'état (le premier segment est l'état de départ, pas un changement)
	changements := segs[1:]
	if len(changements) == 0 {
		return texte
	}
	avecJour := !memeJour(segs[0].Debut, segs[len(segs)-1].Fin)
	cle := "historique.changements"
	if len(changements) > nbChangementsAffiches {
		changements = changements[len(changements)-nbChangementsAffiches:]
		cle = "historique.changements.derniers"
	}
	var lignes []string
	for _, s := range changements {
		lignes = append(lignes, i18n.T("historique.changement.ligne", traduireEtat(domaine, s.Etat), formaterInstant(s.Debut, avecJour)))
	}
	if cle == "historique.changements.derniers" {
		return texte + " " + i18n.T(cle, len(segs)-1, strings.Join(lignes, ", "))
	}
	return texte + " " + i18n.T(cle, strings.Join(lignes, ", "))
}
