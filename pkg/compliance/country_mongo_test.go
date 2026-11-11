package compliance

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// ⚠️⚠️ SUR UNE VRAIE BASE : la borne de pays de la file et de l'arbitrage vit
// dans les FILTRES du dépôt réel — une doublure ne prouve que ce qu'elle imite.
// Et le rattrapage s'éprouve ici sur ce qui compte : une pièce d'avant n'a PAS
// de champ `country` du tout (ni vide, ni nul), et c'est elle qu'il faut voir.
func TestTheRealQueueAndReviewStayInTheirCountry(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_compliance_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })
	repo := NewRepository(m, "documents")
	tg, sn := inCountryCtx("TG"), inCountryCtx("SN")

	tgDoc, err := repo.UpsertDocument(tg, &Document{OwnerID: primitive.NewObjectID(), Kind: DocIDCard, FileURL: "https://f/t", Country: "TG"})
	require.NoError(t, err)
	assert.Equal(t, "TG", tgDoc.Country, "le pays part en base")
	snDoc, err := repo.UpsertDocument(sn, &Document{OwnerID: primitive.NewObjectID(), Kind: DocIDCard, FileURL: "https://f/s", Country: "SN"})
	require.NoError(t, err)
	// Une pièce d'AVANT : insérée sans le champ, comme la base la contient.
	oldOwner := primitive.NewObjectID()
	oldID := primitive.NewObjectID()
	_, err = m.Collection("documents").InsertOne(ctx, bson.M{
		"_id": oldID, "owner_id": oldOwner, "kind": DocIDCard, "file_url": "https://f/o",
		"status": DocPending, "created_at": time.Now(), "updated_at": time.Now(),
	})
	require.NoError(t, err)

	q, err := repo.PendingOrExpiredDocuments(tg, time.Now(), 50)
	require.NoError(t, err)
	require.Len(t, q, 1, "la file de Lomé ne montre que Lomé")
	assert.Equal(t, tgDoc.ID, q[0].ID)

	got, err := repo.ReviewDocument(tg, snDoc.ID, primitive.NewObjectID(), DocValid, "")
	require.NoError(t, err)
	assert.Nil(t, got, "Lomé n'arbitre pas une pièce de Dakar")
	still, err := repo.DocumentByID(ctx, snDoc.ID)
	require.NoError(t, err)
	assert.Equal(t, DocPending, still.Status, "et la pièce n'a pas bougé")
	got, err = repo.ReviewDocument(sn, snDoc.ID, primitive.NewObjectID(), DocValid, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, DocValid, got.Status)

	// Le rattrapage voit la pièce SANS champ, et elle seule.
	without, err := repo.DocumentsWithoutCountry(ctx, primitive.NilObjectID, 10)
	require.NoError(t, err)
	require.Len(t, without, 1)
	assert.Equal(t, oldID, without[0].ID)

	svc := NewService(repo, nil)
	svc.SetCountries(countriesOf{oldOwner.Hex(): "TG"})
	n, err := svc.AdoptCountries(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	q, err = repo.PendingOrExpiredDocuments(tg, time.Now(), 50)
	require.NoError(t, err)
	assert.Len(t, q, 2, "rattrapée, la pièce d'avant revient dans la file de son pays")

	// `SetCountry` ne réécrit jamais un pays posé.
	require.NoError(t, repo.SetCountry(ctx, tgDoc.ID, "SN"))
	again, err := repo.DocumentByID(ctx, tgDoc.ID)
	require.NoError(t, err)
	assert.Equal(t, "TG", again.Country)
}
