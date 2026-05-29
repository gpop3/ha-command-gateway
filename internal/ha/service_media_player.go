package ha

import (
	"ha-command-gateway/internal/utils/text"
	"strings"
)

type ServiceMediaPlayer struct {
	serviceBase
	spotifySources []string
	trouverEntite  func(texte string, estAction bool, domaines []string) (Appareil, int)
}

func NewServiceMediaPlayer(c *Client) *ServiceMediaPlayer {
	return &ServiceMediaPlayer{
		serviceBase: newServiceBase("media_player", c, map[string]string{
			"allume":    "turn_on",
			"éteins":    "turn_off",
			"joue":      "media_play",
			"lance":     "media_play",
			"pause":     "media_pause",
			"stop":      "media_stop",
			"arrête":    "media_stop",
			"suivant":   "media_next_track",
			"précédent": "media_previous_track",
		}),
	}
}

// ChargerSourcesSpotify récupère la source_list depuis HA
func (s *ServiceMediaPlayer) ChargerSourcesSpotify() {
	meilleurMatch, _ := s.trouverEntite("spotify", true, []string{"media_player"})
	etat, err := s.client.RecupererEtatLive(meilleurMatch.EntityID)
	if err != nil || len(etat.Attributes.SourceList) == 0 {
		return
	}
	s.spotifySources = etat.Attributes.SourceList
}

// trouverSourceSpotify trouve la source la plus proche avec Levenshtein
func (s *ServiceMediaPlayer) trouverSourceSpotify(texte string) string {
	if len(s.spotifySources) == 0 {
		return ""
	}
	meilleure := ""
	maxErreurs := 2
	meilleureDistance := 0
	texteNorm := strings.ToLower(texte)

	for _, src := range s.spotifySources {
		motsSource := strings.Fields(src)
		srcNorm := strings.ToLower(src)
		motsMatch := 0

		if strings.Contains(texteNorm, srcNorm) {
			return src
		}

		for _, motSource := range motsSource {
			// Levenshtein sur chaque mot
			for _, mot := range strings.Fields(texteNorm) {
				d := text.DistanceLevenshtein(mot, motSource)
				if d < maxErreurs {
					motsMatch++
				}
			}
		}

		if motsMatch > meilleureDistance {
			meilleureDistance = motsMatch
			meilleure = src
		}
	}
	// Seuil de tolérance
	if meilleureDistance < 2 {
		return ""
	}
	return meilleure
}

func (s *ServiceMediaPlayer) ScoreDomaine(estAction bool) int {
	if estAction {
		return 40
	}
	return 0
}

func (s *ServiceMediaPlayer) EstActionParDefaut() bool { return false }

func (s *ServiceMediaPlayer) ExtraireParams(texte string) map[string]interface{} {
	params := s.serviceBase.ExtraireParams(texte)

	// Détecter "sur X" pour la cible Spotify
	mots := strings.Fields(texte)
	for i, mot := range mots {
		if mot == "sur" && i+1 < len(mots) {
			params["cible"] = strings.Join(mots[i+1:], " ")
			break
		}
	}

	// Sources fixes (apps/entrées)
	sources := []string{"spotify", "deezer", "radio", "hdmi", "bluetooth", "airplay", "youtube"}
	for _, src := range sources {
		if strings.Contains(texte, src) {
			params["source"] = src
			break
		}
	}

	modes := map[string]string{
		"cinéma": "movie", "nuit": "night", "sport": "sport", "dialogue": "speech",
	}
	for mot, mode := range modes {
		if strings.Contains(texte, mot) {
			params["sound_mode"] = mode
			break
		}
	}
	if strings.Contains(texte, "aléatoire") || strings.Contains(texte, "shuffle") {
		params["shuffle"] = true
	}
	return params
}

func (s *ServiceMediaPlayer) SetTrouverEntite(fn func(texte string, estAction bool, domaines []string) (Appareil, int)) {
	s.trouverEntite = fn
}

func (s *ServiceMediaPlayer) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	// Verbes de contrôle prioritaires — ignorer les params source
	action, ok := s.Verbe(verbe)
	if ok && action != "media_play" {
		return s.appeler(app.EntityID, action, nil)
	}

	// "joue/lance spotify sur barre de son" → select_source sur le player Spotify
	if src, ok := params["source"].(string); ok && (src == "spotify" || src == "musique") {
		meilleurMatch, _ := s.trouverEntite("spotify", true, []string{"media_player"})

		body, err := s.appeler(meilleurMatch.EntityID, "media_play", nil)
		if err != nil {
			return "", err
		}

		if meilleurMatch.Domain != "media_player" {
			return "⚠️ [Spotify] Domaine incohérent", nil
		}

		if cible, ok := params["cible"].(string); ok && cible != "" {
			sourceHA := s.trouverSourceSpotify(cible)
			if sourceHA == "" {
				return "⚠️ [Spotify] Impossible de trouver la source", nil
			}

			return s.appeler(meilleurMatch.EntityID, "select_source", map[string]interface{}{
				"source": sourceHA,
			})
		}

		return body, err
	}

	if pct, ok := params["pourcentage"].(int); ok {
		return s.appeler(app.EntityID, "volume_set", map[string]interface{}{
			"volume_level": float64(pct) / 100.0,
		})
	}
	if src, ok := params["source"].(string); ok {
		return s.appeler(app.EntityID, "select_source", map[string]interface{}{"source": src})
	}
	if mode, ok := params["sound_mode"].(string); ok {
		return s.appeler(app.EntityID, "select_sound_mode", map[string]interface{}{"sound_mode": mode})
	}
	if shuffle, ok := params["shuffle"].(bool); ok {
		return s.appeler(app.EntityID, "shuffle_set", map[string]interface{}{"shuffle": shuffle})
	}
	return s.appeler(app.EntityID, "media_play_pause", nil)
}

func (s *ServiceMediaPlayer) MotsReconnus() []string {
	return append(s.Verbes(),
		"spotify", "deezer", "radio", "hdmi", "bluetooth", "airplay", "youtube",
		"cinéma", "musique", "nuit", "sport", "dialogue",
		"aléatoire", "shuffle", "volume", "son", "lecture", "sur",
	)
}
