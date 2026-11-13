package staff

// UNE FICHE DE STAFF ÉCRITE AVANT LE RETRAIT D'UNE PORTÉE — sur une vraie base.
//
// ⚠️ CE QU'UNE ÉPREUVE PURE NE MONTRE PAS : la fiche telle que la préprod la
// porte (`scopes` avec `immo`), relue par le dépôt réel, servie à la console,
// inscrite au jeton, puis modifiée par une console d'avant qui renvoie ce
// qu'elle avait en mémoire — jusqu'au `$set` réel.

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

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/db"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

func TestAMemberWrittenWithARetiredScopeOnARealBase(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_staff_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })
	repo := NewRepository(m)
	svc := NewService(repo, nil)

	col := m.Collection(Collection)
	insert := func(fn string, scopes ...string) (memberID, userID primitive.ObjectID) {
		memberID, userID = primitive.NewObjectID(), primitive.NewObjectID()
		_, err := col.InsertOne(ctx, bson.M{
			"_id": memberID, "user_id": userID, "function": fn, "scopes": scopes,
			"status": StatusActive, "created_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
		})
		require.NoError(t, err)
		return memberID, userID
	}
	_, direction := insert(FunctionAdmin, "core", "food", "vtc", "immo")
	ops, _ := insert(FunctionOps, "core", "immo")
	onlyRetired, onlyRetiredUser := insert(FunctionSupport, "immo")

	// LA CONSOLE LIT : la portée retirée ne sort pas.
	rows, _, err := svc.List(ctx, "", "", "", httpx.Page{Limit: 50})
	require.NoError(t, err)
	byID := map[string]Response{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	assert.Equal(t, []string{"core"}, byID[ops.Hex()].Scopes)
	assert.Equal(t, []string{}, byID[onlyRetired.Hex()].Scopes)

	// LE JETON NE LA PORTE PAS.
	got, err := svc.ScopesOf(ctx, direction.Hex())
	require.NoError(t, err)
	assert.Equal(t, []string{"core", "food", "vtc"}, got)
	got, direction2, err := svc.EntitlementsOf(ctx, direction.Hex())
	require.NoError(t, err)
	assert.Equal(t, []string{"core", "food", "vtc"}, got)
	assert.True(t, direction2)
	got, err = svc.ScopesOf(ctx, onlyRetiredUser.Hex())
	require.NoError(t, err)
	assert.Empty(t, got, "n'administre plus rien")

	// UNE CONSOLE D'AVANT MODIFIE LA FICHE en renvoyant ce qu'elle avait lu.
	actor := auth.WithUser(ctx, direction.Hex(), auth.RoleAdmin)
	echoed := []string{"core", "immo"}
	title := "Exploitation"
	out, err := svc.Update(actor, ops.Hex(), UpdateRequest{Title: &title, Scopes: &echoed})
	require.NoError(t, err, "une console d'avant ne doit pas être bloquée")
	assert.Equal(t, []string{"core"}, out.Scopes)
	var stored Member
	require.NoError(t, col.FindOne(ctx, bson.M{"_id": ops}).Decode(&stored))
	assert.Equal(t, []string{"core"}, stored.Scopes, "la portée retirée n'est pas réécrite")

	alone := []string{"immo"}
	_, err = svc.Update(actor, onlyRetired.Hex(), UpdateRequest{Scopes: &alone})
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code, "une portée retirée seule n'accorde rien")

	typo := []string{"core", "immobilier"}
	_, err = svc.Update(actor, ops.Hex(), UpdateRequest{Scopes: &typo})
	require.Error(t, err, "une faute de frappe reste refusée")
}
