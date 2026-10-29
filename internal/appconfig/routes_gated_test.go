package appconfig_test

// CE QUE LES ROUTES DU SOCLE REFUSENT VRAIMENT — éprouvé SUR LES ROUTES
// MONTÉES, et pas sur l'intergiciel seul.
//
// ⚠️⚠️ C'EST TOUTE LA RAISON DE CE FICHIER. `appconfig.Service` savait refuser
// depuis le premier jour ; personne ne l'appelait. `/app/config` annonçait
// `wallet: false` à Dira Lite pendant que `GET /wallet` rendait le solde — vu
// en direct sur la préprod (#251). Un garde parfaitement éprouvé et branché
// nulle part ne garde rien, et une épreuve de l'intergiciel seul ne l'aurait
// jamais montré : c'est le MONTAGE qui manquait.
//
// ⚠️ ET ON ÉPROUVE AUSSI CE QUI NE DOIT PAS ÊTRE REFUSÉ. Poser le garde sur le
// groupe entier aurait coupé l'administration : un exploitant qui recharge un
// portefeuille depuis la console n'est pas une application réduite, et lui
// refuser la route couperait l'outil qui sert à dépanner la personne.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kgtech-org/dira-core-api/internal/challenge"
	"github.com/kgtech-org/dira-core-api/internal/promocode"
	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/pkg/appconfig"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/stretchr/testify/assert"
)

// porte refuse tout, et NOTE ce qu'on lui a demandé.
//
// ⚠️ ELLE REFUSE TOUT EXPRÈS : c'est ce qui rend l'épreuve lisible. Une route
// gardée répond `403` ; une route qui ne l'est pas va jusqu'à la poignée —
// laquelle, sans service, PANIQUE, et le `500` qui en sort est précisément la
// preuve qu'elle est passée devant la porte sans qu'on la lui ouvre.
type porte struct{ demandé *[]string }

func (p porte) Require(_ context.Context, feature string) error {
	*p.demandé = append(*p.demandé, feature)
	return appconfig.Refuse(feature)
}

// routeur monte un handler avec une authentification de complaisance et un
// filet qui change une panique en `500`.
func routeur(t *testing.T, role string, mount func(chi.Router, func(http.Handler) http.Handler)) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, req)
		})
	})
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), auth.Claims{
				UserID: "6ab253499cb508f24710c036", Role: role, Type: "access",
				Scopes: []string{auth.ScopeCore}, Country: "GA",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
	mount(r, authMW)
	return r
}

func appelle(t *testing.T, r chi.Router, method, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	var body struct {
		Error map[string]any `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body.Error
}

// ⚠️⚠️ LE PORTEFEUILLE SE REFUSE À L'ÉDITION QUI NE L'A PLUS.
//
// Mesuré AVANT le correctif, en direct : `/app/config` annonçait
// `wallet: false` et `GET /wallet` rendait `200` avec le solde complet.
func TestTheWalletRoutesRefuseACutEdition(t *testing.T) {
	demandé := []string{}
	r := routeur(t, auth.RoleClient, func(r chi.Router, authMW func(http.Handler) http.Handler) {
		token.NewHandler(nil).Mount(r, authMW, porte{&demandé})
	})

	for _, path := range []string{"/wallet", "/wallet/transactions"} {
		code, errBody := appelle(t, r, http.MethodGet, path)
		assert.Equal(t, http.StatusForbidden, code, "%s a servi le solde", path)
		assert.Equal(t, "feature_unavailable_in_edition", errBody["code"], "%s", path)
		// ⚠️ ET LE REFUS DIT QUOI CACHER — sinon l'application reproposera le
		// menu au prochain démarrage (#252).
		assert.Equal(t, appconfig.FeatureWallet, errBody["feature"], "%s", path)
	}
	assert.Equal(t, []string{appconfig.FeatureWallet, appconfig.FeatureWallet}, demandé)
}

// ⚠️⚠️ MAIS L'ADMINISTRATION DES PORTEFEUILLES N'EST PAS REFUSÉE. Un exploitant
// qui recharge un portefeuille depuis la console n'est pas une application
// réduite : poser le garde sur le groupe entier aurait coupé l'outil qui sert
// justement à dépanner la personne à qui on a retiré son portefeuille.
func TestTheWalletBackOfficeIsNeverRefused(t *testing.T) {
	demandé := []string{}
	r := routeur(t, auth.RoleAdmin, func(r chi.Router, authMW func(http.Handler) http.Handler) {
		token.NewHandler(nil).Mount(r, authMW, porte{&demandé})
	})

	code, errBody := appelle(t, r, http.MethodGet, "/admin/wallets/6ab253499cb508f24710c036")
	assert.NotEqual(t, http.StatusForbidden, code,
		"la console ne peut plus voir un portefeuille : le garde couvre l'administration")
	assert.NotEqual(t, "feature_unavailable_in_edition", errBody["code"])
	assert.Empty(t, demandé, "la porte a été interrogée pour une route d'administration")
}

// Les objectifs : le même garde, sur l'écran de la personne seulement.
func TestTheChallengesScreenRefusesACutEdition(t *testing.T) {
	demandé := []string{}
	r := routeur(t, auth.RoleDriver, func(r chi.Router, authMW func(http.Handler) http.Handler) {
		challenge.NewHandler(nil).Mount(r, authMW, porte{&demandé})
	})

	code, errBody := appelle(t, r, http.MethodGet, "/me/challenges")
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, appconfig.FeatureChallenges, errBody["feature"])
	assert.Equal(t, []string{appconfig.FeatureChallenges}, demandé)
}

func TestTheChallengesBackOfficeIsNeverRefused(t *testing.T) {
	demandé := []string{}
	r := routeur(t, auth.RoleAdmin, func(r chi.Router, authMW func(http.Handler) http.Handler) {
		challenge.NewHandler(nil).Mount(r, authMW, porte{&demandé})
	})
	code, _ := appelle(t, r, http.MethodGet, "/admin/challenges")
	assert.NotEqual(t, http.StatusForbidden, code)
	assert.Empty(t, demandé, "la porte a été interrogée pour une route d'administration")
}

// Le parrainage : le code de la personne, pas la gestion des codes.
func TestTheReferralCodeRefusesACutEdition(t *testing.T) {
	demandé := []string{}
	r := routeur(t, auth.RoleClient, func(r chi.Router, authMW func(http.Handler) http.Handler) {
		promocode.NewHandler(nil).Mount(r, authMW, porte{&demandé})
	})

	code, errBody := appelle(t, r, http.MethodGet, "/me/referral")
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, appconfig.FeaturePromoCodes, errBody["feature"])
	assert.Equal(t, []string{appconfig.FeaturePromoCodes}, demandé)
}

func TestThePromoBackOfficeIsNeverRefused(t *testing.T) {
	demandé := []string{}
	r := routeur(t, auth.RoleAdmin, func(r chi.Router, authMW func(http.Handler) http.Handler) {
		promocode.NewHandler(nil).Mount(r, authMW, porte{&demandé})
	})
	code, _ := appelle(t, r, http.MethodGet, "/admin/promo-codes")
	assert.NotEqual(t, http.StatusForbidden, code)
	assert.Empty(t, demandé, "la porte a été interrogée pour une route d'administration")
}

// ⚠️⚠️ ET LA PORTE NULLE EST ÉPROUVÉE LÀ OÙ ELLE SE DÉCIDE — dans
// `pkg/middleware`, où un `204` prouve que la requête est ALLÉE AU BOUT.
//
// Elle l'était ici, et c'était un trou : avec un service nul, une route qui
// passe la porte PANIQUE, le filet change ça en `500`, et un `500` n'est pas
// un `403` — si bien que « la porte nulle panique à chaque requête » passait
// AU VERT. Une épreuve qui se contente de « ce n'est pas un refus » accepte
// une panne comme un succès. Voir `TestANilGateLetsEverythingThrough`.
