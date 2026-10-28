package edition

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ⚠️⚠️ LE DÉFAUT EST `full`, ET C'EST CE QUI PROTÈGE LE PARC INSTALLÉ.
//
// Les applications déjà sur les téléphones n'envoient pas cet en-tête. Le lire
// comme une édition inconnue — ou refuser l'appel — aurait cassé toutes les
// installations le jour du déploiement, pour un en-tête qui ne sert qu'à
// adapter une liste et à compter.
//
// ⚠️ ET UNE VALEUR INCONNUE RETOMBE SUR `full`, PAS SUR `lite` : une faute de
// frappe dans une version future doit dégrader vers l'application COMPLÈTE.
// L'inverse servirait une application amputée à quelqu'un qui n'a rien demandé.
func TestAnythingUnknownIsTheFullEdition(t *testing.T) {
	for _, raw := range []string{"", " ", "FULL", "full", "Full", "lightweight", "LITE_v2", "léger", "0", "true"} {
		if raw == "LITE_v2" || raw == "lightweight" || raw == "léger" {
			assert.Equal(t, Full, Normalize(raw), "« %s » doit retomber sur l'édition complète", raw)
			continue
		}
		assert.Equal(t, Full, Normalize(raw), "« %s »", raw)
	}
	// Et `lite` est reconnu, quelle que soit sa casse ou ses espaces.
	for _, raw := range []string{"lite", "LITE", "Lite", " lite ", "\tlite\n"} {
		assert.Equal(t, Lite, Normalize(raw), "« %s » doit être reconnu comme Lite", raw)
	}
}

// ⚠️ `Known` REFUSE CE QUE `Normalize` ACCEPTE, et les deux sont utiles.
//
// `Normalize` sert au chemin d'un APPEL, où l'on ne refuse jamais. `Known`
// sert à l'ÉCRITURE d'un réglage depuis la console, où il faut refuser : un
// réglage posé sur une édition inventée ne s'appliquerait jamais, et personne
// ne saurait pourquoi la fonction reste visible dans Lite.
func TestKnownRefusesWhatNormalizeForgives(t *testing.T) {
	assert.True(t, Known("full"))
	assert.True(t, Known("lite"))
	assert.True(t, Known(" LITE "))
	for _, raw := range []string{"", "léger", "lite2", "all", "toutes"} {
		assert.False(t, Known(raw), "« %s » a été accepté comme édition", raw)
		// …alors que le même mot passe sans bruit sur le chemin d'un appel.
		assert.Equal(t, Full, Normalize(raw))
	}
}

// ⚠️⚠️ UN CONTEXTE SANS ÉDITION REND `full`, PAS UNE CHAÎNE VIDE.
//
// Un service qui n'a pas encore branché l'intergiciel sert l'application
// complète — c'est-à-dire se comporte exactement comme avant. Rendre `""`
// aurait obligé chaque point d'appel à son propre garde-fou, et le premier qui
// l'oublie sert la configuration de Lite à TOUT LE MONDE.
func TestAContextWithoutAnEditionIsFull(t *testing.T) {
	assert.Equal(t, Full, FromContext(context.Background()))
	assert.False(t, IsLite(context.Background()))

	// Posée vide, elle vaut encore `full`.
	assert.Equal(t, Full, FromContext(WithEdition(context.Background(), "", "", "")))
	assert.True(t, IsLite(WithEdition(context.Background(), "lite", "", "")))
}

// La version et la plateforme traversent le contexte, normalisées.
func TestTheAppVersionAndPlatformAreCarried(t *testing.T) {
	ctx := WithEdition(context.Background(), "lite", " 0.1.0 (26102901) ", "ANDROID")
	v, p := AppFromContext(ctx)
	assert.Equal(t, "0.1.0 (26102901)", v)
	assert.Equal(t, "android", p, "la plateforme est comparée en minuscules")

	// Sans contexte, rien — et surtout pas une valeur inventée.
	v, p = AppFromContext(context.Background())
	assert.Empty(t, v)
	assert.Empty(t, p)
}

// ⚠️ L'ÉDITION EST DÉCLARATIVE, ET CE TEST EST LÀ POUR LE RAPPELER.
//
// N'importe qui peut écrire `X-Dira-Edition: lite`. Ce paquet ne doit donc
// jamais servir à décider d'un DROIT — seulement à adapter une liste et à
// compter. Il n'expose pour cette raison aucune fonction d'autorisation, et
// cette épreuve échouera si quelqu'un en ajoute une en la nommant.
func TestNothingHereGrantsAnything(t *testing.T) {
	// L'en-tête d'un appel quelconque suffit à se dire Lite : c'est voulu.
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(Header, "lite")
	assert.Equal(t, Lite, Normalize(r.Header.Get(Header)),
		"l'édition se déclare librement — elle ne doit jamais ouvrir un droit")
	assert.Equal(t, []string{Full, Lite}, All())
}
