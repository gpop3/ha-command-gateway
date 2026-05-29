package ha

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

	// ExecuterCommande est le point d'entrée haut niveau appelé par le NLP
	ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error)

	// EstActionParDefaut retourne true si ce service doit toujours passer par
	// ExecuterCommande même sans verbe d'action détecté.
	EstActionParDefaut() bool
}
