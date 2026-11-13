package callback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/jobs"
)

// fakePayments rend la ligne d'un paiement, comme le module de paiement.
type fakePayments struct {
	payer, currency string
	amount          int
	err             error
	asked           []string
}

func (f *fakePayments) PaidOf(_ context.Context, paymentID string) (string, int, string, error) {
	f.asked = append(f.asked, paymentID)
	return f.payer, f.amount, f.currency, f.err
}

// vertical est une verticale qui garde ce qu'on lui a annoncé.
type vertical struct {
	mu     sync.Mutex
	bodies []map[string]any
	paths  []string
	status int
}

func (v *vertical) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		v.mu.Lock()
		v.bodies = append(v.bodies, body)
		v.paths = append(v.paths, r.URL.Path)
		status := v.status
		v.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func refPaidTask(t *testing.T, p jobs.RefPaidPayload) *asynq.Task {
	t.Helper()
	task, err := jobs.NewTask(jobs.TypeRefPaid, p)
	require.NoError(t, err)
	return task
}

const (
	orderID   = "64b7e2f4a1b2c3d4e5f60001"
	paymentID = "64b7e2f4a1b2c3d4e5f60002"
	payerID   = "64b7e2f4a1b2c3d4e5f60003"
)

// ⚠️ L'ANNONCE PORTE LE MONTANT, LA MONNAIE ET LE PAYEUR, relus sur la ligne du
// paiement : c'est ce que la verticale rapproche de ce qu'elle facture.
func TestTheAnnouncementCarriesTheAmountTheCurrencyAndThePayer(t *testing.T) {
	food := &vertical{}
	srv := food.serve(t)
	payments := &fakePayments{payer: payerID, amount: 12_500, currency: "XOF"}
	w := NewWorker(NewRegistry(map[string]*Client{"order": New("food", srv.URL, "jeton")}), payments)

	err := w.HandleRefPaid(context.Background(), refPaidTask(t, jobs.RefPaidPayload{
		Purpose: "order", RefID: orderID, PaymentID: paymentID,
	}))
	require.NoError(t, err)

	require.Len(t, food.bodies, 1)
	assert.Equal(t, "/api/v1/internal/payments/ref-paid", food.paths[0])
	got := food.bodies[0]
	assert.Equal(t, "order", got["purpose"])
	assert.Equal(t, orderID, got["ref_id"])
	assert.Equal(t, paymentID, got["payment_id"])
	assert.Equal(t, payerID, got["payer_id"])
	assert.EqualValues(t, 12_500, got["amount"], "le montant ENCAISSÉ, lu sur le paiement")
	assert.Equal(t, "XOF", got["currency"])
	assert.Equal(t, []string{paymentID}, payments.asked, "c'est CE paiement qu'on relit")
}

// Une annonce mise en file par une version précédente ne porte que les trois
// identifiants : elle part quand même avec son montant, puisqu'il est relu au
// départ et non recopié dans la tâche.
func TestAnAnnouncementQueuedByAnOlderCoreLeavesWithItsAmount(t *testing.T) {
	vtc := &vertical{}
	srv := vtc.serve(t)
	w := NewWorker(NewRegistry(map[string]*Client{"ride_subscription": New("vtc", srv.URL, "jeton")}),
		&fakePayments{payer: payerID, amount: 40_000, currency: "XAF"})

	// La charge exacte d'avant : pas un champ de plus.
	task := asynq.NewTask(jobs.TypeRefPaid,
		[]byte(`{"purpose":"ride_subscription","ref_id":"`+orderID+`","payment_id":"`+paymentID+`"}`))
	require.NoError(t, w.HandleRefPaid(context.Background(), task))

	require.Len(t, vtc.bodies, 1)
	assert.EqualValues(t, 40_000, vtc.bodies[0]["amount"])
	assert.Equal(t, "XAF", vtc.bodies[0]["currency"])
}

// Un paiement momentanément illisible REVIENT, et rien n'est annoncé : une
// annonce sans montant laisserait la verticale sans rien à rapprocher.
func TestAPaymentThatCannotBeReadIsRetriedAndNotAnnounced(t *testing.T) {
	food := &vertical{}
	srv := food.serve(t)
	w := NewWorker(NewRegistry(map[string]*Client{"order": New("food", srv.URL, "jeton")}),
		&fakePayments{err: errors.New("la base tousse")})

	err := w.HandleRefPaid(context.Background(), refPaidTask(t, jobs.RefPaidPayload{
		Purpose: "order", RefID: orderID, PaymentID: paymentID,
	}))
	require.Error(t, err)
	assert.False(t, errors.Is(err, asynq.SkipRetry), "une panne passagère doit être rejouée")
	assert.Empty(t, food.bodies, "rien ne part sans son montant")
}

// Un paiement INTROUVABLE ne réapparaîtra pas au vingtième essai : la tâche est
// abandonnée, et rien n'est annoncé.
func TestAnAnnouncementForAMissingPaymentIsDropped(t *testing.T) {
	food := &vertical{}
	srv := food.serve(t)
	w := NewWorker(NewRegistry(map[string]*Client{"order": New("food", srv.URL, "jeton")}),
		&fakePayments{err: apperr.NotFound("payment_not_found", "payment not found")})

	err := w.HandleRefPaid(context.Background(), refPaidTask(t, jobs.RefPaidPayload{
		Purpose: "order", RefID: orderID, PaymentID: paymentID,
	}))
	require.Error(t, err)
	assert.True(t, errors.Is(err, asynq.SkipRetry))
	assert.Empty(t, food.bodies)
}

// Un câblage qui a oublié la lecture des paiements n'annonce rien — il ne
// part pas « sans montant ».
func TestNoPaymentReaderMeansNoAnnouncement(t *testing.T) {
	food := &vertical{}
	srv := food.serve(t)
	w := NewWorker(NewRegistry(map[string]*Client{"order": New("food", srv.URL, "jeton")}), nil)

	err := w.HandleRefPaid(context.Background(), refPaidTask(t, jobs.RefPaidPayload{
		Purpose: "order", RefID: orderID, PaymentID: paymentID,
	}))
	require.Error(t, err)
	assert.Empty(t, food.bodies)
}

// Le refus d'une verticale REVIENT, comme avant : c'est ce que la file absorbe.
func TestAVerticalRefusalIsStillRetried(t *testing.T) {
	food := &vertical{status: http.StatusServiceUnavailable}
	srv := food.serve(t)
	w := NewWorker(NewRegistry(map[string]*Client{"order": New("food", srv.URL, "jeton")}),
		&fakePayments{payer: payerID, amount: 1_000, currency: "XOF"})

	err := w.HandleRefPaid(context.Background(), refPaidTask(t, jobs.RefPaidPayload{
		Purpose: "order", RefID: orderID, PaymentID: paymentID,
	}))
	require.Error(t, err)
	assert.False(t, errors.Is(err, asynq.SkipRetry))
	assert.Len(t, food.bodies, 1)
}
