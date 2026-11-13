package appconfig

// LES PAYS DU COLIS, ÉCRITS PAR LA CONSOLE.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconf "github.com/kgtech-org/dira-core-api/pkg/appconfig"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// L'écriture normalise les pays ouverts, et refuse un code qui n'en est pas un
// EN LE NOMMANT : un pays mal écrit qui passerait ouvrirait un pays inconnu, ou
// n'ouvrirait rien sans que l'écran le dise.
func TestTheParcelCountriesAreWrittenCleanAndATypoIsNamed(t *testing.T) {
	repo := &fakeStore{}
	svc := NewService(repo)
	require.NoError(t, svc.Save(t.Context(), pkgconf.Registry{Parcel: []string{"tg", " TG ", "ga"}}, "test"))
	require.NotNil(t, repo.saved)
	assert.Equal(t, []string{"GA", "TG"}, repo.saved.Parcel)

	err := svc.Save(t.Context(), pkgconf.Registry{Parcel: []string{"TG", "Togo"}}, "test")
	require.Error(t, err)
	e := apperr.From(err)
	assert.Equal(t, "validation_failed", e.Code)
	assert.Equal(t, []string{"parcel"}, e.Meta["fields"])
	assert.Equal(t, "Togo", e.Meta["reason"], "le refus nomme le code fautif")

	// Rien n'ouvert : rien de rangé, pas une liste vide.
	require.NoError(t, svc.Save(t.Context(), pkgconf.Registry{}, "test"))
	assert.Nil(t, repo.saved.Parcel)
}

// ⚠️ LA RÉPONSE DE `/app/config` SUIT LE REGISTRE ENREGISTRÉ, pays par pays.
func TestTheServedConfigFollowsTheOpenedCountries(t *testing.T) {
	svc := NewService(&fakeStore{reg: &pkgconf.Registry{Parcel: []string{"TG"}}})
	assert.True(t, svc.Resolve(requestFrom("TG", "full")).Features[pkgconf.FeatureParcel])
	assert.False(t, svc.Resolve(requestFrom("GA", "full")).Features[pkgconf.FeatureParcel])
}
