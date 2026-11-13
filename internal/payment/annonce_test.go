package payment

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// L'annonce d'un paiement abouti relit la LIGNE du paiement : qui a payé,
// combien, et dans quelle monnaie. C'est elle qui fait foi — la monnaie est
// celle du pays où le paiement a été lancé, pas un défaut.
func TestPaidOfReadsWhoPaidHowMuchAndInWhichCurrency(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	payer := primitive.NewObjectID().Hex()
	ctx := country.WithCountry(context.Background(), "GA", country.SourceClaims)

	resp, err := svc.Initiate(ctx, payer, InitiatePaymentRequest{
		Purpose: PurposeOrder, Amount: 7_500, RefID: primitive.NewObjectID().Hex(),
	})
	require.NoError(t, err)

	gotPayer, amount, currency, err := svc.PaidOf(context.Background(), resp.Payment.ID)
	require.NoError(t, err)
	assert.Equal(t, payer, gotPayer, "c'est au PAYEUR que la verticale rend ce qu'elle n'applique pas")
	assert.Equal(t, 7_500, amount)
	assert.Equal(t, "XAF", currency, "la monnaie du pays de l'initiation, pas le repli")
}

// Un paiement introuvable se dit INTROUVABLE — c'est ce qui permet au
// consommateur d'abandonner l'annonce au lieu de la rejouer vingt fois.
func TestPaidOfSaysNotFoundForAnUnknownPayment(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	for _, id := range []string{primitive.NewObjectID().Hex(), "pas-un-identifiant"} {
		_, _, _, err := svc.PaidOf(context.Background(), id)
		require.Error(t, err, id)
		assert.Equal(t, http.StatusNotFound, apperr.From(err).HTTPStatus, id)
	}
}

// Une ligne sans monnaie reçoit le repli du socle : un montant sans monnaie
// n'est pas un montant.
func TestPaidOfNeverReturnsAnEmptyCurrency(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	p := &Payment{
		ID: primitive.NewObjectID(), UserID: primitive.NewObjectID(), Purpose: PurposeOrder,
		RefID: primitive.NewObjectID(), Amount: 1_000, Status: StatusSucceeded,
		ProviderRef: "op-ref-sans-monnaie",
	}
	require.NoError(t, repo.Create(context.Background(), p))

	_, _, currency, err := svc.PaidOf(context.Background(), p.ID.Hex())
	require.NoError(t, err)
	assert.Equal(t, DefaultCurrency, currency)
}
