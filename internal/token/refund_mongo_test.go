package token

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

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// ⚠️⚠️ SUR UNE VRAIE BASE : un remboursement rangé sous la même référence ne
// crédite qu'UNE fois, même rejoué ; sous deux références, chacune le sien.
//
// La garantie tient à l'unicité de `_id` dans `wallet_operations`, réservée
// DANS la transaction du mouvement — et à ce qu'une clé déjà prise annule la
// transaction sans rien laisser derrière elle. Une doublure ne peut que
// recopier cette règle ; seule la base dit si l'abandon d'une transaction sur
// une clé en double laisse bien le solde intact.
func TestARefundReplayedOnARealDatabaseCreditsOnce(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_refund_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })

	svc := NewService(NewRepository(m), nil, nil, nil, nil, 100, 5, nil)
	ctx = country.WithCountry(ctx, "TG", country.SourceHeader)
	owner := primitive.NewObjectID().Hex()
	require.NoError(t, svc.CreateClientWallet(ctx, owner))

	refA, refB := primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()
	require.NoError(t, svc.Refund(ctx, owner, 1_000, RefOrder, refA))
	require.NoError(t, svc.Refund(ctx, owner, 1_000, RefOrder, refA), "rejoué : un succès, sans second crédit")
	require.NoError(t, svc.Refund(ctx, owner, 2_500, RefOrder, refB))

	w, err := svc.WalletOf(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, 3_500, w.BalanceXOF, "1 000 une fois, puis 2 500 — pas 4 500")

	lines, err := m.Collection(transactionsCollection).CountDocuments(ctx, bson.M{"reason": ReasonRefund})
	require.NoError(t, err)
	assert.EqualValues(t, 2, lines, "le grand livre porte deux remboursements, pas trois")
}
