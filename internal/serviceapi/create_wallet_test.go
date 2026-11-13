package serviceapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type ouvertures struct {
	Wallets
	opened []string
}

func (o *ouvertures) CreateWallet(_ context.Context, ownerID, walletType string) error {
	o.opened = append(o.opened, ownerID+":"+walletType)
	return nil
}

// ⚠️⚠️ OUVRIR UN PORTEFEUILLE NE DEMANDE AUCUN MONTANT. La route décodait la
// structure des débits, `amount` obligatoire : une verticale qui ne l'envoyait
// pas recevait `422` à chaque lecture en préprod (11 nov. 2026). Le `1` de
// contournement de la livraison reste accepté.
//
// ⚠️ ET LE TYPE `publisher` NE S'OUVRE PLUS : il servait une verticale qui ne
// fait plus partie de Dira. Les portefeuilles déjà ouverts sous ce type se
// lisent toujours ; on n'en ouvre pas de nouveau.
func TestOpeningAWalletNeedsNoAmount(t *testing.T) {
	o := &ouvertures{}
	h := &Handler{wallets: o}
	call := func(body string) int {
		w := httptest.NewRecorder()
		h.createWallet(w, httptest.NewRequest(http.MethodPost, "/internal/wallets/create", strings.NewReader(body)))
		return w.Code
	}
	const owner = "6b0f3c1a9e2d4f5a6b7c8d90"
	assert.Equal(t, http.StatusNoContent, call(`{"owner_id":"`+owner+`","type":"merchant"}`), "sans montant")
	assert.Equal(t, http.StatusNoContent, call(`{"owner_id":"`+owner+`","type":"driver","amount":1}`), "le contournement de la livraison")
	assert.Equal(t, []string{owner + ":merchant", owner + ":driver"}, o.opened)
	assert.Equal(t, http.StatusUnprocessableEntity, call(`{"owner_id":"`+owner+`"}`), "sans type")
	assert.Equal(t, http.StatusUnprocessableEntity, call(`{"owner_id":"`+owner+`","type":"jardin"}`), "type inconnu")
	assert.Equal(t, http.StatusUnprocessableEntity, call(`{"owner_id":"`+owner+`","type":"publisher"}`), "type retiré")
	assert.Len(t, o.opened, 2, "rien d'ouvert sur un refus")
}
