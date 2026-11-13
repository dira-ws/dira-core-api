package serviceapi

// CE QUE LE COLIS DEMANDE À LA SURFACE DE SERVICE — par la vraie route, avec le
// décodeur des verticales.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/internal/payment"
)

type creditsNotes struct {
	Wallets
	calls []string
}

func (c *creditsNotes) CreditRef(_ context.Context, ownerID string, amount int, reason, refKind, refID string) error {
	c.calls = append(c.calls, strings.Join([]string{ownerID, reason, refKind, refID}, "|"))
	return nil
}

// ⚠️ LE CRÉDIT DE JETONS PREND UNE RÉFÉRENCE FACULTATIVE, ET LA TRANSMET. Sans
// elle, rien ne change ; avec elle, le grand livre la rattache et rend le
// crédit rejouable — c'est ce qui rend les jetons d'une étape annulée sous son
// livreur depuis une boîte d'envoi qui rejoue.
func TestTheTokenCreditRouteCarriesAnOptionalReference(t *testing.T) {
	c := &creditsNotes{}
	h := &Handler{wallets: c}
	call := func(body string) int {
		w := httptest.NewRecorder()
		h.credit(w, httptest.NewRequest(http.MethodPost, "/internal/wallets/credit", strings.NewReader(body)))
		return w.Code
	}
	const owner, leg = "6b0f3c1a9e2d4f5a6b7c8d90", "6b0f3c1a9e2d4f5a6b7c8d91"
	assert.Equal(t, http.StatusNoContent, call(`{"owner_id":"`+owner+`","amount":2,"reason":"order_accept_refund"}`))
	assert.Equal(t, http.StatusNoContent, call(`{"owner_id":"`+owner+`","amount":2,"reason":"order_accept_refund","ref_kind":"shipment_leg","ref_id":"`+leg+`"}`))
	assert.Equal(t, []string{
		owner + "|order_accept_refund||",
		owner + "|order_accept_refund|shipment_leg|" + leg,
	}, c.calls)
}

type paiementsNotes struct {
	clientID, purpose, refID, key string
	amount                        int
}

func (p *paiementsNotes) InitiateFor(_ context.Context, clientID, purpose, refID string, amount int, key string) (*payment.InitiatePaymentResponse, error) {
	p.clientID, p.purpose, p.refID, p.amount, p.key = clientID, purpose, refID, amount, key
	exp := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	return &payment.InitiatePaymentResponse{
		Payment: payment.PaymentResponse{
			ID: "6b0f3c1a9e2d4f5a6b7c8d99", Status: payment.StatusPending, ExpiresAt: &exp,
			OperatorReached: true, PaymentURL: "https://pay.example/p/1",
		},
		PaymentURL: "https://pay.example/p/1",
	}, nil
}

// ⚠️⚠️ LA ROUTE DE SERVICE PREND LA CLÉ DE LA VERTICALE, ET REND DE QUOI
// REPRENDRE LE PAIEMENT. `payment_url` reste là pour les appelants d'avant ;
// `payment_id`, `status`, `expires_at` et `operator_reached` s'y ajoutent.
func TestTheServicePaymentRouteTakesAKeyAndSaysHowToResume(t *testing.T) {
	p := &paiementsNotes{}
	h := &Handler{payments: p}
	const client, shipment = "6b0f3c1a9e2d4f5a6b7c8d90", "6b0f3c1a9e2d4f5a6b7c8d92"
	w := httptest.NewRecorder()
	h.initiatePayment(w, httptest.NewRequest(http.MethodPost, "/internal/payments/initiate", strings.NewReader(
		`{"client_id":"`+client+`","purpose":"shipment","ref_id":"`+shipment+`","amount":2200,"idempotency_key":"parcel:`+shipment+`:mm:1"}`)))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, "parcel:"+shipment+":mm:1", p.key, "la clé atteint le paiement")
	assert.Equal(t, 2200, p.amount)
	assert.Equal(t, "shipment", p.purpose)

	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "https://pay.example/p/1", out["payment_url"], "le champ d'avant reste")
	assert.Equal(t, "6b0f3c1a9e2d4f5a6b7c8d99", out["payment_id"])
	assert.Equal(t, "pending", out["status"])
	assert.Equal(t, "2026-11-13T10:00:00Z", out["expires_at"])
	assert.Equal(t, true, out["operator_reached"])

	// Un appelant d'avant, sans clé : la même route, la même réponse.
	w = httptest.NewRecorder()
	h.initiatePayment(w, httptest.NewRequest(http.MethodPost, "/internal/payments/initiate", strings.NewReader(
		`{"client_id":"`+client+`","purpose":"order","ref_id":"`+shipment+`","amount":1500}`)))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Empty(t, p.key)
}
