package nlp

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"ha-command-gateway/internal/core/adapters/gemini"
	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/logx"
	"ha-command-gateway/internal/utils/text"
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
	cle := cleAnnulation(session)
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	var garde []groupeAnnulation
	for _, x := range a.annulations[cle] {
		if time.Since(x.quand) < dureeAnnulation {
			garde = append(garde, x)
		}
	}
	garde = append(garde, g)
	if len(garde) > maxGroupesAnnulation {
		garde = garde[len(garde)-maxGroupesAnnulation:]
	}
	a.annulations[cle] = garde
	logx.DebugT("annulation.memorisee", len(g.etats), len(g.nonAnnulables), len(garde))
}

func (a *Analyseur) depilerAnnulation(session string) (groupeAnnulation, bool) {
	cle := cleAnnulation(session)
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	pile := a.annulations[cle]
	logx.DebugT("annulation.demandee", len(pile))
	for len(pile) > 0 {
		g := pile[len(pile)-1]
		pile = pile[:len(pile)-1]
		if time.Since(g.quand) < dureeAnnulation {
			a.annulations[cle] = pile
			return g, true
		}
	}
	a.annulations[cle] = nil
	return groupeAnnulation{}, false
}

func normaliserPourPhrases(s string) string {
	t := text.Normaliser(s)
	t = strings.NewReplacer("’", " ", "'", " ", "-", " ", ".", " ", ",", " ", "!", " ", "?", " ", ";", " ", ":", " ", "[", " ", "]", " ").Replace(t)
	return " " + strings.Join(strings.Fields(t), " ") + " "
}

// motsAnnulationAutorises : les seuls mots qui peuvent accompagner « annule » / « défais » dans
// une demande d'annulation (« annuler l'action », « annule ça s'il te plaît »…). « unk » : mot
// inconnu de la grammaire de la reconnaissance vocale ([unk]).
var motsAnnulationAutorises = map[string]bool{
	"ca": true, "cela": true, "ce": true, "que": true, "tu": true, "viens": true, "de": true, "faire": true,
	"as": true, "fait": true, "fais": true, "l": true, "la": true, "le": true, "cette": true, "derniere": true,
	"dernier": true, "action": true, "commande": true, "ordre": true, "tout": true, "s": true, "il": true,
	"te": true, "plait": true, "svp": true, "stp": true, "merci": true, "vite": true, "unk": true, "moi": true,
	"encore": true, "precedente": true, "precedent": true, "actions": true,
	"je": true, "veux": true, "voudrais": true, "peux": true, "peut": true, "pourrais": true, "pourriez": true,
}

// motsBloquesAnnulation : ce qui désigne autre chose qu'une commande d'appareil (« annule le
// minuteur », « annule le rendez-vous »…).
var motsBloquesAnnulation = map[string]bool{
	"minuteur": true, "minuterie": true, "timer": true, "alarme": true, "reveil": true, "rappel": true,
	"sms": true, "message": true, "evenement": true, "reservation": true, "rendez": true, "rdv": true,
	"abonnement": true, "notification": true,
}

// interpreterAnnulation reconnaît une demande d'annulation de la dernière commande, sous toutes
// ses formes courantes : « annule ça », « annuler l'action », « annule la dernière commande »,
// « défais ça », « remets comme avant », « reviens en arrière ».
func interpreterAnnulation(texte string) bool {
	t := normaliserPourPhrases(texte)
	mots := strings.Fields(t)
	if len(mots) == 0 || len(mots) > 8 {
		return false
	}
	for _, m := range mots {
		if motsBloquesAnnulation[m] {
			return false
		}
	}

	// « remets comme avant », « reviens en arrière », « fais marche arrière »
	for _, p := range []string{" comme avant ", " comme c etait ", " comme il etait ", " comme elle etait ", " en arriere ", " marche arriere "} {
		if !strings.Contains(t, p) {
			continue
		}
		for _, m := range mots {
			for _, v := range []string{"remet", "revien", "retour", "fais", "defai", "reprend"} {
				if strings.HasPrefix(m, v) {
					return true
				}
			}
		}
	}

	// « annule … », « défais … » : accompagnés seulement de mots d'appoint
	declencheur := false
	for _, m := range mots {
		switch {
		case strings.HasPrefix(m, "annul"), strings.HasPrefix(m, "defai"):
			declencheur = true
		case motsAnnulationAutorises[m]:
		default:
			return false
		}
	}
	return declencheur
}

// cleAnnulation choisit la pile d'annulation à utiliser pour une session.
//
// Un numéro de téléphone (SMS) garde sa pile à lui. Chaque requête Home Assistant Assist
// (voix HA, dashboard...) a sa propre conversation (un nouveau conversation_id à chaque tour) :
// sans regroupement, « annule ça » ne retrouverait jamais la commande précédente puisqu'elle
// aurait été mémorisée sous un autre identifiant de session. On regroupe donc toutes les
// conversations HA Assist (préfixe "http:") sous une seule pile partagée "ha_assist".
//
// En revanche, on NE mélange PAS ce canal avec les autres canaux locaux (voix native, console) :
// un regroupement global ("local" unique pour tout) faisait qu'une commande donnée par une
// interface pouvait être annulée — ou empêchait un « rien à annuler » correct — à cause d'une
// commande sans rapport passée par une autre interface. Chaque canal fixe (voix, console) garde
// donc sa propre pile, ce qui est déjà stable d'un appel à l'autre (contrairement à HA Assist).
func cleAnnulation(session string) string {
	if _, ok := normaliserNumero(session); ok {
		return session
	}
	if strings.HasPrefix(session, "http:") {
		return "ha_assist"
	}
	return session
}

// aUneAnnulation : une commande récente de la session peut-elle être annulée ?
func (a *Analyseur) aUneAnnulation(session string) bool {
	cle := cleAnnulation(session)
	a.muSessions.Lock()
	defer a.muSessions.Unlock()
	for _, g := range a.annulations[cle] {
		if time.Since(g.quand) < dureeAnnulation {
			return true
		}
	}
	return false
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
		logx.InfoT("annulation.restauration", e.Nom, e.Etat)
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

	case "notifier":
		return a.executerNotification(session, texte, rep, false)

	case "expliquer":
		return a.executerExplication(session, texte)

	case "planifier":
		return a.executerPlanification(session, e)

	case "bilan":
		debut, fin := periode(maintenant.Add(-7*24*time.Hour), 14*24*time.Hour)
		data, resume, err = a.haClient.BilanSemaine(debut, fin)

	case "repas":
		if !ha.MealieActif() {
			msg := messageTexte(i18n.T("mealie.inactif"))
			return &msg, "", true, false, nil, nil
		}
		// Période à venir : par défaut demain (les bornes de periode() sont plafonnées à « maintenant »)
		demain := time.Date(maintenant.Year(), maintenant.Month(), maintenant.Day()+1, 0, 0, 0, 0, time.Local)
		debut, ok := parserDateIA(e.Debut)
		if !ok {
			debut = demain
		}
		fin, ok := parserDateIA(e.Fin)
		if !ok || !fin.After(debut) {
			fin = debut.Add(24 * time.Hour)
		}
		if fin.Sub(debut) > 7*24*time.Hour {
			fin = debut.Add(7 * 24 * time.Hour)
		}
		svc, ok := ha.Lookup("briefing")
		sb, ok2 := svc.(*ha.ServiceBriefing)
		if !ok || !ok2 {
			return nil, "", false, false, nil, nil
		}
		data, resume, err = sb.DonneesRepas(debut, fin, e.Ingredients)

	case "cuisiner":
		if !ha.MealieActif() {
			msg := messageTexte(i18n.T("mealie.inactif"))
			return &msg, "", true, false, nil, nil
		}
		if strings.TrimSpace(e.Ingredients) == "" {
			return nil, "", false, false, nil, []string{i18n.T("gemini.rejet.enquete.ingredients")}
		}
		if !ha.MealieAPIConfiguree() {
			msg := messageTexte(i18n.T("cuisiner.non.configure"))
			return &msg, "", true, false, nil, nil
		}
		data, resume, err = ha.RechercherRecettes(e.Ingredients)
		avis = false

	case "aide":
		data, resume = a.donneesAide()
		avis = false

	case "inventaire":
		if strings.TrimSpace(e.Piece) == "" {
			return nil, "", false, false, nil, []string{i18n.T("gemini.rejet.enquete.piece")}
		}
		data, resume, err = a.haClient.InventairePiece(e.Piece)
		avis = false

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

// ---- Notification sur le téléphone ----

// serviceNotification retourne le service notify.* à utiliser et le nom (convivial) à annoncer
// à l'utilisateur pour identifier l'appareil visé :
//   - si `cible` désigne clairement un appareil/une personne (« Grégory », « le téléphone de
//     Marie »…) parmi les applications mobiles connues de Home Assistant, c'est CELUI-LÀ qui est
//     utilisé, quelle que soit la configuration NOTIFY_SERVICE (qui ne concerne que le cas par
//     défaut, sans destinataire précisé) ;
//   - sinon, NOTIFY_SERVICE s'il est configuré (le téléphone par défaut : « ton téléphone ») ;
//   - sinon, l'application mobile détectée automatiquement (la première par ordre alphabétique
//     s'il y en a plusieurs — dans ce cas, le nom réel de l'appareil est annoncé pour éviter de
//     prétendre à tort qu'il s'agit de « ton téléphone »).
func (a *Analyseur) serviceNotification(cible string) (service, libelle string) {
	if cible = strings.TrimSpace(cible); cible != "" {
		if svc, nom, ok := a.haClient.TrouverAppareilMobile(cible); ok {
			return svc, nom
		}
		logx.InfoT("notification.cible.introuvable", cible)
	}

	if s := strings.TrimSpace(a.ia.ServiceNotification); s != "" {
		return s, i18n.T("notification.libelle.tonTelephone")
	}

	noms := a.haClient.AppareilsMobilesNommes()
	if len(noms) == 0 {
		return "", ""
	}
	services := a.haClient.ServicesMobiles()
	svc := services[0]
	if len(services) == 1 {
		return svc, i18n.T("notification.libelle.tonTelephone")
	}
	logx.InfoT("notification.plusieurs", strings.Join(services, ", "), svc)
	return svc, noms[svc]
}

// executerNotification envoie un texte sur le téléphone (application mobile Home Assistant) de
// la personne visée — par défaut la dernière réponse de l'assistant, envoyée sur le téléphone par
// défaut (NOTIFY_SERVICE ou, à défaut, l'unique application détectée). Si l'IA a identifié un
// destinataire précis (`e.Cible`, ex. « envoie ça à Grégory »), on cible directement son
// application mobile plutôt que de toujours prendre la première trouvée. Confirmation orale comme
// pour un SMS (sauf si `confirme`, déjà obtenue) ; la confirmation et le message final annoncent
// le VRAI destinataire, jamais « ton téléphone » quand ce n'est pas le cas.
func (a *Analyseur) executerNotification(session, texte string, rep *gemini.Reponse, confirme bool) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	e := rep.Enquete
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(a.derniereReponse(session))
	}
	if message == "" {
		msg := messageTexte(i18n.T("notification.rien"))
		return &msg, "", true, false, nil, nil
	}
	e.Message = message // fige le texte : c'est celui qui sera confirmé puis envoyé

	// e.Cible est le champ normal (déclaré dans le schéma envoyé à Gemini) ; e.Piece est un
	// repli défensif au cas où l'IA y aurait quand même mis le destinataire par erreur.
	cible := strings.TrimSpace(e.Cible)
	if cible == "" {
		cible = strings.TrimSpace(e.Piece)
	}
	service, libelle := a.serviceNotification(cible)
	if service == "" {
		msg := messageTexte(i18n.T("notification.aucun.service"))
		return &msg, "", true, false, nil, nil
	}

	if a.ia.Confirmation && !confirme {
		a.definirConfirmation(session, confirmationEnAttente{rep: rep, texte: texte})
		msg := messageTexte(i18n.T("confirmation.demande", i18n.T("confirmation.notification.cible", libelle, tronquerTexte(message, 120))))
		return &msg, "", true, false, nil, nil
	}

	if err := a.haClient.Notifier(service, i18n.T("notification.titre"), message); err != nil {
		logx.WarnT("gemini.notification.erreur", err)
		msg := messageTexte(i18n.T("notification.echec"))
		return &msg, "", true, false, nil, nil
	}
	a.empilerAnnulation(session, groupeAnnulation{quand: time.Now(), nonAnnulables: []string{i18n.T("annulation.notification")}})
	msg := messageTexte(i18n.T("notification.envoyee.cible", libelle))
	return &msg, "", true, false, nil, nil
}

// ---- « Que sais-tu faire ? » ----

// donneesAide rassemble ce que l'assistant sait faire avec les appareils de la maison : par type,
// quelques appareils réels et les verbes utilisables, plus les grandes fonctions. L'IA en tire
// des exemples de phrases.
func (a *Analyseur) donneesAide() (map[string]interface{}, string) {
	capacites := ha.CapacitesIA()
	noms := map[string][]string{}
	for _, app := range a.catalogue {
		if ha.DomaineExcluIA(app.Domain) {
			continue
		}
		if len(noms[app.Domain]) < 3 {
			noms[app.Domain] = append(noms[app.Domain], nomAppareil(app))
		}
	}
	domaines := make([]string, 0, len(noms))
	for d := range noms {
		domaines = append(domaines, d)
	}
	sort.Strings(domaines)

	var commandes []map[string]interface{}
	for _, d := range domaines {
		verbes := capacites[d].Verbes
		if len(verbes) == 0 {
			continue
		}
		if len(verbes) > 4 {
			verbes = verbes[:4]
		}
		commandes = append(commandes, map[string]interface{}{"domaine": d, "exemples_d_appareils": noms[d], "verbes": verbes})
	}
	var pieces []string
	for _, p := range a.GetPieces() {
		pieces = append(pieces, p.Name)
	}
	data := map[string]interface{}{
		"commandes_possibles": commandes,
		"pieces":              pieces,
		"fonctions": []string{
			"météo (maintenant, demain, après-demain, la semaine)", "agenda et repas du jour", "briefing (météo, agenda, menu, alertes, saint du jour)",
			"minuteurs", "historique et statistiques d'un capteur", "qui a allumé quoi, et pourquoi", "trouver une chute ou un seuil de température",
			"classer les pièces (la plus humide, la plus chaude)", "diagnostic d'une pièce ou de la maison", "consommation d'énergie",
			"conseil (arroser, étendre le linge)", "menu de demain", "trouver une recette avec ce que tu as", "annuler la dernière commande", "envoyer une réponse sur le téléphone",
		},
	}
	return data, "Je peux commander tes appareils, te donner la météo, l'agenda, faire le briefing, lire l'historique de tes capteurs et t'expliquer ce qui se passe dans la maison."
}

// ---- « Pourquoi tu as fait ça ? » ----

func libelleMoteur(m string) string {
	switch m {
	case "gemini":
		return "l'IA"
	case "classique":
		return "la compréhension classique (sans IA)"
	case "appris":
		return "une phrase déjà comprise par l'IA puis retenue (réutilisée sans elle)"
	case "confirmation":
		return "ta réponse à ma demande de confirmation"
	case "choix":
		return "ton choix parmi plusieurs propositions"
	case "annulation":
		return "ta demande d'annulation"
	}
	return m
}

// executerExplication répond à « pourquoi tu as fait ça ? » en relisant le dernier échange de la
// session dans le journal des décisions : phrase dite, moteur, type choisi, entités visées,
// rejets et seconde chance, phrase apprise. L'IA reformule et propose de corriger.
func (a *Analyseur) executerExplication(session, texte string) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	d := a.decisions.derniereDe(session)
	if d == nil || time.Since(d.Quand) > 30*time.Minute {
		msg := messageTexte(i18n.T("explication.rien"))
		return &msg, "", true, false, nil, nil
	}

	var cibles []map[string]interface{}
	for _, id := range d.entites {
		if app := a.trouverAppareil(id); app != nil {
			cibles = append(cibles, map[string]interface{}{"nom": nomAppareil(*app), "entity_id": id, "type": app.Domain})
		}
	}
	data := map[string]interface{}{
		"phrase_dite":        d.Phrase,
		"comprise_par":       libelleMoteur(d.Moteur),
		"type_de_reponse":    d.Type,
		"sujet":              d.Sujet,
		"actions":            d.Actions,
		"entites_choisies":   cibles,
		"resultat_annonce":   d.Resultat,
		"reussi":             d.Reussi,
		"rejets_corriges":    d.Rejets,
		"seconde_chance":     d.SecondeChance,
		"phrase_deja_apprise": d.cleAppris != "",
		"marque_comme_faux":  d.Faux,
	}
	if d.Ombre != nil {
		data["avis_de_l_autre_moteur"] = d.Ombre
	}

	repli := i18n.T("explication.repli", d.Phrase, libelleMoteur(d.Moteur), strings.Join(d.Actions, ", "))
	if len(d.Actions) == 0 {
		repli = i18n.T("explication.repli.sans", d.Phrase, libelleMoteur(d.Moteur))
	}
	repli += " " + i18n.T("explication.corriger")

	if analyse := a.analyserDonneesIA(texte, map[string]interface{}{"avis_demande": false, "sujet": "expliquer", "donnees": data}); analyse != "" {
		repli = analyse
	}
	msg := messageTexte(repli)
	return &msg, "", true, false, nil, nil
}

// ---- Planifier un repas dans Mealie ----

// executerPlanification met une recette (ou une recette au hasard) au plan de repas de Mealie.
func (a *Analyseur) executerPlanification(session string, e *gemini.Enquete) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
	repondre := func(cle string, args ...interface{}) (*types.Message, string, bool, bool, *ha.Appareil, []string) {
		msg := messageTexte(i18n.T(cle, args...))
		return &msg, "", true, false, nil, nil
	}
	if !ha.MealieActif() {
		return repondre("mealie.inactif")
	}

	// Date : le jour demandé (par défaut, aujourd'hui), à minuit local
	jour := time.Now()
	if d, ok := parserDateIA(e.Debut); ok {
		jour = d
	}
	jour = time.Date(jour.Year(), jour.Month(), jour.Day(), 0, 0, 0, 0, time.Local)
	quand := fmt.Sprintf("%s %s", jourRelatifNlp(jour), strings.TrimSpace(e.Repas))
	quand = strings.TrimSpace(quand)

	// Recette au hasard (« propose-moi un dîner au hasard »)
	if strings.TrimSpace(e.Recette) == "" {
		titre, err := a.haClient.PlanifierAuHasard(jour, e.Repas)
		if err != nil {
			logx.WarnT("mealie.planifier.erreur", err)
			return repondre("mealie.planifier.echec")
		}
		if titre == "" {
			return repondre("mealie.hasard.fait", quand)
		}
		return repondre("mealie.hasard.titre", titre, quand)
	}

	// Recette précise : on la retrouve par son nom
	trouvees, err := ha.TrouverRecettes(e.Recette)
	if err != nil {
		logx.WarnT("mealie.planifier.erreur", err)
		return repondre("mealie.planifier.echec")
	}
	if len(trouvees) == 0 {
		return repondre("mealie.recette.inconnue", e.Recette)
	}
	// Plusieurs recettes proches : on demande laquelle (la première est retenue si son nom colle exactement)
	exacte := normaliserPourPhrases(trouvees[0].Nom) == normaliserPourPhrases(e.Recette)
	if len(trouvees) > 1 && !exacte {
		noms := make([]string, 0, len(trouvees))
		for _, r := range trouvees {
			noms = append(noms, r.Nom)
		}
		a.definirEcoute(session)
		return repondre("mealie.recette.choix", strings.Join(noms, ", "))
	}
	if err := a.haClient.PlanifierRepas(jour, e.Repas, trouvees[0]); err != nil {
		logx.WarnT("mealie.planifier.erreur", err)
		return repondre("mealie.planifier.echec")
	}
	return repondre("mealie.planifie", trouvees[0].Nom, quand)
}

// jourRelatifNlp : « aujourd'hui », « demain », sinon « vendredi 25 septembre ».
func jourRelatifNlp(t time.Time) string {
	now := time.Now()
	minuit := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.Local) }
	switch int(math.Round(minuit(t).Sub(minuit(now)).Hours() / 24)) {
	case 0:
		return "aujourd'hui"
	case 1:
		return "demain"
	}
	jours := []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	mois := []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	return fmt.Sprintf("%s %d %s", jours[t.Weekday()], t.Day(), mois[t.Month()-1])
}
