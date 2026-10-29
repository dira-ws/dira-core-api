package country

// LES BORNES DE RECHARGE D'UN PAYS (#250).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// ⚠️⚠️ UN PAYS JAMAIS RÉGLÉ RECHARGE. C'est l'inverse du retrait, et c'est la
// seule valeur défendable : un retrait éteint ne gêne personne tant qu'on ne
// l'annonce pas, une recharge éteinte coupe le moyen de payer une course.
func TestACountryNeverConfiguredCanStillTopUp(t *testing.T) {
	for _, code := range []string{"TG", "GN", "GA", "GH", "NG"} {
		out := topupResponse(code, Topup{})
		assert.Positive(t, out.Min, "%s : un minimum nul laisserait passer un franc", code)
		assert.Positive(t, out.Max, "%s : un maximum nul se lit « aucun montant accepté »", code)
		assert.NotEmpty(t, out.Presets, "%s : sans montants proposés, l'app garde les siens", code)
		assert.NotEmpty(t, out.Currency, "%s : la monnaie doit partir avec les nombres", code)
		require.NoError(t, checkTopupCoherence(out), "%s : les défauts doivent être cohérents", code)
	}
}

// ⚠️⚠️ LES NOMBRES NE SONT PAS LES MÊMES PARTOUT, et c'est le piège de cette
// couche. 1 000 vaut un petit trajet au Togo et sept centimes d'euro en
// Guinée : des bornes uniques auraient proposé « 1 000 FG » à Conakry.
func TestTheBoundsFollowTheCountrysCurrencyNotTheFranc(t *testing.T) {
	togo := topupResponse("TG", Topup{})
	guinée := topupResponse("GN", Topup{})

	assert.Equal(t, "XOF", togo.Currency)
	assert.Equal(t, "GNF", guinée.Currency)
	assert.Greater(t, guinée.Min, togo.Min,
		"le franc guinéen vaut ~15 fois moins : les mêmes nombres y seraient dérisoires")
	assert.Greater(t, guinée.Presets[0], togo.Presets[0])

	// ⚠️ ET LES DÉCIMALES PARTENT AVEC. L'entier d'un cedi est en centièmes :
	// sans `decimals`, l'écran afficherait « 2 000 GH₵ » pour 20 cedis.
	ghana := topupResponse("GH", Topup{})
	assert.Equal(t, 2, ghana.Decimals, "le cedi porte deux décimales")
	assert.Equal(t, 0, togo.Decimals, "le franc CFA n'en porte aucune")
}

// ⚠️ UNE MONNAIE AJOUTÉE AU CATALOGUE SANS BORNES COUPERAIT LA RECHARGE DE SON
// PAYS — `max = 0` se lit « aucun montant accepté ». Le repli l'empêche ; cette
// épreuve, elle, fait rougir l'ajout pour qu'on choisisse les nombres pour de
// vrai au lieu de laisser des francs CFA en Éthiopie.
func TestEveryCatalogueCurrencyHasItsTopupDefaults(t *testing.T) {
	for _, info := range country.Catalog {
		_, ok := topupDefaults[info.Currency]
		assert.True(t, ok,
			"la monnaie %s (%s) n'a pas de bornes de recharge : ajoutez-la à "+
				"`topupDefaults` avec des nombres ronds DANS CETTE MONNAIE",
			info.Currency, info.Code)
	}
	// Le repli existe quand même, et il est cohérent.
	require.NoError(t, checkTopupCoherence(topupResponse("ZZ", Topup{})))
	assert.Positive(t, topupResponse("ZZ", Topup{}).Max)
}

// ⚠️⚠️ UN MONTANT PROPOSÉ HORS BORNES EST UN BOUTON CONDAMNÉ. L'écran affiche
// « 10 000 », le client appuie, le serveur refuse ce qu'il venait lui-même de
// proposer — et personne ne soupçonne le réglage, on soupçonne l'opérateur.
func TestASuggestedAmountOutsideTheBoundsIsRefused(t *testing.T) {
	err := checkTopupCoherence(TopupResponse{Min: 1000, Max: 50000, Presets: []int{1000, 100000}})
	require.Error(t, err)
	assert.Equal(t, "topup_preset_out_of_bounds", apperr.From(err).Code)
	assert.Contains(t, apperr.From(err).Meta["reason"], "preset=100000",
		"le refus doit nommer LE montant fautif ET les bornes")

	err = checkTopupCoherence(TopupResponse{Min: 1000, Max: 50000, Presets: []int{500}})
	require.Error(t, err, "sous le minimum, c'est le même bouton condamné")
}

// ⚠️ UN MAXIMUM SOUS LE MINIMUM NE LAISSE PASSER AUCUN MONTANT, et chaque borne
// est pourtant valide seule.
func TestAMaximumUnderTheMinimumIsRefusedWithBothNumbers(t *testing.T) {
	err := checkTopupCoherence(TopupResponse{Min: 50000, Max: 10000})
	require.Error(t, err)
	e := apperr.From(err)
	assert.Equal(t, "topup_max_under_min", e.Code)
	assert.Contains(t, e.Meta["reason"], "min=50000")
	assert.Contains(t, e.Meta["reason"], "max=10000",
		"⚠️ LES DEUX nombres : celui qui lit vient d'en changer un seul, et c'est "+
			"souvent l'autre qu'il faut corriger")
}

// ⚠️⚠️ LA COHÉRENCE SE VÉRIFIE SUR LE RÉSULTAT FUSIONNÉ, JAMAIS SUR LA REQUÊTE.
// `{"min": 50000}` seul suffit à rendre impossible un `max: 10000` écrit trois
// mois plus tôt — sans que la requête le nomme. Une règle qui ne regarde que ce
// qu'on lui envoie laisse creuser le trou par le côté.
func TestTheCoherenceCheckReadsTheMergedPolicy(t *testing.T) {
	// Le pays porte déjà un maximum bas et des montants proposés bas.
	existant := Topup{Max: 10000, Presets: []int{1000, 5000}}
	// La console ne change QUE le minimum, et ne nomme pas le maximum.
	existant.Min = 50000

	err := checkTopupCoherence(topupResponse("TG", existant))
	require.Error(t, err, "le trio fusionné est impossible, et la requête ne le disait pas")
}

// Les montants proposés sont rangés et dédoublonnés à la LECTURE : un pays
// réglé avant ce tri porte peut-être déjà une liste en désordre, et l'écran
// afficherait « 5 000 · 1 000 · 10 000 ».
func TestSuggestedAmountsAreSortedAndDeduplicatedOnRead(t *testing.T) {
	out := topupResponse("TG", Topup{Min: 500, Max: 100000, Presets: []int{5000, 1000, 5000, 10000}})
	assert.Equal(t, []int{1000, 5000, 10000}, out.Presets)
}
