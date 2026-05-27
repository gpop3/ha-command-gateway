package ha

import "fmt"

// registre global des services — peuplé via Register()
var registre = map[string]Service{}

// Register enregistre un service pour son domaine.
// Peut être appelé depuis main.go ou depuis le init() d'une lib externe :
//
//	import _ "github.com/quelquun/assistant-ha-irrigation"
func Register(s Service) {
	registre[s.Domaine()] = s
}

// Lookup retourne le service pour un domaine donné
func Lookup(domaine string) (Service, bool) {
	s, ok := registre[domaine]
	return s, ok
}

// ListDomaines retourne tous les domaines enregistrés
func ListDomaines() []string {
	domaines := make([]string, 0, len(registre))
	for d := range registre {
		domaines = append(domaines, d)
	}
	return domaines
}

// ExecuterService est le point d'entrée centralisé pour exécuter une action
// sur n'importe quel domaine enregistré
func ExecuterService(domaine, entityID, action string, params map[string]interface{}) (string, error) {
	svc, ok := registre[domaine]
	if !ok {
		return "", fmt.Errorf("domaine '%s' non enregistré", domaine)
	}
	return svc.Executer(entityID, action, params)
}

// TrouverActionParVerbe parcourt tous les services enregistrés pour trouver
func TrouverActionParVerbe(verbe string) (domaine, action string, ok bool) {
	for d, svc := range registre {
		if a, found := svc.Verbe(verbe); found {
			return d, a, true
		}
	}
	return "", "", false
}
