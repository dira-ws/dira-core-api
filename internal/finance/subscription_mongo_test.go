package finance

// UN ABONNEMENT DE COURSES PAYÉ AU SOLDE, SES COURSES, PUIS SON REMBOURSEMENT —
// SUR UNE VRAIE BASE.
//
// ⚠️⚠️ CE QU'UNE DOUBLURE NE PEUT PAS MONTRER : le mouvement et son écriture
// dans la MÊME transaction Mongo, les clés tenues par l'unicité de `_id`, les
// règlements que les courses déclarent au journal, et le balayage d'intégrité
// qui recalcule chaque solde depuis ses mouvements. L'abonnement doit laisser un
// journal ÉQUILIBRÉ, le passage des courses VIDÉ une fois tout réparti, rien
// « à classer », et un balayage MUET.
//
// Lancée seulement si `DIRA_TEST_MONGO_URI` est posé, sur une base jetable.
// ⚠️ Pays d'essai : le Togo — jamais le Sénégal.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

func TestARideSubscriptionPaidFromTheWalletBalancesOnARealDatabase(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	base, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	m, err := db.Connect(base, uri, fmt.Sprintf("dira_test_abonnement_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })

	finRepo := NewRepository(m)
	fin := NewService(finRepo, nil, time.Time{})
	tokRepo := token.NewRepository(m)
	tokRepo.SetJournal(NewJournal(finRepo, 100))
	tok := token.NewService(tokRepo, audit.NewRecorder(m.DB), nil, nil, nil, 100, 5, nil)
	ctx := country.WithCountry(base, "TG", country.SourceHeader)

	rider, driver := primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()
	sub, ride := primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()
	require.NoError(t, tok.CreateClientWallet(ctx, rider))
	require.NoError(t, tok.TopUp(ctx, rider, 10_000, map[string]any{"payment_id": primitive.NewObjectID().Hex()}))

	// L'abonnement, payé au solde — rejoué, il ne débite qu'une fois.
	require.NoError(t, tok.Pay(ctx, rider, 8_000, token.RefRideSubscription, sub))
	require.NoError(t, tok.Pay(ctx, rider, 8_000, token.RefRideSubscription, sub), "rejoué : un succès, sans second débit")

	// Une course de l'abonnement, réglée au chauffeur par le grand livre des
	// courses, comme le fait `dira-vtc-api` à la fin de chaque course.
	n, err := fin.PostEvent(ctx, Event{
		Kind: "ride_settled", Country: "TG", Vertical: "vtc", RefKind: "ride", RefID: ride, OwnerID: driver,
		At: time.Now().UTC().Format(time.RFC3339), PaymentMethod: "subscription",
		FareXOF: 2_000, CommissionXOF: 400, DriverXOF: 1_600,
	})
	require.NoError(t, err)
	require.Equal(t, 2, n)

	// L'abonnement annulé : le reste est rendu, une fois.
	require.NoError(t, tok.Refund(ctx, rider, 6_000, token.RefRideSubscription, sub))
	require.NoError(t, tok.Refund(ctx, rider, 6_000, token.RefRideSubscription, sub), "rejoué : un succès, sans second crédit")

	w, err := tok.WalletOf(ctx, rider)
	require.NoError(t, err)
	assert.Equal(t, 10_000-8_000+6_000, w.BalanceXOF)
	lines, err := m.Collection("token_transactions").CountDocuments(ctx, bson.M{"ref_kind": token.RefRideSubscription})
	require.NoError(t, err)
	assert.EqualValues(t, 2, lines, "un paiement et un remboursement, pas quatre")

	// Le journal : équilibré, rien à classer, le passage des courses vidé.
	totals, err := finRepo.Totals(ctx, EntryFilter{})
	require.NoError(t, err)
	byAcc := map[string]AccountTotal{}
	d, c := 0, 0
	for _, at := range totals {
		byAcc[at.Account] = at
		d += at.Debit
		c += at.Credit
	}
	assert.Equal(t, d, c, "la balance générale")
	assert.Zero(t, byAcc[AccSuspense].Debit+byAcc[AccSuspense].Credit, "l'abonnement est tombé « à classer »")
	assert.Equal(t, 8_000, byAcc[AccRideClearing].Credit, "le paiement de l'abonnement est arrivé au passage des courses")
	assert.Zero(t, byAcc[AccRideClearing].Credit-byAcc[AccRideClearing].Debit,
		"le passage des courses n'est pas vidé : la course réglée et le remboursement ne répondent pas au paiement")
	assert.Equal(t, 1_600, byAcc[AccAgentPayables].Credit, "la part du chauffeur")
	assert.Equal(t, 400, byAcc[AccRevenueCommission].Credit, "la commission")
	assert.Equal(t, 10_000-8_000+6_000, byAcc[AccClientWallets].Credit-byAcc[AccClientWallets].Debit,
		"les soldes clients valent ce que le passager détient")

	// Et le balayage ne relève rien.
	run, err := fin.RunIntegrity(ctx)
	require.NoError(t, err)
	var findings []bson.M
	cur, err := m.Collection(findingsCollection).Find(ctx, bson.M{})
	require.NoError(t, err)
	require.NoError(t, cur.All(ctx, &findings))
	assert.Zero(t, run.Findings, "le balayage a relevé : %v", findings)
}
