package diramaps

// LE MODE DE DÉPLACEMENT ARRIVE-T-IL JUSQU'AU SIG ?
//
// ⚠️ CE FICHIER EXISTE PARCE QUE LA RÉPONSE ÉTAIT NON. Le SIG tient un graphe
// par profil — voiture, moto, tricycle — et sait choisir l'instance selon le
// mode reçu. Ce client écrivait `"driving"` en dur : toute la chaîne était en
// place sauf sa première ligne, et aucune épreuve ne regardait ce qui PARTAIT.
//
// Une moto routée sur le graphe voiture se voit annoncer un délai qu'elle bat
// et facturer une distance qu'elle n'a pas parcourue. Dans les deux sens,
// c'est de l'argent — et rien ne le signalait.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sigEcho rend un SIG qui NOTE ce qu'on lui demande.
func sigEcho(t *testing.T, vu *map[string]any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corps, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(corps, vu))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"routes":[{"duration_s":300,"geometry":{"type":"LineString","coordinates":[[1.2255,6.1319],[1.28,6.17]]}}]}`))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/api", "")
}

var deuxPoints = [][2]float64{{1.2255, 6.1319}, {1.2800, 6.1700}}

func TestLeModeArriveJusquAuSIG(t *testing.T) {
	for _, mode := range []string{ModeMotorcycle, ModeTricycle, ModeWalking, ModeCycling} {
		t.Run(mode, func(t *testing.T) {
			var vu map[string]any
			c := sigEcho(t, &vu)
			_, err := c.RouteAs(context.Background(), "lome", deuxPoints, mode)
			require.NoError(t, err)
			assert.Equal(t, mode, vu["mode"],
				"le SIG doit recevoir le mode demandé, pas « voiture »")
		})
	}
}

// ⚠️ LES TREIZE APPELANTS EXISTANTS NE CHANGENT PAS DE COMPORTEMENT. `Route`
// reste la voiture : un devis d'adresse à adresse n'a pas de véhicule, et le
// faire basculer en silence aurait changé des prix sans que personne ne l'ait
// demandé.
func TestRouteSansModeResteEnVoiture(t *testing.T) {
	var vu map[string]any
	c := sigEcho(t, &vu)
	_, err := c.Route(context.Background(), "lome", deuxPoints)
	require.NoError(t, err)
	assert.Equal(t, ModeDriving, vu["mode"])
}

// ⚠️ UN MODE VIDE N'EST PAS UNE ERREUR : il vaut voiture. Un appelant qui lit
// le véhicule en base et le trouve non renseigné doit obtenir un devis, pas un
// refus.
func TestUnModeVideVautVoiture(t *testing.T) {
	var vu map[string]any
	c := sigEcho(t, &vu)
	_, err := c.RouteAs(context.Background(), "lome", deuxPoints, "")
	require.NoError(t, err)
	assert.Equal(t, ModeDriving, vu["mode"])
}

// ⚠️ ET UN MODE INCONNU PASSE QUAND MÊME. Le SIG retombe sur la voiture et
// répond ; refuser ici ferait échouer un devis parce qu'une verticale a ajouté
// une classe de véhicule — une course perdue pour un mot.
func TestUnModeInconnuNEstPasUnRefus(t *testing.T) {
	var vu map[string]any
	c := sigEcho(t, &vu)
	r, err := c.RouteAs(context.Background(), "lome", deuxPoints, "hovercraft")
	require.NoError(t, err)
	assert.Equal(t, "hovercraft", vu["mode"])
	assert.Equal(t, 300, r.DurationS, "le SIG a répondu, c'est tout ce qui compte ici")
}

// La durée par mode passe par le même chemin.
func TestLaDureeSuitLeMode(t *testing.T) {
	var vu map[string]any
	c := sigEcho(t, &vu)
	s, err := c.RouteDurationAs(context.Background(), "lome", deuxPoints, ModeTricycle)
	require.NoError(t, err)
	assert.Equal(t, 300, s)
	assert.Equal(t, ModeTricycle, vu["mode"])
}
