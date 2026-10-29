package token

// LES BORNES DE RECHARGE SERVIES AVEC LE SOLDE (#250).
//
// ⚠️ L'APPLICATION LES CODAIT EN DUR. Dira Lite proposait 1 000 / 2 000 /
// 5 000 / 10 000 F et un montant libre de sept chiffres — des nombres choisis
// pour le Togo, recopiés dans trois applications, et dérisoires en Guinée où
// l'unité vaut quinze fois moins. Le contrat ne fixait que `amount > 0`.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// bornesDuPays est la politique telle que le câblage la fournit.
type bornesDuPays struct {
	vuePays string
	rendue  TopupResponse
}

func (b *bornesDuPays) TopupOf(_ context.Context, code string) TopupResponse {
	b.vuePays = code
	return b.rendue
}

func politique() *bornesDuPays {
	return &bornesDuPays{rendue: TopupResponse{
		Min: 500, Max: 2_000_000, Presets: []int{1_000, 2_000, 5_000, 10_000},
		Currency: "XOF", Decimals: 0,
	}}
}

// Le portefeuille d'un client porte ses bornes, et le PAYS DE LA REQUÊTE les
// choisit.
func TestAClientWalletCarriesTheCountrysTopupBounds(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	p := politique()
	svc.SetTopupBounds(p)

	ctx := country.WithCountry(context.Background(), "GN", country.SourceClaims)
	got, err := svc.GetWallet(ctx, seedWallet(t, repo, WalletTypeClient, 0).Hex(), auth.RoleClient, "")
	require.NoError(t, err)

	require.NotNil(t, got.Topup, "sans bornes servies, l'app garde ses montants codés en dur")
	assert.Equal(t, 500, got.Topup.Min)
	assert.Equal(t, 2_000_000, got.Topup.Max)
	assert.Equal(t, []int{1_000, 2_000, 5_000, 10_000}, got.Topup.Presets)
	assert.Equal(t, "GN", p.vuePays,
		"⚠️ les bornes du pays de LA REQUÊTE : servir celles du déploiement "+
			"proposerait des francs CFA à Conakry")

	// ⚠️ LA MONNAIE ET SES DÉCIMALES PARTENT AVEC LES NOMBRES. Sans elles,
	// l'écran lit des nairas comme des francs, et affiche « 2 000 GH₵ » pour
	// 20 cedis.
	assert.Equal(t, "XOF", got.Topup.Currency)
	assert.NotNil(t, got.Topup.Decimals)
}

// ⚠️ UN PORTEFEUILLE QUI NE SE RECHARGE PAS EN ARGENT N'EN PORTE PAS. Celui
// d'un marchand s'alimente par ses ventes, celui d'un livreur par ses
// commissions : leur servir des montants de recharge ferait apparaître un écran
// « Recharger » sur un portefeuille qu'aucune route ne recharge.
func TestOnlyAClientWalletCarriesTopupBounds(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, stores := newTestService(repo)
	svc.SetTopupBounds(politique())
	ctx := context.Background()

	livreur := seedWallet(t, repo, WalletTypeDriver, 10)
	got, err := svc.GetWallet(ctx, livreur.Hex(), auth.RoleDriver, "")
	require.NoError(t, err)
	assert.Nil(t, got.Topup, "un livreur ne recharge pas son portefeuille en argent")

	boutique := seedWallet(t, repo, WalletTypeMerchant, 10)
	stores.owned[boutique.Hex()] = "marchand"
	got, err = svc.GetWallet(ctx, "marchand", auth.RoleMerchant, boutique.Hex())
	require.NoError(t, err)
	assert.Nil(t, got.Topup, "un marchand s'alimente par ses ventes")
}

// Sans politique câblée, la réponse reste valide et l'application retombe sur
// ses propres montants, comme avant. Un service de développement sans pays
// réglés ne doit pas perdre son portefeuille.
func TestWithoutAWiredPolicyTheWalletStillAnswers(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	got, err := svc.GetWallet(context.Background(),
		seedWallet(t, repo, WalletTypeClient, 0).Hex(), auth.RoleClient, "")
	require.NoError(t, err)
	assert.Nil(t, got.Topup)
}
