package token

// L'ABONNEMENT DE COURSES, PAYÉ AU SOLDE ET REMBOURSÉ.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ⚠️ UN ABONNEMENT SE PAIE AU SOLDE SOUS SON GENRE, UNE FOIS, ET SE REMBOURSE
// UNE FOIS — sous l'identifiant de l'abonnement, comme son paiement en ligne.
func TestARideSubscriptionIsPaidAndRefundedOnceFromTheWallet(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	rider := seedMoneyWallet(t, repo, 10_000, 0)
	sub := primitive.NewObjectID().Hex()

	for i := 0; i < 2; i++ {
		require.NoError(t, svc.Pay(context.Background(), rider, 8_000, RefRideSubscription, sub), "rejoué : un succès")
	}
	owner, _ := primitive.ObjectIDFromHex(rider)
	assert.Equal(t, 2_000, repo.moneyOf(owner).BalanceXOF, "débité une fois")
	last := repo.transactions[len(repo.transactions)-1]
	assert.Equal(t, ReasonPayment, last.Reason)
	assert.Equal(t, RefRideSubscription, last.RefKind)
	assert.Equal(t, sub, last.RefID.Hex())

	for i := 0; i < 2; i++ {
		require.NoError(t, svc.Refund(context.Background(), rider, 6_000, RefRideSubscription, sub), "rejoué : un succès")
	}
	assert.Equal(t, 8_000, repo.moneyOf(owner).BalanceXOF, "remboursé une fois")
}
