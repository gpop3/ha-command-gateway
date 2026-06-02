package plugins

import (
	"fmt"
	"ha-command-gateway/internal/i18n"
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
