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
	"ha-command-gateway/internal/utils/text"
)

// maxPointsHistorique borne le nombre de changements d'état lus pour une entité.
const maxPointsHistorique = 20000

// nbChangementsAffiches : au-delà, seuls les derniers changements sont énumérés.
const nbChangementsAffiches = 6

type pointHistorique struct {
	EntityID    string `json:"entity_id"`
	State       string `json:"state"`
	LastChanged string `json:"last_changed"`
}

// segmentHistorique est une durée pendant laquelle l'entité est restée dans un même état.
type segmentHistorique struct {
	Etat  string
	Debut time.Time
	Fin   time.Time
}

// segmentsDepuisPoints découpe l'historique d'une entité en segments d'état constant.
// Le premier segment porte l'état que l'entité avait au début de la période.
func segmentsDepuisPoints(points []pointHistorique, debut, fin time.Time) []segmentHistorique {
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
	return segs
}

// recupererSegmentsMulti lit l'historique HA de plusieurs entités en UNE requête et
// le découpe en segments par entité.
func (c *Client) recupererSegmentsMulti(ids []string, debut, fin time.Time) (map[string][]segmentHistorique, error) {
	const layout = "2006-01-02T15:04:05Z"
	path := fmt.Sprintf("/api/history/period/%s?filter_entity_id=%s&end_time=%s&no_attributes&minimal_response",
		debut.UTC().Format(layout), strings.Join(ids, ","), fin.UTC().Format(layout))

	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var hist [][]pointHistorique
	if err := json.Unmarshal(body, &hist); err != nil {
		return nil, err
	}

	res := make(map[string][]segmentHistorique, len(hist))
	for _, points := range hist {
		if len(points) == 0 {
			continue
		}
		id := points[0].EntityID
		if id == "" && len(ids) == 1 {
			id = ids[0]
		}
		if id != "" {
			res[id] = segmentsDepuisPoints(points, debut, fin)
		}
	}
	return res, nil
}

// recupererSegments lit l'historique HA d'une entité sur [debut, fin].
func (c *Client) recupererSegments(entityID string, debut, fin time.Time) ([]segmentHistorique, error) {
	res, err := c.recupererSegmentsMulti([]string{entityID}, debut, fin)
	if err != nil {
		return nil, err
	}
	return res[entityID], nil
}

// metaEntite retourne l'unité de mesure (« °C », « % ») et la classe d'appareil
// (« temperature », « humidity »...) actuelles de l'entité.
func (c *Client) metaEntite(entityID string) (unite, classe string) {
	etat, err := c.RecupererEtatLive(entityID)
	if err != nil || etat == nil {
		return "", ""
	}
	return etat.Attributes.Unit, etat.Attributes.DeviceClass
}

// ValeurHeure : une valeur et l'instant où elle a été atteinte.
type ValeurHeure struct {
	Valeur float64 `json:"valeur"`
	Heure  string  `json:"heure"`
}

// EchantillonHist : une valeur échantillonnée dans la période (courbe simplifiée).
type EchantillonHist struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

// EtatDuree : temps passé dans un état (entités non numériques).
type EtatDuree struct {
	Etat  string `json:"etat"`
	Duree string `json:"duree"`
}

// DonneesHist regroupe ce que le code sait d'une entité sur une période : le
// résumé prononçable (Resume) et des données structurées, compactes, qu'on peut
// confier à l'IA pour qu'elle les commente (analyse en deux appels).
type DonneesHist struct {
	Nom          string            `json:"nom"`
	Piece        string            `json:"piece,omitempty"`
	TypeMesure   string            `json:"type_mesure,omitempty"`
	Unite        string            `json:"unite,omitempty"`
	Periode      string            `json:"periode"`
	Numerique    bool              `json:"numerique"`
	Min          *ValeurHeure      `json:"min,omitempty"`
	Max          *ValeurHeure      `json:"max,omitempty"`
	Moyenne      *float64          `json:"moyenne,omitempty"`
	Echantillons []EchantillonHist `json:"echantillons,omitempty"`
	Etats        []EtatDuree       `json:"etats,omitempty"`
	Changements  []string          `json:"changements,omitempty"`
	Resume       string            `json:"-"`
}

// nbEchantillons : taille de la courbe simplifiée envoyée à l'IA.
const nbEchantillons = 24

// echantillonner ramène l'historique à n valeurs régulièrement espacées.
func echantillonner(segs []segmentHistorique, n int, avecJour bool) []EchantillonHist {
	if len(segs) == 0 || n < 2 {
		return nil
	}
	debut, fin := segs[0].Debut, segs[len(segs)-1].Fin
	pas := fin.Sub(debut) / time.Duration(n-1)
	var out []EchantillonHist
	k := 0
	for i := 0; i < n; i++ {
		t := debut.Add(time.Duration(i) * pas)
		for k+1 < len(segs) && !t.Before(segs[k].Fin) {
			k++
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(segs[k].Etat, ",", "."), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, EchantillonHist{T: formaterInstant(t, avecJour), V: math.Round(v*10) / 10})
	}
	return out
}

// DonneesHistorique lit l'historique d'une entité sur une période (passée).
func (c *Client) DonneesHistorique(app Appareil, debut, fin time.Time) (*DonneesHist, error) {
	segs, err := c.recupererSegments(app.EntityID, debut, fin)
	if err != nil {
		return nil, err
	}

	nom := app.FriendlyNameExact
	if nom == "" {
		nom = app.FriendlyName
	}
	periode := i18n.T("historique.periode", formaterInstant(debut, true), formaterInstant(fin, true))
	unite, classe := c.metaEntite(app.EntityID)
	d := &DonneesHist{Nom: nom, Piece: c.ZonesEntites()[app.EntityID], TypeMesure: classe, Unite: unite, Periode: periode}

	if len(segs) == 0 {
		d.Resume = i18n.T("historique.vide", nom, periode)
		return d, nil
	}
	avecJour := !memeJour(segs[0].Debut, segs[len(segs)-1].Fin)

	if st, ok := statsNumeriques(segs); ok {
		d.Numerique = true
		arrondi := func(v float64) float64 { return math.Round(v*10) / 10 }
		moy := arrondi(st.Moyenne)
		d.Min = &ValeurHeure{Valeur: arrondi(st.Min), Heure: formaterInstant(st.TMin, avecJour)}
		d.Max = &ValeurHeure{Valeur: arrondi(st.Max), Heure: formaterInstant(st.TMax, avecJour)}
		d.Moyenne = &moy
		d.Echantillons = echantillonner(segs, nbEchantillons, avecJour)
		d.Resume, _ = resumeNumerique(nom, periode, segs, unite)
		return d, nil
	}

	durees := map[string]time.Duration{}
	var ordre []string
	for _, s := range segs {
		if _, ok := durees[s.Etat]; !ok {
			ordre = append(ordre, s.Etat)
		}
		durees[s.Etat] += s.Fin.Sub(s.Debut)
	}
	sort.SliceStable(ordre, func(i, j int) bool { return durees[ordre[i]] > durees[ordre[j]] })
	for _, e := range ordre {
		d.Etats = append(d.Etats, EtatDuree{Etat: traduireEtat(app.Domain, e), Duree: decrireDuree(durees[e])})
	}
	changements := segs[1:]
	if len(changements) > nbChangementsAffiches {
		changements = changements[len(changements)-nbChangementsAffiches:]
	}
	for _, s := range changements {
		d.Changements = append(d.Changements, i18n.T("historique.changement.ligne", traduireEtat(app.Domain, s.Etat), formaterInstant(s.Debut, avecJour)))
	}
	d.Resume = resumeEtats(app.Domain, nom, periode, segs)
	return d, nil
}

// ResumerHistorique lit l'historique d'une entité sur une période (passée) et le
// résume en une phrase : minimum / maximum / moyenne pour un capteur numérique,
// durées passées dans chaque état et changements pour le reste.
// Le texte est construit ici (pas par l'IA).
func (c *Client) ResumerHistorique(app Appareil, debut, fin time.Time) (string, error) {
	d, err := c.DonneesHistorique(app, debut, fin)
	if err != nil {
		return "", err
	}
	return d.Resume, nil
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

// statsNum : statistiques d'un capteur numérique sur une période.
type statsNum struct {
	Min, Max, Moyenne float64
	TMin, TMax        time.Time
}

// statsNumeriques : min / max (avec leur instant) et moyenne pondérée par le temps.
// Retourne false si l'entité n'est pas majoritairement numérique.
func statsNumeriques(segs []segmentHistorique) (statsNum, bool) {
	var somme, duree float64
	var st statsNum
	n := 0
	for _, s := range segs {
		v, err := strconv.ParseFloat(strings.ReplaceAll(s.Etat, ",", "."), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		d := s.Fin.Sub(s.Debut).Seconds()
		somme += v * d
		duree += d
		if n == 0 || v < st.Min {
			st.Min, st.TMin = v, s.Debut
		}
		if n == 0 || v > st.Max {
			st.Max, st.TMax = v, s.Debut
		}
		n++
	}
	if n == 0 || n*2 < len(segs) {
		return statsNum{}, false
	}
	st.Moyenne = st.Min
	if duree > 0 {
		st.Moyenne = somme / duree
	}
	return st, true
}

// resumeNumerique : min / max / moyenne pondérée par le temps pour un capteur
// numérique. Retourne false si l'entité n'est pas majoritairement numérique.
func resumeNumerique(nom, periode string, segs []segmentHistorique, unite string) (string, bool) {
	st, ok := statsNumeriques(segs)
	if !ok {
		return "", false
	}
	if st.Min == st.Max {
		return i18n.T("historique.constant", nom, periode, formaterValeur(st.Min, unite)), true
	}
	avecJour := !memeJour(segs[0].Debut, segs[len(segs)-1].Fin)
	return i18n.T("historique.numerique", nom, periode,
		formaterValeur(st.Min, unite), formaterInstant(st.TMin, avecJour),
		formaterValeur(st.Max, unite), formaterInstant(st.TMax, avecJour),
		formaterValeur(st.Moyenne, unite)), true
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

// ---- Classement de capteurs (« quelle pièce est la plus humide ? ») ----

// maxCapteursClassement borne le nombre de capteurs dont on lit l'historique en une requête.
const maxCapteursClassement = 40

type capteurInfo struct {
	EntityID string
	Nom      string
	Piece    string
	Unite    string
	Valeur   float64 // valeur actuelle (NaN si non numérique)
}

// nomMesure : nom parlé d'une classe d'appareil (« humidity » → « humidité »).
func nomMesure(classe string) string {
	if cle := "mesure." + classe; i18n.Existe(cle) {
		return i18n.T(cle)
	}
	return classe
}

// capteursParClasse liste les capteurs (sensor.*) d'une classe d'appareil, avec leur
// pièce, éventuellement restreints à une pièce (nom de la zone HA ou du capteur).
func (c *Client) capteursParClasse(classe, piece string) []capteurInfo {
	body, err := c.get("/api/states")
	if err != nil {
		return nil
	}
	var etats []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			FriendlyName string `json:"friendly_name"`
			DeviceClass  string `json:"device_class"`
			Unit         string `json:"unit_of_measurement"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &etats); err != nil {
		return nil
	}

	zones := c.ZonesEntites()
	classe = strings.ToLower(strings.TrimSpace(classe))
	pieceNorm := text.Normaliser(strings.TrimSpace(piece))

	var out []capteurInfo
	for _, e := range etats {
		if !strings.HasPrefix(e.EntityID, "sensor.") || strings.ToLower(e.Attributes.DeviceClass) != classe {
			continue
		}
		zone := zones[e.EntityID]
		if pieceNorm != "" &&
			!strings.Contains(text.Normaliser(zone), pieceNorm) &&
			!strings.Contains(text.Normaliser(e.Attributes.FriendlyName), pieceNorm) {
			continue
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(e.State, ",", "."), 64)
		if err != nil {
			v = math.NaN()
		}
		nom := e.Attributes.FriendlyName
		if nom == "" {
			nom = e.EntityID
		}
		out = append(out, capteurInfo{EntityID: e.EntityID, Nom: nom, Piece: zone, Unite: e.Attributes.Unit, Valeur: v})
	}
	return out
}

type ligneClassement struct {
	label    string
	piece    string
	valeur   float64
	t        time.Time
	aInstant bool
	unite    string
}

// ClasserCapteurs classe les capteurs d'une classe d'appareil (humidité, température...).
//   - critere : « max », « min », « moyenne » (sur la période debut → fin), « actuel »
//     ou « actuel_min » (valeurs du moment, du plus bas au plus haut) ;
//   - piece : restreint à une pièce ; vide = on compare les pièces entre elles
//     (une ligne par pièce, la meilleure) ;
//   - debut / fin : période (zéro = valeurs actuelles), 7 jours maximum côté appelant.
//
// Le classement est calculé ici, pas par l'IA.
func (c *Client) ClasserCapteurs(classe, critere, piece string, debut, fin time.Time, top int) (string, error) {
	mesure := nomMesure(strings.ToLower(strings.TrimSpace(classe)))
	dans := ""
	if strings.TrimSpace(piece) != "" {
		dans = " " + i18n.T("classement.dans", strings.TrimSpace(piece))
	}

	capteurs := c.capteursParClasse(classe, piece)
	if len(capteurs) == 0 {
		return i18n.T("classement.vide", mesure, dans), nil
	}
	if top <= 0 {
		top = 3
	}
	if top > 8 {
		top = 8
	}

	periodique := !debut.IsZero() && fin.After(debut)
	switch strings.ToLower(strings.TrimSpace(critere)) {
	case "max", "maximum":
		critere = "max"
	case "min", "minimum":
		critere = "min"
	case "moyenne", "moyen":
		critere = "moyenne"
	case "actuel_min":
		critere = "actuel_min"
	default:
		critere = "actuel"
		if periodique {
			critere = "max"
		}
	}
	if !periodique && (critere == "max" || critere == "min" || critere == "moyenne") {
		critere = "actuel"
	}
	croissant := critere == "min" || critere == "actuel_min"

	var lignes []ligneClassement
	comparePieces := strings.TrimSpace(piece) == ""
	etiquette := func(cp capteurInfo) string {
		if comparePieces && cp.Piece != "" {
			return cp.Piece
		}
		return cp.Nom
	}

	if periodique && (critere == "max" || critere == "min" || critere == "moyenne") {
		if len(capteurs) > maxCapteursClassement {
			capteurs = capteurs[:maxCapteursClassement]
		}
		ids := make([]string, 0, len(capteurs))
		for _, cp := range capteurs {
			ids = append(ids, cp.EntityID)
		}
		segsParID, err := c.recupererSegmentsMulti(ids, debut, fin)
		if err != nil {
			return "", err
		}
		for _, cp := range capteurs {
			st, ok := statsNumeriques(segsParID[cp.EntityID])
			if !ok {
				continue
			}
			l := ligneClassement{label: etiquette(cp), piece: cp.Piece, unite: cp.Unite}
			switch critere {
			case "max":
				l.valeur, l.t, l.aInstant = st.Max, st.TMax, true
			case "min":
				l.valeur, l.t, l.aInstant = st.Min, st.TMin, true
			default:
				l.valeur = st.Moyenne
			}
			lignes = append(lignes, l)
		}
	} else {
		for _, cp := range capteurs {
			if !math.IsNaN(cp.Valeur) {
				lignes = append(lignes, ligneClassement{label: etiquette(cp), piece: cp.Piece, valeur: cp.Valeur, unite: cp.Unite})
			}
		}
	}
	if len(lignes) == 0 {
		return i18n.T("classement.vide", mesure, dans), nil
	}

	meilleur := func(a, b float64) bool {
		if croissant {
			return a < b
		}
		return a > b
	}
	// Comparaison de pièces : une seule ligne par pièce (la meilleure)
	if comparePieces {
		parPiece := map[string]int{}
		var gardees []ligneClassement
		for _, l := range lignes {
			if l.piece == "" {
				gardees = append(gardees, l)
				continue
			}
			if idx, ok := parPiece[l.piece]; ok {
				if meilleur(l.valeur, gardees[idx].valeur) {
					gardees[idx] = l
				}
				continue
			}
			parPiece[l.piece] = len(gardees)
			gardees = append(gardees, l)
		}
		lignes = gardees
	}
	sort.SliceStable(lignes, func(i, j int) bool { return meilleur(lignes[i].valeur, lignes[j].valeur) })
	if len(lignes) > top {
		lignes = lignes[:top]
	}

	avecJour := periodique && !memeJour(debut, fin)
	items := make([]string, 0, len(lignes))
	for _, l := range lignes {
		item := i18n.T("classement.ligne", l.label, formaterValeur(l.valeur, l.unite))
		if l.aInstant {
			item += " " + i18n.T("classement.a", formaterInstant(l.t, avecJour))
		}
		items = append(items, item)
	}

	libelle := i18n.T("critere." + critere)
	if periodique && (critere == "max" || critere == "min" || critere == "moyenne") {
		libelle += " " + i18n.T("historique.periode", formaterInstant(debut, true), formaterInstant(fin, true))
	}
	return i18n.T("classement.resultat", mesure, dans, libelle, strings.Join(items, " ; ")), nil
}
