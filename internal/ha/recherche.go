package ha

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
)

// Recherche d'événements dans la courbe d'un capteur numérique : plus forte chute ou
// hausse, première variation d'au moins X (éventuellement en moins de N minutes),
// passage sous ou au-dessus d'un seuil. C'est le code qui cherche dans les points de
// l'historique HA, pas l'IA.

type pointNum struct {
	t time.Time
	v float64
}

// PointRecherche : une valeur et le moment où elle a été atteinte.
type PointRecherche struct {
	Valeur string `json:"valeur"`
	Quand  string `json:"quand"`
}

// ResultatRecherche : ce que le code a trouvé (Resume = phrase déjà formulée).
type ResultatRecherche struct {
	Nom       string          `json:"nom"`
	Periode   string          `json:"periode"`
	Mode      string          `json:"mode"`
	Trouve    bool            `json:"trouve"`
	Depart    *PointRecherche `json:"depart,omitempty"`
	Arrivee   *PointRecherche `json:"arrivee,omitempty"`
	Amplitude string          `json:"amplitude,omitempty"`
	Duree     string          `json:"duree,omitempty"`
	Passages  int             `json:"passages,omitempty"`
	Resume    string          `json:"-"`
}

func pointsNumeriques(segs []segmentHistorique) []pointNum {
	var out []pointNum
	for _, s := range segs {
		v, err := strconv.ParseFloat(strings.ReplaceAll(s.Etat, ",", "."), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, pointNum{t: s.Debut, v: v})
	}
	return out
}

func negatif(pts []pointNum) []pointNum {
	out := make([]pointNum, len(pts))
	for i, p := range pts {
		out[i] = pointNum{t: p.t, v: -p.v}
	}
	return out
}

// plusForteChute : la plus grande baisse « pic → creux » (le pic précède le creux).
func plusForteChute(pts []pointNum) (iPic, iCreux int, amplitude float64) {
	maxI := 0
	for i := 1; i < len(pts); i++ {
		if pts[i].v > pts[maxI].v {
			maxI = i
		}
		if d := pts[maxI].v - pts[i].v; d > amplitude {
			amplitude, iPic, iCreux = d, maxI, i
		}
	}
	return iPic, iCreux, amplitude
}

// premiereChute : le premier creux atteignant une baisse d'au moins `seuil` depuis un pic
// antérieur, dans une fenêtre de temps (0 = sans limite). File monotone : O(n).
func premiereChute(pts []pointNum, seuil float64, fenetre time.Duration) (iPic, iCreux int, ok bool) {
	var dq []int // indices dont les valeurs décroissent : dq[0] = maximum de la fenêtre
	for i := range pts {
		if fenetre > 0 {
			for len(dq) > 0 && pts[i].t.Sub(pts[dq[0]].t) > fenetre {
				dq = dq[1:]
			}
		}
		if len(dq) > 0 && pts[dq[0]].v-pts[i].v >= seuil {
			return dq[0], i, true
		}
		for len(dq) > 0 && pts[dq[len(dq)-1]].v <= pts[i].v {
			dq = dq[:len(dq)-1]
		}
		dq = append(dq, i)
	}
	return 0, 0, false
}

// RechercheEvenement cherche un événement dans la courbe d'un capteur :
//   - « plus_forte_chute » / « plus_forte_hausse » ;
//   - « chute_au_moins » / « hausse_au_moins » : valeur = amplitude, fenetre facultative ;
//   - « passage_sous » / « passage_au_dessus » : valeur = seuil.
func (c *Client) RechercheEvenement(app Appareil, mode string, valeur float64, fenetre time.Duration, debut, fin time.Time) (*ResultatRecherche, error) {
	segs, err := c.recupererSegments(app.EntityID, debut, fin)
	if err != nil {
		return nil, err
	}
	nom := app.FriendlyNameExact
	if nom == "" {
		nom = app.FriendlyName
	}
	unite, _ := c.metaEntite(app.EntityID)
	periode := decrirePeriode(debut, fin)
	res := &ResultatRecherche{Nom: nom, Periode: periode, Mode: mode}

	pts := pointsNumeriques(segs)
	if len(pts) == 0 {
		res.Resume = i18n.T("recherche.vide", nom, periode)
		return res, nil
	}
	fmtV := func(v float64) string { return formaterValeur(v, unite) }
	quand := formaterQuandAuto

	// remplit le résultat d'une variation entre deux points (indices dans pts)
	variation := func(iP, iC int) (string, string, string, string, string, string) {
		p, q := pts[iP], pts[iC] // valeurs d'origine
		amp := math.Abs(p.v - q.v)
		duree := decrireDuree(q.t.Sub(p.t))
		res.Trouve = true
		res.Depart = &PointRecherche{Valeur: fmtV(p.v), Quand: quand(p.t)}
		res.Arrivee = &PointRecherche{Valeur: fmtV(q.v), Quand: quand(q.t)}
		res.Amplitude, res.Duree = fmtV(amp), duree
		return fmtV(p.v), quand(p.t), fmtV(q.v), quand(q.t), fmtV(amp), duree
	}

	switch mode {
	case "plus_forte_chute", "plus_forte_hausse":
		monte := mode == "plus_forte_hausse"
		serie := pts
		if monte {
			serie = negatif(pts)
		}
		iP, iC, amp := plusForteChute(serie)
		if amp <= 0 {
			res.Resume = i18n.T("recherche.aucune", nom, periode)
			return res, nil
		}
		v1, t1, v2, t2, a, d := variation(iP, iC)
		cle := "recherche.baisse.max"
		if monte {
			cle = "recherche.hausse.max"
		}
		res.Resume = i18n.T(cle, nom, periode, v1, t1, v2, t2, a, d)

	case "chute_au_moins", "hausse_au_moins":
		monte := mode == "hausse_au_moins"
		serie := pts
		if monte {
			serie = negatif(pts)
		}
		seuil := fmtV(valeur)
		if fenetre > 0 {
			seuil += " " + i18n.T("recherche.fenetre", decrireDuree(fenetre))
		}
		iP, iC, ok := premiereChute(serie, valeur, fenetre)
		if !ok {
			_, _, amp := plusForteChute(serie)
			cle := "recherche.baisse.absente"
			if monte {
				cle = "recherche.hausse.absente"
			}
			res.Resume = i18n.T(cle, nom, seuil, periode, fmtV(amp))
			return res, nil
		}
		v1, t1, v2, t2, a, d := variation(iP, iC)
		cle := "recherche.baisse.trouvee"
		if monte {
			cle = "recherche.hausse.trouvee"
		}
		res.Resume = i18n.T(cle, nom, seuil, periode, v1, t1, v2, t2, a, d)

	case "passage_sous", "passage_au_dessus":
		sous := mode == "passage_sous"
		enJeu := func(v float64) bool {
			if sous {
				return v < valeur
			}
			return v > valeur
		}
		var idx []int
		for i, p := range pts {
			if enJeu(p.v) && (i == 0 || !enJeu(pts[i-1].v)) {
				idx = append(idx, i)
			}
		}
		cle := "recherche.dessus"
		if sous {
			cle = "recherche.sous"
		}
		seuil := fmtV(valeur)
		switch {
		case len(idx) == 0:
			ext := pts[0]
			for _, p := range pts {
				if (sous && p.v < ext.v) || (!sous && p.v > ext.v) {
					ext = p
				}
			}
			res.Resume = i18n.T(cle+".absent", nom, seuil, periode, fmtV(ext.v), quand(ext.t))
		case idx[0] == 0:
			res.Trouve = true
			res.Resume = i18n.T(cle+".deja", nom, seuil, periode)
			if len(idx) > 1 {
				res.Passages = len(idx) - 1
				res.Resume += " " + i18n.T("recherche.repasse", quand(pts[idx[1]].t))
			}
		default:
			res.Trouve = true
			res.Passages = len(idx)
			res.Arrivee = &PointRecherche{Valeur: fmtV(pts[idx[0]].v), Quand: quand(pts[idx[0]].t)}
			res.Resume = i18n.T(cle+".trouve", nom, seuil, quand(pts[idx[0]].t), periode)
			if len(idx) > 1 {
				res.Resume += " " + i18n.T("recherche.passages", len(idx))
			}
		}

	default:
		return nil, fmt.Errorf("mode de recherche inconnu : %q", mode)
	}
	return res, nil
}
