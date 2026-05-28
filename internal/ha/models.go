package ha

// Appareil représente une entité Home Assistant en mémoire locale
type Appareil struct {
	EntityID          string
	FriendlyName      string // Normalisé (chiffres → lettres)
	FriendlyNameExact string // Nom original tel que retourné par HA
	State             string
	Domain            string // "light", "cover", "sensor", etc.
}

// Piece représente une pièce (area) déclarée dans HA
type Piece struct {
	ID   string `json:"area_id"`
	Name string `json:"name"`
}

// EtatComplet contient l'état et les attributs courants d'une entité
type EtatComplet struct {
	EntityID   string          `json:"entity_id"`
	State      string          `json:"state"`
	Attributes AttributsEntite `json:"attributes"`
}

// AttributsEntite regroupe les attributs les plus courants des entités HA
type AttributsEntite struct {
	FriendlyName       string   `json:"friendly_name"`
	CurrentTemperature float64  `json:"current_temperature"`
	Temperature        float64  `json:"temperature"`
	HvacAction         string   `json:"hvac_action"`
	Humidity           int      `json:"humidity"`
	WindSpeed          float64  `json:"wind_speed"`
	WindBearing        float64  `json:"wind_bearing"`
	Pressure           float64  `json:"pressure"`
	Visibility         float64  `json:"visibility"`
	SourceList         []string `json:"source_list"`
}

// ReponseIntent est la réponse de l'API /api/intent/handle
type ReponseIntent struct {
	Speech struct {
		Plain struct {
			Speech string `json:"speech"`
		} `json:"plain"`
	} `json:"speech"`
}

// entiteRaw est la structure brute retournée par /api/states
type entiteRaw struct {
	EntityID   string `json:"entity_id"`
	State      string `json:"state"`
	Attributes struct {
		FriendlyName string `json:"friendly_name"`
	} `json:"attributes"`
}

// AttributsMeteo contient les attributs d'une entité weather
type AttributsMeteo struct {
	Temperature  float64 `json:"temperature"`
	Humidity     int     `json:"humidity"`
	WindSpeed    float64 `json:"wind_speed"`
	WindBearing  float64 `json:"wind_bearing"`
	Pressure     float64 `json:"pressure"`
	Visibility   float64 `json:"visibility"`
	FriendlyName string  `json:"friendly_name"`
}

// EtatMeteo représente l'état complet d'une entité weather
type EtatMeteo struct {
	State      string         `json:"state"`
	Attributes AttributsMeteo `json:"attributes"`
}

// PrevisionHoraire représente une prévision météo sur une période
type PrevisionHoraire struct {
	DateTime      string  `json:"datetime"`
	Temperature   float64 `json:"temperature"`
	Condition     string  `json:"condition"`
	Precipitation float64 `json:"precipitation"`
	WindSpeed     float64 `json:"wind_speed"`
}

// EvenementCalendrier représente un événement du calendrier HA
type CalendarDateTime struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}

// Value retourne la valeur disponible (dateTime ou date)
func (c CalendarDateTime) Value() string {
	if c.DateTime != "" {
		return c.DateTime
	}
	return c.Date
}

// EvenementCalendrier représente un événement du calendrier HA
type EvenementCalendrier struct {
	Start       CalendarDateTime `json:"start"`
	End         CalendarDateTime `json:"end"`
	Summary     string           `json:"summary"`
	Description string           `json:"description"`
}
