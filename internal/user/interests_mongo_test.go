package user

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// ⚠️⚠️ SUR UNE VRAIE BASE : les badges d'intérêt (demande du 11 nov. 2026) — et
// ceux qu'une verticale retirée a laissés sur des comptes. Un badge retiré
// reste ÉCRIT (on ne réécrit pas un compte pour le taire) ; il ne se montre
// plus, ne se filtre plus, ne se pose plus. Et la console d'avant, qui envoie
// encore `not_only=immo` à chaque liste de clients, reçoit sa liste.
func TestTheInterestBadgesAndTheOnesARetiredVerticalLeft(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_interests_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })
	repo := NewRepository(m)
	svc := &Service{repo: repo}
	tg := country.WithCountry(ctx, "TG", country.SourceHeader)

	// `legacy` : écrit PAR LE DÉPÔT, sans la validation du service — comme une
	// version d'avant l'a écrit en préprod.
	mk := func(name string, badges []string, legacy ...string) primitive.ObjectID {
		u := &User{Role: "client", Phone: "+2289" + fmt.Sprint(time.Now().UnixNano()%10_000_000), Name: name,
			Status: "active", Country: "TG", CreatedAt: time.Now()}
		require.NoError(t, repo.CreateUser(tg, u))
		for _, b := range badges {
			require.NoError(t, svc.AddInterest(tg, u.ID.Hex(), b))
		}
		for _, b := range legacy {
			require.NoError(t, repo.AddInterest(tg, u.ID, b))
		}
		return u.ID
	}
	onlyRetired := mk("Afi", nil, "immo")
	both := mk("Kodjo", []string{"food"}, "immo")
	foodie := mk("Ama", []string{"food", "vtc"})
	none := mk("Yawa", nil)
	require.NoError(t, svc.AddInterest(tg, foodie.Hex(), "food"), "idempotent")

	rows := func(f AccountFilter) map[primitive.ObjectID][]string {
		got, _, err := svc.ListAccounts(tg, f, "", 100)
		require.NoError(t, err)
		out := map[primitive.ObjectID][]string{}
		for _, r := range got {
			require.NotNil(t, r.Interests, "toujours un tableau")
			out[r.OID] = r.Interests
		}
		return out
	}

	all := rows(AccountFilter{Role: "client"})
	assert.Len(t, all, 4, "un badge retiré n'écarte personne")
	assert.Equal(t, []string{}, all[onlyRetired], "le badge retiré ne se montre plus")
	assert.Equal(t, []string{"food"}, all[both], "les autres badges restent")
	assert.Equal(t, []string{"food", "vtc"}, all[foodie], "posé une fois")
	assert.Equal(t, []string{}, all[none])

	food := rows(AccountFilter{Role: "client", Interest: "food"})
	assert.ElementsMatch(t, []primitive.ObjectID{both, foodie}, keys(food))

	row, err := svc.AccountByID(tg, onlyRetired.Hex())
	require.NoError(t, err)
	assert.Equal(t, []string{}, row.Interests, "la fiche non plus ne le montre")

	_, _, err = svc.ListAccounts(tg, AccountFilter{Interest: "immo"}, "", 10)
	assert.Error(t, err, "on ne filtre pas sur un badge qui ne se montre plus")
	_, _, err = svc.ListAccounts(tg, AccountFilter{Interest: "jardinage"}, "", 10)
	assert.Error(t, err, "un badge inconnu est refusé, pas ignoré")
	assert.Error(t, svc.AddInterest(tg, none.Hex(), "immo"), "un badge retiré ne se pose plus")
	assert.Error(t, svc.AddInterest(tg, none.Hex(), "jardinage"))
	assert.Error(t, svc.AddInterest(tg, "pas-un-id", "food"))

	// ⚠️ PAR LA VRAIE ROUTE : la console d'avant envoie `not_only=immo` à
	// chaque liste de clients. Elle reçoit la liste entière, pas un `422`.
	r := chi.NewRouter()
	NewHandler(svc).Mount(r, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			c := auth.WithClaims(req.Context(), auth.Claims{
				UserID: primitive.NewObjectID().Hex(), Role: auth.RoleAdmin, Type: "access",
			})
			next.ServeHTTP(w, req.WithContext(country.WithCountry(c, "TG", country.SourceHeader)))
		})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/users?role=client&not_only=immo&limit=100", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var page struct {
		Items []struct {
			ID        string   `json:"id"`
			Interests []string `json:"interests"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	listed := map[string][]string{}
	for _, it := range page.Items {
		listed[it.ID] = it.Interests
	}
	assert.Len(t, listed, 4, "la liste entière")
	assert.Contains(t, listed, onlyRetired.Hex(), "le client qui n'avait que le badge retiré est listé")
	assert.Equal(t, []string{}, listed[onlyRetired.Hex()])
}

func keys(m map[primitive.ObjectID][]string) []primitive.ObjectID {
	out := make([]primitive.ObjectID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
