package appconfig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/edition"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liteIn(code string) context.Context {
	return edition.WithEdition(
		country.WithCountry(context.Background(), code, country.SourceHeader),
		edition.Lite, "", "")
}

func fullIn(code string) context.Context {
	return edition.WithEdition(
		country.WithCountry(context.Background(), code, country.SourceHeader),
		edition.Full, "", "")
}

// registryServer sert un registre, et compte les lectures.
func serveurDeRegistre(t *testing.T, body string, status int) (*Client, *int32) {
	t.Helper()
	var reads int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reads, 1)
		assert.Equal(t, "/api/v1/internal/app-config", r.URL.Path)
		assert.Equal(t, "Bearer jeton", r.Header.Get("Authorization"),
			"le registre est derrière le jeton de service")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "jeton"), &reads
}

// ⚠️⚠️ ON LIT LE REGISTRE BRUT, PAS LA VUE RÉSOLUE, et c'est la décision qui
// évite un appel HTTP par requête. `GET /app/config` rendrait « ce que voit
// Lite au Gabon » — une réponse juste pour UN pays et UNE édition. Or une verticale
// sert les deux éditions et quatorze pays : il faudrait interroger le socle
// à chaque appel, sur le chemin d'un passager qui attend devant son écran.
func TestTheRegistryIsReadOnceAndResolvedForEveryCall(t *testing.T) {
	c, reads := serveurDeRegistre(t, `{"features":{"rental":{"editions":["lite"],"countries":["GA"]}}}`, 200)

	// Une seule lecture, et quatre réponses différentes.
	assert.False(t, c.Allows(liteIn("GA"), FeatureRental), "la coupure demandée")
	assert.True(t, c.Allows(fullIn("GA"), FeatureRental), "l'app normale au Gabon")
	assert.True(t, c.Allows(liteIn("TG"), FeatureRental), "Lite au Togo")
	assert.True(t, c.Allows(fullIn("TG"), FeatureRental), "l'app normale au Togo")

	assert.Equal(t, int32(1), atomic.LoadInt32(reads),
		"un appel au socle par requête : le cache ne sert à rien")
}

// ⚠️⚠️ UN SOCLE MUET REND LA PLATEFORME ENTIÈRE, PAS UNE PLATEFORME AMPUTÉE.
// `pkg/appconfig` est écrit pour qu'un registre nil laisse TOUT passer : une
// coupure de réseau ne doit pas vider le choix des véhicules d'un pays.
func TestAnUnreachableCoreTakesNothingAway(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "jeton") // personne n'écoute
	for _, f := range []string{FeatureRental, FeaturePool, FeatureTripShare} {
		assert.True(t, c.Allows(liteIn("GA"), f), "fonction %s", f)
	}
	served := []string{"moto", "eco", "van"}
	assert.Equal(t, served, c.AllowedIn(liteIn("GA"), ListVehicleModes, served))
	assert.NoError(t, c.Require(liteIn("GA"), FeatureRental))
}

// Et un socle NON CONFIGURÉ non plus : un service de développement sans socle
// ne doit pas servir une application amputée.
func TestAnUnconfiguredCoreTakesNothingAway(t *testing.T) {
	c := NewClient("", "")
	assert.Nil(t, c.Registry(liteIn("GA")))
	assert.True(t, c.Allows(liteIn("GA"), FeatureRental))
}

// ⚠️⚠️ ET IL NE TENTE MÊME PAS L'APPEL. Sans le garde `Enabled()`, chaque
// course d'un service de développement partirait sur une adresse vide et
// attendrait l'échec — cinq secondes de délai par appel, pour une réponse
// qu'on sait d'avance. Le comportement final est le même (rien n'est coupé),
// si bien qu'une mutation retirant le garde passait au vert : seule la trace
// des appels le montre.
func TestAnUnconfiguredCoreDoesNotEvenTry(t *testing.T) {
	c := NewClient("", "")
	var tries int32
	c.http = &http.Client{Transport: countingTransport{n: &tries}}

	for i := 0; i < 3; i++ {
		_ = c.Allows(liteIn("GA"), FeatureRental)
	}
	assert.Equal(t, int32(0), atomic.LoadInt32(&tries),
		"un socle non configuré part quand même sur le réseau, et attend l'échec")
}

type countingTransport struct{ n *int32 }

func (c countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(c.n, 1)
	return nil, errNoNetworkInTest
}

// ⚠️⚠️ MAIS LE DERNIER REGISTRE CONNU PASSE AVANT NIL. Si on a déjà lu les
// règles une fois, une panne passagère ne doit pas ROUVRIR ce que
// l'exploitation vient de couper : elle le rouvrirait silencieusement, pour la
// durée de la panne, et plus rien ne le signalerait.
func TestAFailedReadKeepsTheLastKnownRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"features":{"raffle":{"editions":["lite"]}}}`))
	}))
	c := NewClient(srv.URL, "jeton")
	require.False(t, c.Allows(liteIn("GA"), FeatureRaffle))

	// ⚠️ LE SOCLE DISPARAÎT POUR DE BON — pas un `500`, une adresse qui ne
	// répond plus. C'est le chemin de l'erreur RÉSEAU, distinct du refus HTTP,
	// et une mutation l'a montré en passant au vert : l'épreuve d'avant
	// n'exerçait que le second.
	srv.Close()
	c.mu.Lock()
	c.readAt = time.Now().Add(-2 * clientTTL)
	c.mu.Unlock()

	assert.False(t, c.Allows(liteIn("GA"), FeatureRaffle),
		"la tombola s'est rouverte parce que le socle ne répond plus")
}

// ⚠️⚠️ ET UN `500` AVEC UN CORPS LISIBLE NE DOIT PAS ÊTRE LU COMME UN
// REGISTRE. Un socle en panne peut très bien répondre `{}` derrière un `500`
// — une passerelle, une page d'erreur en JSON, un service qui démarre. Sans la
// vérification du STATUT, ce `{}` serait décodé, mis en cache, et rouvrirait
// tout ce qu'on venait de couper pendant une minute. Une mutation qui retirait
// cette vérification passait au vert, parce que l'épreuve d'avant renvoyait un
// corps VIDE que le décodeur refusait de lui-même.
func TestAnErrorStatusIsNeverReadAsARegistry(t *testing.T) {
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{}`)) // un corps parfaitement lisible
			return
		}
		_, _ = w.Write([]byte(`{"features":{"raffle":{"editions":["lite"]}}}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "jeton")
	require.False(t, c.Allows(liteIn("GA"), FeatureRaffle))

	down.Store(true)
	c.mu.Lock()
	c.readAt = time.Now().Add(-2 * clientTTL)
	c.mu.Unlock()

	assert.False(t, c.Allows(liteIn("GA"), FeatureRaffle),
		"un `{}` derrière un `500` a été pris pour le registre")
}

// Une réponse illisible est traitée comme une panne, pas comme un registre
// vide : un JSON tronqué ne doit pas rouvrir ce qu'on vient de couper.
func TestAnUnreadableAnswerIsTreatedAsAFailure(t *testing.T) {
	var broken atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if broken.Load() {
			_, _ = w.Write([]byte(`{"features":{"raffle"`))
			return
		}
		_, _ = w.Write([]byte(`{"features":{"raffle":{"editions":["lite"]}}}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "jeton")
	require.False(t, c.Allows(liteIn("GA"), FeatureRaffle))

	broken.Store(true)
	c.mu.Lock()
	c.readAt = time.Now().Add(-2 * clientTTL)
	c.mu.Unlock()
	assert.False(t, c.Allows(liteIn("GA"), FeatureRaffle),
		"un JSON tronqué a rouvert la tombola")
}

// Un registre VIDE, lui, ne retire rien — et c'est le défaut d'un déploiement
// neuf. Il faut le distinguer d'une panne : l'un est normal, l'autre non.
func TestAnEmptyRegistryFromTheBridgeTakesNothingAway(t *testing.T) {
	c, _ := serveurDeRegistre(t, `{}`, 200)
	assert.True(t, c.Allows(liteIn("GA"), FeatureRental))
	served := []string{"moto", "eco"}
	assert.Equal(t, served, c.AllowedIn(liteIn("GA"), ListVehicleModes, served))
}

// Le croisement d'une liste garde NOTRE ordre et ne vide jamais la liste —
// les deux garde-fous vivent dans `pkg/appconfig`, et ce pont ne doit pas les
// contourner.
func TestTheListCrossingKeepsOurOrderAndNeverEmpties(t *testing.T) {
	c, _ := serveurDeRegistre(t,
		`{"lists":{"vehicle_modes":{"editions":["lite"],"allow":["pool","free"]}}}`, 200)
	served := []string{"free", "rental", "pool"}

	assert.Equal(t, []string{"free", "pool"}, c.AllowedIn(liteIn("GA"), ListVehicleModes, served),
		"l'ordre de la liste blanche a remplacé le nôtre")
	assert.Equal(t, served, c.AllowedIn(fullIn("GA"), ListVehicleModes, served),
		"l'application normale a perdu des modes")
}

// ⚠️⚠️ ET LE PAYS DE L'APPEL COMPTE AUTANT QUE L'ÉDITION, dans les listes
// comme dans les fonctions. Une mutation qui passait `""` en guise de pays
// passait au vert, parce que l'épreuve d'au-dessus emploie une règle SANS
// pays : le symptôme réel aurait été une liste gabonaise appliquée au Togo.
func TestTheCountryCountsInTheListCrossing(t *testing.T) {
	c, _ := serveurDeRegistre(t,
		`{"lists":{"vehicle_modes":{"editions":["lite"],"countries":["GA"],"allow":["free"]}}}`, 200)
	served := []string{"free", "rental", "pool"}

	assert.Equal(t, []string{"free"}, c.AllowedIn(liteIn("GA"), ListVehicleModes, served))
	assert.Equal(t, served, c.AllowedIn(liteIn("TG"), ListVehicleModes, served),
		"la liste blanche gabonaise s'est appliquée au Togo")
}

// ⚠️ ET LE REFUS EST CELUI DU PAQUET PARTAGÉ, pas une copie locale : trois
// services le lèvent, et le code est ce que les applications lisent pour
// cacher un bouton.
func TestTheRefusalComesFromTheSharedPackage(t *testing.T) {
	c, _ := serveurDeRegistre(t, `{"features":{"trip_share":{"editions":["lite"]}}}`, 200)

	require.NoError(t, c.Require(fullIn("GA"), FeatureTripShare))
	err := c.Require(liteIn("GA"), FeatureTripShare)
	require.Error(t, err)

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "feature_unavailable_in_edition", appErr.Code,
		"les applications cachent leurs boutons sur ce code précis")
	assert.Equal(t, 403, appErr.HTTPStatus)
	assert.Equal(t, FeatureTripShare, appErr.Meta["feature"],
		"sans la fonction nommée, l'écran ne sait pas laquelle cacher")
}

// errNoNetworkInTest : l'épreuve ne va pas sur le réseau.
var errNoNetworkInTest = errNoNetwork{}

type errNoNetwork struct{}

func (errNoNetwork) Error() string { return "pas de réseau dans cette épreuve" }
