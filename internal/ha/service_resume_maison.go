package ha

import (
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/pkg/types"
)

type ServiceResumeMaison struct{ serviceBase }

func NewServiceResumeMaison(c *Client) *ServiceResumeMaison {
	return &ServiceResumeMaison{newServiceBase("resume_maison", c, map[string]VerbeConfig{})}
}

func (s *ServiceResumeMaison) ScoreDomaine(_ bool) int { return 20 }

func (s *ServiceResumeMaison) EstActionParDefaut() bool { return false }

func (s *ServiceResumeMaison) AutoriseMotsSansEntites() bool { return true }

func (s *ServiceResumeMaison) AppareilsVirtuels() []Appareil {
	return []Appareil{{
		EntityID: "resume_maison.local", FriendlyName: "résumé maison", FriendlyNameExact: "résumé maison", Domain: "resume_maison",
	}}
}

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

func (s *ServiceResumeMaison) MotsReconnus() []string {
	return []string{
		"résumé", "maison", "état", "statut", "rapport",
		"température", "lumière", "allumé", "volet", "ouvert",
	}
}

type tempLigne struct{ Nom, Etat string }

type climateLigne struct {
	Nom      string
	Actuelle float64
	Consigne float64
}

type ResumeData struct {
	Mode       string
	Lumieres   []string
	Volets     []string
	Temps      []tempLigne
	Climates   []climateLigne
	NbLumieres int
	NbVolets   int
}

func (s *ServiceResumeMaison) RecupererEtat(app Appareil, dateCible time.Time, params map[string]interface{}) (*EtatComplet, any, error) {
	mode, _ := params["mode"].(string)
	appareils, err := s.client.RecupererEntites()
	if err != nil {
		return nil, nil, err
	}

	d := ResumeData{Mode: mode}
	for _, a := range appareils {
		switch {
		case a.Domain == "light" && a.State == "on":
			d.Lumieres = append(d.Lumieres, a.FriendlyNameExact)
			d.NbLumieres++
		case a.Domain == "cover" && (a.State == "open" || a.State == "opening"):
			d.Volets = append(d.Volets, a.FriendlyNameExact)
			d.NbVolets++
		}
	}

	if mode == "temperatures" {
		for _, a := range appareils {
			if a.Domain != "sensor" {
				continue
			}
			nom := strings.ToLower(a.FriendlyName)
			if strings.Contains(nom, "température") || strings.Contains(nom, "temp") {
				d.Temps = append(d.Temps, tempLigne{Nom: a.FriendlyNameExact, Etat: a.State})
			}
		}
		for _, a := range appareils {
			if a.Domain != "climate" {
				continue
			}
			etat, err := s.client.RecupererEtatLive(a.EntityID)
			if err != nil {
				continue
			}
			d.Climates = append(d.Climates, climateLigne{
				Nom: a.FriendlyNameExact, Actuelle: etat.Attributes.CurrentTemperature, Consigne: etat.Attributes.Temperature,
			})
		}
	}

	return nil, d, nil
}

func (s *ServiceResumeMaison) EtatEnMessage(app Appareil, etat *EtatComplet, etatCustom any, dateCible time.Time) types.Message {
	data, ok := etatCustom.(ResumeData)
	if !ok {
		return types.Message{
			SMS:  types.MessageDetails{Texte: i18n.T("erreur.lecture.parler"), Params: []interface{}{}},
			Voix: types.MessageDetails{Texte: i18n.T("erreur.lecture.parler"), Params: []interface{}{}},
		}
	}

	texteSMS, paramsSMS := s.construire(data, "sms")
	texteVoix, paramsVoix := s.construire(data, "voix")

	return types.Message{
		SMS:  types.MessageDetails{Texte: texteSMS, Params: paramsSMS},
		Voix: types.MessageDetails{Texte: texteVoix, Params: paramsVoix},
	}
}

func patternCanal(base, canal string) string {
	cle := strings.ReplaceAll(base, "%canal%", canal)
	if i18n.Existe(cle) {
		return i18n.GetPattern(cle)
	}
	return i18n.GetPattern(strings.ReplaceAll(base, ".%canal%", ""))
}

func (s *ServiceResumeMaison) construire(d ResumeData, canal string) (string, []interface{}) {
	gp := func(base string) string { return patternCanal(base, canal) }

	switch d.Mode {
	case "lumieres":
		if len(d.Lumieres) == 0 {
			return gp("maison.lumieres.off"), nil
		}
		return gp("maison.lumieres.on"), []interface{}{strings.Join(d.Lumieres, ", ")}

	case "volets":
		if len(d.Volets) == 0 {
			return gp("maison.volets.fermes"), nil
		}
		return gp("maison.volets.ouverts"), []interface{}{strings.Join(d.Volets, ", ")}

	case "temperatures":
		if len(d.Temps) == 0 && len(d.Climates) == 0 {
			return gp("maison.temp.aucune"), nil
		}
		var sb strings.Builder
		var params []interface{}
		sb.WriteString(gp("maison.temperatures"))
		for _, t := range d.Temps {
			sb.WriteString(gp("maison.%canal%.temp.ligne"))
			params = append(params, t.Nom, t.Etat)
		}
		for _, c := range d.Climates {
			sb.WriteString(gp("maison.%canal%.temp.climate"))
			params = append(params, c.Nom, c.Actuelle, c.Consigne)
		}
		return sb.String(), params

	default:
		return gp("maison.resume.concis"), []interface{}{d.NbLumieres, d.NbVolets}
	}
}
