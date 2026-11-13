package serviceapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
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
	} {
		var req walletRequest
		require.NoError(t, httpx.DecodeService(httptest.NewRequest("POST", "/", strings.NewReader(refBody(kind))), &req), kind)
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
