package country

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"
)

// ⚠️ UNE LECTURE QUI COMPTE ÉCARTE LES ESSAIS — avec ou sans pays. Le marqueur
// est sur l'objet : une jauge qui compte la plateforme entière, faute de liste
// des pays, ne doit pas pour autant compter une course d'essai.
func TestACountingReadLeavesTheTrialsOut(t *testing.T) {
	counting := Counting(context.Background())
	assert.True(t, IsCounting(counting))

	got := Restrict(WithCountry(counting, "tg", SourceClaims), bson.M{"status": "searching"})
	assert.Equal(t, bson.M{
		"status": "searching", Field: "TG", TestingField: bson.M{"$ne": true},
	}, got)

	// Sans pays : la plateforme entière, mais toujours sans les essais.
	assert.Equal(t, bson.M{TestingField: bson.M{"$ne": true}}, Restrict(counting, nil))

	d := RestrictD(WithCountry(counting, "TG", SourceClaims), bson.D{{Key: "status", Value: "searching"}})
	assert.Equal(t, bson.D{
		{Key: "status", Value: "searching"}, {Key: Field, Value: "TG"},
		{Key: TestingField, Value: bson.M{"$ne": true}},
	}, d)
}

// ⚠️ UNE LECTURE ORDINAIRE NE CHANGE PAS — c'est tout ce que l'exploitation et
// les applications lisent. Le livreur d'une course d'essai doit la voir, et la
// console doit suivre la recette.
func TestAnOrdinaryReadKeepsTheTrials(t *testing.T) {
	ctx := WithCountry(context.Background(), "TG", SourceClaims)
	assert.False(t, IsCounting(ctx))
	assert.Equal(t, bson.M{"status": "searching", Field: "TG"}, Restrict(ctx, bson.M{"status": "searching"}))
	assert.Equal(t, bson.D{{Key: Field, Value: "TG"}}, RestrictD(ctx, nil))
	assert.Equal(t, bson.M{}, Restrict(context.Background(), nil))
}
