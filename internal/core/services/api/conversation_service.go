package api

import (
	"fmt"
	"ha-command-gateway/internal/i18n"
	"ha-command-gateway/internal/nlp"
)

// ConversationService porte la logique métier d'analyse du langage naturel
type ConversationService struct {
	analyseur *nlp.Analyseur
}

// NewConversationService crée le service de conversation.
func NewConversationService(analyseur *nlp.Analyseur) *ConversationService {
	return &ConversationService{analyseur: analyseur}
}

// Reponse est le résultat métier d'une analyse, agnostique du transport.
type Reponse struct {
	Speech   string
	Handled  bool
	Verbe    string
	Appareil string
	// Continuer : l'assistant attend une réponse (question de l'IA, confirmation,
	// choix) → Home Assistant garde le micro ouvert (continue_conversation).
	Continuer bool
}

// Traiter analyse un texte, exécute l'action correspondante via l'analyseur
//
// conversationID (fourni par Home Assistant) identifie la conversation : la mémoire de
// l'IA est propre à chaque conversation, jamais partagée avec une autre discussion.
func (s *ConversationService) Traiter(texte, conversationID string) Reponse {
	session := "http"
	if conversationID != "" {
		session = "http:" + conversationID
	}
	reponse, verbe, match, isAction, appareil := s.analyseur.AnalyserEtExecuter(session, texte)

	r := Reponse{Handled: match, Verbe: verbe, Continuer: s.analyseur.AttenteDeChoix(session)}
	if appareil != nil {
		r.Appareil = appareil.EntityID
	}

	if reponse == nil {
		return r
	}

	switch {
	case isAction:
		r.Speech = reponse.SMS.Texte
	case i18n.Existe(reponse.SMS.Texte):
		r.Speech = i18n.T(reponse.SMS.Texte, reponse.SMS.Params...)
	default:
		r.Speech = fmt.Sprintf(reponse.SMS.Texte, reponse.SMS.Params...)
	}

	return r
}
