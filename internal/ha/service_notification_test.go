package ha

// =============================================================================
//  Tests unitaires — ciblage des notifications mobiles
// =============================================================================
//
//  But : vérifier la logique de correspondance nom → service notify.mobile_app_*
//  (celle qui a corrigé le bug « notification envoyée au mauvais téléphone »),
//  sans dépendre d'une instance Home Assistant réelle.
//
// =============================================================================

import "testing"

func TestTrouverAppareilMobileDans(t *testing.T) {
	noms := map[string]string{
		"notify.mobile_app_iphone_de_gregory": "iPhone de Grégory",
		"notify.mobile_app_pixel_8":           "Pixel 8",
		"notify.mobile_app_iphone_de_marie":   "iPhone de Marie",
	}

	cas := []struct {
		nom            string
		cible          string
		serviceAttendu string
		okAttendu      bool
	}{
		{"nom exact", "iPhone de Grégory", "notify.mobile_app_iphone_de_gregory", true},
		{"prénom seul, insensible aux accents/majuscules", "gregory", "notify.mobile_app_iphone_de_gregory", true},
		{"prénom seul, avec accent explicite", "Grégory", "notify.mobile_app_iphone_de_gregory", true},
		{"autre personne", "Marie", "notify.mobile_app_iphone_de_marie", true},
		{"nom d'appareil sans personne", "Pixel 8", "notify.mobile_app_pixel_8", true},
		{"mot trop court ignoré (« de » ne doit matcher personne)", "de", "", false},
		{"aucune correspondance", "Grégoire", "", false},
		{"cible vide", "", "", false},
	}

	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			svc, _, ok := trouverAppareilMobileDans(c.cible, noms)
			if ok != c.okAttendu {
				t.Fatalf("%q : ok=%v attendu=%v (service=%q)", c.cible, ok, c.okAttendu, svc)
			}
			if ok && svc != c.serviceAttendu {
				t.Fatalf("%q : service=%q attendu=%q", c.cible, svc, c.serviceAttendu)
			}
		})
	}
}

// Deux appareils dont le nom convivial se recoupe tous les deux avec la cible : on ne doit
// JAMAIS deviner, sous peine de reproduire le bug initial (notification au mauvais téléphone).
func TestTrouverAppareilMobileDansAmbiguite(t *testing.T) {
	noms := map[string]string{
		"notify.mobile_app_telephone_florian": "Téléphone Florian",
		"notify.mobile_app_tablette_florian":  "Tablette Florian",
	}
	_, _, ok := trouverAppareilMobileDans("Florian", noms)
	if ok {
		t.Fatalf("deux appareils correspondent à « Florian » : ok devrait être false (jamais deviner), pas true")
	}
}

func TestNomDepuisService(t *testing.T) {
	cas := map[string]string{
		"notify.mobile_app_pixel_8":           "Pixel 8",
		"notify.mobile_app_iphone_de_gregory": "Iphone De Gregory",
		"notify.mobile_app_a":                 "A",
		"mobile_app_sans_prefixe_notify":      "Sans Prefixe Notify",
	}
	for service, attendu := range cas {
		if got := nomDepuisService(service); got != attendu {
			t.Errorf("nomDepuisService(%q) = %q, attendu %q", service, got, attendu)
		}
	}
}

func TestSlugifie(t *testing.T) {
	cas := map[string]string{
		"iPhone de Grégory":      "iphone_de_gregory",
		"Pixel 8":                "pixel_8",
		"  Espaces   Multiples  ": "espaces_multiples",
		"Été-Été":                "ete_ete",
	}
	for entree, attendu := range cas {
		if got := slugifie(entree); got != attendu {
			t.Errorf("slugifie(%q) = %q, attendu %q", entree, got, attendu)
		}
	}
}
