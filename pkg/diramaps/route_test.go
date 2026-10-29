package diramaps

// LE TRACÉ D'UNE TOURNÉE, ET CE QU'IL N'EST PAS (core#230).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sig(t *testing.T, corps string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/calc/route", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(corps))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

var tournée = [][2]float64{{1.22, 6.13}, {1.25, 6.16}}

func TestARouteCarriesItsDurationAndItsGeometry(t *testing.T) {
	c := sig(t, `{"routes":[{"duration_s":612.4,"distance_m":4210,
	  "geometry":{"type":"LineString","coordinates":[[1.22,6.13],[1.23,6.14],[1.25,6.16]]}}]}`)

	r, err := c.Route(context.Background(), "lome", tournée)
	require.NoError(t, err)
	assert.Equal(t, 612, r.DurationS, "arrondi à la seconde")
	require.Len(t, r.Geometry, 3)
	assert.Equal(t, [2]float64{1.23, 6.14}, r.Geometry[1], "⚠️ l'ordre reste [lng, lat]")
	assert.NotEmpty(t, r.Polyline())

	// L'aller-retour par l'encodeur retrouve le tracé.
	retour := DecodePolyline(r.Polyline())
	require.Len(t, retour, 3)
	assert.InDelta(t, 1.23, retour[1][0], 1e-5)
	assert.InDelta(t, 6.14, retour[1][1], 1e-5)
}

// ⚠️⚠️ UNE RÉPONSE SANS GÉOMÉTRIE NE DOIT PAS EN INVENTER UNE. Le SIG rend
// aussi des réponses consolidées par Google, qui n'ont pas de géométrie du
// tout. Et surtout : pas de repli sur la ligne droite entre les arrêts — une
// ligne droite présentée comme un itinéraire est exactement le défaut de #230.
func TestAnAnswerWithoutGeometryInventsNothing(t *testing.T) {
	for _, corps := range []string{
		`{"routes":[{"duration_s":600}]}`,
		`{"routes":[{"duration_s":600,"geometry":null}]}`,
		`{"routes":[{"duration_s":600,"geometry":{"type":"Point","coordinates":[[1.22,6.13]]}}]}`,
		`{"routes":[{"duration_s":600,"geometry":{"type":"LineString","coordinates":[[1.22,6.13]]}}]}`,
	} {
		r, err := sig(t, corps).Route(context.Background(), "lome", tournée)
		require.NoError(t, err, corps)
		assert.Equal(t, 600, r.DurationS, "la durée arrive quand même : %s", corps)
		assert.Empty(t, r.Geometry, "aucun tracé ne doit sortir de : %s", corps)
		assert.Empty(t, r.Polyline(), "et rien à encoder : %s", corps)
	}
}

// ⚠️ UN TRACÉ D'UN SEUL POINT N'EST PAS UN TRACÉ : `Polyline()` rend "" plutôt
// qu'une chaîne qu'un lecteur dessinerait comme un segment de longueur nulle.
func TestASinglePointIsNotAPath(t *testing.T) {
	assert.Empty(t, Route{Geometry: [][2]float64{{1.22, 6.13}}}.Polyline())
	assert.Empty(t, Route{}.Polyline())
}

// Un SIG absent ou une tournée d'un point : rien, et pas d'erreur. La course
// doit se créer sans tracé plutôt que pas du tout.
func TestNoSIGMeansNoPathAndNoError(t *testing.T) {
	r, err := New("").Route(context.Background(), "lome", tournée)
	require.NoError(t, err)
	assert.Zero(t, r.DurationS)
	assert.Empty(t, r.Geometry)

	r, err = sig(t, `{"routes":[]}`).Route(context.Background(), "lome", [][2]float64{{1.22, 6.13}})
	require.NoError(t, err)
	assert.Empty(t, r.Geometry)
}

// ⚠️ `RouteDuration` reste le même contrat pour qui n'a pas besoin du tracé :
// les appelants existants ne doivent rien changer.
func TestRouteDurationStillAnswersAlone(t *testing.T) {
	d, err := sig(t, `{"routes":[{"duration_s":42.6,
	  "geometry":{"type":"LineString","coordinates":[[1.22,6.13],[1.25,6.16]]}}]}`).
		RouteDuration(context.Background(), "lome", tournée)
	require.NoError(t, err)
	assert.Equal(t, 43, d)
}
