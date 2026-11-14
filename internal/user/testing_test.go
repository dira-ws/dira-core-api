package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// claimsOf relit ce qu'un jeton accorde, avec le secret du service d'épreuve.
func claimsOf(t *testing.T, token string) auth.Claims {
	t.Helper()
	c, err := auth.NewManager("test-secret", 0, 0).Verify(token)
	require.NoError(t, err)
	return c
}

// ⚠️⚠️ LE MARQUEUR D'ESSAI PART DANS CHAQUE JETON DU COMPTE — la connexion ET le
// rafraîchissement. C'est le jeton que la livraison lit quand ce client
// commande : sans lui, la commande d'une recette au Togo serait une vraie
// commande, et réveillerait les alertes.
func TestATestingAccountsTokensCarryTheMarker(t *testing.T) {
	svc, _, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	assert.False(t, claimsOf(t, reg.AccessToken).Testing, "un compte neuf est un vrai compte")

	before, err := svc.SetAccountTesting(context.Background(), reg.User.ID, true)
	require.NoError(t, err)
	assert.False(t, before, "l'état d'avant est rendu")

	login, err := svc.Login(context.Background(), LoginRequest{Phone: "+22890000000", Password: "s3cret-password"})
	require.NoError(t, err)
	assert.True(t, claimsOf(t, login.AccessToken).Testing, "le jeton suivant le porte")

	pair, err := svc.Refresh(context.Background(), login.RefreshToken, "")
	require.NoError(t, err)
	assert.True(t, claimsOf(t, pair.AccessToken).Testing, "le rafraîchissement aussi")

	// La recette finie, l'équipe l'ôte : le jeton suivant redevient réel.
	before, err = svc.SetAccountTesting(context.Background(), reg.User.ID, false)
	require.NoError(t, err)
	assert.True(t, before)
	again, err := svc.Refresh(context.Background(), pair.RefreshToken, "")
	require.NoError(t, err)
	assert.False(t, claimsOf(t, again.AccessToken).Testing)
}

// ⚠️ LA MARQUE N'A DE SENS QUE POUR CE QUI FAIT NAÎTRE DE L'ACTIVITÉ : un client
// qui commande, un livreur ou un chauffeur. Sur un compte de staff ou de
// marchand, rien ne la lit — elle se refuse plutôt que de se poser pour rien.
func TestOnlyClientsAndDriversCanBeMarked(t *testing.T) {
	svc, repo, _ := newUserTestService()
	for _, role := range []string{auth.RoleClient, auth.RoleDriver, auth.RoleMerchant, auth.RoleAdmin, auth.RolePartner} {
		u := &User{Role: role, Phone: "+2289000" + map[string]string{
			auth.RoleClient: "1001", auth.RoleDriver: "1002", auth.RoleMerchant: "1003",
			auth.RoleAdmin: "1004", auth.RolePartner: "1005"}[role], Name: role, Status: StatusActive}
		require.NoError(t, repo.CreateUser(context.Background(), u))

		_, err := svc.SetAccountTesting(context.Background(), u.ID.Hex(), true)
		if role == auth.RoleClient || role == auth.RoleDriver {
			require.NoError(t, err, role)
			assert.True(t, repo.users[u.ID].Testing, role)
			continue
		}
		assertUserCode(t, err, "testing_not_applicable")
		assert.Equal(t, http.StatusConflict, apperr.From(err).HTTPStatus, role)
		assert.False(t, repo.users[u.ID].Testing, "%s : rien n'est écrit", role)

		// ⚠️ L'ÔTER reste toujours possible — un compte qui l'aurait gardée
		// d'avant un changement de rôle ne doit pas rester bloqué.
		repo.users[u.ID].Testing = true
		_, err = svc.SetAccountTesting(context.Background(), u.ID.Hex(), false)
		require.NoError(t, err, role)
		assert.False(t, repo.users[u.ID].Testing, role)
	}

	_, err := svc.SetAccountTesting(context.Background(), "pas-un-identifiant", true)
	assertUserCode(t, err, "user_not_found")
}

// ⚠️ LA ROUTE EXIGE `testing`, et le dit. Un corps vide ne se lit pas « sortir
// des essais » : un `bool` nu le ferait, faute de champ.
func TestTheTestingRouteNeedsItsField(t *testing.T) {
	svc, _, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	r := chi.NewRouter()
	admin := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithClaims(req.Context(), auth.Claims{
				UserID: "6ab253499cb508f24710c036", Role: auth.RoleAdmin, Type: auth.TokenTypeAccess,
			})))
		})
	}
	NewHandler(svc).Mount(r, admin)
	call := func(body string) (int, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/admin/users/"+reg.User.ID+"/testing", strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	code, _ := call(`{}`)
	assert.Equal(t, http.StatusUnprocessableEntity, code, "sans champ, rien ne bouge")
	code, _ = call(`{"testing":true,"until":"demain"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, code, "un champ inconnu se refuse, comme partout")

	code, out := call(`{"testing":true}`)
	require.Equal(t, http.StatusOK, code, "%v", out)
	assert.Equal(t, map[string]any{"before": false, "testing": true}, out)

	code, out = call(`{"testing":true}`)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{"before": true, "testing": true}, out, "rejouer réussit, et dit que c'était fait")
}
