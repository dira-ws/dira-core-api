package token

// CE QUE LE COLIS DEMANDE AU GRAND LIVRE — ses genres, ses jetons rendus, le
// portefeuille d'une entreprise et son approvisionnement sur pièce.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// Les trois genres du colis s'écrivent au grand livre, sous leur identifiant.
func TestTheThreeParcelKindsAreAccepted(t *testing.T) {
	for _, kind := range []string{RefShipment, RefShipmentCharge, RefShipmentLeg} {
		id := primitive.NewObjectID()
		got, err := parseRef(kind, id.Hex())
		require.NoError(t, err, kind)
		assert.Equal(t, id, *got)
	}
}

// ⚠️⚠️ RENDRE LES JETONS D'UNE ÉTAPE SE REJOUE SANS EN RENDRE DEUX FOIS — et un
// crédit sans référence reste ce qu'il a toujours été.
//
// La boîte d'envoi du colis rejoue tant que le socle n'a pas répondu : sans
// clé, trois reprises rendraient trois fois les jetons d'une acceptation.
func TestATokenCreditWithAReferenceIsAppliedOnce(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := seedWallet(t, repo, WalletTypeDriver, 0)
	leg := primitive.NewObjectID().Hex()

	for i := 0; i < 3; i++ {
		require.NoError(t, svc.CreditRef(context.Background(), owner.Hex(), 2, "order_accept_refund", RefShipmentLeg, leg), "rejoué : un succès")
	}
	assert.Equal(t, 2, repo.balanceOf(owner), "deux jetons rendus, une fois")
	require.Equal(t, 1, repo.transactionCount())
	assert.Equal(t, RefShipmentLeg, repo.transactions[0].RefKind, "le mouvement dit à quoi il se rattache")
	require.NotNil(t, repo.transactions[0].RefID)
	assert.Equal(t, leg, repo.transactions[0].RefID.Hex())

	// Une AUTRE étape rend les siens.
	require.NoError(t, svc.CreditRef(context.Background(), owner.Hex(), 2, "order_accept_refund", RefShipmentLeg, primitive.NewObjectID().Hex()))
	assert.Equal(t, 4, repo.balanceOf(owner))

	// ⚠️ SANS RÉFERENCE, RIEN NE CHANGE : deux crédits identiques sont deux gestes.
	require.NoError(t, svc.Credit(context.Background(), owner.Hex(), 1, ReasonOperatorCredit))
	require.NoError(t, svc.Credit(context.Background(), owner.Hex(), 1, ReasonOperatorCredit))
	assert.Equal(t, 6, repo.balanceOf(owner))
	last := repo.transactions[len(repo.transactions)-1]
	assert.Empty(t, last.RefKind)
	assert.Nil(t, last.RefID)

	// Une référence à moitié est refusée, comme partout au grand livre.
	err := svc.CreditRef(context.Background(), owner.Hex(), 1, "order_accept_refund", "", leg)
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code)
}

// Le portefeuille d'une entreprise s'ouvre — pour une fiche que le socle ne
// connaît pas —, une fois.
func TestABusinessWalletOpens(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := primitive.NewObjectID().Hex()
	require.NoError(t, svc.CreateWallet(context.Background(), owner, WalletTypeBusiness))
	require.NoError(t, svc.CreateWallet(context.Background(), owner, WalletTypeBusiness), "idempotent")
	w, err := svc.WalletOf(context.Background(), owner)
	require.NoError(t, err)
	assert.Equal(t, WalletTypeBusiness, w.Type)
	assert.Zero(t, w.BalanceXOF)
}

// auditNotes : le journal d'audit, tel qu'une verticale l'expédierait.
type auditNotes struct{ entries []audit.Entry }

func (a *auditNotes) ShipAudit(_ context.Context, e audit.Entry) error {
	a.entries = append(a.entries, e)
	return nil
}

// ⚠️⚠️ UN APPROVISIONNEMENT PORTE SA PREUVE : le montant, le moyen, la
// référence de la pièce et le motif — et l'audit dit qui l'a posé.
//
// C'est le seul geste de la console qui pose de l'argent RÉEL sur un solde sans
// qu'un prestataire l'ait confirmé : « aucune route ne crédite `balance_xof`
// gratuitement », et celle-ci tient la règle parce qu'elle exige sa pièce. Par
// la vraie route, avec le vrai décodeur — puis le service seul, qui revérifie.
func TestAProvisionNeedsItsProof(t *testing.T) {
	repo := newFakeRepo()
	notes := &auditNotes{}
	svc := NewService(repo, audit.NewRemoteRecorder(audit.ServiceCore, notes), nil, nil, nil, 100, 5, nil)
	business := primitive.NewObjectID()
	require.NoError(t, svc.CreateWallet(context.Background(), business.Hex(), WalletTypeBusiness))
	const actor = "6ab253499cb508f24710c036"

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithClaims(req.Context(), auth.Claims{UserID: actor, Role: auth.RoleAdmin, Type: "access"})))
		})
	})
	r.Post("/admin/wallets/{ownerID}/provision", NewHandler(svc).adminProvision)
	provision := func(owner, body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/admin/wallets/"+owner+"/provision", strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	fields := func(out map[string]any) any { return out["error"].(map[string]any)["fields"] }

	// Chaque pièce manquante est refusée, et nommée.
	for field, body := range map[string]string{
		"amount":        `{"means":"bank","reference":"VIR-2026-11-0042","justification":"virement reçu"}`,
		"means":         `{"amount":50000,"reference":"VIR-2026-11-0042","justification":"virement reçu"}`,
		"reference":     `{"amount":50000,"means":"bank","justification":"virement reçu"}`,
		"justification": `{"amount":50000,"means":"bank","reference":"VIR-2026-11-0042"}`,
	} {
		code, out := provision(business.Hex(), body)
		require.Equal(t, http.StatusUnprocessableEntity, code, "sans %s : %v", field, out)
		assert.Equal(t, []any{field}, fields(out), "le refus nomme %s", field)
	}
	code, out := provision(business.Hex(), `{"amount":50000,"means":"troc","reference":"VIR-1","justification":"virement reçu"}`)
	require.Equal(t, http.StatusUnprocessableEntity, code, "%v", out)
	assert.Equal(t, []any{"means"}, fields(out), "un moyen que la trésorerie ne connaît pas")
	assert.Zero(t, repo.moneyOf(business).BalanceXOF, "rien n'est posé sans sa pièce")
	assert.Empty(t, notes.entries, "rien n'est audité de ce qui n'a pas eu lieu")

	// La pièce entière : l'argent arrive, la ligne dit par où, l'audit dit qui et pourquoi.
	code, out = provision(business.Hex(), `{"amount":50000,"means":"bank","reference":"VIR-2026-11-0042",
		"justification":"virement reçu le 13 nov.","receipt_url":"https://files.dira.llc/dira-media/receipt/u1/r.pdf?X-Amz-Signature=abc"}`)
	require.Equal(t, http.StatusOK, code, "%v", out)
	assert.EqualValues(t, 50000, out["balance_xof"])
	assert.Equal(t, 50000, repo.moneyOf(business).BalanceXOF)
	last := repo.transactions[len(repo.transactions)-1]
	assert.Equal(t, ReasonBusinessProvision, last.Reason)
	assert.Equal(t, KindPurchase, last.Kind)
	assert.Equal(t, UnitXOF, last.Unit)
	assert.Equal(t, "bank", last.Ref["means"], "le journal en tire la trésorerie")
	assert.Equal(t, "VIR-2026-11-0042", last.Ref["reference"], "le rapprochement bancaire la cherche")
	assert.Equal(t, "https://files.dira.llc/dira-media/receipt/u1/r.pdf", last.Ref["receipt_url"], "l'adresse NUE, jamais la signée")
	require.Len(t, notes.entries, 1)
	e := notes.entries[0]
	assert.Equal(t, "token.business_provision", e.Action)
	assert.Equal(t, actor, e.ActorID, "qui l'a posé")
	after := e.After.(map[string]any)
	assert.Equal(t, "virement reçu le 13 nov.", after["justification"])
	assert.Equal(t, "VIR-2026-11-0042", after["reference"])
	assert.Equal(t, "bank", after["means"])
	assert.Equal(t, 50000, after["amount"])

	// La même pièce une seconde fois : refusée, et l'argent n'arrive pas deux fois.
	code, out = provision(business.Hex(), `{"amount":50000,"means":"bank","reference":" vir-2026-11-0042 ","justification":"virement reçu"}`)
	require.Equal(t, http.StatusConflict, code, "%v", out)
	assert.Equal(t, "provision_already_recorded", out["error"].(map[string]any)["code"])
	assert.Equal(t, 50000, repo.moneyOf(business).BalanceXOF)

	// ⚠️ SEUL UN PORTEFEUILLE D'ENTREPRISE S'APPROVISIONNE AINSI : celui d'un
	// client se recharge par un paiement confirmé.
	client := seedWallet(t, repo, WalletTypeClient, 0)
	code, out = provision(client.Hex(), `{"amount":5000,"means":"cash","reference":"CAISSE-7","justification":"versé au bureau"}`)
	require.Equal(t, http.StatusUnprocessableEntity, code, "%v", out)
	assert.Equal(t, "wallet_not_business", out["error"].(map[string]any)["code"])
	assert.Equal(t, "client", out["error"].(map[string]any)["reason"])
	assert.Zero(t, repo.moneyOf(client).BalanceXOF)

	// ⚠️ ET LE SERVICE VÉRIFIE AUSSI, seul : la règle ne dépend pas de la route.
	_, err := svc.ProvisionBusiness(context.Background(), actor, business.Hex(), ProvisionInput{Amount: 10, Means: "bank", Reference: "VIR-9"})
	require.Error(t, err)
	assert.Equal(t, []string{"justification"}, apperr.From(err).Meta["fields"])
}

// ⚠️ LA DETTE D'UNE ENTREPRISE — un retour qu'elle n'avait pas pu payer — se
// rembourse d'office sur l'approvisionnement qui arrive.
func TestAProvisionRepaysTheBusinessDebt(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	business := primitive.NewObjectID()
	require.NoError(t, svc.CreateWallet(context.Background(), business.Hex(), WalletTypeBusiness))
	charge := primitive.NewObjectID().Hex()
	paid, owed, err := svc.Owe(context.Background(), business.Hex(), 950, ReasonParcelReturnFee, ReasonPaymentDue, RefShipmentCharge, charge)
	require.NoError(t, err)
	assert.Zero(t, paid)
	assert.Equal(t, 950, owed)

	_, err = svc.ProvisionBusiness(context.Background(), "", business.Hex(), ProvisionInput{
		Amount: 5000, Means: "mobile_money", Reference: "MM-778899", Justification: "versement mobile reçu",
	})
	require.NoError(t, err)
	got := repo.moneyOf(business)
	assert.Zero(t, got.DebtXOF, "la dette est remboursée sur ce qui arrive")
	assert.Equal(t, 4050, got.BalanceXOF)
}
