package diramaps

// LA VILLE D'UN PAYS — et le refus de deviner.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ CHAQUE PAYS SERVI A SA VILLE, ET LA CASSE NE DÉCIDE PAS. Un jeton qui
// porte `ga` et une console qui écrit `GA` désignent le même pays ; comparer
// tel quel aurait cherché à Lomé une requête sur deux, de façon parfaitement
// reproductible et parfaitement inexplicable.
func TestEveryServedCountryHasItsCity(t *testing.T) {
	for _, c := range []struct{ code, city string }{
		{"GA", "libreville"}, {"ga", "libreville"}, {" TD ", "ndjamena"},
		{"TG", "lome"}, {"SN", "dakar"}, {"GN", "conakry"},
	} {
		city, ok := CityOf(c.code)
		require.True(t, ok, c.code)
		assert.Equal(t, c.city, city, c.code)
	}
}

// ⚠️⚠️ UN PAYS INCONNU NE REND PAS UNE VILLE DE REPLI, et c'est LA décision de
// ce fichier. Chercher « Louis » à Lomé pour quelqu'un au Bénin rendrait un
// résultat plausible, précis et FAUX — et un résultat faux voyage jusqu'au prix
// débité. C'est exactement ce qui est arrivé au Gabon le 29 octobre 2026 :
// cinq devis de Lomé pour deux quartiers de Libreville.
func TestAnUnknownCountryGetsNoFallbackCity(t *testing.T) {
	for _, code := range []string{"BJ", "CI", "FR", "", "XX", "TCHAD"} {
		city, ok := CityOf(code)
		assert.False(t, ok, "le pays %q s'est vu attribuer une ville", code)
		assert.Empty(t, city, "une ville a été rendue pour %q : la recherche partira au mauvais endroit", code)
	}
}

// ⚠️ ET LA TABLE SERVIE EST UNE COPIE : un appelant qui la modifie ne doit pas
// déplacer les villes du processus entier.
func TestTheServedTableIsACopy(t *testing.T) {
	Cities()["GA"] = "lome"
	city, _ := CityOf("GA")
	assert.Equal(t, "libreville", city)
}
