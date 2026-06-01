package core

import (
	"context"
	"errors"
	"log"
)

type Service interface {
	// Nom identifie le service dans les logs (doit être unique).
	Nom() string

	// Démarrer exécute le service
	Démarrer(ctx context.Context) error
}

// Initialisable est implémenté par les services ayant une phase d'init
type Initialisable interface {
	Init(ctx context.Context) error
}

// Fermable est implémenté par les services ayant des ressources à libérer
type Fermable interface {
	Fermer(ctx context.Context) error
}

// Manager pilote le cycle de vie d'un ensemble de services.
type Manager struct {
	services []Service
	noms     map[string]bool
}

// New crée un Manager vide.
func New() *Manager {
	return &Manager{noms: map[string]bool{}}
}

// Register ajoute un service. À appeler avant Démarrer.
func (m *Manager) Register(s Service) {
	if s == nil {
		return
	}
	if m.noms[s.Nom()] {
		log.Printf("⚠️ [core] service '%s' déjà enregistré — ignoré", s.Nom())
		return
	}
	m.noms[s.Nom()] = true
	m.services = append(m.services, s)
}

// Démarrer initialise tous les services Initialisable
func (m *Manager) Démarrer(ctx context.Context) error {
	for _, s := range m.services {
		if i, ok := s.(Initialisable); ok {
			if err := i.Init(ctx); err != nil {
				return errors.New("init " + s.Nom() + " : " + err.Error())
			}
		}
	}

	for _, s := range m.services {
		go func(s Service) {
			log.Printf("▶️ service %s démarré", s.Nom())
			if err := s.Démarrer(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("⚠️ service %s arrêté : %v", s.Nom(), err)
			}
		}(s)
	}
	return nil
}

// Fermer libère les ressources de chaque service implémentant Fermable.
func (m *Manager) Fermer(ctx context.Context) {
	for _, s := range m.services {
		if f, ok := s.(Fermable); ok {
			if err := f.Fermer(ctx); err != nil {
				log.Printf("⚠️ fermeture %s : %v", s.Nom(), err)
			}
		}
	}
}
