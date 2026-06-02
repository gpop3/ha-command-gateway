package conversion

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	"ha-command-gateway/internal/i18n"
)

var (
	muNombres        sync.Mutex
	cacheNombres     map[string]int
	cacheNombresLang string
)

// NombresEnLettres construit la table « mot en lettres » → valeur entière à partir des clés i18n "nombre.1".."nombre.100" de la langue courante
func NombresEnLettres() map[string]int {
	muNombres.Lock()
	defer muNombres.Unlock()

	if lang := i18n.GetLocale(); cacheNombres == nil || lang != cacheNombresLang {
		m := make(map[string]int, 100)
		for i := 1; i <= 100; i++ {
			m[i18n.T(fmt.Sprintf("nombre.%d", i))] = i
		}
		cacheNombres = m
		cacheNombresLang = lang
	}
	return cacheNombres
}

// LettreVersEntier tente de convertir un mot (lettre ou chiffre) en entier
func LettreVersEntier(mot string) (int, bool) {
	mot = strings.ToLower(strings.TrimSpace(mot))

	if v, ok := NombresEnLettres()[mot]; ok {
		return v, true
	}

	var n int
	for _, c := range mot {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if len(mot) > 0 {
		return n, true
	}

	return 0, false
}

// MotsVersEntier parcourt une liste de mots et retourne la première valeur numérique trouvée.
func MotsVersEntier(mots []string) (int, bool) {
	for _, mot := range mots {
		if v, ok := LettreVersEntier(mot); ok {
			return v, true
		}
	}
	return 0, false
}

// RemplacerMotsParChiffres parcourt la phrase et remplace les mots-nombres par des chiffres
func RemplacerMotsParChiffres(phrase string) string {
	mots := strings.Fields(phrase)

	for i, mot := range mots {
		motNettoye := strings.TrimFunc(mot, func(r rune) bool {
			return unicode.IsPunct(r)
		})

		if chiffre, ok := LettreVersEntier(motNettoye); ok {
			chiffreStr := fmt.Sprintf("%d", chiffre)
			mots[i] = strings.Replace(mot, motNettoye, chiffreStr, 1)
		}
	}

	return strings.Join(mots, " ")
}

// ChiffreVersLettre remplace les nombres dans un nom par leur équivalent textuel.
func ChiffreVersLettre(nom string) string {
	nombres := NombresEnLettres()
	inverse := make(map[int]string, len(nombres))
	for lettre, chiffre := range nombres {
		if existing, ok := inverse[chiffre]; !ok || len(lettre) < len(existing) {
			inverse[chiffre] = lettre
		}
	}

	mots := strings.Fields(nom)
	for i, mot := range mots {
		var n int
		if _, err := fmt.Sscanf(mot, "%d", &n); err == nil {
			if lettre, ok := inverse[n]; ok {
				mots[i] = lettre
			}
		}
	}
	return strings.Join(mots, " ")
}
