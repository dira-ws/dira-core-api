package appconfig

// CE QUI OUVRE — ET CE QUI N'OUVRE PAS — L'ONGLET DE L'IMMOBILIER.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ LA LISTE OUVRE, LA RÈGLE RETIRE — et jamais l'inverse. Le cas qui
// compte est le dernier : une règle de catalogue qui vise un AUTRE pays ne
// doit pas ouvrir celui-ci, alors que pour toutes les autres fonctions une
// règle absente veut dire « ouvert ».
func TestImmoOpensByCountryAndClosesByEdition(t *testing.T) {
	cases := []struct {
		name    string
		reg     *Registry
		country string
		ed      string
		want    bool
	}{
		{"registre nil", nil, "GA", "full", false},
		{"aucune liste", &Registry{}, "GA", "full", false},
		{"le pays est ouvert", &Registry{Immo: []string{"GA", "TD"}}, "GA", "full", true},
		{"le pays s'écrit en minuscules", &Registry{Immo: []string{"GA"}}, " ga ", "full", true},
		{"un autre pays", &Registry{Immo: []string{"GA"}}, "SN", "full", false},
		{"retiré de Lite", &Registry{
			Immo:     []string{"GA"},
			Features: map[string]Rule{FeatureImmo: {Editions: []string{"lite"}}},
		}, "GA", "lite", false},
		{"retiré de Lite, ouvert dans l'application complète", &Registry{
			Immo:     []string{"GA"},
			Features: map[string]Rule{FeatureImmo: {Editions: []string{"lite"}}},
		}, "GA", "full", true},
		{"une règle d'édition n'ouvre pas un pays absent", &Registry{
			Features: map[string]Rule{FeatureImmo: {Editions: []string{"lite"}, Countries: []string{"SN"}}},
		}, "SN", "full", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.reg.ImmoOpen(c.country, c.ed))
			assert.Equal(t, c.want, c.reg.Resolve(c.country, c.ed).Features[FeatureImmo],
				"/app/config doit dire la même chose")
		})
	}
}

// Normalisée, sans doublon, triée — et le premier code faux est nommé.
func TestTheOpenCountriesAreCleaned(t *testing.T) {
	got, bad := CleanCountries([]string{" td", "GA", "ga", "TD"})
	require.Empty(t, bad)
	assert.Equal(t, []string{"GA", "TD"}, got)

	for _, wrong := range []string{"GAB", "G", "", "1A", "G1", "G A"} {
		_, bad = CleanCountries([]string{"GA", wrong})
		assert.Equal(t, wrong, bad, "%q n'est pas un pays", wrong)
	}

	got, bad = CleanCountries(nil)
	assert.Empty(t, bad)
	assert.Nil(t, got, "rien d'ouvert s'écrit sans liste, pas avec une liste vide")
}
