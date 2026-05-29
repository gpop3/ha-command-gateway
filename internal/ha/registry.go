package ha

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
