package ha

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
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
			log.Printf("⚠️ [services] service sans domaine ignoré")
			continue
		}
		// Ne pas écraser un service built-in
		if _, exists := Lookup(cfg.Domain); exists {
			log.Printf("⚠️ [services] domaine '%s' déjà enregistré — remplacé", cfg.Domain)
		}
		svc := newServiceCustom(cfg, client)
		Register(svc)
		log.Printf("✅ [services] service custom '%s' chargé (%d verbes, %d mots)",
			cfg.Domain, len(cfg.Verbs), len(cfg.Words))
	}

	return nil
}
