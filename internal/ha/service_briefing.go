package ha

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/utils/text"
	"ha-command-gateway/pkg/types"
)

// Briefing À LA DEMANDE (« briefing », « fais-moi le point », « bonjour »...) : il ne
// démarre jamais tout seul, à aucune heure. Le code lit les données (météo du jour,
// agenda, repas du jour des calendriers Mealie, alertes) ; quand l'IA est disponible
// elle en fait un texte oral naturel et y ajoute le saint du jour (elle le connaît :
// aucun calendrier n'est embarqué dans le code). Sans IA, le code assemble lui-même
// un briefing complet, sans saint du jour. Chaque partie est facultative : si une
// source est indisponible, elle est simplement omise.
//
// Entité virtuelle : briefing.home.

// motRepasBriefing : mot présent dans le nom des calendriers de repas (Mealie).
// Ces calendriers sont présentés à part (« Au menu ») et exclus de l'agenda.
var motRepasBriefing = "mealie"

// DefinirBriefingRepas règle le mot-clé des calendriers de repas (vide = pas de section repas).
func DefinirBriefingRepas(mot string) { motRepasBriefing = strings.TrimSpace(mot) }

// maxLignesBriefing borne les éléments énumérés dans chaque section.
const maxLignesBriefing = 6

// seuilBatterieFaible : en dessous (en %), une batterie est signalée.
const seuilBatterieFaible = 15

type ServiceBriefing struct {
	serviceBase
	analyseur Analyseur
}

func NewServiceBriefing(c *Client) *ServiceBriefing {
	return &ServiceBriefing{serviceBase: newServiceBase("briefing", c, map[string]VerbeConfig{})}
}

// Init reçoit l'analyseur (catalogue des entités) au démarrage.
func (s *ServiceBriefing) Init(a Analyseur) { s.analyseur = a }

func (s *ServiceBriefing) AppareilsVirtuels() []Appareil {
	return []Appareil{
		{EntityID: "briefing.home", FriendlyName: "briefing", FriendlyNameExact: "briefing", Domain: "briefing"},
	}
}

func (s *ServiceBriefing) ScoreDomaine(_ bool) int { return 60 }

func (s *ServiceBriefing) EstActionParDefaut() bool { return false }

func (s *ServiceBriefing) AutoriseMotsSansEntites() bool { return true }

func (s *ServiceBriefing) MotsReconnus() []string {
	return []string{"briefing", "bilan"}
}

// BriefingData transporte le texte construit jusqu'à EtatEnMessage.
type BriefingData struct{ Texte string }

func (s *ServiceBriefing) RecupererEtat(_ Appareil, _ time.Time, _ map[string]interface{}) (*EtatComplet, any, error) {
	return nil, BriefingData{Texte: s.construire(time.Now())}, nil
}

func (s *ServiceBriefing) EtatEnMessage(_ Appareil, _ *EtatComplet, etatCustom any, _ time.Time) types.Message {
	d, ok := etatCustom.(BriefingData)
	if !ok || strings.TrimSpace(d.Texte) == "" {
		return messageErreurMeteo()
	}
	return types.Message{
		SMS:  types.MessageDetails{Texte: i18n.Echapper(d.Texte)},
		Voix: types.MessageDetails{Texte: d.Texte},
	}
}

func libelleJour(t time.Time) string {
	return fmt.Sprintf("%s %d %s", joursFR[t.Weekday()], t.Day(), moisFR[t.Month()-1])
}

// BriefingSections : les données du briefing, section par section (chaque section est
// une phrase française déjà formulée, vide si la source est indisponible). C'est ce
// qu'on confie à l'IA pour qu'elle en fasse un texte oral naturel.
type BriefingSections struct {
	Moment  string `json:"moment"` // matin | après-midi | soir
	Date    string `json:"date"`
	Meteo   string `json:"meteo,omitempty"`
	Agenda  string `json:"agenda,omitempty"`
	Repas   string `json:"repas,omitempty"`
	Alertes string `json:"alertes,omitempty"`
}

// Sections lit les données du briefing à l'instant donné.
func (s *ServiceBriefing) Sections(now time.Time) BriefingSections {
	moment := "matin"
	switch {
	case now.Hour() >= 18:
		moment = "soir"
	case now.Hour() >= 12:
		moment = "après-midi"
	}
	return BriefingSections{
		Moment:  moment,
		Date:    libelleJour(now),
		Meteo:   s.meteo(),
		Agenda:  s.agenda(now),
		Repas:   s.repas(now),
		Alertes: s.alertes(),
	}
}

// construire assemble le briefing sans l'IA (repli) : salutation, date, puis les sections.
func (s *ServiceBriefing) construire(now time.Time) string {
	sec := s.Sections(now)
	salut := "briefing.bonjour"
	if sec.Moment == "soir" {
		salut = "briefing.bonsoir"
	}
	parts := []string{i18n.T(salut, sec.Date)}
	for _, p := range []string{sec.Meteo, sec.Agenda, sec.Repas, sec.Alertes} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// meteo : conditions actuelles et prévision du jour (première entité météo qui répond).
func (s *ServiceBriefing) meteo() string {
	if s.analyseur == nil {
		return ""
	}
	svc, ok := Lookup("weather")
	if !ok {
		return ""
	}
	sw, ok := svc.(*ServiceWeather)
	if !ok {
		return ""
	}

	for _, app := range s.analyseur.GetCatalogue() {
		if app.Domain != "weather" {
			continue
		}
		var parties []string
		if etat, err := s.client.RecupererEtatLive(app.EntityID); err == nil && etat != nil {
			parties = append(parties, i18n.T("briefing.meteo.actuelle", tradCondition(etat.State), formaterValeur(etat.Attributes.Temperature, "°C")))
		}
		if prev, err := sw.getPrevisions(app.EntityID, "daily"); err == nil && len(prev) > 0 {
			p := prev[0]
			ligne := i18n.T("briefing.meteo.jour", tradCondition(p.Condition), formaterValeur(p.Temperature, "°C"))
			if p.TempLow != nil {
				ligne += i18n.T("briefing.meteo.min", formaterValeur(*p.TempLow, "°C"))
			}
			if p.Precipitation > 0 {
				ligne += i18n.T("briefing.meteo.pluie", formaterValeur(p.Precipitation, ""))
			}
			parties = append(parties, ligne+".")
		}
		if len(parties) > 0 {
			return strings.Join(parties, " ")
		}
	}
	return ""
}

func (s *ServiceBriefing) serviceAgenda() *ServiceAgenda {
	svc, ok := Lookup("agenda")
	if !ok {
		return nil
	}
	sa, _ := svc.(*ServiceAgenda)
	return sa
}

// estCalendrierRepas : le calendrier est-il un calendrier de repas (nom contenant le mot-clé) ?
func estCalendrierRepas(app Appareil) bool {
	mot := text.Normaliser(motRepasBriefing)
	return mot != "" && strings.Contains(text.Normaliser(app.EntityID+" "+app.FriendlyNameExact), mot)
}

func debutFinJour(now time.Time) (time.Time, time.Time) {
	debut := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	return debut, debut.Add(24 * time.Hour)
}

// agenda : les événements du jour encore à venir (hors calendriers de repas).
func (s *ServiceBriefing) agenda(now time.Time) string {
	sa := s.serviceAgenda()
	if sa == nil {
		return ""
	}
	debut, fin := debutFinJour(now)
	evenements := sa.evenementsFiltres(debut, fin, func(app Appareil) bool { return !estCalendrierRepas(app) })

	var lignes []string
	for _, e := range evenements {
		// On ne raconte pas ce qui est déjà terminé
		if e.End.DateTime != "" {
			if finEv, err := time.Parse(time.RFC3339, e.End.DateTime); err == nil && finEv.Before(now) {
				continue
			}
		}
		titre := strings.TrimSpace(e.Summary)
		if titre == "" {
			continue
		}
		if e.Start.DateTime != "" {
			lignes = append(lignes, formaterInstant(debutEvenement(e), false)+" "+titre)
		} else {
			lignes = append(lignes, titre)
		}
	}
	if len(lignes) == 0 {
		return i18n.T("briefing.agenda.vide")
	}
	reste := 0
	if len(lignes) > maxLignesBriefing {
		reste = len(lignes) - maxLignesBriefing
		lignes = lignes[:maxLignesBriefing]
	}
	liste := strings.Join(lignes, ", ")
	if reste > 0 {
		liste += " " + i18n.T("briefing.agenda.autres", reste)
	}
	return i18n.T("briefing.agenda", liste)
}

// libelleRepas : nom parlé d'un calendrier de repas (« Mealie Dinner » → « dîner »).
func libelleRepas(nom string) string {
	n := strings.TrimSpace(nom)
	if motRepasBriefing != "" {
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(motRepasBriefing))
		n = strings.TrimSpace(re.ReplaceAllString(n, ""))
	}
	switch strings.ToLower(n) {
	case "breakfast":
		return "petit-déjeuner"
	case "lunch":
		return "déjeuner"
	case "dinner":
		return "dîner"
	case "side", "sides":
		return "accompagnement"
	}
	if n == "" {
		return nom
	}
	return strings.ToLower(n)
}

// repas : le menu du jour, lu dans les calendriers de repas (Mealie).
func (s *ServiceBriefing) repas(now time.Time) string {
	sa := s.serviceAgenda()
	if sa == nil || s.analyseur == nil || motRepasBriefing == "" {
		return ""
	}
	debut, fin := debutFinJour(now)

	var lignes []string
	for _, app := range s.analyseur.GetCatalogue() {
		if app.Domain != "calendar" || !estCalendrierRepas(app) {
			continue
		}
		var noms []string
		for _, e := range sa.evenementsCalendrier(app, debut, fin) {
			if t := strings.TrimSpace(e.Summary); t != "" {
				noms = append(noms, t)
			}
		}
		if len(noms) > 0 {
			lignes = append(lignes, i18n.T("briefing.repas.ligne", libelleRepas(app.FriendlyNameExact), strings.Join(noms, ", ")))
		}
	}
	if len(lignes) == 0 {
		return ""
	}
	return i18n.T("briefing.repas", strings.Join(lignes, " ; "))
}

// alertes : ouvertures (portes, fenêtres) restées ouvertes et batteries faibles.
func (s *ServiceBriefing) alertes() string {
	body, err := s.client.get("/api/states")
	if err != nil {
		return ""
	}
	var etats []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			FriendlyName string `json:"friendly_name"`
			DeviceClass  string `json:"device_class"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &etats); err != nil {
		return ""
	}

	var ouverts, batteries []string
	for _, e := range etats {
		nom := e.Attributes.FriendlyName
		if nom == "" {
			nom = e.EntityID
		}
		classe := e.Attributes.DeviceClass
		switch {
		case strings.HasPrefix(e.EntityID, "binary_sensor.") && e.State == "on":
			switch classe {
			case "door", "window", "garage_door", "opening":
				ouverts = append(ouverts, nom)
			case "battery": // pour un binary_sensor, « on » = batterie faible
				batteries = append(batteries, nom)
			}
		case strings.HasPrefix(e.EntityID, "sensor.") && classe == "battery":
			if v, err := strconv.ParseFloat(e.State, 64); err == nil && v < seuilBatterieFaible {
				batteries = append(batteries, nom)
			}
		}
	}

	limiter := func(noms []string) string {
		if len(noms) > 4 {
			noms = noms[:4]
		}
		return strings.Join(noms, ", ")
	}
	var parts []string
	if len(ouverts) > 0 {
		parts = append(parts, i18n.T("briefing.alerte.ouverts", limiter(ouverts)))
	}
	if len(batteries) > 0 {
		parts = append(parts, i18n.T("briefing.alerte.batteries", limiter(batteries)))
	}
	return strings.Join(parts, " ")
}
