package i18n

import "fmt"

// Locale est une map de clés → messages pour une langue donnée
type Locale map[string]string

var (
	locales  = map[string]Locale{}
	courante = "fr"
)

// Register enregistre une locale. Appelé via init() dans chaque fichier de locale.
func Register(lang string, l Locale) {
	locales[lang] = l
}

// SetLocale change la langue courante
func SetLocale(lang string) {
	courante = lang
}

// GetLocale retourne la langue courante
func GetLocale() string {
	return courante
}

// T retourne le message associé à la clé dans la locale courante.
func T(cle string, args ...interface{}) string {
	msg, ok := locales[courante][cle]
	if !ok {
		// Fallback sur le français
		msg, ok = locales["fr"][cle]
		if !ok {
			return cle
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// TD retourne le message ou une valeur par défaut si la clé est absente
func TD(cle, defaut string, args ...interface{}) string {
	result := T(cle, args...)
	if result == cle {
		if len(args) > 0 {
			return fmt.Sprintf(defaut, args...)
		}
		return defaut
	}
	return result
}

func Existe(cle string) bool {
	if _, ok := locales[courante][cle]; ok {
		return true
	}
	if _, ok := locales["fr"][cle]; ok {
		return true
	}
	return false
}

func GetPattern(cle string) string {
	if msg, ok := locales[courante][cle]; ok {
		return msg
	}
	if msg, ok := locales["fr"][cle]; ok {
		return msg
	}
	return cle
}
