package ha

import (
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
)

type ServiceTime struct{ serviceBase }

var moisFR = []string{
	"janvier", "février", "mars", "avril", "mai", "juin",
	"juillet", "août", "septembre", "octobre", "novembre", "décembre",
}

var joursFR = []string{
	"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi",
}

func NewServiceTime(c *Client) *ServiceTime {
	// Map vide — "heure", "date", "jour" sont des sujets, pas des verbes
	return &ServiceTime{newServiceBase("time", c, map[string]string{})}
}

func (s *ServiceTime) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.repondre(params), nil
}

func (s *ServiceTime) ScoreDomaine(_ bool) int { return 20 }

func (s *ServiceTime) EstActionParDefaut() bool { return true }

func (s *ServiceTime) ExtraireParams(texte string) map[string]interface{} {
	if strings.Contains(texte, "heure") || strings.Contains(texte, "midi") || strings.Contains(texte, "minuit") {
		return map[string]interface{}{"mode": "heure"}
	}
	if strings.Contains(texte, "date") || strings.Contains(texte, "jour") ||
		strings.Contains(texte, "mois") || strings.Contains(texte, "semaine") || strings.Contains(texte, "année") {
		return map[string]interface{}{"mode": "date"}
	}
	return map[string]interface{}{"mode": "complet"}
}

func (s *ServiceTime) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	return s.repondre(params), nil
}

func (s *ServiceTime) MotsReconnus() []string {
	return []string{
		"heure", "date", "jour", "mois", "année", "semaine",
		"midi", "minuit", "matin", "soir",
	}
}

func (s *ServiceTime) repondre(params map[string]interface{}) string {
	now := time.Now()
	mode, _ := params["mode"].(string)
	switch mode {
	case "heure":
		return i18n.T("time.heure", now.Hour(), now.Minute())
	case "date":
		return i18n.T("time.date", joursFR[now.Weekday()], now.Day(), moisFR[now.Month()-1], now.Year())
	default:
		return i18n.T("time.complet", now.Hour(), now.Minute(), joursFR[now.Weekday()], now.Day(), moisFR[now.Month()-1], now.Year())
	}
}
