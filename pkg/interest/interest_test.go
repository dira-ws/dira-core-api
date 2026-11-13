package interest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

type envois struct {
	mu   sync.Mutex
	got  []string
	fail bool
}

func (e *envois) post(_ context.Context, uid string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, uid)
	if e.fail {
		return errors.New("socle muet")
	}
	return nil
}

func (e *envois) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.got)
}

func newRep(e *envois) *Reporter {
	r := NewReporter("http://socle", "secret", Food, nil)
	r.post = e.post
	return r
}

func call(r *Reporter, uid, role string) {
	h := r.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/food/orders", nil)
	if uid != "" {
		req = req.WithContext(auth.WithUser(req.Context(), uid, role))
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
}

// ⚠️ UN CLIENT, UNE FOIS PAR PROCESSUS — et seulement un client : le chauffeur
// ou le marchand qui appelle la verticale n'en est pas un client.
func TestAClientIsReportedOnceAndOnlyAClient(t *testing.T) {
	e := &envois{}
	r := newRep(e)
	call(r, "6b0f3c1a9e2d4f5a6b7c8d90", auth.RoleClient)
	call(r, "6b0f3c1a9e2d4f5a6b7c8d90", auth.RoleClient)
	call(r, "6b0f3c1a9e2d4f5a6b7c8d91", auth.RoleMerchant)
	call(r, "6b0f3c1a9e2d4f5a6b7c8d92", auth.RoleDriver)
	call(r, "", "")
	require.Eventually(t, func() bool { return e.count() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, []string{"6b0f3c1a9e2d4f5a6b7c8d90"}, e.got)
}

// Un envoi raté se REFAIT au prochain appel : sinon un socle muet une minute
// laissait ce client sans badge jusqu'au redémarrage.
func TestAFailedReportIsRetriedNextTime(t *testing.T) {
	e := &envois{fail: true}
	r := newRep(e)
	call(r, "6b0f3c1a9e2d4f5a6b7c8d90", auth.RoleClient)
	require.Eventually(t, func() bool { return e.count() == 1 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { _, seen := r.seen.Load("6b0f3c1a9e2d4f5a6b7c8d90"); return !seen },
		time.Second, 5*time.Millisecond, "oublié après l'échec")
	e.mu.Lock()
	e.fail = false
	e.mu.Unlock()
	call(r, "6b0f3c1a9e2d4f5a6b7c8d90", auth.RoleClient)
	require.Eventually(t, func() bool { return e.count() == 2 }, time.Second, 5*time.Millisecond)
}

// Sans socle (ou sans badge valide), le rapporteur ne fait RIEN, et `After`
// rend l'authentification telle quelle.
func TestWithoutCoreNothingIsReportedAndAuthIsUnchanged(t *testing.T) {
	for name, r := range map[string]*Reporter{
		"sans adresse":  NewReporter("", "secret", Food, nil),
		"sans secret":   NewReporter("http://socle", "", Food, nil),
		"badge inconnu": NewReporter("http://socle", "secret", "jardinage", nil),
		// ⚠️ UN BADGE RETIRÉ N'EST PLUS UN BADGE : une verticale qui le
		// passerait encore ne rapporterait rien, plutôt que de recevoir un
		// refus du socle à chaque client.
		"badge retiré": NewReporter("http://socle", "secret", "immo", nil),
		"nul":          nil,
	} {
		assert.False(t, r.Enabled(), name)
		called := false
		authMW := func(next http.Handler) http.Handler { called = true; return next }
		_ = r.After(authMW)(http.NotFoundHandler())
		assert.True(t, called, name)
	}
}

// `After` pose le rapporteur DERRIÈRE l'authentification : c'est elle qui met
// le compte dans le contexte.
func TestAfterRunsTheReporterBehindTheAuth(t *testing.T) {
	e := &envois{}
	r := newRep(e)
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), "6b0f3c1a9e2d4f5a6b7c8d90", auth.RoleClient)))
		})
	}
	served := false
	h := r.After(authMW)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, served, "la requête n'attend pas le socle")
	require.Eventually(t, func() bool { return e.count() == 1 }, time.Second, 5*time.Millisecond)
}

func TestTheBadgesAreTheTwoVerticals(t *testing.T) {
	assert.Equal(t, []string{"food", "vtc"}, All)
	assert.True(t, Valid("food"))
	assert.True(t, Valid("vtc"))
	assert.False(t, Valid("Food"))
	assert.False(t, Valid(""))
	// ⚠️ Le badge d'une verticale qui ne fait plus partie de Dira n'est plus
	// un badge, même s'il reste écrit sur des comptes anciens.
	assert.False(t, Valid("immo"))
}

// L'envoi réel : la bonne route, le secret, le corps.
func TestTheReportReachesTheCoreRoute(t *testing.T) {
	var got struct {
		path, auth, body string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		buf := make([]byte, 200)
		n, _ := req.Body.Read(buf)
		got.path, got.auth, got.body = req.URL.Path, req.Header.Get("Authorization"), string(buf[:n])
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	r := NewReporter(srv.URL, "secret", Food, nil)
	require.NoError(t, r.send(context.Background(), "6b0f3c1a9e2d4f5a6b7c8d90"))
	assert.Equal(t, Path, got.path)
	assert.Equal(t, "Bearer secret", got.auth)
	assert.JSONEq(t, `{"user_id":"6b0f3c1a9e2d4f5a6b7c8d90","interest":"food"}`, got.body)

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer bad.Close()
	assert.Error(t, NewReporter(bad.URL, "secret", Food, nil).send(context.Background(), "x"), "un refus est un échec, à refaire")
}
