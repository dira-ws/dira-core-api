package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kgtech-org/dira-core-api/pkg/edition"
)

func seenEdition(t *testing.T, r *http.Request) (string, *httptest.ResponseRecorder) {
	t.Helper()
	var got string
	rec := httptest.NewRecorder()
	Edition()(http.HandlerFunc(func(_ http.ResponseWriter, rr *http.Request) {
		got = edition.FromContext(rr.Context())
	})).ServeHTTP(rec, r)
	return got, rec
}

// ⚠️ L'EN-TÊTE EST LU, ET REPUBLIÉ DANS LA RÉPONSE.
//
// La republication n'est pas décorative : c'est ce qui permet à une
// application de voir ce que le serveur a RETENU. Une application qui se croit
// Lite et reçoit `full` sait immédiatement que son en-tête n'arrive pas — une
// passerelle qui le filtre, un cache qui le mange — au lieu de chercher deux
// jours pourquoi sa configuration n'est pas réduite.
func TestTheEditionIsReadAndEchoedBack(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set(edition.Header, "lite")
	got, rec := seenEdition(t, r)
	assert.Equal(t, edition.Lite, got)
	assert.Equal(t, edition.Lite, rec.Header().Get(edition.Header),
		"l'application ne peut pas vérifier que son en-tête est arrivé")
}

// ⚠️⚠️ RIEN N'EST JAMAIS REFUSÉ, et le parc installé continue de marcher.
//
// Les applications déjà sur les téléphones n'envoient pas cet en-tête. Un
// refus — ou même un `400` — les aurait toutes cassées le jour du déploiement,
// pour une information qui ne sert qu'à adapter une liste.
func TestNoRequestIsEverRefused(t *testing.T) {
	for _, raw := range []string{"", "lite", "full", "léger", "LITE", "n'importe quoi"} {
		r := httptest.NewRequest("GET", "/x", nil)
		if raw != "" {
			r.Header.Set(edition.Header, raw)
		}
		got, rec := seenEdition(t, r)
		assert.Equal(t, http.StatusOK, rec.Code, "« %s » a été refusé", raw)
		if raw == "lite" || raw == "LITE" {
			assert.Equal(t, edition.Lite, got, "« %s »", raw)
		} else {
			assert.Equal(t, edition.Full, got, "« %s » doit retomber sur l'édition complète", raw)
		}
	}
}

// ⚠️ LE PARAMÈTRE D'URL EST LE SECOND RECOURS : un navigateur ne pose pas
// d'en-tête sur l'ouverture d'un WebSocket, et c'est précisément là que le
// suivi en direct en a besoin.
func TestTheQueryParamServesTheWebSocket(t *testing.T) {
	r := httptest.NewRequest("GET", "/track/subscribe?edition=lite", nil)
	got, _ := seenEdition(t, r)
	assert.Equal(t, edition.Lite, got)

	// ⚠️ ET L'EN-TÊTE L'EMPORTE SUR L'URL quand les deux sont là : l'en-tête
	// vient de l'application, l'URL peut traîner dans un lien partagé.
	r = httptest.NewRequest("GET", "/x?edition=lite", nil)
	r.Header.Set(edition.Header, "full")
	got, _ = seenEdition(t, r)
	assert.Equal(t, edition.Full, got)
}

// La version et la plateforme traversent aussi l'intergiciel.
func TestTheVersionAndPlatformReachTheHandler(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set(edition.Header, "lite")
	r.Header.Set(edition.VersionHeader, "0.1.0 (26102901)")
	r.Header.Set(edition.PlatformHeader, "android")
	var version, platform string
	Edition()(http.HandlerFunc(func(_ http.ResponseWriter, rr *http.Request) {
		version, platform = edition.AppFromContext(rr.Context())
	})).ServeHTTP(httptest.NewRecorder(), r)
	assert.Equal(t, "0.1.0 (26102901)", version)
	assert.Equal(t, "android", platform)
}
