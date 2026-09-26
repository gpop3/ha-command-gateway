// Package hapublisher rend l'assistant visible dans Home Assistant : capteurs d'état
// (sensor.assistant_statut, tokens du jour, disjoncteur, dernière erreur), republiés
// périodiquement, et un événement à chaque commande (cf. nlp.Analyseur.DefinirSurDecision).
package hapublisher

import (
	"context"
	"strconv"
	"time"

	"ha-command-gateway/internal/ha"
	"ha-command-gateway/internal/logx"
)

// Etat est l'instantané publié dans HA.
type Etat struct {
	Statut         string // ok | ia_suspendue | ia_desactivee
	Version        string
	TokensJour     int
	AppelsJour     int
	IASuspendue    bool
	DerniereErreur string
	Extra          map[string]interface{}
}

type Publisher struct {
	client     *ha.Client
	etat       func() Etat
	intervalle time.Duration
	erreurFait bool
}

// New crée le service ; etat fournit l'instantané, intervalle la fréquence de republication.
func New(c *ha.Client, etat func() Etat, intervalle time.Duration) *Publisher {
	if intervalle < 10*time.Second {
		intervalle = 60 * time.Second
	}
	return &Publisher{client: c, etat: etat, intervalle: intervalle}
}

func (p *Publisher) Nom() string { return "publication-ha" }

// Démarrer republie les états jusqu'à l'arrêt.
func (p *Publisher) Démarrer(ctx context.Context) error {
	p.publier()
	t := time.NewTicker(p.intervalle)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			p.publier()
		}
	}
}

func (p *Publisher) publier() {
	e := p.etat()
	attrs := func(nom, icone string, extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{"friendly_name": nom, "icon": icone}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	statut := attrs("Assistant", "mdi:robot", map[string]interface{}{"version": e.Version, "appels_ia_jour": e.AppelsJour})
	for k, v := range e.Extra {
		statut[k] = v
	}
	suspendue := "off"
	if e.IASuspendue {
		suspendue = "on"
	}
	erreur := e.DerniereErreur
	if erreur == "" {
		erreur = "aucune"
	}
	if r := []rune(erreur); len(r) > 250 {
		erreur = string(r[:250]) + "…"
	}

	var err error
	publier := func(id, etat string, a map[string]interface{}) {
		if e2 := p.client.PublierEtat(id, etat, a); e2 != nil && err == nil {
			err = e2
		}
	}
	publier("sensor.assistant_statut", e.Statut, statut)
	publier("sensor.assistant_tokens_jour", itoa(e.TokensJour), attrs("Assistant : tokens du jour", "mdi:counter",
		map[string]interface{}{"unit_of_measurement": "tokens", "appels": e.AppelsJour}))
	publier("binary_sensor.assistant_ia_suspendue", suspendue, attrs("Assistant : IA suspendue", "mdi:robot-off",
		map[string]interface{}{"device_class": "problem"}))
	publier("sensor.assistant_derniere_erreur_ia", erreur, attrs("Assistant : dernière erreur IA", "mdi:alert-circle-outline", nil))

	if err != nil && !p.erreurFait {
		p.erreurFait = true // un seul avertissement tant que ça échoue
		logx.WarnT("publication.erreur", err)
	} else if err == nil {
		p.erreurFait = false
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
