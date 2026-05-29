package ha

import (
	"ha-command-gateway/pkg/types"
	"time"
)

// Service est le contrat que doit implémenter chaque domaine HA.
type Service interface {
	// Domaine retourne le nom du domaine HA (ex: "light", "cover")
	Domaine() string

	// Actions retourne la liste des actions HA supportées
	Actions() []string

	// Verbes retourne tous les verbes reconnus par ce service
	Verbes() []string

	// MotsReconnus retourne tous les mots que ce service veut voir dans la grammaire
	MotsReconnus() []string

	// Verbe mappe un verbe vers une action HA
	Verbe(verbe string) (action string, ok bool)

	// ScoreDomaine retourne le bonus/malus NLP selon le contexte
	ScoreDomaine(estAction bool) int

	// ExtraireParams analyse le texte et retourne les paramètres compris
	ExtraireParams(texte string) map[string]interface{}

	// ExecuterCommande est le point d'entrée pour commander une entité
	ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error)

	// RecupererEtat est le point d'entrée pour récupérer l'état d'une entité
	RecupererEtat(app Appareil, dateCible time.Time, params map[string]interface{}) (*EtatComplet, any, error)

	// EtatEnMessage création des messages de voix et sms
	EtatEnMessage(app Appareil, etat *EtatComplet, etatCustom any, dateCible time.Time) types.Message

	// EstActionParDefaut retourne true si ce service doit toujours passer par
	// ExecuterCommande même sans verbe d'action détecté.
	EstActionParDefaut() bool
}
