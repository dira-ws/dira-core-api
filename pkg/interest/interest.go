// Package interest : les BADGES D'INTÉRÊT d'un client — `food`, `vtc`, `immo`.
//
// Demande du propriétaire du 11 nov. 2026 : identifier les clients par les
// métiers qui les intéressent, selon la plateforme où ils se connectent, et
// ensuite dès qu'ils réservent ou même cherchent. La console de l'immobilier
// liste les clients `immo` ; la console mère ÉCARTE ceux qui n'ont QUE `immo`.
//
// ⚠️⚠️ LA CONNEXION NE LE DIT PAS. `app: "client"` sert TOUTES les applications
// de clients — les repas, les courses, et Dira Immo —, et un compte client est
// UNIQUE à travers elles. Ce qui le dit, c'est l'USAGE : un appel authentifié
// d'un client à une verticale (une recherche, une commande, une course) vaut
// intérêt pour elle. D'où un middleware que chaque verticale pose derrière son
// authentification (`Reporter.After`), et non un champ de connexion.
//
// ⚠️ LES FAITS NE SUFFISAIENT PAS : la livraison n'y met pas le client
// (`actor`), et la recherche immobilière, exprès, non plus.
package interest

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Les badges.
const (
	Food = "food"
	VTC  = "vtc"
	Immo = "immo"
)

// All : les badges connus, dans l'ordre d'affichage.
var All = []string{Food, VTC, Immo}

// Valid dit si `s` est un badge connu.
func Valid(s string) bool {
	for _, v := range All {
		if v == s {
			return true
		}
	}
	return false
}

// Path : la route interne du socle qui pose un badge.
const Path = "/api/v1/internal/accounts/interests"

// maxSeen borne la mémoire du rapporteur : au-delà, il oublie tout et
// redemande — le socle est idempotent, un oubli coûte un appel, pas un doublon.
const maxSeen = 200_000

// Reporter dit au socle, UNE fois par compte et par processus, qu'un client
// utilise cette verticale.
type Reporter struct {
	base, token, vertical string
	http                  *http.Client
	logger                *slog.Logger

	seen  sync.Map
	count atomic.Int64
	// post : l'envoi, remplaçable par l'épreuve.
	post func(ctx context.Context, userID string) error
}

// NewReporter : `baseURL` est celui du socle (sans `/api/v1`), `token` le
// secret de service. Un rapporteur sans socle ne rapporte rien, en silence :
// le badge est une commodité d'exploitation, jamais une condition de service.
func NewReporter(baseURL, serviceToken, vertical string, logger *slog.Logger) *Reporter {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Reporter{base: baseURL, token: serviceToken, vertical: vertical,
		http: &http.Client{Timeout: 5 * time.Second}, logger: logger}
	r.post = r.send
	return r
}

// Enabled dit si le rapporteur peut joindre le socle.
func (r *Reporter) Enabled() bool {
	return r != nil && r.base != "" && r.token != "" && Valid(r.vertical)
}

// After enchaîne l'authentification de la verticale et le rapporteur : la
// verticale remplace `authMW` par `reporter.After(authMW)`, et toutes ses
// routes authentifiées rapportent.
func (r *Reporter) After(authMW func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	if !r.Enabled() {
		return authMW
	}
	return func(next http.Handler) http.Handler { return authMW(r.Middleware(next)) }
}

// Middleware rapporte le client authentifié de la requête, sans la retarder.
func (r *Reporter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.Note(req.Context())
		next.ServeHTTP(w, req)
	})
}

// Note rapporte le compte du contexte s'il est CLIENT — un chauffeur, un
// marchand ou un annonceur n'est pas un client de la verticale.
func (r *Reporter) Note(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	uid, ok := auth.UserFromContext(ctx)
	role, _ := auth.RoleFromContext(ctx)
	if !ok || uid == "" || role != auth.RoleClient {
		return
	}
	if _, dup := r.seen.LoadOrStore(uid, struct{}{}); dup {
		return
	}
	if r.count.Add(1) > maxSeen {
		r.seen.Clear()
		r.count.Store(0)
	}
	// ⚠️ HORS DE LA REQUÊTE : une recherche n'attend pas le socle. Le pays
	// voyage avec (le socle ne s'en sert pas pour un compte, mais le journal
	// oui).
	code := country.FromContext(ctx)
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if code != "" {
			cctx = country.WithCountry(cctx, code, country.SourceHeader)
		}
		if err := r.post(cctx, uid); err != nil {
			// Raté : on redemandera au prochain appel de ce client.
			r.seen.Delete(uid)
			r.logger.Warn("interest: not reported", "vertical", r.vertical, "error", err)
		}
	}()
}

func (r *Reporter) send(ctx context.Context, userID string) error {
	body, _ := json.Marshal(map[string]string{"user_id": userID, "interest": r.vertical})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+Path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.token)
	if code := country.FromContext(ctx); code != "" {
		req.Header.Set(country.Header, code)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &statusError{resp.StatusCode}
	}
	return nil
}

type statusError struct{ code int }

func (e *statusError) Error() string { return "interest: core answered " + http.StatusText(e.code) }
