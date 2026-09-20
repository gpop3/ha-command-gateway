package ha

import (
	"fmt"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/utils/text"
	"strings"
)

type ServiceMediaPlayer struct {
	serviceBase
	spotifySources []string
	analyseur      Analyseur
}

func NewServiceMediaPlayer(c *Client) *ServiceMediaPlayer {
	sources := []string{"spotify", "radio", "hdmi", "bluetooth", "youtube"}
	modes := []string{"cinema", "nuit", "sport", "dialogue", "aleatoire", "shuffle"}
	paramsLecture := append(sources, modes...)

	return &ServiceMediaPlayer{
		serviceBase: newServiceBase("media_player", c, map[string]VerbeConfig{
			"allume":    {Action: "turn_on"},
			"éteins":    {Action: "turn_off"},
			"joue":      {Action: "media_play", Params: paramsLecture},
			"lance":     {Action: "media_play", Params: paramsLecture},
			"pause":     {Action: "media_pause"},
			"stop":      {Action: "media_stop"},
			"arrête":    {Action: "media_stop"},
			"suivant":   {Action: "media_next_track"},
			"précédent": {Action: "media_previous_track"},
			// Minuteur natif d'un Echo (Alexa Media Player) : voir minuteurAlexa
			"minuteur": {Action: "alexa_minuteur"},
		}),
	}
}

// ChargerSourcesSpotify récupère la source_list depuis HA
func (s *ServiceMediaPlayer) ChargerSourcesSpotify() {
	if s.analyseur == nil {
		return
	}
	meilleurMatch, _ := s.analyseur.TrouverMeilleurMatch("spotify", true, []string{"media_player"})
	s.chargerSources(meilleurMatch.EntityID)
}

// chargerSources relit la liste des appareils Spotify Connect (source_list) de
// l'entité donnée. Appelée aussi juste avant de choisir une cible : une enceinte
// allumée après le démarrage de l'assistant n'était sinon jamais connue.
func (s *ServiceMediaPlayer) chargerSources(entityID string) {
	if entityID == "" {
		return
	}
	etat, err := s.client.RecupererEtatLive(entityID)
	if err != nil || etat == nil || len(etat.Attributes.SourceList) == 0 {
		return
	}
	s.spotifySources = etat.Attributes.SourceList
}

// motsVidesCible : mots ignorés pour comparer un nom d'enceinte dicté à une source Spotify
var motsVidesCible = map[string]bool{
	"de": true, "du": true, "des": true, "la": true, "le": true, "les": true,
	"l": true, "d": true, "sur": true, "the": true,
}

func motsUtiles(s string) []string {
	s = strings.NewReplacer("'", " ", "-", " ", "_", " ").Replace(text.Normaliser(s))
	var out []string
	for _, m := range strings.Fields(s) {
		if !motsVidesCible[m] {
			out = append(out, m)
		}
	}
	return out
}

func motsProches(a, b string) bool {
	if a == b {
		return true
	}
	return len(a) >= 5 && len(b) >= 5 && text.DistanceLevenshtein(a, b) <= 1
}

// trouverSourceSpotify retrouve dans source_list la source qui correspond le
// mieux à la cible demandée (« barre de son »). Fonctionne aussi pour une source
// à un seul mot, et tolère une faute de transcription d'une lettre.
func (s *ServiceMediaPlayer) trouverSourceSpotify(cible string) string {
	if len(s.spotifySources) == 0 {
		return ""
	}
	motsCible := motsUtiles(cible)
	if len(motsCible) == 0 {
		return ""
	}

	meilleure := ""
	meilleurScore := 0.0
	for _, src := range s.spotifySources {
		motsSrc := motsUtiles(src)
		if len(motsSrc) == 0 {
			continue
		}
		trouves := 0
		for _, ms := range motsSrc {
			for _, mc := range motsCible {
				if motsProches(ms, mc) {
					trouves++
					break
				}
			}
		}
		score := float64(trouves) / float64(len(motsSrc))
		if trouves > 0 && score > meilleurScore {
			meilleurScore = score
			meilleure = src
		}
	}
	if meilleurScore < 0.5 {
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

	// Minuteur Alexa : « mets un minuteur de dix minutes » / « annule le minuteur »
	if strings.Contains(texte, "minuteur") {
		params["duree"] = texte
	}

	// Détecter "sur X" pour la cible Spotify
	mots := strings.Fields(texte)
	for i, mot := range mots {
		if mot == "sur" && i+1 < len(mots) {
			params["cible"] = strings.Join(mots[i+1:], " ")
			break
		}
	}

	// Sources fixes (apps/entrées)
	sources := []string{"spotify", "deezer", "radio", "hdmi", "bluetooth", "youtube"}
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

// Init reçoit l'analyseur (injecté au démarrage) et pré-charge les sources Spotify.
func (s *ServiceMediaPlayer) Init(a Analyseur) {
	s.analyseur = a
	s.ChargerSourcesSpotify()
}

func (s *ServiceMediaPlayer) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	// Verbes de contrôle prioritaires — ignorer les params source
	action, ok := s.Verbe(verbe)
	if ok && action == "alexa_minuteur" {
		return s.minuteurAlexa(app, params)
	}
	if ok && action != "media_play" {
		return s.appeler(app.EntityID, action, nil)
	}

	// "joue/lance spotify sur barre de son" → select_source sur le player Spotify
	if src, ok := params["source"].(string); ok && (src == "spotify" || src == "musique") {
		return s.jouerSpotify(params)
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
	return s.Verbes()
}

// jouerSpotify lance Spotify, éventuellement sur l'appareil demandé (params["cible"]).
// Ordre important : on choisit d'abord l'appareil (select_source), puis on relance la lecture.
func (s *ServiceMediaPlayer) jouerSpotify(params map[string]interface{}) (string, error) {
	if s.analyseur == nil {
		return i18n.T("media.spotify.domaine.incoherent"), nil
	}
	spotify, _ := s.analyseur.TrouverMeilleurMatch("spotify", true, []string{"media_player"})
	if spotify.Domain != "media_player" {
		return i18n.T("media.spotify.domaine.incoherent"), nil
	}

	cible, _ := params["cible"].(string)
	if strings.TrimSpace(cible) == "" {
		return s.appeler(spotify.EntityID, "media_play", nil)
	}

	s.chargerSources(spotify.EntityID)
	sourceHA := s.trouverSourceSpotify(cible)
	if sourceHA == "" {
		return i18n.T("media.spotify.source.introuvable"), nil
	}

	retour, err := s.appeler(spotify.EntityID, "select_source", map[string]interface{}{"source": sourceHA})
	if err != nil {
		return "", err
	}
	// Relance la lecture sur le nouvel appareil ; l'échec éventuel n'est pas bloquant
	_, _ = s.appeler(spotify.EntityID, "media_play", nil)
	return retour, nil
}

// minuteurAlexa règle (ou annule) un minuteur NATIF sur un Echo, via
// Alexa Media Player : media_player.play_media avec media_content_type "custom"
// envoie une commande vocale à l'Echo comme si on lui parlait (AMP >= 3.4.0).
// Le minuteur apparaît dans l'app Alexa et sonne sur l'Echo : rien à annoncer côté assistant.
// params["duree"] : « 10 minutes », « 1h30 »... ou « annuler ».
func (s *ServiceMediaPlayer) minuteurAlexa(app Appareil, params map[string]interface{}) (string, error) {
	duree, _ := params["duree"].(string)

	var phrase string
	if d, ok := analyserDuree(duree); ok {
		phrase = i18n.T("media.alexa.minuteur.regler", decrireDuree(d))
	} else if strings.Contains(text.Normaliser(duree), "annul") {
		phrase = i18n.T("media.alexa.minuteur.annuler")
	} else {
		return "", fmt.Errorf("%s", i18n.T("media.alexa.minuteur.duree.manquante"))
	}

	return s.appeler(app.EntityID, "play_media", map[string]interface{}{
		"media_content_type": "custom",
		"media_content_id":   phrase,
	})
}
