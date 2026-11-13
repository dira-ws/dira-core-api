package appconfig

// CE QUE VOIT UNE APPLICATION, FIGÉ — et la seule chose que le colis y ajoute.
//
// ⚠️⚠️ L'ARRIVÉE DU COLIS NE DOIT RIEN CHANGER À `/app/config`, SAUF UNE CLÉ :
// `features.parcel`, à `false` tant qu'aucun pays n'est ouvert. Le fichier doré
// a été produit par le code d'AVANT : pour chaque registre, chaque pays et
// chaque édition, la réponse d'aujourd'hui, privée de cette seule clé, doit
// lui être identique — les autres fonctions, les listes, le direct, la voix et
// les langues.
//
//	DIRA_GOLDEN_UPDATE=1 go test ./pkg/appconfig/ -run TestAppConfigOnlyGainsAClosedParcel
//
// ⚠️ NE RÉGÉNÉREZ PAS CE FICHIER POUR FAIRE PASSER L'ÉPREUVE : s'il doit
// changer, c'est que ce que voit une application change.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const configGolden = "testdata/app_config.golden.json"

func TestAppConfigOnlyGainsAClosedParcel(t *testing.T) {
	update := os.Getenv("DIRA_GOLDEN_UPDATE") == "1"
	registries := []struct {
		name string
		reg  *Registry
	}{
		{"nil", nil},
		{"vide", &Registry{}},
		{"réglé", &Registry{
			Features: map[string]Rule{
				FeatureRental:    {Editions: []string{"lite"}},
				FeatureWallet:    {Editions: []string{"lite"}, Countries: []string{"TD"}},
				FeatureFeed:      {Editions: []string{"full", "lite"}, Countries: []string{"GA"}},
				FeatureTripShare: {Editions: []string{"lite"}, Countries: []string{"TG"}},
			},
			ListRules: map[string]Rule{
				ListPaymentMethods: {Editions: []string{"lite"}, Allow: []string{"mobile_money", "cash"}},
				ListVehicleModes:   {Editions: []string{"lite"}, Countries: []string{"TD"}, Allow: []string{"moto", "eco"}},
			},
			Live: map[string]LiveRule{"TD": {MaxSessionSeconds: 300}, "TG": {}},
		}},
	}
	// ⚠️ LA VOIX ET LES LANGUES NE SONT PAS COMPARÉES ICI : elles viennent de
	// leurs propres paquets, que le colis ne touche pas, et leurs épreuves les
	// tiennent. Ce qui se compare est ce que le registre décide.
	type seen struct {
		Registry string              `json:"registry"`
		Country  string              `json:"country"`
		Edition  string              `json:"edition"`
		Features map[string]bool     `json:"features"`
		Lists    map[string][]string `json:"lists,omitempty"`
		Live     *LiveBlock          `json:"live,omitempty"`
	}
	var got []seen
	for _, r := range registries {
		for _, cc := range []string{"TG", "GA", "TD", ""} {
			for _, ed := range []string{"full", "lite"} {
				res := r.reg.Resolve(cc, ed)
				if !update {
					on, present := res.Features["parcel"]
					require.True(t, present, "%s %s %s : la clé du colis doit être servie", r.name, cc, ed)
					assert.False(t, on, "%s %s %s : aucun pays n'est ouvert, le colis est fermé", r.name, cc, ed)
				}
				// La seule clé que le colis ajoute ; tout le reste se compare.
				delete(res.Features, "parcel")
				got = append(got, seen{Registry: r.name, Country: res.Country, Edition: res.Edition,
					Features: res.Features, Lists: res.Lists, Live: res.Live})
			}
		}
	}
	raw, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	raw = append(raw, '\n')

	if update {
		require.NoError(t, os.MkdirAll(filepath.Dir(configGolden), 0o755))
		require.NoError(t, os.WriteFile(configGolden, raw, 0o644))
		t.Logf("fichier doré réécrit : %s", configGolden)
		return
	}
	want, err := os.ReadFile(configGolden)
	require.NoError(t, err, "le fichier doré manque : il se produit avec le code d'AVANT, jamais pour faire passer l'épreuve")
	assert.Equal(t, string(want), string(raw), "`/app/config` a changé au-delà de `features.parcel`")
}
