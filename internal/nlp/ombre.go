package nlp

import (
	"encoding/json"
	"strings"

	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/logx"
)

// contexteIA prépare le contexte (entités pertinentes) et les capacités envoyés à l'IA.
func (a *Analyseur) contexteIA(texte string) (contexte, capacites string, err error) {
	_ = a.RafraichirCatalogue()

	// Contexte réduit aux entités pertinentes (désactivable : GEMINI_PRESELECTION=false)
	var retenus map[string]bool
	if a.ia.Preselection && a.ia.ContexteMax > 0 {
		retenus = a.preselectionIA(texte)
	}
	contexte, err = a.haClient.ContexteJSON(a.GetPieces(), retenus)
	if err != nil {
		return "", "", err
	}
	brut, err := json.Marshal(ha.CapacitesIA())
	if err != nil {
		return "", "", err
	}
	return contexte, string(brut), nil
}

// ---- Mode ombre ----
//
// Le NLP classique et l'IA analysent chacun la phrase, mais un seul exécute ; l'autre dit
// seulement ce qu'il aurait fait. Les désaccords sont journalisés (GET /decisions, warn) :
// ils montrent où régler le scoring, et où le prompt se trompe. L'IA ombre coûte des tokens.

// predireClassique dit quelle entité et quel verbe le NLP classique aurait choisis, sans exécuter.
func (a *Analyseur) predireClassique(texte string) (entityID, verbe string, ok bool) {
	nettoye := strings.ToLower(texte)
	verbe, estAction := detecterVerbe(nettoye)
	domaines := []string{}
	if estAction && a.activePreselection {
		domaines = detecterDomaines(nettoye)
	}
	if err := a.RafraichirCatalogue(); err != nil {
		return "", "", false
	}
	classement := a.classerAppareils(nettoye, domaines)
	if len(classement) == 0 || classement[0].Score < a.score.Minimal {
		return "", "", false
	}
	app := classement[0].Appareil
	if v, trouve := domaineAUnVerbe(nettoye, app.Domain); trouve {
		verbe = v
	}
	return app.EntityID, verbe, true
}

func contient(liste []string, x string) bool {
	for _, y := range liste {
		if y == x {
			return true
		}
	}
	return false
}

// executerOmbre calcule la prédiction de l'AUTRE moteur et la rattache à l'échange id.
func (a *Analyseur) executerOmbre(id int64, texte, moteur, typeIA string, entites []string) {
	var o *Ombre

	switch {
	case moteur == "gemini" && typeIA == "action":
		entite, verbe, ok := a.predireClassique(texte)
		o = &Ombre{Moteur: "classique"}
		if ok {
			o.Prediction = []string{entite + " " + verbe}
			accord := contient(entites, entite)
			o.Accord = &accord
		} else {
			o.Prediction = []string{"(n'a pas compris)"}
			faux := false
			o.Accord = &faux
		}

	case moteur == "classique" && a.gemini != nil:
		contexte, capacites, err := a.contexteIA(texte)
		if err != nil {
			return
		}
		rep, err := a.gemini.Interroger(nil, texte, contexte, capacites)
		if err != nil {
			return // quota, disjoncteur… : pas d'ombre, sans bruit
		}
		o = &Ombre{Moteur: "gemini"}
		if rep.Type == "action" {
			for _, act := range rep.Actions {
				o.Prediction = append(o.Prediction, act.EntityID+" "+act.Verbe)
			}
			if len(entites) > 0 {
				accord := false
				for _, act := range rep.Actions {
					if contient(entites, act.EntityID) {
						accord = true
					}
				}
				o.Accord = &accord
			}
		} else {
			o.Prediction = []string{"type:" + rep.Type}
		}
	default:
		return
	}

	a.decisions.definirOmbre(id, o)
	if o.Accord != nil && !*o.Accord {
		logx.WarnT("ombre.desaccord", texte, moteur, strings.Join(entites, ", "), o.Moteur, strings.Join(o.Prediction, ", "))
	} else {
		logx.DebugT("ombre.accord", texte)
	}
}
