package nlp

// =============================================================================
//  Tests unitaires — clé de session (« annule ça » par canal)
// =============================================================================
//
//  But : vérifier que cleAnnulation isole bien les piles d'annulation entre
//  canaux (numéro SMS, conversation HA Assist, voix, console), pour éviter
//  qu'une commande d'un canal soit annulée depuis un autre.
//
// =============================================================================

import "testing"

func TestCleAnnulation(t *testing.T) {
	cas := []struct {
		nom     string
		session string
		attendu string
	}{
		{"numéro français avec indicatif", "+33612345678", "+33612345678"},
		{"numéro français local", "0612345678", "0612345678"},
		{"conversation HA Assist 1", "http:conv-abc-123", "ha_assist"},
		{"conversation HA Assist 2 (id différent)", "http:conv-xyz-999", "ha_assist"},
		{"voix", "voix", "voix"},
		{"console", "console", "console"},
	}

	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			if got := cleAnnulation(c.session); got != c.attendu {
				t.Errorf("cleAnnulation(%q) = %q, attendu %q", c.session, got, c.attendu)
			}
		})
	}
}

// Deux numéros de téléphone différents ne doivent JAMAIS partager la même pile : chacun garde
// sa propre clé (contrairement aux conversations HA Assist, volontairement regroupées).
func TestCleAnnulationNumerosDistincts(t *testing.T) {
	a := cleAnnulation("+33611111111")
	b := cleAnnulation("+33622222222")
	if a == b {
		t.Fatalf("deux numéros différents ne doivent pas partager la même clé d'annulation (%q == %q)", a, b)
	}
}

// « voix » et « console » ne doivent pas non plus se retrouver mélangés entre eux, ni avec le
// seau partagé « ha_assist ».
func TestCleAnnulationCanauxDistincts(t *testing.T) {
	voix := cleAnnulation("voix")
	console := cleAnnulation("console")
	haAssist := cleAnnulation("http:conv-1")

	if voix == console {
		t.Fatalf("« voix » et « console » ne doivent pas partager la même pile (%q == %q)", voix, console)
	}
	if voix == haAssist || console == haAssist {
		t.Fatalf("« voix »/« console » ne doivent pas se mélanger avec le seau ha_assist")
	}
}
