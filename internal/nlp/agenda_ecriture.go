package nlp

// ---- « Ajoute un rendez-vous… » : écriture dans un calendrier Home Assistant ----
//
// Contrairement à la lecture de l'agenda (internal/ha/service_agenda.go, lecture seule),
// ceci écrit un VRAI événement via le service HA `calendar.create_event`. Jamais sur un
// calendrier de menus (Mealie, déjà couvert par « planifier ») — uniquement un agenda
// personnel. Confirmation orale systématique avant création (c'est persistant).

import (
	"strings"
	"time"

	"ha-command-gateway/internal/core/adapters/gemini"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/text"
	"ha-command-gateway/pkg/types"
)

// calendriersDisponibles retourne les entités "calendar" utilisables pour y ajouter un
// événement (exclut tout calendrier qui ressemble à un calendrier de menus Mealie).
func (a *Analyseur) calendriersDisponibles() []ha.Appareil {
	var out []ha.Appareil
	for _, app := range a.catalogue {
		if app.Domain != "calendar" {
			continue
		}
		if strings.Contains(text.Normaliser(app.EntityID+" "+app.FriendlyNameExact), "mealie") {
			continue
		}
		out = append(out, app)
	}
	return out
}

// trouverCalendrier choisit le calendrier cible : par nom si l'utilisateur en a désigné un
// (« sur l'agenda de Marie »), sinon le seul calendrier disponible. Si plusieurs calendriers
// existent et qu'aucun nom ne permet de trancher, `ambigu` est vrai (on ne devine jamais).
func (a *Analyseur) trouverCalendrier(nom string) (app ha.Appareil, ok bool, ambigu bool) {
	cals := a.calendriersDisponibles()
	nom = strings.TrimSpace(nom)
	if nom != "" {
		nomNorm := text.Normaliser(nom)
		var trouve []ha.Appareil
		for _, c := range cals {
			if strings.Contains(text.Normaliser(c.FriendlyNameExact), nomNorm) {
				trouve = append(trouve, c)
			}
		}
		if len(trouve) == 1 {
			return trouve[0], true, false
		}
		if len(trouve) > 1 {
			return ha.Appareil{}, false, true
		}
		// Nom donné mais aucune correspondance : retombe sur la règle "pas de nom".
	}
	if len(cals) == 1 {
		return cals[0], true, false
	}
	return ha.Appareil{}, false, len(cals) > 1
}

// formatDateHeureNlp : « jeudi 25 septembre à 15 heures » (ou sans heure si minuit pile).
func formatDateHeureNlp(t time.Time) string {
	jour := jourRelatifNlp(t)
	if t.Hour() == 0 && t.Minute() == 0 {
		return jour
	}
	heure := i18n.T("voix.heures", t.Hour())
	if t.Minute() > 0 {
		heure = i18n.T("voix.heures.minute", t.Hour(), t.Minute())
	}
	return jour + " à " + heure
}

// executerCreationEvenement crée un événement dans un agenda Home Assistant (service
// `calendar.create_event`), après confirmation orale — comme une notification, mais
// persistant, d'où la confirmation systématique.
func (a *Analyseur) executerCreationEvenement(session, texte string, rep *gemini.Reponse, confirme bool) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	e := rep.Enquete

	titre := strings.TrimSpace(e.Message)
	if titre == "" {
		msg := messageTexte(i18n.T("agenda.creation.titre.manquant"))
		return &msg, "", true, false, nil, nil
	}

	debut, ok := parserDateIA(e.Debut)
	if !ok {
		msg := messageTexte(i18n.T("agenda.creation.date.incomprise"))
		return &msg, "", true, false, nil, nil
	}
	fin, ok := parserDateIA(e.Fin)
	if !ok || !fin.After(debut) {
		fin = debut.Add(time.Hour) // durée par défaut : 1h
	}

	cal, ok, ambigu := a.trouverCalendrier(e.Piece)
	if !ok {
		if ambigu {
			msg := messageTexte(i18n.T("agenda.creation.calendrier.ambigu"))
			return &msg, "", true, false, nil, nil
		}
		msg := messageTexte(i18n.T("agenda.creation.calendrier.absent"))
		return &msg, "", true, false, nil, nil
	}

	resume := i18n.T("agenda.creation.resume", titre, formatDateHeureNlp(debut))

	if a.ia.Confirmation && !confirme {
		a.definirConfirmation(session, confirmationEnAttente{rep: rep, texte: texte})
		msg := messageTexte(i18n.T("confirmation.demande", resume))
		return &msg, "", true, false, nil, nil
	}

	data := map[string]interface{}{
		"summary":         titre,
		"start_date_time": debut.Format("2006-01-02 15:04:05"),
		"end_date_time":   fin.Format("2006-01-02 15:04:05"),
	}
	if err := a.haClient.AppelerService("calendar", "create_event", cal.EntityID, data); err != nil {
		logx.WarnT("gemini.agenda.creation.erreur", err)
		msg := messageTexte(i18n.T("agenda.creation.echec"))
		return &msg, "", true, false, nil, nil
	}

	msg := messageTexte(i18n.T("agenda.creation.ok", titre, formatDateHeureNlp(debut)))
	return &msg, "", true, false, nil, nil
}
