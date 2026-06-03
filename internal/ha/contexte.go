package ha

// Analyseur expose à un service ce dont il a besoin de la part du moteur NLP
type Analyseur interface {
	TrouverMeilleurMatch(texte string, estAction bool, domaines []string) (Appareil, int)
	GetCatalogue() []Appareil
}

// ServiceInitialisable : un service qui a besoin d'une référence à l'analyseur
type ServiceInitialisable interface {
	Init(a Analyseur)
}

// ServiceAvecAppareils : un service qui veut ajouter ses propres entités
type ServiceAvecAppareils interface {
	AppareilsVirtuels() []Appareil
}
