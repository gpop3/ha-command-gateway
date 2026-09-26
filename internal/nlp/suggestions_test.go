package nlp

// =============================================================================
//  Tests unitaires — apprentissage proactif d'une suggestion confirmée souvent
// =============================================================================
//
//  But : vérifier que suivreConfirmationSuggestion n'apprend une phrase qu'après
//  seuilPropositionApprentissage confirmations IDENTIQUES (même appareil, même
//  verbe), qu'elle se réinitialise si l'appareil confirmé change, et qu'elle
//  ignore les domaines non apprenables (jamais un script, une automatisation…).
//
// =============================================================================

import (
	"testing"

	"ha-command-gateway/internal/ha"
)

func TestSuivreConfirmationSuggestionApprendAuSeuil(t *testing.T) {
	a := analyseurDeTest(t)
	app := ha.Appareil{EntityID: "light.salon", FriendlyName: "Salon", Domain: "light"}
	texte := "eclaire le coin lecture"

	for i := 1; i < seuilPropositionApprentissage; i++ {
		rec := &Decision{}
		if note := a.suivreConfirmationSuggestion(rec, texte, app, "allume"); note != "" {
			t.Fatalf("appel %d/%d : note inattendue avant le seuil : %q", i, seuilPropositionApprentissage, note)
		}
		if rec.cleAppris != "" {
			t.Fatalf("appel %d/%d : cleAppris déjà renseignée avant le seuil", i, seuilPropositionApprentissage)
		}
	}

	// Dernier appel : le seuil est atteint, la phrase doit être apprise
	rec := &Decision{}
	note := a.suivreConfirmationSuggestion(rec, texte, app, "allume")
	if note == "" {
		t.Fatalf("aucune note au seuil (%d confirmations) : rien n'a été proposé/appris", seuilPropositionApprentissage)
	}
	if rec.cleAppris == "" {
		t.Fatalf("cleAppris non renseignée alors que la phrase vient d'être apprise")
	}

	entree := a.appris.chercher(clePhrase(texte))
	if entree == nil {
		t.Fatalf("la phrase %q n'a pas été mémorisée dans la base apprise", texte)
	}
	if len(entree.Actions) != 1 || entree.Actions[0].EntityID != app.EntityID || entree.Actions[0].Verbe != "allume" {
		t.Fatalf("action apprise incorrecte : %+v", entree.Actions)
	}
}

func TestSuivreConfirmationSuggestionReinitialiseSiAppareilChange(t *testing.T) {
	a := analyseurDeTest(t)
	texte := "eclaire le coin lecture"
	appSalon := ha.Appareil{EntityID: "light.salon", FriendlyName: "Salon", Domain: "light"}
	appChambre := ha.Appareil{EntityID: "light.chambre", FriendlyName: "Chambre", Domain: "light"}

	// Confirmé une fois sur le salon, puis change d'avis pour la chambre : le compteur du
	// salon ne doit pas compter pour la chambre.
	a.suivreConfirmationSuggestion(&Decision{}, texte, appSalon, "allume")
	for i := 1; i < seuilPropositionApprentissage; i++ {
		rec := &Decision{}
		if note := a.suivreConfirmationSuggestion(rec, texte, appChambre, "allume"); note != "" {
			t.Fatalf("appel %d/%d (après changement d'appareil) : note inattendue avant le seuil : %q",
				i, seuilPropositionApprentissage, note)
		}
	}
	rec := &Decision{}
	note := a.suivreConfirmationSuggestion(rec, texte, appChambre, "allume")
	if note == "" {
		t.Fatalf("la chambre aurait dû être apprise après %d confirmations consécutives", seuilPropositionApprentissage)
	}
	entree := a.appris.chercher(clePhrase(texte))
	if entree == nil || entree.Actions[0].EntityID != appChambre.EntityID {
		t.Fatalf("c'est la chambre qui aurait dû être apprise, pas le salon : %+v", entree)
	}
}

func TestSuivreConfirmationSuggestionIgnoreDomaineNonApprenable(t *testing.T) {
	a := analyseurDeTest(t)
	// script/automation/scene ne sont jamais appris (texte libre, risque) : même après de
	// nombreuses confirmations, aucune note ne doit apparaître.
	app := ha.Appareil{EntityID: "script.arroser", FriendlyName: "Arroser", Domain: "script"}
	texte := "lance l arrosage"

	for i := 0; i < seuilPropositionApprentissage+5; i++ {
		rec := &Decision{}
		if note := a.suivreConfirmationSuggestion(rec, texte, app, "lance"); note != "" {
			t.Fatalf("domaine %q ne devrait jamais être appris (appel %d) : %q", app.Domain, i, note)
		}
	}
	if a.appris.chercher(clePhrase(texte)) != nil {
		t.Fatalf("une entrée a été mémorisée pour un domaine non apprenable")
	}
}
