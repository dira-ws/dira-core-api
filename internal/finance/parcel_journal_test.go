package finance

// L'ARGENT DU COLIS AU JOURNAL — son passage, les avances des entreprises.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/internal/token"
)

// lineOf rend (débit, crédit) d'une écriture à deux lignes.
func lineOf(t *testing.T, e *Entry) (string, string) {
	t.Helper()
	require.NotNil(t, e)
	require.True(t, e.Balanced(), "équilibrée")
	require.Len(t, e.Lines, 2)
	return e.Lines[0].Account, e.Lines[1].Account
}

// ⚠️⚠️ LE PAIEMENT D'UN ENVOI PASSE PAR LE COMPTE DES COLIS — et par lui seul.
//
// Sans son genre dans `clearingOf`, le prix d'un envoi tombait « à classer », le
// compte d'attente que le balayage signale, et son remboursement de même. Payé
// par une entreprise, il part de ses AVANCES — jamais des soldes clients, que
// personne n'a crédités pour elle.
func TestAShipmentPaymentBooksToParcelClearing(t *testing.T) {
	cases := []struct {
		name          string
		tx            *token.Transaction
		w             *token.Wallet
		debit, credit string
	}{
		{"un particulier paie son envoi", tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 2200, token.RefShipment, map[string]any{"source": "cash"}), wallet("client"), AccClientWallets, AccParcelClearing},
		{"… avec un crédit offert", tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 200, token.RefShipment, map[string]any{"source": "promo"}), wallet("client"), AccPromoCredits, AccParcelClearing},
		{"son envoi annulé lui est rendu", tx(token.KindPurchase, token.ReasonRefund, token.UnitXOF, 1760, token.RefShipment, nil), wallet("client"), AccParcelClearing, AccClientWallets},
		{"une entreprise paie son envoi", tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 2200, token.RefShipment, map[string]any{"source": "cash"}), wallet(token.WalletTypeBusiness), AccBusinessAdvances, AccParcelClearing},
		{"son envoi annulé lui est rendu", tx(token.KindPurchase, token.ReasonRefund, token.UnitXOF, 2200, token.RefShipment, nil), wallet(token.WalletTypeBusiness), AccParcelClearing, AccBusinessAdvances},
		{"le frais d'un retour, pris sur le solde", tx(token.KindConsume, token.ReasonParcelReturnFee, token.UnitXOF, 950, token.RefShipmentCharge, nil), wallet(token.WalletTypeBusiness), AccBusinessAdvances, AccParcelClearing},
		{"le frais d'un retour, porté à la dette", tx(token.KindConsume, token.ReasonPaymentDue, token.UnitXOF, 950, token.RefShipmentCharge, map[string]any{"debt_delta": 950, "no_balance": true}), wallet(token.WalletTypeBusiness), AccAgentReceivable, AccParcelClearing},
		{"le même frais, payé par un particulier", tx(token.KindConsume, token.ReasonParcelReturnFee, token.UnitXOF, 950, token.RefShipmentCharge, nil), wallet("client"), AccClientWallets, AccParcelClearing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			debit, credit := lineOf(t, EntryFor(c.tx, c.w, 100))
			assert.Equal(t, c.debit, debit)
			assert.Equal(t, c.credit, credit)
		})
	}
	// Le compte est au plan, en transit, et l'aperçu le lit comme tel.
	assert.Contains(t, Chart, Account{AccParcelClearing, KindClearing, "Colis payés à répartir"})
	assert.Contains(t, Chart, Account{AccBusinessAdvances, KindLiability, "Avances des entreprises"})
}

// ⚠️⚠️ LE GAIN D'UNE ÉTAPE DÉBITE LE COMPTE DES COLIS ; CELUI D'UNE COMMANDE,
// CELUI DES COMMANDES — la ligne de toujours.
//
// Le compte des commandes était écrit EN DUR pour tout gain de livraison : le
// gain d'un livreur de colis aurait vidé un compte que le colis n'a jamais
// rempli, et laissé plein celui qu'il a rempli.
func TestADeliveryFeeFollowsItsReference(t *testing.T) {
	debit, credit := lineOf(t, EntryFor(tx(token.KindPurchase, token.ReasonDeliveryFee, token.UnitXOF, 2200, token.RefShipmentLeg, nil), wallet("driver"), 100))
	assert.Equal(t, AccParcelClearing, debit, "une étape de colis")
	assert.Equal(t, AccAgentPayables, credit)

	debit, credit = lineOf(t, EntryFor(tx(token.KindPurchase, token.ReasonDeliveryFee, token.UnitXOF, 1000, token.RefOrder, nil), wallet("driver"), 100))
	assert.Equal(t, AccOrderClearing, debit, "une commande de repas : la ligne de toujours")
	assert.Equal(t, AccAgentPayables, credit)

	// Le trajet d'un livreur annulé sous lui : pris au passage des colis.
	debit, credit = lineOf(t, EntryFor(tx(token.KindPurchase, token.ReasonParcelCancelCompensation, token.UnitXOF, 440, token.RefShipmentLeg, nil), wallet("driver"), 100))
	assert.Equal(t, AccParcelClearing, debit)
	assert.Equal(t, AccAgentPayables, credit)

	// ⚠️ Le même trajet quand l'équipe rend TOUT : une dépense de la plateforme,
	// jamais le passage des colis, qui serait alors sous zéro.
	debit, credit = lineOf(t, EntryFor(tx(token.KindPurchase, token.ReasonParcelCancelGoodwill, token.UnitXOF, 440, token.RefShipmentLeg, nil), wallet("driver"), 100))
	assert.Equal(t, AccExpenseCredits, debit)
	assert.Equal(t, AccAgentPayables, credit)

	// La commission sur ce gain, et les jetons rendus d'une étape, gardent leur ligne.
	debit, credit = lineOf(t, EntryFor(tx(token.KindConsume, token.ReasonCommission, token.UnitXOF, 330, token.RefShipmentLeg, map[string]any{"on": "delivery_fee"}), wallet("driver"), 100))
	assert.Equal(t, AccAgentPayables, debit)
	assert.Equal(t, AccRevenueCommission, credit)
	debit, credit = lineOf(t, EntryFor(tx(token.KindPurchase, "order_accept_refund", "", 2, token.RefShipmentLeg, nil), wallet("driver"), 100))
	assert.Equal(t, AccRevenueTokens, debit)
	assert.Equal(t, AccAgentTokens, credit)
}

// ⚠️ L'APPROVISIONNEMENT D'UNE ENTREPRISE VA DE LA TRÉSORERIE À SES AVANCES —
// la trésorerie que le MOYEN de la pièce désigne. Et sa dette, remboursée sur ce
// qui arrive, s'éteint sur ses avances, pas sur le compte des agents.
func TestAProvisionBooksFromTreasuryToAdvances(t *testing.T) {
	for means, treasury := range map[string]string{"bank": AccBank, "mobile_money": AccMobileMoney, "cash": AccCashBox} {
		debit, credit := lineOf(t, EntryFor(tx(token.KindPurchase, token.ReasonBusinessProvision, token.UnitXOF, 50_000, "", map[string]any{"means": means, "reference": "VIR-1"}), wallet(token.WalletTypeBusiness), 100))
		assert.Equal(t, treasury, debit, means)
		assert.Equal(t, AccBusinessAdvances, credit, means)
	}
	debit, credit := lineOf(t, EntryFor(tx(token.KindConsume, token.ReasonDebtRepaid, token.UnitXOF, 950, "", map[string]any{"debt_delta": -950}), wallet(token.WalletTypeBusiness), 100))
	assert.Equal(t, AccBusinessAdvances, debit)
	assert.Equal(t, AccAgentReceivable, credit)

	// Un moyen que la trésorerie ne connaît pas : à classer, jamais deviné.
	debit, _ = lineOf(t, EntryFor(tx(token.KindPurchase, token.ReasonBusinessProvision, token.UnitXOF, 10, "", map[string]any{"means": "troc"}), wallet(token.WalletTypeBusiness), 100))
	assert.Equal(t, AccSuspense, debit)
}

// ⚠️ LES MOYENS QU'ACCEPTE L'APPROVISIONNEMENT SONT CEUX DE LA TRÉSORERIE : un
// moyen accepté par le portefeuille et inconnu du journal tomberait « à
// classer » à chaque approvisionnement.
func TestEveryProvisionMeansHasItsTreasury(t *testing.T) {
	require.NotEmpty(t, token.ProvisionMeans)
	for _, m := range token.ProvisionMeans {
		assert.NotEmpty(t, TreasuryAccount(m), "le moyen %q n'a pas de compte de trésorerie", m)
	}
}

// Ce que l'exploitation lit au journal : « colis », jamais « commande » — et
// le portefeuille d'une entreprise s'y nomme comme tel.
func TestTheJournalSaysParcel(t *testing.T) {
	e := EntryFor(tx(token.KindPurchase, token.ReasonDeliveryFee, token.UnitXOF, 2200, token.RefShipmentLeg, nil), wallet("driver"), 100)
	require.NotNil(t, e)
	assert.Contains(t, e.Description, "étape de colis #")
	assert.NotContains(t, e.Description, "order")
	e = EntryFor(tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 2200, token.RefShipment, nil), wallet(token.WalletTypeBusiness), 100)
	require.NotNil(t, e)
	assert.Contains(t, e.Description, "payment · entreprise · colis #")
	e = EntryFor(tx(token.KindConsume, token.ReasonPaymentDue, token.UnitXOF, 950, token.RefShipmentCharge, nil), wallet(token.WalletTypeBusiness), 100)
	require.NotNil(t, e)
	assert.Contains(t, e.Description, "frais de colis #")
}
