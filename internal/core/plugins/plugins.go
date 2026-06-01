package plugins

import (
	"fmt"
	"path/filepath"
	"plugin"

	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/nlp"
)

// Env regroupe les ressources partagées fournies à chaque service / plugin
type Env struct {
	Bus       *core.Bus
	Analyseur *nlp.Analyseur
	Speaker   core.Speaker
	Sender    core.SMSSender
}

// Fabrique est la signature du symbole `NewService` attendu dans chaque plugin.
type Fabrique = func(Env) core.Service

// Charger ouvre tous les .so du dossier et instancie leurs services
func Charger(dir string, env Env) ([]core.Service, error) {
	fichiers, err := filepath.Glob(filepath.Join(dir, "*.so"))
	if err != nil {
		return nil, err
	}

	var services []core.Service
	for _, f := range fichiers {
		p, err := plugin.Open(f)
		if err != nil {
			return services, fmt.Errorf("ouverture %s : %w", f, err)
		}

		sym, err := p.Lookup("NewService")
		if err != nil {
			return services, fmt.Errorf("%s : symbole NewService introuvable : %w", f, err)
		}

		fab, ok := sym.(func(Env) core.Service)
		if !ok {
			return services, fmt.Errorf("%s : NewService doit être de type func(plugins.Env) core.Service", f)
		}

		if svc := fab(env); svc != nil {
			services = append(services, svc)
		}
	}
	return services, nil
}
