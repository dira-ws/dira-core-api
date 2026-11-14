package country

// LA LECTURE QUI COMPTE, SUR UNE VRAIE BASE.
//
// ⚠️ UN FILTRE MONGO NE S'ÉPROUVE PAS CONTRE UNE DOUBLURE : la doublure dit ce
// que son auteur croit que la base fait. Ce que celle-ci doit montrer : qu'un
// document SANS le champ — tous ceux d'avant, et tous les vrais — compte, et
// que seul l'essai marqué est écarté.
//
// Lancée seulement si `DIRA_TEST_MONGO_URI` est posé, sur une base jetable.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

func TestACountingReadSkipsOnlyTheMarkedTrialsOnARealDatabase(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_country_counting_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })
	col := m.Collection("rows")

	_, err = col.InsertMany(ctx, []any{
		bson.M{"_id": "avant", Field: "TG"},                        // un document d'avant le marqueur
		bson.M{"_id": "reel", Field: "TG", TestingField: false},    // écrit faux, explicitement
		bson.M{"_id": "essai", Field: "TG", TestingField: true},    // l'essai
		bson.M{"_id": "ailleurs", Field: "SN", TestingField: true}, // un essai d'un autre pays
	})
	require.NoError(t, err)

	ids := func(read context.Context) []string {
		t.Helper()
		cur, err := col.Find(ctx, Restrict(read, nil))
		require.NoError(t, err)
		var rows []struct {
			ID string `bson:"_id"`
		}
		require.NoError(t, cur.All(ctx, &rows))
		out := []string{}
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}

	togo := WithCountry(context.Background(), "TG", SourceClaims)
	assert.ElementsMatch(t, []string{"avant", "reel"}, ids(Counting(togo)),
		"ce qui compte au Togo : tout, sauf l'essai — et un document d'avant compte")
	assert.ElementsMatch(t, []string{"avant", "reel", "essai"}, ids(togo),
		"une lecture ordinaire voit l'essai : la console suit la recette")
	assert.ElementsMatch(t, []string{"avant", "reel"}, ids(Counting(context.Background())),
		"sans pays, la plateforme entière — toujours sans les essais")
}
