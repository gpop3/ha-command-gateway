package ha

import (
	"fmt"
	"strings"

	"ha-command-gateway/internal/i18n"
)

type ServiceResumeMaison struct{ serviceBase }

func NewServiceResumeMaison(c *Client) *ServiceResumeMaison {
	// Map vide — "résumé", "état" sont des sujets, pas des verbes
	return &ServiceResumeMaison{newServiceBase("resume_maison", c, map[string]string{})}
}

func (s *ServiceResumeMaison) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return "", nil
}

func (s *ServiceResumeMaison) ScoreDomaine(_ bool) int { return 20 }

func (s *ServiceResumeMaison) EstActionParDefaut() bool { return true }

func (s *ServiceResumeMaison) ExtraireParams(texte string) map[string]interface{} {
	params := map[string]interface{}{"mode": "complet"}
	if strings.Contains(texte, "température") {
		params["mode"] = "temperatures"
	} else if strings.Contains(texte, "lumière") || strings.Contains(texte, "allumé") {
		params["mode"] = "lumieres"
	} else if strings.Contains(texte, "volet") {
		params["mode"] = "volets"
	}
	return params
}

func (s *ServiceResumeMaison) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	mode, _ := params["mode"].(string)
	appareils, err := s.client.RecupererEntites()
	if err != nil {
		return "", fmt.Errorf("impossible de récupérer les entités : %w", err)
	}
	switch mode {
	case "temperatures":
		return s.resumeTemperatures(appareils), nil
	case "lumieres":
		return s.resumeLumieres(appareils), nil
	case "volets":
		return s.resumeVolets(appareils), nil
	default:
		return s.resumeComplet(appareils), nil
	}
}

func (s *ServiceResumeMaison) MotsReconnus() []string {
	return []string{
		"résumé", "maison", "état", "statut", "rapport",
		"température", "lumière", "allumé", "volet", "ouvert",
	}
}

func (s *ServiceResumeMaison) resumeTemperatures(appareils []Appareil) string {
	var sb strings.Builder
	sb.WriteString(i18n.T("maison.temperatures"))
	found := false
	for _, app := range appareils {
		if app.Domain != "sensor" {
			continue
		}
		nom := strings.ToLower(app.FriendlyName)
		if !strings.Contains(nom, "température") && !strings.Contains(nom, "temp") {
			continue
		}
		sb.WriteString(i18n.T("maison.temp.ligne", app.FriendlyNameExact, app.State))
		found = true
	}
	for _, app := range appareils {
		if app.Domain != "climate" {
			continue
		}
		etat, err := s.client.RecupererEtatLive(app.EntityID)
		if err != nil {
			continue
		}
		sb.WriteString(i18n.T("maison.temp.climate", app.FriendlyNameExact, etat.Attributes.CurrentTemperature, etat.Attributes.Temperature))
		found = true
	}
	if !found {
		return i18n.T("maison.temp.aucune")
	}
	return sb.String()
}

func (s *ServiceResumeMaison) resumeLumieres(appareils []Appareil) string {
	var allumees []string
	for _, app := range appareils {
		if app.Domain == "light" && app.State == "on" {
			allumees = append(allumees, app.FriendlyNameExact)
		}
	}
	if len(allumees) == 0 {
		return i18n.T("maison.lumieres.off")
	}
	return i18n.T("maison.lumieres.on", strings.Join(allumees, ", "))
}

func (s *ServiceResumeMaison) resumeVolets(appareils []Appareil) string {
	var ouverts []string
	for _, app := range appareils {
		if app.Domain == "cover" && (app.State == "open" || app.State == "opening") {
			ouverts = append(ouverts, app.FriendlyNameExact)
		}
	}
	if len(ouverts) == 0 {
		return i18n.T("maison.volets.fermes")
	}
	return i18n.T("maison.volets.ouverts", strings.Join(ouverts, ", "))
}

func (s *ServiceResumeMaison) resumeComplet(appareils []Appareil) string {
	var sb strings.Builder
	sb.WriteString(i18n.T("maison.resume"))
	sb.WriteString(s.resumeLumieres(appareils) + "\n")
	sb.WriteString(s.resumeVolets(appareils) + "\n")
	sb.WriteString(s.resumeTemperatures(appareils))
	return sb.String()
}
