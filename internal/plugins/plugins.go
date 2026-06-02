// Package plugins charge des services tiers compilés en .so, sur le même
// principe que les plugins de domaines Home Assistant. Il vit hors de core pour
// que core reste un package feuille (core n'importe ni nlp ni plugins).
//
// Un plugin est un package `main` compilé avec `-buildmode=plugin` qui expose
// une fonction `NewService` :
//
//	package main
//
//	import (
//	    "context"
//	    "ha-command-gateway/internal/core"
//	    "ha-command-gateway/internal/plugins"
//	)
//
//	type service struct{ env plugins.Env }
//
//	func NewService(env plugins.Env) core.Service { return &service{env} }
//
//	func (s *service) Nom() string { return "mon-plugin" }
//	func (s *service) Démarrer(ctx context.Context) error {
//	    // ... reçoit des entrées, puis :
//	    // s.env.Bus.Soumettre(func() { /* analyse via s.env.Analyseur, réponse via s.env.Speaker/Sender */ })
//	    <-ctx.Done()
//	    return ctx.Err()
//	}
//
// Compilation : go build -buildmode=plugin -o plugins/mon-plugin.so ./chemin/du/plugin
//
// Contraintes des plugins Go : Linux uniquement, mêmes versions de Go et de
// dépendances que l'hôte.
package plugins

import (
	"fmt"
	"path/filepath"
	"plugin"

	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/nlp"
)

// Env regroupe les ressources partagées fournies à chaque service-plugin :
// le bus (pour soumettre un traitement sérialisé), l'analyseur NLP, et les
// ports de sortie (parole, envoi SMS).
type Env struct {
	Bus       *core.Bus
	Analyseur *nlp.Analyseur
	Speaker   core.Speaker
	Sender    core.SMSSender
}

// Fabrique est la signature du symbole `NewService` attendu dans chaque plugin.
type Fabrique = func(Env) core.Service

// Charger ouvre tous les .so du dossier et instancie leurs services. Un dossier
// absent n'est pas une erreur (retourne une liste vide).
func Charger(dir string, env Env) ([]core.Service, error) {
	fichiers, err := filepath.Glob(filepath.Join(dir, "*.so"))
	if err != nil {
		return nil, err
	}

	var services []core.Service
	for _, f := range fichiers {
		p, err := plugin.Open(f)
		if err != nil {
			return services, fmt.Errorf("%s : %w", i18n.T("erreur.plugin.ouverture", f), err)
		}

		sym, err := p.Lookup("NewService")
		if err != nil {
			return services, fmt.Errorf("%s : %w", i18n.T("erreur.plugin.symbole.introuvable", f), err)
		}

		fab, ok := sym.(func(Env) core.Service)
		if !ok {
			return services, fmt.Errorf("%s", i18n.T("erreur.plugin.type", f))
		}

		if svc := fab(env); svc != nil {
			services = append(services, svc)
		}
	}
	return services, nil
}
