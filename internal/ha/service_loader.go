package ha

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"

	"gopkg.in/yaml.v3"
	"ha-command-gateway/internal/logx"
)

// LoadServicesFromFile charge les services custom depuis un fichier YAML
// et les enregistre dans le registre global
func LoadServicesFromFile(path string, client *Client) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // pas de fichier = pas d'erreur, c'est optionnel
		}
		return fmt.Errorf("lecture %s : %w", path, err)
	}

	var configs []ConfigService
	if err := yaml.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("parsing %s : %w", path, err)
	}

	for _, cfg := range configs {
		if cfg.Domain == "" {
			logx.WarnT("plugin.services.service.sans.domaine")
			continue
		}
		// Ne pas écraser un service built-in
		if _, exists := Lookup(cfg.Domain); exists {
			logx.WarnT("plugin.services.domaine.deja.enregistre", cfg.Domain)
		}
		svc := newServiceCustom(cfg, client)
		Register(svc)
		logx.InfoT("plugin.services.service.custom.charge",
			cfg.Domain, len(cfg.Verbs), len(cfg.Words))
	}

	return nil
}

// LoadPlugins charge les services custom depuis des so compilé
func LoadPlugins(dir string, client *Client) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		p, err := plugin.Open(path)
		if err != nil {
			logx.WarnT("plugin.plugin", entry.Name(), err)
			continue
		}

		sym, err := p.Lookup("PluginService")
		if err != nil {
			logx.WarnT("plugin.plugin.symbole.pluginservice.manquant", entry.Name())
			continue
		}

		svc, ok := sym.(*Service)
		if !ok {
			logx.WarnT("plugin.plugin.type.invalide", entry.Name())
			continue
		}

		Register(*svc)
		logx.InfoT("plugin.plugin.charge.domaine", entry.Name(), (*svc).Domaine())
	}
	return nil
}
