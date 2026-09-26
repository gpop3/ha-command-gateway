package ha

import (
	"strings"
	"sync"
	"time"

	"ha-command-gateway/internal/utils/text"
)

// ---- Appareils mobiles nommés (pour cibler une notification sur la bonne personne/le bon appareil) ----
//
// Un service notify.mobile_app_<slug> ne dit pas, à lui seul, à qui appartient l'appareil.
// On associe chaque service à son nom convivial (celui donné à l'appareil dans Home Assistant,
// ex. « iPhone de Grégory ») via le registre des appareils, pour pouvoir : (1) afficher un nom
// clair dans la confirmation envoyée à l'utilisateur, et (2) retrouver le bon service quand
// l'utilisateur (ou l'IA) nomme un destinataire précis (« envoie ça à Grégory »).

var cacheAppareilsMobiles struct {
	sync.Mutex
	parService map[string]string // service notify.* → nom convivial
	maj        time.Time
	echec      time.Time
}

// AppareilsMobilesNommes retourne, pour chaque service notify.mobile_app_* actuellement
// déclaré dans Home Assistant, un nom convivial : celui du registre des appareils si
// disponible (nom donné par l'utilisateur en priorité), sinon un nom dérivé du service
// lui-même (ex. « mobile_app_pixel_8 » → « Pixel 8 »). Mis en cache 30 minutes.
func (c *Client) AppareilsMobilesNommes() map[string]string {
	services := c.ServicesMobiles()
	res := make(map[string]string, len(services))
	for _, s := range services {
		res[s] = nomDepuisService(s)
	}
	if len(services) == 0 {
		return res
	}

	cacheAppareilsMobiles.Lock()
	defer cacheAppareilsMobiles.Unlock()
	if cacheAppareilsMobiles.parService == nil || time.Since(cacheAppareilsMobiles.maj) >= dureeCacheZones {
		if cacheAppareilsMobiles.echec.IsZero() || time.Since(cacheAppareilsMobiles.echec) >= delaiReessaiZones {
			if noms, err := c.chargerNomsAppareilsMobiles(); err != nil {
				cacheAppareilsMobiles.echec = time.Now()
			} else {
				cacheAppareilsMobiles.parService = noms
				cacheAppareilsMobiles.maj = time.Now()
				cacheAppareilsMobiles.echec = time.Time{}
			}
		}
	}
	for s, nom := range cacheAppareilsMobiles.parService {
		if _, ok := res[s]; ok && nom != "" {
			res[s] = nom
		}
	}
	return res
}

func (c *Client) chargerNomsAppareilsMobiles() (map[string]string, error) {
	var appareils []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		NameByUser string `json:"name_by_user"`
	}
	if err := c.commandeWS(commandeDeviceRegist, &appareils); err != nil {
		return nil, err
	}
	res := make(map[string]string, len(appareils))
	for _, a := range appareils {
		nom := strings.TrimSpace(a.NameByUser)
		if nom == "" {
			nom = strings.TrimSpace(a.Name)
		}
		if nom == "" {
			continue
		}
		res["mobile_app_"+slugifie(nom)] = nom
	}
	return res, nil
}

// nomDepuisService dérive un nom convivial approximatif d'un service notify.mobile_app_<slug>
// quand le registre des appareils est indisponible (ex. « mobile_app_pixel_8 » → « Pixel 8 »).
func nomDepuisService(service string) string {
	suffixe := strings.TrimPrefix(strings.TrimPrefix(service, "notify."), "mobile_app_")
	mots := strings.FieldsFunc(suffixe, func(r rune) bool { return r == '_' || r == '-' })
	for i, m := range mots {
		if m == "" {
			continue
		}
		mots[i] = strings.ToUpper(m[:1]) + m[1:]
	}
	if len(mots) == 0 {
		return service
	}
	return strings.Join(mots, " ")
}

// slugifie reproduit (approximativement) la règle de Home Assistant pour dériver l'identifiant
// d'un service notify.mobile_app_<slug> à partir du nom de l'appareil.
func slugifie(s string) string {
	s = text.Normaliser(s)
	var b strings.Builder
	dernierTiret := true // évite un tiret bas en tête
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dernierTiret = false
		case !dernierTiret:
			b.WriteByte('_')
			dernierTiret = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}

// TrouverAppareilMobile cherche, parmi les services notify.mobile_app_* disponibles, celui dont
// le nom convivial correspond le mieux à `cible` (nom de personne ou d'appareil dit par
// l'utilisateur, ex. « Grégory », « le téléphone de Marie », « mon pixel »). Correspondance par
// inclusion de mots, insensible aux accents/majuscules. Retourne ok=false si rien ne correspond
// clairement (plusieurs candidats à égalité, ou aucun).
func (c *Client) TrouverAppareilMobile(cible string) (service, nom string, ok bool) {
	cible = strings.TrimSpace(cible)
	if cible == "" {
		return "", "", false
	}
	return trouverAppareilMobileDans(cible, c.AppareilsMobilesNommes())
}

// trouverAppareilMobileDans est la logique de correspondance pure (isolée de l'appel réseau à
// Home Assistant) pour pouvoir être testée sans instance HA réelle.
func trouverAppareilMobileDans(cible string, noms map[string]string) (service, nom string, ok bool) {
	cibleNorm := " " + text.Normaliser(cible) + " "

	var candidatsService, candidatsNom []string
	for svc, n := range noms {
		nNorm := " " + text.Normaliser(n) + " "
		if strings.Contains(nNorm, cibleNorm) || strings.Contains(cibleNorm, nNorm) {
			candidatsService = append(candidatsService, svc)
			candidatsNom = append(candidatsNom, n)
			continue
		}
		// Recoupement mot à mot (« Grégory » dans « iPhone de Grégory »)
		for _, mot := range strings.Fields(strings.TrimSpace(cibleNorm)) {
			if len(mot) < 3 {
				continue
			}
			if strings.Contains(nNorm, " "+mot+" ") {
				candidatsService = append(candidatsService, svc)
				candidatsNom = append(candidatsNom, n)
				break
			}
		}
	}
	if len(candidatsService) == 1 {
		return candidatsService[0], candidatsNom[0], true
	}
	return "", "", false
}
