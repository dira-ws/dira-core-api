package appconfig

// LES PAYS DU COLIS, PAR LA VRAIE ROUTE ET SUR UNE VRAIE BASE.
//
// ⚠️⚠️ CE QU'UNE DOUBLURE NE PEUT PAS MONTRER : le décodeur strict de
// l'écriture, le `$set` réel qui REMPLACE, et la lecture qui suit. Le colis ne
// s'ouvre que par ce document : si le `$set` l'oubliait, la console afficherait
// le pays ouvert — elle relit sa propre requête — et le colis resterait fermé.
//
// Lancée seulement si `DIRA_TEST_MONGO_URI` est posé, sur une base jetable.

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

func TestAnOpenedCountryIsStoredServedAndKeptOnARealDatabase(t *testing.T) {
	h, col := routesEtBase(t)

	// Avant toute écriture : fermé partout.
	code, cfg := appelle(t, h, http.MethodGet, "/app/config", "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, cfg["features"].(map[string]any)["parcel"], "un déploiement neuf ne vend rien")

	// La console ouvre le Togo.
	code, out := appelle(t, h, http.MethodPut, "/admin/app-config", `{"parcel":["tg"]}`)
	require.Equal(t, http.StatusOK, code, "%v", out)
	assert.Equal(t, []any{"TG"}, out["rules"].(map[string]any)["parcel"])

	var doc bson.M
	require.NoError(t, col.FindOne(context.Background(), bson.M{"_id": docID}).Decode(&doc))
	assert.Equal(t, bson.A{"TG"}, doc["parcel"], "le pays est réellement écrit")

	// ⚠️ LE REGISTRE BRUT, POUR LES VERTICALES, LE PORTE.
	code, raw := appelle(t, h, http.MethodGet, "/internal/app-config", "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []any{"TG"}, raw["parcel"])

	// Et ce que voit une application, pays par pays.
	for cc, want := range map[string]bool{"TG": true, "GA": false} {
		code, view := appelle(t, h, http.MethodGet, "/admin/app-config/preview?country="+cc+"&edition=full", "")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, want, view["features"].(map[string]any)["parcel"], "pays %s", cc)
	}

	// ⚠️ UNE CONSOLE QUI RENVOIE TOUT CE QU'ELLE A LU le garde : c'est en étalant
	// le registre, pas en le recopiant champ par champ, qu'on ne ferme rien.
	code, out = appelle(t, h, http.MethodPut, "/admin/app-config",
		`{"parcel":["TG"],"features":{"pool":{"editions":["lite"]}}}`)
	require.Equal(t, http.StatusOK, code, "%v", out)
	assert.Equal(t, []any{"TG"}, out["rules"].(map[string]any)["parcel"])

	// Une faute de frappe se refuse, et nomme le champ.
	code, out = appelle(t, h, http.MethodPut, "/admin/app-config", `{"parcel":["Togo"]}`)
	require.Equal(t, http.StatusUnprocessableEntity, code, "%v", out)
	assert.Equal(t, []any{"parcel"}, out["error"].(map[string]any)["fields"])
	require.NoError(t, col.FindOne(context.Background(), bson.M{"_id": docID}).Decode(&doc))
	assert.Equal(t, bson.A{"TG"}, doc["parcel"], "un refus n'écrit rien")
}
