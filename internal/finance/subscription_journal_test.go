package finance

// L'ABONNEMENT DE COURSES AU JOURNAL — payé au solde, remboursé.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/internal/token"
)

// ⚠️⚠️ UN ABONNEMENT PAYÉ AU SOLDE PASSE PAR LE COMPTE DES COURSES, comme une
// course.
//
// Ses courses ne se débitent pas une à une : chacune se règle au chauffeur sur
// le passage des courses (`ride_settled`, moyen `subscription`). L'argent de
// l'abonnement doit donc y arriver — payé au solde comme par paiement mobile,
// c'est le même argent, au même endroit. Ailleurs, il tombait « à classer », et
// chaque course de l'abonnement vidait un compte que rien n'avait rempli.
func TestARideSubscriptionBooksToRideClearing(t *testing.T) {
	cases := []struct {
		name          string
		tx            *token.Transaction
		debit, credit string
	}{
		{"payé au solde", tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 8000, token.RefRideSubscription, map[string]any{"source": "cash"}), AccClientWallets, AccRideClearing},
		{"payé avec un crédit offert", tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 500, token.RefRideSubscription, map[string]any{"source": "promo"}), AccPromoCredits, AccRideClearing},
		{"remboursé", tx(token.KindPurchase, token.ReasonRefund, token.UnitXOF, 6000, token.RefRideSubscription, nil), AccRideClearing, AccClientWallets},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			debit, credit := lineOf(t, EntryFor(c.tx, wallet("client"), 100))
			assert.Equal(t, c.debit, debit)
			assert.Equal(t, c.credit, credit)
		})
	}
	// Et le compte que règlent ses courses est bien celui-là.
	settled := EntriesForEvent(Event{Kind: "ride_settled", Country: "TG", Vertical: "vtc", RefKind: "ride", PaymentMethod: "subscription", FareXOF: 2000, CommissionXOF: 400, DriverXOF: 1600}, time.Now().UTC())
	require.Len(t, settled, 2, "la part du chauffeur et la commission")
	for _, e := range settled {
		assert.Equal(t, AccRideClearing, e.Lines[0].Account, "une course d'abonnement se règle sur le passage des courses")
	}
}
