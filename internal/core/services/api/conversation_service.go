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
}

// Traiter analyse un texte, exécute l'action correspondante via l'analyseur
func (s *ConversationService) Traiter(texte string) Reponse {
	reponse, verbe, match, isAction, appareil := s.analyseur.AnalyserEtExecuter("http", texte)

	r := Reponse{Handled: match, Verbe: verbe}
	if appareil != nil {
		r.Appareil = appareil.EntityID
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
