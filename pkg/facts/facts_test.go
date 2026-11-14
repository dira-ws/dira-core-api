package facts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Un fait fait l'aller-retour par le flux sans rien perdre : c'est la seule
// garantie que l'analytique lit ce que la verticale a écrit.
func TestFactSurvivesTheStream(t *testing.T) {
	at := time.Date(2026, 9, 12, 18, 30, 0, 0, time.UTC)
	in := Fact{Kind: DemandUnmet, Vertical: VerticalVTC, At: at, Geo: [2]float64{1.2385, 6.1481},
		Ref: "ride1", Actor: "rider1", Reason: ReasonExhausted, Attrs: map[string]string{"attempts": "3"}}
	values, err := Encode(in)
	require.NoError(t, err)
	assert.Equal(t, "demand.unmet", values["kind"])
	assert.Equal(t, "1789237800000", values["at"])

	out, err := Decode(values)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestGarbageIsAnError(t *testing.T) {
	_, err := Decode(map[string]any{"json": "{"})
	assert.Error(t, err)
}

// ⚠️ LE MARQUEUR D'ESSAI TRAVERSE LE FLUX — c'est par lui que l'analytique sait
// qu'une demande non servie au Togo vient d'une recette, et ne l'additionne à
// aucune zone rouge.
func TestATrialFactSurvivesTheStream(t *testing.T) {
	in := Fact{Kind: DemandUnmet, Vertical: VerticalFood, At: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC),
		Country: "TG", Ref: "course1", Reason: ReasonExhausted, Testing: true}
	values, err := Encode(in)
	require.NoError(t, err)
	assert.Contains(t, values["json"], `"testing":true`)
	out, err := Decode(values)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

// ⚠️ UN VRAI FAIT EST CELUI D'AVANT, octet pour octet : pas de `testing: false`
// qu'un lecteur d'avant ce champ aurait à ignorer.
func TestARealFactCarriesNoTrialMarker(t *testing.T) {
	values, err := Encode(Fact{Kind: DemandRequested, Vertical: VerticalFood, At: time.Now().UTC(), Ref: "c"})
	require.NoError(t, err)
	assert.NotContains(t, values["json"], "testing")
}

// ⚠️ LE FIL NE DÉDUIT PAS L'ESSAI DE LA REQUÊTE. Le pays, si ; le marqueur, non :
// un fait part souvent de la requête d'un autre acteur que celui qui a fait
// naître l'objet — le livreur d'essai qui accepte une VRAIE commande. Déduit
// de son jeton, le marqueur ferait disparaître une vraie course des chiffres.
func TestTheFeedNeverInfersATrialFromTheRequest(t *testing.T) {
	courier := auth.WithClaims(context.Background(), auth.Claims{UserID: "u1", Role: auth.RoleDriver, Testing: true})
	courier = country.WithCountry(courier, "TG", country.SourceClaims)

	got := stamp(courier, Fact{Kind: DemandMet, Vertical: VerticalFood, Ref: "vraie"})
	assert.False(t, got.Testing, "la course est vraie : son fait aussi")
	assert.Equal(t, "TG", got.Country, "le pays, lui, vient bien de la requête")
	assert.False(t, got.At.IsZero())

	kept := stamp(context.Background(), Fact{Kind: DemandMet, Vertical: VerticalFood, Ref: "essai", Testing: true})
	assert.True(t, kept.Testing, "le marqueur posé par le producteur reste")
}
