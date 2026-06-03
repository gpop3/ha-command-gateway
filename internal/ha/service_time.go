package ha

import (
	"fmt"
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
	return &ServiceTime{newServiceBase("time", c, map[string]VerbeConfig{})}

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
		"quelle", "quel", "heure", "date", "jour", "mois", "année", "semaine",
		"midi", "minuit", "matin", "soir", "aujourd'hui",
		"lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche",
	}
}

func (s *ServiceTime) repondre(params map[string]interface{}) string {
	now := time.Now()
	mode, _ := params["mode"].(string)
	heureStr := fmt.Sprintf("%d", now.Hour())
	minuteStr := fmt.Sprintf("%02d", now.Minute())
	jourNumStr := fmt.Sprintf("%d", now.Day())
	anneeStr := fmt.Sprintf("%d", now.Year())

	switch mode {
	case "heure":
		return i18n.T("time.heure", heureStr, minuteStr)
	case "date":
		return i18n.T("time.date", joursFR[now.Weekday()], jourNumStr, moisFR[now.Month()-1], anneeStr)
	default:
		return i18n.T("time.complet", heureStr, minuteStr, joursFR[now.Weekday()], jourNumStr, moisFR[now.Month()-1], anneeStr)
	}
}

func (s *ServiceTime) AutoriseMotsSansEntites() bool {
	return true
}

func (s *ServiceTime) AppareilsVirtuels() []Appareil {
	return []Appareil{
		{EntityID: "time.local", FriendlyName: "heure", FriendlyNameExact: "heure", Domain: "time"},
		{EntityID: "time.local", FriendlyName: "date", FriendlyNameExact: "date", Domain: "time"},
		{EntityID: "time.local", FriendlyName: "jour", FriendlyNameExact: "jour", Domain: "time"},
	}
}
