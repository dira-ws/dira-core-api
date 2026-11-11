package user

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// ⚠️⚠️ SUR UNE VRAIE BASE : les badges d'intérêt (demande du 11 nov. 2026). La
// console de l'immobilier liste `interest=immo` ; la console mère passe
// `not_only=immo` et doit garder un client qui a AUSSI commandé un repas, et un
// client qui n'a encore rien fait — seule l'égalité EXACTE `["immo"]` écarte.
func TestTheInterestBadgesFilterTheConsoles(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_interests_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })
	repo := NewRepository(m)
	svc := &Service{repo: repo}
	tg := country.WithCountry(ctx, "TG", country.SourceHeader)

	mk := func(name string, badges ...string) primitive.ObjectID {
		u := &User{Role: "client", Phone: "+2289" + fmt.Sprint(time.Now().UnixNano()%10_000_000), Name: name,
			Status: "active", Country: "TG", CreatedAt: time.Now()}
		require.NoError(t, repo.CreateUser(tg, u))
		for _, b := range badges {
			require.NoError(t, svc.AddInterest(tg, u.ID.Hex(), b))
		}
		return u.ID
	}
	onlyImmo := mk("Afi", "immo")
	both := mk("Kodjo", "immo", "food")
	foodie := mk("Ama", "food", "vtc")
	none := mk("Yawa")
	require.NoError(t, svc.AddInterest(tg, onlyImmo.Hex(), "immo"), "idempotent")

	ids := func(f AccountFilter) []primitive.ObjectID {
		rows, _, err := svc.ListAccounts(tg, f, "", 100)
		require.NoError(t, err)
		out := []primitive.ObjectID{}
		for _, r := range rows {
			out = append(out, r.OID)
			assert.NotNil(t, r.Interests, "toujours un tableau")
		}
		return out
	}
	assert.ElementsMatch(t, []primitive.ObjectID{onlyImmo, both}, ids(AccountFilter{Role: "client", Interest: "immo"}),
		"la console de l'immobilier")
	assert.ElementsMatch(t, []primitive.ObjectID{both, foodie, none}, ids(AccountFilter{Role: "client", NotOnly: "immo"}),
		"la console mère : sans ceux qui n'ont QUE l'immobilier")
	assert.ElementsMatch(t, []primitive.ObjectID{both}, ids(AccountFilter{Role: "client", Interest: "immo", NotOnly: "immo"}))

	rows, _, err := svc.ListAccounts(tg, AccountFilter{Role: "client", Interest: "immo"}, "", 100)
	require.NoError(t, err)
	for _, r := range rows {
		if r.OID == onlyImmo {
			assert.Equal(t, []string{"immo"}, r.Interests, "posé une fois")
		}
	}

	_, _, err = svc.ListAccounts(tg, AccountFilter{Interest: "jardinage"}, "", 10)
	assert.Error(t, err, "un badge inconnu est refusé, pas ignoré")
	assert.Error(t, svc.AddInterest(tg, none.Hex(), "jardinage"))
	assert.Error(t, svc.AddInterest(tg, "pas-un-id", "immo"))
}
