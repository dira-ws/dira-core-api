package appconfig

// UN REGISTRE ÉCRIT AVANT, LU ET RÉÉCRIT APRÈS — sur une vraie base.
//
// ⚠️⚠️ CE QU'UNE DOUBLURE NE PEUT PAS MONTRER : le document que la préprod
// porte déjà. Il a été écrit par une version d'avant, avec la liste de pays
// d'une fonction retirée (`immo`) et sa règle d'édition (`features.immo`). Le
// décodage, le tri de ce qui se sert, le décodeur strict de l'écriture et le
// `$set` réel décident ensemble de ce qui arrive : c'est par la vraie route et
// sur une vraie base que cela se vérifie.

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
)

func TestARegistryWrittenBeforeTheRetirementStillServesAndSaves(t *testing.T) {
	h, col := routesEtBase(t)
	_, err := col.InsertOne(context.Background(), bson.M{
		"_id":  docID,
		"immo": bson.A{"GA", "TD", "TG"},
		"features": bson.M{
			"immo": bson.M{"editions": bson.A{"lite"}},
			"pool": bson.M{"editions": bson.A{"lite"}},
		},
		"live": bson.M{"TD": bson.M{"max_session_seconds": 300}},
	})
	require.NoError(t, err)

	// 1. LA CONSOLE LIT : la règle de la fonction retirée n'est pas servie —
	// servie, elle reviendrait à l'écriture.
	code, view := appelle(t, h, http.MethodGet, "/admin/app-config", "")
	require.Equal(t, http.StatusOK, code, "%v", view)
	rules := view["rules"].(map[string]any)
	assert.NotContains(t, rules, "immo", "la liste de pays retirée ne se sert plus")
	features := rules["features"].(map[string]any)
	assert.NotContains(t, features, "immo", "la règle retirée ne se sert plus")
	assert.Contains(t, features, "pool", "le reste du registre est intact")
	assert.Contains(t, rules["live"], "TD")
	for _, f := range view["features"].([]any) {
		assert.NotEqual(t, "immo", f.(map[string]any)["key"], "le catalogue ne la propose plus")
	}

	// 2. UNE APPLICATION LIT : `features.immo` a disparu de `/app/config`.
	code, cfg := appelle(t, h, http.MethodGet, "/app/config", "")
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, cfg["features"], "immo")

	// 3. UNE CONSOLE D'AVANT RENVOIE TOUT CE QU'ELLE AVAIT LU : accepté.
	old := `{"immo":["GA","TD","TG"],
		"features":{"immo":{"editions":["lite"]},"pool":{"editions":["lite"]}},
		"live":{"TD":{"max_session_seconds":300}}}`
	code, out := appelle(t, h, http.MethodPut, "/admin/app-config", old)
	require.Equal(t, http.StatusOK, code, "une console d'avant ne doit pas être bloquée : %v", out)
	saved := out["rules"].(map[string]any)
	assert.NotContains(t, saved, "immo")
	assert.NotContains(t, saved["features"], "immo")
	assert.Contains(t, saved["features"], "pool")

	var doc bson.M
	require.NoError(t, col.FindOne(context.Background(), bson.M{"_id": docID}).Decode(&doc))
	assert.NotContains(t, doc["features"], "immo", "la règle retirée n'est pas réécrite")

	// 4. Une VRAIE faute de frappe reste refusée, et nommée.
	code, out = appelle(t, h, http.MethodPut, "/admin/app-config",
		`{"features":{"scheduled_ride":{"editions":["lite"]}}}`)
	require.Equal(t, http.StatusUnprocessableEntity, code, "%v", out)
	code, _ = appelle(t, h, http.MethodPut, "/admin/app-config", `{"immobilier":["GA"]}`)
	assert.Equal(t, http.StatusUnprocessableEntity, code, "seul le champ retiré est toléré, pas tout champ inconnu")
}
