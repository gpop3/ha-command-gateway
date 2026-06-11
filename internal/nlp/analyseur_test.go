package nlp

// =============================================================================
//  Harnais de calibrage NLP — white-box (package nlp)
// =============================================================================
//
//  But : mesurer la qualité du *matching d'entités* (la couche scorée)
//
// =============================================================================

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"ha-command-gateway/config"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	_ "ha-command-gateway/internal/i18n/locales" // enregistre la locale fr (init)
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/text"
)

// ----------------------------------------------------------------------------
//  Réglages de référence (les valeurs "actuelles" supposées du système)
// ----------------------------------------------------------------------------

const (
	mesPieces = "Salon,Cuisine,Chambre,Cellier,Bureau,Exterieur,Pergola,Serre"

	seuilMinimalRef  = 30
	seuilDesambigRef = 8
	margeFragile     = 10
)

func TestMain(m *testing.M) {
	logx.SetNiveau(logx.NiveauError)
	i18n.SetLocale("fr")
	m.Run()
}

func ap(id, fn, dom string) ha.Appareil {
	return ha.Appareil{EntityID: id, FriendlyName: fn, FriendlyNameExact: fn, Domain: dom, State: "off"}
}

func vraiCatalogue() []ha.Appareil {
	cat := []ha.Appareil{
		// --- Volets roulants ---
		ap("cover.salon_1", "Salon 1", "cover"),
		ap("cover.salon_2", "Salon 2", "cover"),
		ap("cover.salon_3", "Salon 3", "cover"),
		ap("cover.chambre", "Chambre", "cover"),
		ap("cover.chambre_2", "Chambre 2", "cover"),
		ap("cover.cuisine", "Cuisine", "cover"),
		ap("cover.volets_maison", "Volets maison", "cover"),

		// --- Thérmostats ---
		ap("climate.netatmo_smart_thermostat", "Netatmo Smart Thermostat", "climate"),
		ap("climate.thermostat_bureau_thermostat", "Thermostat bureau Thermostat", "climate"),
		ap("climate.radiateur_chambre_1_thermostat", "Radiateur chambre 1 Thermostat", "climate"),

		// --- Lumière ---
		ap("light.lumiere_tele", "Lumière télé", "light"),
		ap("light.lumiere_tele_segments", "Lumière télé Segments", "light"),
		ap("light.p1s_lumiere_de_la_chambre", "P1S Lumière de la chambre", "light"),

		// --- Capteurs ---
		ap("sensor.thermostat_serre_temperature", "Thermostat serre Température", "sensor"),
		ap("sensor.thermostat_serre_humidite", "Thermostat serre Humidité", "sensor"),
		ap("sensor.thermometre_cuisine_temperature", "Thermomètre cuisine Température", "sensor"),
		ap("sensor.thermometre_cuisine_humidite", "Thermomètre cuisine Humidité", "sensor"),
		ap("sensor.netatmo_smart_thermostat_current_temperature", "Netatmo Smart Thermostat Current Temperature", "sensor"),
		ap("sensor.day_0_name", "day_0_name", "sensor"),
		ap("sensor.day_1_name", "day_1_name", "sensor"),

		// --- Interrupteurs ---
		ap("switch.thermostat_bureau_commutateur", "Thermostat bureau Commutateur", "switch"),
		ap("switch.radiateur_chambre_1_commutateur", "Radiateur chambre 1 Commutateur", "switch"),
		ap("switch.lave_vaisselle", "Lave vaisselle", "switch"),
		ap("switch.machine_a_laver", "Machine à laver", "switch"),
		ap("switch.sonoff_s60zbtpf", "SONOFF S60ZBTPF", "switch"),
		ap("switch.lumiere_tele_music_mode", "Lumière télé Music Mode", "switch"),
		ap("switch.lumiere_tele_dreamview", "Lumière télé DreamView", "switch"),

		// --- Media ---
		ap("media_player.spotify", "Spotify", "media_player"),
		ap("media_player.barre_de_son", "Barre de son", "media_player"),
		ap("media_player.echo_dot", "Echo Dot", "media_player"),
		ap("media_player.partout", "Partout", "media_player"),
		ap("media_player.freebox_player_pop", "Freebox Player POP", "media_player"),
		ap("media_player.android_tv_2062229079", "Android TV-2062229079", "media_player"),

		// --- Aspirateur ---
		ap("vacuum.laveur", " laveur", "vacuum"),

		// --- Temps ---
		ap("weather.forecast_maison", "Forecast Maison", "weather"),

		// --- Bruits divers ---
		ap("sensor.salon_1_battery", "Salon 1 Battery", "sensor"),
		ap("sensor.salon_2_battery", "Salon 2 Battery", "sensor"),
		ap("sensor.chambre_battery", "Chambre Battery", "sensor"),
		ap("sensor.chambre_2_battery", "Chambre 2 Battery", "sensor"),
		ap("switch.barre_de_son_ne_pas_deranger", "Barre de son Ne pas déranger", "switch"),
		ap("fan.p1s_ventilateur_de_la_chambre", "P1S Ventilateur de la chambre", "fan"),
		ap("sensor.thermostat_bureau_batterie", "Thermostat bureau Batterie", "sensor"),
		ap("sensor.radiateur_chambre_1_batterie", "Radiateur chambre 1 Batterie", "sensor"),
	}

	// Entités virtuelles injectées
	cat = append(cat,
		ap("time.local", "heure", "time"),
		ap("time.date", "jour date", "time"),
		ap("agenda.home", "agenda", "agenda"),
		ap("resume_maison.local", "résumé maison", "resume_maison"),
	)
	return cat
}

// ----------------------------------------------------------------------------
//  Construction de l'analyseur de test
// ----------------------------------------------------------------------------

func analyseurDeTest(t *testing.T) *Analyseur {
	t.Helper()
	cfg := &config.Config{ServicesFile: "", HAPieces: mesPieces}
	cli := ha.NewClient("http://127.0.0.1:1", "fake-token", mesPieces, time.Second, cfg)

	a := New(cli, false,
		ConfigDesambiguisation{
			Active:   true,
			Seuil:    seuilDesambigRef,
			MaxChoix: 3,
		},
		ConfigScore{
			Minimal:               seuilMinimalRef,
			BonusPiece:            100,
			BonusMot:              20,
			BonusFuzzy:            15,
			MalusPieceSeule:       80,
			BonusLieuFonction:     60,
			BonusCouvertureExacte: 10,
			MalusMotSuperflu:      2,
			MalusActionSansCible:  50,
		},
	)

	cat := vraiCatalogue()
	a.catalogue = cat
	a.catalogueIndex = indexerParDomaine(cat)
	a.dernierRafraichissement = time.Now()
	return a
}

func indexerParDomaine(cat []ha.Appareil) map[string][]ha.Appareil {
	idx := make(map[string][]ha.Appareil)
	for _, app := range cat {
		idx[app.Domain] = append(idx[app.Domain], app)
	}
	return idx
}

// ----------------------------------------------------------------------------
//  Classement par retrait du gagnant
// ----------------------------------------------------------------------------

type resultat struct {
	App   ha.Appareil
	Score int
}

// classement renvoie le VRAI classement global, en scorant chaque entité
func classement(a *Analyseur, phrase string, k int) []resultat {
	texte := text.Normaliser(phrase)
	estAction := phraseEstAction(texte)
	motsSMS := strings.Fields(texte)

	modificateur := ""
	for _, p := range motsParasites {
		if strings.Contains(texte, p) {
			modificateur = p
			break
		}
	}

	out := make([]resultat, 0, len(a.catalogue))
	for _, app := range a.catalogue {
		s := a.scorerAppareil(app, motsSMS, texte, modificateur, estAction)
		out = append(out, resultat{App: app, Score: s})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out
}

// choixReel renvoie ce que le système choisit
func choixReel(a *Analyseur, phrase string) (ha.Appareil, int) {
	texte := text.Normaliser(phrase)
	return a.TrouverMeilleurMatch(texte, phraseEstAction(texte), ha.ListDomaines())
}

// phraseEstAction réplique l'esprit de detecterVerbe via Service.Verbe
func phraseEstAction(texteNormalise string) bool {
	for _, mot := range strings.Fields(texteNormalise) {
		for _, dom := range ha.ListDomaines() {
			if svc, ok := ha.Lookup(dom); ok {
				if _, ok := svc.Verbe(mot); ok {
					return true
				}
			}
		}
	}
	return false
}

// ----------------------------------------------------------------------------
//  Décision : reproduit la politique seuil/minimal/désambiguïsation, balayable
// ----------------------------------------------------------------------------

type decision int

const (
	decREJET decision = iota
	decDESAMBIG
	decDIRECT
)

func (d decision) String() string {
	switch d {
	case decREJET:
		return "REJET"
	case decDESAMBIG:
		return "DESAMBIG"
	default:
		return "DIRECT"
	}
}

// decide applique (minimal, seuil) au classement
func decide(rang []resultat, minimal, seuil int) (decision, []resultat) {
	if len(rang) == 0 || rang[0].Score < minimal {
		return decREJET, nil
	}
	candidats := []resultat{rang[0]}
	for _, r := range rang[1:] {
		if rang[0].Score-r.Score <= seuil {
			candidats = append(candidats, r)
		}
	}
	if len(candidats) >= 2 {
		return decDESAMBIG, candidats
	}
	return decDIRECT, candidats
}

// ----------------------------------------------------------------------------
//  Jeu de cas : combinatoire (couverture) + curatés (cas durs / réels)
// ----------------------------------------------------------------------------

type cas struct {
	phrase   string
	domaine  string
	piece    string
	desambig bool
	note     string
}

// intention = un objet réel qu'on peut viser, son domaine/pièce attendus et les verbes plausibles
type intention struct {
	objet   string
	domaine string
	piece   string
	verbes  []string
}

func jeuDeCas() []cas {
	var liste []cas

	vCover := []string{"ouvre", "ferme", "baisse", "monte", "remonte"}
	vClim := []string{"monte", "baisse", "regle", "augmente", "diminue"}
	vLight := []string{"allume", "eteins"}
	vMedia := []string{"lance", "mets", "joue", "coupe"}
	vVac := []string{"lance", "demarre", "arrete"}

	intents := []intention{
		{"volet", "cover", "salon", vCover},
		{"volet", "cover", "chambre", vCover},
		{"volet", "cover", "cuisine", vCover},
		{"store", "cover", "salon", vCover},
		{"volets", "cover", "", vCover},
		{"thermostat", "climate", "bureau", vClim},
		{"radiateur", "climate", "chambre", vClim},
		{"chauffage", "climate", "bureau", vClim},
		{"temperature", "sensor", "serre", nil},
		{"humidite", "sensor", "serre", nil},
		{"temperature", "sensor", "cuisine", nil},
		{"humidite", "sensor", "cuisine", nil},
		{"lumiere tele", "light", "", vLight},
		{"spotify", "media_player", "", vMedia},
		{"barre de son", "media_player", "", vMedia},
		{"musique", "media_player", "", vMedia},
		{"laveur", "vacuum", "", vVac},
		{"heure", "time", "", nil},
		{"meteo", "weather", "", nil},
		{"agenda", "agenda", "", nil},
	}

	for _, it := range intents {
		o, p, dom := it.objet, it.piece, it.domaine
		add := func(phrase string) {
			liste = append(liste, cas{phrase: phrase, domaine: dom, piece: p, note: "généré"})
		}
		if p != "" {
			add(o + " " + p)
			add(p + " " + o)
			add("etat " + o + " " + p)
			for _, v := range it.verbes {
				add(v + " " + o + " " + p)
				add(v + " le " + o + " " + p)
				add(v + " " + o + " de la " + p)
			}
			if dom == "sensor" {
				add("quelle " + o + " " + p)
				add("quelle est la " + o + " de la " + p)
				add(o + " dans la " + p)
			}
		} else {
			add(o)
			for _, v := range it.verbes {
				add(v + " " + o)
				add(v + " la " + o)
			}
			if len(it.verbes) == 0 {
				add("quelle " + o)
				add("donne moi " + o)
				add("dis moi " + o)
			}
		}
	}

	// --- Cas possibles ---
	curates := []cas{
		structCas("thermostat serre", "sensor", "serre", false, "réel: -> capteurs serre (pas de climate.serre)"),
		structCas("thermostat serre humidie", "sensor", "serre", false, "réel mangle 'humidié' -> capteur humidité serre"),
		structCas("quelle heure est il", "time", "", false, "réel: heure"),
		structCas("allume lumiere tele", "light", "", false, "réel: Lumière télé (accents dans le vrai nom !)"),
		structCas("quel jour sommes nous", "time", "", false, "réel mangle: jour/date"),

		// Vosk
		structCas("humidie serre", "sensor", "serre", false, "fuzzy 'humidie'->'humidité' (accent !)"),
		structCas("temperature cuisine", "sensor", "cuisine", false, "Thermomètre cuisine Température (accent)"),
		structCas("met spotify", "media_player", "", false, "mangle 'mets'"),
		structCas("lumiere televiseur", "light", "", false, "fuzzy 'televiseur'->'télé' ?"),

		// Ambigus
		structCas("salon", "", "salon", true, "3 covers Salon + batteries -> choix"),
		structCas("chambre", "", "chambre", true, "covers + climate + light imprimante + battery"),
		structCas("thermostat", "", "", true, "netatmo vs bureau vs (commutateur switch)"),
		structCas("ferme volet", "", "", true, "objet sans pièce -> ambigu"),

		// Rejets attendus
		structCas("temperature cellier", "", "", false, "aucune entité cellier -> rejet"),
		structCas("volet exterieur", "", "", false, "aucune entité exterieur -> rejet"),
		structCas("lumiere pergola", "", "", false, "aucune lampe pergola -> rejet"),
		structCas("aspirateur", "", "", false, "l'aspi s'appelle 'laveur' -> 'aspirateur' ne matche rien"),
		structCas("lance aspirateur", "", "", false, "idem: nommage 'laveur' vs 'aspirateur'"),
		structCas("bonjour", "", "", false, "salutation -> rejet"),
		structCas("raconte une blague", "", "", false, "hors périmètre -> rejet"),
		structCas("ferme la porte du garage", "", "", false, "ni pièce ni objet connus -> rejet"),
	}
	liste = append(liste, curates...)
	return liste
}

func structCas(phrase, domaine, piece string, desambig bool, note string) cas {
	return cas{phrase: phrase, domaine: domaine, piece: piece, desambig: desambig, note: note}
}

// ----------------------------------------------------------------------------
//  Utilitaires d'évaluation
// ----------------------------------------------------------------------------

var piecesNorm = func() []string {
	var p []string
	for _, brut := range strings.Split(mesPieces, ",") {
		p = append(p, text.Normaliser(strings.TrimSpace(brut)))
	}
	return p
}()

// pieceDe déduit la pièce d'une entité
func pieceDe(app ha.Appareil) string {
	hay := text.Normaliser(app.FriendlyName + " " + app.EntityID)
	for _, p := range piecesNorm {
		for _, mot := range strings.FieldsFunc(hay, func(r rune) bool { return r == ' ' || r == '_' || r == '.' }) {
			if mot == p {
				return p
			}
		}
	}
	return ""
}

// ok indique si le résultat correspond à l'attendu du cas
func (c cas) ok(d decision, rang []resultat) bool {
	switch {
	case c.desambig:
		return d == decDESAMBIG
	case c.domaine == "":
		return d == decREJET
	default:
		if d == decREJET || len(rang) == 0 {
			return false
		}
		top := rang[0].App
		return top.Domain == c.domaine && pieceDe(top) == c.piece
	}
}

func formatRang(rang []resultat, n int) string {
	var b strings.Builder
	for i, r := range rang {
		if i >= n {
			break
		}
		fmt.Fprintf(&b, "    #%d %-26s dom=%-13s pièce=%-9s score=%d\n",
			i+1, r.App.EntityID, r.App.Domain, pieceDe(r.App), r.Score)
	}
	return b.String()
}

// ----------------------------------------------------------------------------
//  TEST 1 — Matching + marge (réglages de référence)
// ----------------------------------------------------------------------------

func TestHarnaisMatching(t *testing.T) {
	a := analyseurDeTest(t)
	jeu := jeuDeCas()

	var reussis, fragiles int
	var sommeMarge int
	var nbMargeMesuree int

	for _, c := range jeu {
		rang := classement(a, c.phrase, 4)
		d, _ := decide(rang, seuilMinimalRef, seuilDesambigRef)
		bon := c.ok(d, rang)
		if bon {
			reussis++
		}

		marge := -1
		if len(rang) >= 2 {
			marge = rang[0].Score - rang[1].Score
			sommeMarge += marge
			nbMargeMesuree++
		} else if len(rang) == 1 {
			marge = rang[0].Score
		}

		if !bon {
			t.Errorf("❌ %q\n    attendu: %s | obtenu: %s | note: %s\n%s",
				c.phrase, attenduStr(c), d, c.note, formatRang(rang, 3))
		} else if d == decDIRECT && marge >= 0 && marge < margeFragile {
			fragiles++
			t.Logf("⚠️  fragile (marge %d) %q -> %s", marge, c.phrase, rang[0].App.EntityID)
		}
	}

	moyMarge := 0
	if nbMargeMesuree > 0 {
		moyMarge = sommeMarge / nbMargeMesuree
	}
	t.Logf("\n=== Matching (minimal=%d seuil=%d) : %d/%d OK | %d victoires fragiles | marge moyenne top1-top2 = %d ===",
		seuilMinimalRef, seuilDesambigRef, reussis, len(jeu), fragiles, moyMarge)
}

func attenduStr(c cas) string {
	switch {
	case c.desambig:
		return "DESAMBIG"
	case c.domaine == "":
		return "REJET"
	default:
		return fmt.Sprintf("DIRECT %s/%s", c.domaine, c.piece)
	}
}

// ----------------------------------------------------------------------------
//  TEST 2 — Cohérence de la désambiguïsation
// ----------------------------------------------------------------------------

func TestHarnaisDesambiguisation(t *testing.T) {
	a := analyseurDeTest(t)

	for _, c := range jeuDeCas() {
		rang := classement(a, c.phrase, 4)
		d, candidats := decide(rang, seuilMinimalRef, seuilDesambigRef)

		if c.desambig && d != decDESAMBIG {
			t.Errorf("❌ devait désambiguïser %q -> %s\n%s", c.phrase, d, formatRang(rang, 3))
			continue
		}
		if !c.desambig && c.domaine != "" && d == decDESAMBIG {
			t.Errorf("❌ désambiguïsation parasite %q (attendu gagnant net)\n%s", c.phrase, formatRang(rang, 3))
			continue
		}

		if d == decDESAMBIG {
			domaines := map[string]bool{}
			for _, r := range candidats {
				domaines[r.App.Domain] = true
			}
			if len(domaines) > 1 {
				t.Logf("⚠️  options hétérogènes pour %q : %d domaines\n%s",
					c.phrase, len(domaines), formatRang(candidats, len(candidats)))
			}
		}
	}
}

// ----------------------------------------------------------------------------
//
//	TEST 3 — Calibrage : balayage (minimal × seuil) sur le classement caché
//
// ----------------------------------------------------------------------------
func TestCalibrage(t *testing.T) {
	a := analyseurDeTest(t)
	jeu := jeuDeCas()

	rangs := make([][]resultat, len(jeu))
	for i, c := range jeu {
		rangs[i] = classement(a, c.phrase, 4)
	}

	minimaux := []int{20, 25, 30, 35, 40}
	seuils := []int{3, 5, 8, 12}

	t.Logf("\n=== Balayage de calibrage (%d cas) ===", len(jeu))
	t.Logf("%-8s %-7s %-7s %-10s %-12s %-10s %-9s",
		"MINIMAL", "SEUIL", "OK", "rejets+", "désambig", "fauxNég", "%OK")

	type ligne struct {
		minimal, seuil, ok int
	}
	var meilleure ligne

	for _, mn := range minimaux {
		for _, sl := range seuils {
			var ok, rejetsBons, desambigs, fauxNeg int
			for i, c := range jeu {
				d, _ := decide(rangs[i], mn, sl)
				if c.ok(d, rangs[i]) {
					ok++
				}
				if d == decDESAMBIG {
					desambigs++
				}
				if !c.desambig && c.domaine == "" && d == decREJET {
					rejetsBons++
				}
				if !c.desambig && c.domaine != "" && d == decREJET {
					fauxNeg++
				}
			}
			pct := ok * 100 / len(jeu)
			t.Logf("%-8d %-7d %-7d %-10d %-12d %-10d %-9d",
				mn, sl, ok, rejetsBons, desambigs, fauxNeg, pct)
			if ok > meilleure.ok {
				meilleure = ligne{mn, sl, ok}
			}
		}
	}
	t.Logf("\n>>> Meilleur couple sur ce jeu : MINIMAL=%d SEUIL=%d (%d/%d OK)\n",
		meilleure.minimal, meilleure.seuil, meilleure.ok, len(jeu))
}

// ----------------------------------------------------------------------------
//
//	TEST 5 — Divergence sélection : top1 du SCORING vs choix RÉEL de TMM
//
// ----------------------------------------------------------------------------
func TestHarnaisSelection(t *testing.T) {
	a := analyseurDeTest(t)
	var divergences int
	for _, c := range jeuDeCas() {
		rang := classement(a, c.phrase, 1)
		if len(rang) == 0 {
			continue
		}
		vrai := rang[0]
		reel, scoreReel := choixReel(a, c.phrase)
		if reel.EntityID != vrai.App.EntityID && vrai.Score >= seuilMinimalRef {
			divergences++
			t.Logf("≠ %-34q top-scoring=%s(%d)  mais TMM choisit=%s(%d)",
				c.phrase, vrai.App.EntityID, vrai.Score, reel.EntityID, scoreReel)
		}
	}
	t.Logf("\n=== Sélection : %d cas où TrouverMeilleurMatch masque le meilleur score global ===", divergences)
}

func TestHarnaisConfusion(t *testing.T) {
	a := analyseurDeTest(t)
	type paire struct{ attendu, obtenu string }
	conf := map[paire]int{}

	for _, c := range jeuDeCas() {
		rang := classement(a, c.phrase, 4)
		d, _ := decide(rang, seuilMinimalRef, seuilDesambigRef)
		if c.ok(d, rang) {
			continue
		}
		obtenu := d.String()
		if d == decDIRECT && len(rang) > 0 {
			obtenu = fmt.Sprintf("%s/%s", rang[0].App.Domain, pieceDe(rang[0].App))
		}
		conf[paire{attenduStr(c), obtenu}]++
	}

	if len(conf) == 0 {
		t.Logf("Aucun raté 🎉")
		return
	}
	type kv struct {
		p paire
		n int
	}
	var lignes []kv
	for p, n := range conf {
		lignes = append(lignes, kv{p, n})
	}
	sort.Slice(lignes, func(i, j int) bool { return lignes[i].n > lignes[j].n })
	t.Logf("\n=== Confusions (attendu -> obtenu) ===")
	for _, l := range lignes {
		t.Logf("  %-22s -> %-22s  x%d", l.p.attendu, l.p.obtenu, l.n)
	}
}
