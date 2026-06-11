package api

import (
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
	msg, verbe, traite, _, appareil := s.analyseur.AnalyserEtExecuter("http", texte)

	r := Reponse{Handled: traite, Verbe: verbe}
	if appareil != nil {
		r.Appareil = appareil.EntityID
	}
	if msg != nil {
		r.Speech = msg.Voix.Texte
		if r.Speech == "" {
			r.Speech = msg.SMS.Texte
		}
	}
	return r
}
