package serviceapi

// LES GAINS D'UN ESSAI, PAR LA VRAIE ROUTE ET SUR UNE VRAIE BASE.
//
// ⚠️⚠️ CE QUE LA LIVRAISON ENVOIE POUR UNE COURSE D'ESSAI : le même corps qu'une
// vraie course, avec `testing: true` DANS `ref` — le champ libre que le grand
// livre range tel quel. Ni clé nouvelle au premier niveau du corps, ni route
// nouvelle : un socle d'avant ce lot l'accepte déjà, et celui-ci aussi. Cette
// épreuve le montre avec le décodeur de la route et le vrai grand livre, parce
// qu'un champ ajouté face à un décodeur strict donne un `422` muet.
//
// Lancée seulement si `DIRA_TEST_MONGO_URI` est posé, sur une base jetable.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

func TestATrialsMoneyKeepsItsMarkerOnARealLedger(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_trial_money_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })

	wallets := token.NewService(token.NewRepository(m), nil, nil, nil, nil, 100, 5, nil)
	h := &Handler{wallets: wallets}
	tg := country.WithCountry(ctx, "TG", country.SourceHeader)
	courier := primitive.NewObjectID().Hex()
	require.NoError(t, wallets.CreateWallet(tg, courier, "driver"))
	require.NoError(t, wallets.Credit(tg, courier, 5, "purchase"))

	course, real := primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()
	post := func(route string, handle http.HandlerFunc, body string) {
		t.Helper()
		w := httptest.NewRecorder()
		handle(w, httptest.NewRequest(http.MethodPost, route, strings.NewReader(body)).WithContext(tg))
		require.Equal(t, http.StatusNoContent, w.Code, "%s : %s", route, w.Body.String())
	}
	// Le droit d'entrée et le gain d'une course d'ESSAI…
	post("/internal/wallets/consume", h.consume, `{"owner_id":"`+courier+`","amount":1,"reason":"order_accept",`+
		`"ref_kind":"order","ref_id":"`+course+`","ref":{"testing":true}}`)
	post("/internal/wallets/credit-earnings", h.creditEarnings, `{"owner_id":"`+courier+`","amount":800,`+
		`"reason":"delivery_fee","ref_kind":"order","ref_id":"`+course+`","ref":{"delivery_fee":800,"testing":true}}`)
	// … et le gain d'une VRAIE course, au corps d'avant.
	post("/internal/wallets/credit-earnings", h.creditEarnings, `{"owner_id":"`+courier+`","amount":700,`+
		`"reason":"delivery_fee","ref_kind":"order","ref_id":"`+real+`","ref":{"delivery_fee":700}}`)

	marked := func(refID, reason string) (bool, bool) {
		t.Helper()
		oid, err := primitive.ObjectIDFromHex(refID)
		require.NoError(t, err)
		var tx struct {
			Ref map[string]any `bson:"ref"`
		}
		require.NoError(t, m.Collection("token_transactions").FindOne(ctx,
			bson.M{"ref_id": oid, "reason": reason}).Decode(&tx))
		v, ok := tx.Ref["testing"]
		return v == true, ok
	}
	on, _ := marked(course, "order_accept")
	assert.True(t, on, "le droit d'entrée d'un essai porte le marqueur au grand livre")
	on, _ = marked(course, "delivery_fee")
	assert.True(t, on, "son gain aussi")
	_, present := marked(real, "delivery_fee")
	assert.False(t, present, "un vrai gain n'en porte aucun")

	w, err := wallets.WalletOf(tg, courier)
	require.NoError(t, err)
	assert.Equal(t, 1_500, w.BalanceXOF, "l'argent bouge comme pour une vraie course : le marqueur ne change aucun montant")
}
