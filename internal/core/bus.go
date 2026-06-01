package core

import "context"

// Tache est une unité de traitement soumise par un service
type Tache func()

// Bus sérialise l'exécution des tâches : elles sont jouées une à une sur une unique goroutine. Cela garde l'analyse NLP mono-thread (pas de course) tout en laissant chaque service libre de son traitement.
type Bus struct {
	taches chan Tache
}

// NewBus crée un bus avec une file de la taille donnée.
func NewBus(taille int) *Bus {
	if taille <= 0 {
		taille = 10
	}
	return &Bus{taches: make(chan Tache, taille)}
}

// Soumettre met une tâche dans la file. Appelable depuis n'importe quelle goroutine (les sources tournent chacune dans la leur).
func (b *Bus) Soumettre(t Tache) {
	if t != nil {
		b.taches <- t
	}
}

// Lancer exécute les tâches jusqu'à l'annulation du contexte.
func (b *Bus) Lancer(ctx context.Context) {
	for {
		select {
		case t := <-b.taches:
			t()
		case <-ctx.Done():
			return
		}
	}
}
