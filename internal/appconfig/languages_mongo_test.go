package appconfig

// LES ROUTES DES LANGUES, SUR UNE VRAIE BASE.
//
// ⚠️⚠️ CE QU'UNE DOUBLURE NE PEUT PAS MONTRER : le décodeur strict, deux étages
// au-dessus du service, et le `$set` réel. C'est lui qui fait qu'un enregistrement
// de la grille des éditions n'efface pas le catalogue — et cette propriété-là tient
// à un champ ABSENT d'une liste écrite à la main.
//
// Lancée seulement si `DIRA_TEST_MONGO_URI` est posé, sur une base jetable.

import (
	"bytes"
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

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

func routesSurBase(t *testing.T) http.Handler {
	t.Helper()
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_langues_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })

	repo := NewRepository(m)
	r := chi.NewRouter()
	admin := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithClaims(req.Context(), auth.Claims{
				UserID: "6ab253499cb508f24710c036", Role: auth.RoleAdmin, Type: "access",
			})))
		})
	}
	pass := func(next http.Handler) http.Handler { return next }
	NewHandler(NewService(repo), repo).Mount(r, admin, pass)
	return r
}

func appelle(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

const deuxLangues = `{"items":[
	{"code":"fr","name":"Français","native":"Français","app":true,"spoken":true,"live":true},
	{"code":"mos","name":"Mooré","native":"Mòoré","app":false,"spoken":true,"live":false,
	 "script":"arab","hint":"mooré, en alphabet latin","whisper":"mo"}
]}`

func TestTheLanguageRoutesOnARealBase(t *testing.T) {
	h := routesSurBase(t)

	code, view := appelle(t, h, http.MethodGet, "/admin/languages", "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, view["saved"], "rien d'enregistré : la liste de départ")
	assert.Len(t, view["items"], 8)

	code, view = appelle(t, h, http.MethodPut, "/admin/languages", deuxLangues)
	require.Equal(t, http.StatusOK, code, "%v", view)
	assert.Equal(t, true, view["saved"])
	assert.Equal(t, "6ab253499cb508f24710c036", view["updated_by"])
	require.Len(t, view["items"], 2)

	// CE QU'UNE APPLICATION LIT : rien des modèles, et le retournement de l'écriture.
	code, served := appelle(t, h, http.MethodGet, "/app/config", "")
	require.Equal(t, http.StatusOK, code)
	block := served["languages"].(map[string]any)
	assert.Equal(t, "fr", block["default"])
	items := block["items"].([]any)
	require.Len(t, items, 2)
	mos := items[1].(map[string]any)
	assert.Equal(t, true, mos["rtl"])
	assert.NotContains(t, mos, "hint")
	assert.NotContains(t, mos, "whisper")

	// CE QUE L'ANALYTIQUE LIT : tout, c'est elle qui parle aux modèles.
	_, raw := appelle(t, h, http.MethodGet, "/internal/app-config", "")
	stored := raw["languages"].([]any)[1].(map[string]any)
	assert.Equal(t, "mooré, en alphabet latin", stored["hint"])
	assert.Equal(t, "mo", stored["whisper"])
}

// ⚠️⚠️ LA GRILLE DES ÉDITIONS RENVOIE TOUT CE QU'ELLE A LU — catalogue compris.
func TestTheEditionGridLeavesTheCatalogueAloneOnARealBase(t *testing.T) {
	h := routesSurBase(t)
	code, _ := appelle(t, h, http.MethodPut, "/admin/languages", deuxLangues)
	require.Equal(t, http.StatusOK, code)

	old := `{"languages":[{"code":"fr","name":"Vieux","native":"Vieux","app":true,"spoken":true,"live":false}],
		"live":{"TD":{"max_session_seconds":300}}}`
	code, out := appelle(t, h, http.MethodPut, "/admin/app-config", old)
	require.Equal(t, http.StatusOK, code, "le champ part avec le reste : il ne doit rien refuser — %v", out)

	_, view := appelle(t, h, http.MethodGet, "/admin/languages", "")
	assert.Len(t, view["items"], 2, "la grille a réécrit le catalogue")

	_, again := appelle(t, h, http.MethodGet, "/admin/app-config", "")
	rules := again["rules"].(map[string]any)
	assert.Contains(t, rules["live"], "TD")
}

// LE DÉCODEUR STRICT ET LE REFUS NOMMÉ, à travers la vraie route.
func TestARefusalThroughTheRealRoute(t *testing.T) {
	h := routesSurBase(t)

	code, _ := appelle(t, h, http.MethodPut, "/admin/languages", `{"items":[],"oups":true}`)
	assert.Equal(t, http.StatusUnprocessableEntity, code)

	code, out := appelle(t, h, http.MethodPut, "/admin/languages",
		`{"items":[{"code":"fr","name":"Français","app":true,"spoken":true},{"code":"w o","name":"x","spoken":true}]}`)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	refus := out["error"].(map[string]any)
	assert.Equal(t, "w o", refus["reason"], "la console affiche ce champ")
	assert.Equal(t, []any{"items"}, refus["fields"])

	_, view := appelle(t, h, http.MethodGet, "/admin/languages", "")
	assert.Equal(t, false, view["saved"], "un refus n'écrit rien")
}
