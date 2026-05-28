package ha

import (
	"fmt"
)

// ServiceCustom est un service générique chargé depuis la config YAML
type ServiceCustom struct {
	serviceBase
	mots           []string
	estParDefaut   bool
	scoreDomaine   int
	extraireParams func(texte string) map[string]interface{}
}

// ConfigService décrit un service custom dans services.yaml
type ConfigService struct {
	Domain        string            `yaml:"domain"`
	Verbs         map[string]string `yaml:"verbs"`
	Words         []string          `yaml:"words"`
	DefaultAction bool              `yaml:"default_action"`
	Score         int               `yaml:"score"`
}

// newServiceCustom crée un service à partir d'une ConfigService
func newServiceCustom(cfg ConfigService, client *Client) *ServiceCustom {
	s := &ServiceCustom{
		serviceBase:  newServiceBase(cfg.Domain, client, cfg.Verbs),
		mots:         cfg.Words,
		estParDefaut: cfg.DefaultAction,
		scoreDomaine: cfg.Score,
	}
	return s
}

func (s *ServiceCustom) Executer(entityID, action string, params map[string]interface{}) (string, error) {
	return s.appeler(entityID, action, params)
}

func (s *ServiceCustom) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	action, ok := s.Verbe(verbe)
	if !ok {
		verbes := s.Verbes()
		if len(verbes) > 0 {
			action, _ = s.Verbe(verbes[0])
		}
	}
	if action == "" {
		return "", fmt.Errorf("aucune action trouvée pour le verbe '%s' sur %s", verbe, s.domaine)
	}
	return s.appeler(app.EntityID, action, params)
}

func (s *ServiceCustom) MotsReconnus() []string {
	return append(s.Verbes(), s.mots...)
}

func (s *ServiceCustom) EstActionParDefaut() bool { return s.estParDefaut }

func (s *ServiceCustom) ScoreDomaine(estAction bool) int {
	if s.scoreDomaine != 0 {
		return s.scoreDomaine
	}
	if estAction {
		return 30
	}
	return 0
}
