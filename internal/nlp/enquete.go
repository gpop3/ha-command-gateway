package nlp

import (
	"strings"
	"time"

	"ha-command-gateway/internal/core/adapters/gemini"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/pkg/types"
)

// ---- « Annule ça » : pile d'annulation par session ----

const (
	dureeAnnulation      = 15 * time.Minute
	maxGroupesAnnulation = 5
)

// groupeAnnulation : les états d'avant d'UNE commande de l'IA (peut concerner plusieurs
// entités) et les cibles qu'on ne saurait pas annuler (scripts, SMS...).
type groupeAnnulation struct {
	quand         time.Time
	etats         []ha.EtatSauve
	nonAnnulables []string
}

func (a *Analyseur) empilerAnnulation(session string, g groupeAnnulation) {
	if len(g.etats) == 0 && len(g.nonAnnulables) == 0 {
		return
	}
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	var garde []groupeAnnulation
	for _, x := range a.annulations[session] {
		if time.Since(x.quand) < dureeAnnulation {
			garde = append(garde, x)
		}
	}
	garde = append(garde, g)
	if len(garde) > maxGroupesAnnulation {
		garde = garde[len(garde)-maxGroupesAnnulation:]
	}
	a.annulations[session] = garde
}

func (a *Analyseur) depilerAnnulation(session string) (groupeAnnulation, bool) {
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	pile := a.annulations[session]
	for len(pile) > 0 {
		g := pile[len(pile)-1]
		pile = pile[:len(pile)-1]
		if time.Since(g.quand) < dureeAnnulation {
			a.annulations[session] = pile
			return g, true
		}
	}
	a.annulations[session] = nil
	return groupeAnnulation{}, false
}

func sansDoublons(noms []string) []string {
	vus := map[string]bool{}
	var out []string
	for _, n := range noms {
		if !vus[n] {
			vus[n] = true
			out = append(out, n)
		}
	}
	return out
}

// executerAnnulation remet les entités dans l'état où elles étaient avant la dernière
// commande de l'IA. Ni un SMS envoyé, ni un script ou une automatisation exécutés ne
// peuvent être annulés : on le dit.
func (a *Analyseur) executerAnnulation(session string) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	g, ok := a.depilerAnnulation(session)
	if !ok {
		msg := messageTexte(i18n.T("annulation.rien"))
		return &msg, "", true, false, nil, nil
	}

	var remis, echecs []string
	// Ordre inverse : si une entité a été touchée deux fois, on finit sur son état le plus ancien
	for i := len(g.etats) - 1; i >= 0; i-- {
		e := g.etats[i]
		if err := a.haClient.Restaurer(e); err != nil {
			logx.WarnT("annulation.echec.log", e.EntityID, err)
			echecs = append(echecs, e.Nom)
		} else {
			remis = append(remis, e.Nom)
		}
	}
	remis, echecs = sansDoublons(remis), sansDoublons(echecs)

	var parts []string
	if len(remis) > 0 {
		parts = append(parts, i18n.T("annulation.ok", listeNoms(remis)))
	}
	if len(echecs) > 0 {
		parts = append(parts, i18n.T("annulation.echec", listeNoms(echecs)))
	}
	if nn := sansDoublons(g.nonAnnulables); len(nn) > 0 {
		parts = append(parts, i18n.T("annulation.impossible", listeNoms(nn)))
	}
	msg := messageTexte(strings.Join(parts, " "))
	return &msg, "", true, false, nil, nil
}

// ---- Enquêtes : le code rassemble des faits, l'IA les explique ----

// executerEnqueteGemini traite le type « enquete » : pourquoi une automatisation ne s'est pas
// déclenchée, diagnostic d'une pièce ou de la maison, résumé d'une période, consommation
// d'énergie, conseil (météo + mesures), annulation.
func (a *Analyseur) executerEnqueteGemini(session, texte string, rep *gemini.Reponse) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	e := rep.Enquete
	if e == nil || strings.TrimSpace(e.Sujet) == "" {
		return nil, "", false, false, nil, []string{i18n.T("gemini.rejet.enquete")}
	}
	sujet := strings.ToLower(strings.TrimSpace(e.Sujet))
	maintenant := time.Now()

	// entites valide les entités de l'IA (« actions ») ; les invalides sont signalées.
	var rejets []string
	entites := func() []ha.Appareil {
		var out []ha.Appareil
		for i, act := range rep.Actions {
			if i >= maxActionsGemini {
				break
			}
			act.Domain = domaineDeEntite(act.EntityID, act.Domain)
			app := a.trouverAppareil(act.EntityID)
			if raison := raisonEntite(act, app); raison != "" {
				rejets = append(rejets, raison)
				continue
			}
			out = append(out, *app)
		}
		return out
	}
	periode := func(defautDebut time.Time, plafond time.Duration) (time.Time, time.Time) {
		debut, ok := parserDateIA(e.Debut)
		if !ok {
			debut = defautDebut
		}
		fin, ok := parserDateIA(e.Fin)
		if !ok || fin.After(maintenant) {
			fin = maintenant
		}
		if !fin.After(debut) {
			debut, fin = defautDebut, maintenant
		}
		if fin.Sub(debut) > plafond {
			debut = fin.Add(-plafond)
		}
		return debut, fin
	}

	var data map[string]interface{}
	var resume string
	var err error
	avis := true // les diagnostics et conseils appellent un avis ; les résumés et chiffres non
	var premier *ha.Appareil

	switch sujet {
	case "annuler":
		return a.executerAnnulation(session)

	case "pourquoi_automatisation":
		var auto *ha.Appareil
		apps := entites()
		for i := range apps {
			if apps[i].Domain == "automation" {
				auto = &apps[i]
				break
			}
		}
		if auto == nil {
			return nil, "", false, false, nil, append(rejets, i18n.T("gemini.rejet.enquete.automation"))
		}
		premier = auto
		data, resume, err = a.haClient.DonneesAutomatisation(*auto)

	case "diagnostic_piece":
		if strings.TrimSpace(e.Piece) == "" {
			return nil, "", false, false, nil, []string{i18n.T("gemini.rejet.enquete.piece")}
		}
		data, resume, err = a.haClient.DiagnosticPiece(e.Piece)

	case "diagnostic_maison":
		data, resume, err = a.haClient.DiagnosticMaison()

	case "resume":
		debut, fin := periode(maintenant.Add(-12*time.Hour), 3*24*time.Hour)
		data, resume, err = a.haClient.ResumePeriode(debut, fin)
		avis = false

	case "energie":
		minuit := time.Date(maintenant.Year(), maintenant.Month(), maintenant.Day(), 0, 0, 0, 0, time.Local)
		debut, fin := periode(minuit, maxPlageHistorique)
		id := ""
		if apps := entites(); len(apps) > 0 {
			id = apps[0].EntityID
		}
		data, resume, err = a.haClient.Consommation(debut, fin, id)
		avis = false

	case "conseil":
		apps := entites()
		if len(apps) > 0 {
			premier = &apps[0]
		}
		data, resume, err = a.haClient.DonneesConseil(apps)

	default:
		return nil, "", false, false, nil, []string{i18n.T("gemini.rejet.enquete")}
	}

	if err != nil {
		logx.WarnT("gemini.enquete.erreur", sujet, err)
		return nil, "", false, false, nil, rejets
	}

	// Second appel : l'IA explique les faits (repli : le résumé du code)
	texteFinal := resume
	if analyse := a.analyserDonneesIA(texte, map[string]interface{}{"avis_demande": avis, "sujet": sujet, "donnees": data}); analyse != "" {
		texteFinal = analyse
	}
	msg := messageTexte(texteFinal)
	return &msg, "", true, false, premier, nil
}
