package ha

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/utils/conversion"
	"ha-command-gateway/pkg/types"
)

// ServiceTimer gère le domaine "timer" : les helpers « Minuteur » de Home
// Assistant (Paramètres → Appareils et services → Entrées → Ajouter → Minuteur).
//
// Exemples : « lance le minuteur cuisine dix minutes », « annule le minuteur »,
// « combien de temps reste-t-il sur le minuteur ? ».
// La fin d'un minuteur est annoncée par l'assistant (événement HA timer.finished,
// cf. DefinirSurMinuteurTermine).
type ServiceTimer struct{ serviceBase }

func NewServiceTimer(c *Client) *ServiceTimer {
	return &ServiceTimer{newServiceBase("timer", c, map[string]VerbeConfig{
		"lance":    {Action: "start"},
		"démarre":  {Action: "start"},
		"reprends": {Action: "start"},
		"pause":    {Action: "pause"},
		"annule":   {Action: "cancel"},
		"arrête":   {Action: "cancel"},
		"stoppe":   {Action: "cancel"},
	})}
}

func (s *ServiceTimer) ScoreDomaine(estAction bool) int {
	if estAction {
		return 30
	}
	return 10
}

func (s *ServiceTimer) MotsReconnus() []string {
	return append(s.Verbes(), "minuteur", "minutes", "secondes", "heures")
}

var (
	reDuree       = regexp.MustCompile(`(\d+)\s*(heures?|h|minutes?|mins?|secondes?|secs?)\b`)
	reHeureMinute = regexp.MustCompile(`(\d+)\s*(?:heures?|h)\s*(\d{1,2})\b`)
)

// analyserDuree extrait une durée d'un texte : « 10 minutes », « dix minutes »,
// « 1h30 », « 1 heure 30 », « 2 heures 30 secondes », « 45 secondes ».
func analyserDuree(texte string) (time.Duration, bool) {
	t := strings.ToLower(conversion.RemplacerMotsParChiffres(strings.TrimSpace(texte)))
	if t == "" {
		return 0, false
	}

	// « 1h30 » / « 1 heure 30 » (minutes sans unité) — sauf « 2 heures 30 secondes »
	if loc := reHeureMinute.FindStringSubmatchIndex(t); loc != nil {
		reste := strings.TrimSpace(t[loc[1]:])
		if !strings.HasPrefix(reste, "sec") {
			h, e1 := strconv.Atoi(t[loc[2]:loc[3]])
			m, e2 := strconv.Atoi(t[loc[4]:loc[5]])
			if e1 == nil && e2 == nil {
				return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, true
			}
		}
	}

	var total time.Duration
	trouve := false
	for _, m := range reDuree.FindAllStringSubmatch(t, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(m[2], "h"):
			total += time.Duration(n) * time.Hour
		case strings.HasPrefix(m[2], "m"):
			total += time.Duration(n) * time.Minute
		default:
			total += time.Duration(n) * time.Second
		}
		trouve = true
	}
	return total, trouve && total > 0
}

// formaterDuree formate une durée au format attendu par timer.start (HH:MM:SS).
func formaterDuree(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, (s%3600)/60, s%60)
}

// decrireDuree formate une durée pour être lue à voix haute.
func decrireDuree(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s < 0 {
		s = 0
	}
	var parts []string
	if h := s / 3600; h > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", h, i18n.T("mot.heures")))
	}
	if m := (s % 3600) / 60; m > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", m, i18n.T("mot.minutes")))
	}
	if sec := s % 60; sec > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d %s", sec, i18n.T("mot.secondes")))
	}
	return strings.Join(parts, " ")
}

// parserRemaining lit l'attribut "remaining" d'un minuteur en pause (« 0:04:32 »).
func parserRemaining(s string) (time.Duration, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	sec, e3 := strconv.ParseFloat(parts[2], 64)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, false
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec*float64(time.Second)), true
}

// ExtraireParams : la durée du minuteur (« dix minutes » → 00:10:00).
func (s *ServiceTimer) ExtraireParams(texte string) map[string]interface{} {
	params := map[string]interface{}{}
	if d, ok := analyserDuree(texte); ok {
		params["duration"] = formaterDuree(d)
	}
	return params
}

func (s *ServiceTimer) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "start"
	}

	var data map[string]interface{}
	if action == "start" {
		if d, ok := params["duration"].(string); ok && d != "" {
			data = map[string]interface{}{"duration": d}
		}
	}
	return s.appeler(app.EntityID, action, data)
}

// EtatEnMessage : temps restant d'un minuteur actif ou en pause.
func (s *ServiceTimer) EtatEnMessage(app Appareil, etat *EtatComplet, _ any, _ time.Time) types.Message {
	nom := app.FriendlyNameExact
	if nom == "" {
		nom = app.FriendlyName
	}

	cle := "timer.inactif"
	params := []interface{}{nom}
	if etat != nil {
		switch etat.State {
		case "active":
			if fin, err := time.Parse(time.RFC3339, etat.Attributes.FinishesAt); err == nil {
				cle = "timer.reste"
				params = append(params, decrireDuree(time.Until(fin)))
			}
		case "paused":
			if reste, ok := parserRemaining(etat.Attributes.Remaining); ok {
				cle = "timer.pause"
				params = append(params, decrireDuree(reste))
			}
		}
	}

	return types.Message{
		SMS:  types.MessageDetails{Texte: cle, Params: params},
		Voix: types.MessageDetails{Texte: cle, Params: params},
	}
}
