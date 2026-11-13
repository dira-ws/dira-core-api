package serviceapi

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/refkind"
)

func refBody(kind string) string {
	return `{"owner_id":"` + primitive.NewObjectID().Hex() + `","amount":1,"ref_kind":"` + kind +
		`","ref_id":"` + primitive.NewObjectID().Hex() + `"}`
}

// ⚠️ UN GENRE QUE LE GRAND LIVRE ACCEPTE DOIT PASSER LA ROUTE DE SERVICE. La
// liste vit deux fois — `token.parseRef` et le `oneof` de `walletRequest` — et
// c'est la seconde, oubliée, qui refuse en `422` un débit que la première
// aurait inscrit. (`equipment` a sa propre route : `/internal/equipment/collect`.)
func TestEveryReferenceKindTheLedgerTakesPassesTheServiceRoute(t *testing.T) {
	for _, kind := range []string{
		token.RefOrder, token.RefRide, token.RefTip, token.RefRideAdjustment,
		// L'abonnement de courses, payé au solde comme son paiement en ligne.
		token.RefRideSubscription,
		// Les trois genres du colis : l'envoi, un frais, une étape.
		token.RefShipment, token.RefShipmentCharge, token.RefShipmentLeg,
	} {
		var req walletRequest
		require.NoError(t, httpx.DecodeService(httptest.NewRequest("POST", "/", strings.NewReader(refBody(kind))), &req), kind)
	}
}

// ⚠️⚠️ LE `oneof` DE LA ROUTE EST LA LISTE DE `pkg/refkind`, NI PLUS NI MOINS.
//
// Un tag ne sait pas lire une variable : la route garde sa liste écrite, et
// c'est cette épreuve qui la tient égale à celle que les verticales lisent pour
// éprouver ce qu'elles envoient. Un genre ajouté d'un côté seulement serait
// accepté par une verticale qui s'y fie, et refusé ici en `422` — ou
// l'inverse.
func TestTheServiceRouteTakesExactlyTheSharedKinds(t *testing.T) {
	field, ok := reflect.TypeOf(walletRequest{}).FieldByName("RefKind")
	require.True(t, ok)
	var oneof []string
	for _, rule := range strings.Split(field.Tag.Get("validate"), ",") {
		if list, found := strings.CutPrefix(rule, "oneof="); found {
			oneof = strings.Fields(list)
		}
	}
	require.NotEmpty(t, oneof, "le `oneof` de `ref_kind` a disparu : la route accepterait n'importe quel genre")
	assert.ElementsMatch(t, refkind.ServiceRoutes(), oneof)
	// Et chacun s'écrit au grand livre.
	for _, kind := range oneof {
		assert.True(t, refkind.Writable(kind), kind)
	}
}

// ⚠️⚠️ ET UN GENRE RETIRÉ NE LA PASSE PLUS. `listing` et `listing_boost` restent
// LISIBLES au grand livre — des écritures passées les portent —, mais aucune
// écriture nouvelle ne s'y rattache : le refus nomme le champ.
func TestARetiredReferenceKindIsRefusedByTheServiceRoute(t *testing.T) {
	for _, kind := range []string{token.RefListing, token.RefListingBoost} {
		var req walletRequest
		err := httpx.DecodeService(httptest.NewRequest("POST", "/", strings.NewReader(refBody(kind))), &req)
		require.Error(t, err, kind)
		assert.Equal(t, []string{"ref_kind"}, apperr.From(err).Meta["fields"], kind)
	}
}
