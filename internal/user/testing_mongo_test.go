package user

// LES COMPTES D'ESSAI, SUR UNE VRAIE BASE.
//
// ⚠️ CE QU'UNE DOUBLURE NE MONTRE PAS : que le `$set` est réellement écrit, que
// la console relit la marque sur la fiche du compte, que le jeton émis à la
// connexion la porte, et que l'ôter laisse un compte SANS le champ — la forme de
// tous les vrais comptes.
//
// Lancée seulement si `DIRA_TEST_MONGO_URI` est posé, sur une base jetable.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

func TestATestingAccountIsMarkedShownAndTokenisedOnARealDatabase(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_user_testing_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })

	repo := NewRepository(m)
	svc := NewService(repo, auth.NewManager("test-secret", 15*time.Minute, time.Hour), &fakeWalletCreator{})
	svc.auditor = audit.NewRecorder(m.DB)
	tg := country.WithCountry(ctx, "TG", country.SourceHeader)

	reg, err := svc.Register(tg, RegisterRequest{Phone: "+22890001234", Name: "Client de recette", Password: "s3cret-password"})
	require.NoError(t, err)

	// L'équipe marque le compte.
	staff := auth.WithClaims(tg, auth.Claims{UserID: "6ab253499cb508f24710c036", Role: auth.RoleAdmin})
	before, err := svc.SetAccountTesting(staff, reg.User.ID, true)
	require.NoError(t, err)
	assert.False(t, before)

	var raw bson.M
	require.NoError(t, m.Collection(usersCollection).FindOne(ctx, bson.M{"phone": "+22890001234"}).Decode(&raw))
	assert.Equal(t, true, raw["testing"], "la marque est réellement écrite")

	row, err := svc.AccountByID(tg, reg.User.ID)
	require.NoError(t, err)
	assert.True(t, row.Testing, "la console la relit sur la fiche du compte")
	rows, _, err := svc.ListAccounts(tg, AccountFilter{Role: auth.RoleClient}, "", 20)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Testing, "et dans la liste")

	login, err := svc.Login(tg, LoginRequest{Phone: "+22890001234", Password: "s3cret-password"})
	require.NoError(t, err)
	assert.True(t, claimsOf(t, login.AccessToken).Testing, "le jeton émis depuis la base la porte")

	n, err := m.Collection(audit.Collection).CountDocuments(ctx, bson.M{"action": "user.testing", "resource.id": reg.User.ID})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "le geste est au journal d'audit")

	// Rejouer ne trace rien de plus ; l'ôter rend un compte SANS le champ.
	_, err = svc.SetAccountTesting(staff, reg.User.ID, true)
	require.NoError(t, err)
	_, err = svc.SetAccountTesting(staff, reg.User.ID, false)
	require.NoError(t, err)
	raw = bson.M{}
	require.NoError(t, m.Collection(usersCollection).FindOne(ctx, bson.M{"phone": "+22890001234"}).Decode(&raw))
	_, still := raw["testing"]
	assert.False(t, still, "un vrai compte est un compte sans le champ")
	n, err = m.Collection(audit.Collection).CountDocuments(ctx, bson.M{"action": "user.testing", "resource.id": reg.User.ID})
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "la pose et le retrait ; le rejeu n'a rien écrit")

	login, err = svc.Login(tg, LoginRequest{Phone: "+22890001234", Password: "s3cret-password"})
	require.NoError(t, err)
	assert.False(t, claimsOf(t, login.AccessToken).Testing)
}
