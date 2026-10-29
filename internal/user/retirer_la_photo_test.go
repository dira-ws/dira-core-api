package user

// RETIRER SA PHOTO DE PROFIL — PAR LA ROUTE (#250 §5).
//
// ⚠️⚠️ CES ÉPREUVES PASSENT PAR `PATCH /me`, ET C'EST TOUTE LA LEÇON.
//
// J'avais écrit une épreuve qui appelait `svc.UpdateProfile` directement. Elle
// passait : le service écrit `u.AvatarURL = ""` sans broncher, et le dépôt pose
// bien la valeur vide. Sur cette foi, la promesse « `PATCH /me {"avatar_url":
// ""}` retire la photo » est partie dans CINQ contrats.
//
// Elle était fausse. En direct sur la préprod : `422 validation_failed`,
// `fields: ["avatar_url"]`. Le refus arrivait DEUX étages au-dessus du service,
// au décodage de la requête — là où aucune de mes épreuves ne regardait.
//
// `omitempty` saute la validation d'une valeur NULLE. Sur un `*string`, le
// validateur voit un pointeur NON NUL — « présent » — puis applique `url` à la
// chaîne vide, qui échoue. Il n'existait donc AUCUN moyen de retirer sa photo
// autrement qu'en téléversant une autre.
//
// C'est la même leçon que #257 et que le transfert de commande, pour la
// troisième fois : *une épreuve qui exerce la pièce, et non le chemin réel, ne
// prouve rien.* Et cette fois le prix était une phrase fausse dans cinq
// contrats que des équipes mobiles allaient lire.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// routeurDuProfil monte la vraie poignée, avec un porteur.
func routeurDuProfil(t *testing.T, svc *Service, userID string) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), auth.Claims{
				UserID: userID, Role: auth.RoleClient, Type: "access",
				Scopes: []string{auth.ScopeCore}, Country: "GA",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
	NewHandler(svc).Mount(r, authMW)
	return r
}

func patchMoi(t *testing.T, r chi.Router, corps string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/me", strings.NewReader(corps))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

// ⚠️⚠️ LA CHAÎNE VIDE RETIRE LA PHOTO — PAR LA ROUTE, DÉCODAGE COMPRIS.
func TestTheRouteAcceptsAnEmptyAvatarURLToRemoveThePhoto(t *testing.T) {
	svc, uid := addrFixture(t)
	r := routeurDuProfil(t, svc, uid)

	code, body := patchMoi(t, r, `{"avatar_url":"https://cdn.dira.llc/a/awa.jpg"}`)
	require.Equal(t, http.StatusOK, code, "%v", body)
	require.Equal(t, "https://cdn.dira.llc/a/awa.jpg", body["avatar_url"])

	code, body = patchMoi(t, r, `{"avatar_url":""}`)
	require.Equal(t, http.StatusOK, code,
		"la chaîne vide doit être ACCEPTÉE : c'est le seul moyen de retirer une photo (%v)", body)
	assert.NotContains(t, body, "avatar_url", "la réponse ne porte plus de photo")

	// Et après relecture : retirée en base, pas seulement dans la réponse.
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var relu map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &relu))
	assert.NotContains(t, relu, "avatar_url", "la photo doit être retirée EN BASE")
}

// ⚠️ ET N'IMPORTE QUOI D'AUTRE EST TOUJOURS REFUSÉ. `url|eq=` se lit « une URL,
// OU explicitement vide » : accepter la chaîne vide ne doit pas ouvrir la porte
// à une valeur quelconque, qui finirait dans un `<img src>`.
func TestTheRouteStillRefusesSomethingThatIsNotAURL(t *testing.T) {
	svc, uid := addrFixture(t)
	code, body := patchMoi(t, routeurDuProfil(t, svc, uid), `{"avatar_url":"pas-une-url"}`)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	champs := body["error"].(map[string]any)["fields"].([]any)
	assert.Equal(t, "avatar_url", champs[0])
}

// ⚠️ LE MÊME DÉFAUT TOUCHAIT LA DATE DE NAISSANCE, et le service le DISAIT
// depuis toujours : il traite `""` comme « retirer la date ». Le validateur
// (`len=10` sur "") refusait avant qu'il n'ait son mot à dire — ce code n'a
// jamais été atteint, et retirer sa date de naissance n'a jamais marché.
func TestTheRouteAcceptsAnEmptyBirthDateToRemoveIt(t *testing.T) {
	svc, uid := addrFixture(t)
	r := routeurDuProfil(t, svc, uid)

	code, body := patchMoi(t, r, `{"birth_date":"1994-03-17"}`)
	require.Equal(t, http.StatusOK, code, "%v", body)
	require.Equal(t, "1994-03-17", body["birth_date"])

	code, body = patchMoi(t, r, `{"birth_date":""}`)
	require.Equal(t, http.StatusOK, code,
		"le service gère déjà la chaîne vide : le validateur ne doit plus la couper (%v)", body)
	assert.NotContains(t, body, "birth_date")

	// Et une date malformée reste refusée.
	code, _ = patchMoi(t, r, `{"birth_date":"17/03/1994"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, code)
}

// ⚠️ UN CHAMP ABSENT N'EFFACE RIEN, et la distinction est tout l'intérêt du
// pointeur : sans elle, un formulaire qui ne poste pas la photo effacerait
// celle de quelqu'un qui changeait seulement son nom.
func TestAnAbsentFieldErasesNothing(t *testing.T) {
	svc, uid := addrFixture(t)
	r := routeurDuProfil(t, svc, uid)

	_, _ = patchMoi(t, r, `{"avatar_url":"https://cdn.dira.llc/a/awa.jpg","birth_date":"1994-03-17"}`)
	code, body := patchMoi(t, r, `{"name":"Awa N."}`)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "https://cdn.dira.llc/a/awa.jpg", body["avatar_url"])
	assert.Equal(t, "1994-03-17", body["birth_date"])
}

// ⚠️⚠️ ET LA CONSOLE AUSSI — par le VRAI décodeur, sans dépôt.
//
// `NewAdminUsers` prend un `*Repository` concret : il n'y a pas de couture où
// glisser une doublure, et monter `PATCH /admin/users/{id}` demanderait une
// base. Mais le défaut ne vit pas dans le dépôt — il vit dans le DÉCODAGE, et
// `httpx.Decode` se laisse appeler sur une requête montée à la main. L'épreuve
// prend donc le vrai validateur, sur la vraie structure, par le vrai chemin de
// décodage, et n'a besoin de rien d'autre.
//
// Sans elle, le correctif de la console survivait à toutes les mutations : une
// épreuve de moins pour un champ de plus.
func TestEveryClearableFieldIsAcceptedEmptyByTheRealDecoder(t *testing.T) {
	décode := func(t *testing.T, corps string, dans any) error {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(corps))
		req.Header.Set("Content-Type", "application/json")
		return httpx.Decode(req, dans)
	}

	for _, c := range []struct {
		nom   string
		corps string
		dans  func() any
	}{
		{"console · photo", `{"avatar_url":""}`, func() any { return &AdminUpdateUserRequest{} }},
		{"moi · photo", `{"avatar_url":""}`, func() any { return &UpdateMeRequest{} }},
		{"moi · date de naissance", `{"birth_date":""}`, func() any { return &UpdateMeRequest{} }},
	} {
		assert.NoError(t, décode(t, c.corps, c.dans()), "%s : la chaîne vide doit passer le décodeur", c.nom)
	}

	// Et n'importe quoi d'autre reste refusé, des deux côtés.
	for _, c := range []struct {
		nom   string
		corps string
		dans  func() any
	}{
		{"console · photo", `{"avatar_url":"pas-une-url"}`, func() any { return &AdminUpdateUserRequest{} }},
		{"moi · photo", `{"avatar_url":"pas-une-url"}`, func() any { return &UpdateMeRequest{} }},
		// ⚠️ `"1994"` ET NON `"17/03/1994"` : le décodeur ne vérifie que la
		// LONGUEUR (10), et `17/03/1994` en fait bien dix. C'est le SERVICE
		// qui refuse un format non ISO — ce que l'épreuve de route ci-dessus
		// vérifie. Ma première version assertait le contraire ici et rougissait
		// à juste titre : les deux étages ne défendent pas la même chose.
		{"moi · date de naissance", `{"birth_date":"1994"}`, func() any { return &UpdateMeRequest{} }},
	} {
		assert.Error(t, décode(t, c.corps, c.dans()), "%s : une valeur quelconque doit rester refusée", c.nom)
	}
}
