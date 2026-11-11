package serviceapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/internal/token"
)

// achats : le portefeuille, réduit à l'achat pour le compte d'un propriétaire.
type achats struct {
	Wallets
	actor, owner string
	tokens       int
}

func (a *achats) PurchaseForOwner(_ context.Context, actorID, ownerID string, tokens int) (token.PurchaseResponse, error) {
	a.actor, a.owner, a.tokens = actorID, ownerID, tokens
	return token.PurchaseResponse{PaymentID: "pay-1", Tokens: tokens, AmountXOF: tokens * 100}, nil
}

// L'ACHAT DE JETONS POUR UNE FICHE D'ANNONCEUR (décision du 11 nov. 2026) :
// le portefeuille est celui de la fiche, le paiement celui de qui paie — et
// les deux ne se confondent pas.
func TestAVerticalBuysTokensForAWalletItOwns(t *testing.T) {
	a := &achats{}
	h := &Handler{wallets: a}
	const fiche, porteur = "6b0f3c1a9e2d4f5a6b7c8d90", "6b0f3c1a9e2d4f5a6b7c8d91"
	call := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.purchaseForOwner(w, httptest.NewRequest(http.MethodPost, "/internal/wallets/purchase", strings.NewReader(body)))
		return w
	}

	w := call(`{"owner_id":"` + fiche + `","actor_id":"` + porteur + `","tokens":20}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, fiche, a.owner, "le portefeuille crédité est celui de la FICHE")
	assert.Equal(t, porteur, a.actor, "le paiement est au nom de celui qui paie")
	assert.Equal(t, 20, a.tokens)
	assert.Contains(t, w.Body.String(), `"payment_id":"pay-1"`)

	for name, body := range map[string]string{
		"sans jetons":     `{"owner_id":"` + fiche + `","actor_id":"` + porteur + `","tokens":0}`,
		"sans payeur":     `{"owner_id":"` + fiche + `","tokens":5}`,
		"fiche malformée": `{"owner_id":"fiche","actor_id":"` + porteur + `","tokens":5}`,
	} {
		a.tokens = -1
		w := call(body)
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code, name)
		assert.Equal(t, -1, a.tokens, "%s : rien n'est lancé", name)
	}
}
