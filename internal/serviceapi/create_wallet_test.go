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
// structure des débits, `amount` obligatoire : l'immobilier, qui ne l'envoyait
// pas, recevait `422` à chaque lecture en préprod (11 nov. 2026). Le `1` de
// contournement de la livraison reste accepté.
func TestOpeningAWalletNeedsNoAmount(t *testing.T) {
	o := &ouvertures{}
	h := &Handler{wallets: o}
	call := func(body string) int {
		w := httptest.NewRecorder()
		h.createWallet(w, httptest.NewRequest(http.MethodPost, "/internal/wallets/create", strings.NewReader(body)))
		return w.Code
	}
	const fiche = "6b0f3c1a9e2d4f5a6b7c8d90"
	assert.Equal(t, http.StatusNoContent, call(`{"owner_id":"`+fiche+`","type":"publisher"}`), "sans montant")
	assert.Equal(t, http.StatusNoContent, call(`{"owner_id":"`+fiche+`","type":"driver","amount":1}`), "le contournement de la livraison")
	assert.Equal(t, []string{fiche + ":publisher", fiche + ":driver"}, o.opened)
	assert.Equal(t, http.StatusUnprocessableEntity, call(`{"owner_id":"`+fiche+`"}`), "sans type")
	assert.Equal(t, http.StatusUnprocessableEntity, call(`{"owner_id":"`+fiche+`","type":"jardin"}`), "type inconnu")
	assert.Len(t, o.opened, 2, "rien d'ouvert sur un refus")
}
