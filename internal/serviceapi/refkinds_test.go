package serviceapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// ⚠️ UN GENRE QUE LE GRAND LIVRE ACCEPTE DOIT PASSER LA ROUTE DE SERVICE. La
// liste vit deux fois — `token.parseRef` et le `oneof` de `walletRequest` — et
// c'est la seconde, oubliée, qui refuse en `422` un débit que la première
// aurait inscrit. (`equipment` a sa propre route : `/internal/equipment/collect`.)
func TestEveryReferenceKindTheLedgerTakesPassesTheServiceRoute(t *testing.T) {
	for _, kind := range []string{
		token.RefOrder, token.RefRide, token.RefTip, token.RefRideAdjustment,
		token.RefListing, token.RefListingBoost,
	} {
		body := `{"owner_id":"` + primitive.NewObjectID().Hex() + `","amount":1,"ref_kind":"` + kind +
			`","ref_id":"` + primitive.NewObjectID().Hex() + `"}`
		var req walletRequest
		require.NoError(t, httpx.DecodeService(httptest.NewRequest("POST", "/", strings.NewReader(body)), &req), kind)
	}
}
