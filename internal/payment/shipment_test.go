package payment

// LE PAIEMENT D'UN ENVOI DE COLIS — lancé par le serveur, crédité au
// portefeuille du payeur, PUIS annoncé.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// journalDesCrochets note, DANS L'ORDRE, ce que la confirmation déclenche.
//
// ⚠️ L'ANNONCE DU DOUBLON EST BRANCHÉE COMME EN PRODUCTION
// (`OnRefPaidDuplicate = OnRefPaid`, `cmd/api/main.go`) : une épreuve qui
// laisserait le rejeu débranché ne verrait jamais un `purpose` oublié dans la
// liste du rejeu.
func journalDesCrochets(svc *Service) *[]string {
	var log []string
	svc.OnWalletToppedUp = func(_ context.Context, userID string, amount int, paymentID string) error {
		log = append(log, fmt.Sprintf("recharge %s %d %s", userID, amount, paymentID))
		return nil
	}
	svc.OnRefPaid = func(_ context.Context, purpose, refID, paymentID string) error {
		log = append(log, fmt.Sprintf("annonce %s %s %s", purpose, refID, paymentID))
		return nil
	}
	svc.OnRefPaidDuplicate = svc.OnRefPaid
	return &log
}

// ⚠️⚠️ UN PAIEMENT D'ENVOI ABOUTI CRÉDITE LE PORTEFEUILLE DU PAYEUR UNE FOIS,
// PUIS EST ANNONCÉ — à la confirmation ET à chaque rejeu, sans jamais
// recréditer. Et il se relit, par `GET /payments?purpose=shipment`.
//
// L'ordre compte : la livraison, prévenue, débite l'envoi de ce portefeuille —
// annoncer avant de créditer la ferait refuser faute de solde. Et recharger au
// rejeu d'un doublon créditerait deux fois un paiement reçu une fois.
func TestAShipmentPaymentTopsUpThenAnnounces(t *testing.T) {
	svc, repo, _, mock := newTestService(t)
	log := journalDesCrochets(svc)
	ctx := country.WithCountry(context.Background(), "TG", country.SourceHeader)
	shipment := primitive.NewObjectID().Hex()

	resp, err := svc.InitiateFor(ctx, payeur, PurposeShipment, shipment, 2_200, "parcel:"+shipment+":mm:1")
	require.NoError(t, err)
	assert.Equal(t, PurposeShipment, resp.Payment.Purpose)
	assert.Equal(t, 2_200, resp.Payment.Amount, "le montant figé par la verticale")
	assert.Equal(t, "XOF", resp.Payment.Currency, "la monnaie du pays de l'envoi")
	assert.Equal(t, StatusPending, resp.Payment.Status)
	assert.NotEmpty(t, resp.PaymentURL)
	assert.Empty(t, *log, "rien ne bouge avant la confirmation de l'opérateur")

	payload, sig := signedEvent(t, mock, resp.Payment.ProviderRef, StatusSucceeded)
	require.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig))
	want := []string{
		fmt.Sprintf("recharge %s 2200 %s", payeur, resp.Payment.ID),
		fmt.Sprintf("annonce shipment %s %s", shipment, resp.Payment.ID),
	}
	assert.Equal(t, want, *log, "le portefeuille du PAYEUR d'abord, du montant confirmé ; l'annonce ensuite")

	// Le prestataire réessaie : l'annonce repart, la recharge non.
	require.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig))
	want = append(want, fmt.Sprintf("annonce shipment %s %s", shipment, resp.Payment.ID))
	assert.Equal(t, want, *log, "le rejeu ré-annonce sans recréditer")

	stored, err := repo.FindByProviderRef(context.Background(), resp.Payment.ProviderRef)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, stored.Status)

	// ⚠️ IL SE RELIT : une application qui a perdu la réponse retrouve le
	// paiement de son envoi par la route des applications.
	r := routeur(t, svc, payeur, auth.RoleClient)
	code, out := appel(t, r, http.MethodGet, "/payments?purpose=shipment&ref_id="+shipment, "", nil)
	require.Equal(t, http.StatusOK, code, "%v", out)
	items := out["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, resp.Payment.ID, items[0].(map[string]any)["id"])
	assert.Equal(t, PurposeShipment, items[0].(map[string]any)["purpose"])
}

// ⚠️⚠️ LE PAIEMENT D'UN ENVOI SE LANCE PAR LE SERVEUR, au montant que la
// verticale a figé — la route des applications ne le propose pas. Elle refuse
// `shipment` en nommant le champ, et rien n'est réservé.
func TestAShipmentIsNotAnAppPurpose(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	r := routeur(t, svc, payeur, auth.RoleClient)
	code, out := appel(t, r, http.MethodPost, "/payments/initiate",
		`{"purpose":"shipment","amount":100,"ref_id":"`+primitive.NewObjectID().Hex()+`"}`,
		map[string]string{"Idempotency-Key": "app-1"})
	require.Equal(t, http.StatusUnprocessableEntity, code, "%v", out)
	assert.Equal(t, []any{"purpose"}, out["error"].(map[string]any)["fields"])
	assert.Empty(t, repo.byID, "un refus ne réserve aucune ligne")
}

// ⚠️ LE PAIEMENT D'UN ENVOI SE LANCE AVEC SA CLÉ, et la même clé rend le même
// paiement : un rappel qui réessaie après une réponse perdue ne fait pas
// demander un second paiement sur le téléphone du client.
func TestAShipmentPaymentIsLaunchedWithItsKey(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	ctx := country.WithCountry(context.Background(), "TG", country.SourceHeader)
	shipment := primitive.NewObjectID().Hex()

	_, err := svc.InitiateFor(ctx, payeur, PurposeShipment, shipment, 2_200, "  ")
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code)
	assert.Equal(t, []string{"idempotency_key"}, apperr.From(err).Meta["fields"])
	assert.Empty(t, repo.byID, "sans clé, rien n'est réservé")

	first, err := svc.InitiateFor(ctx, payeur, PurposeShipment, shipment, 2_200, "parcel:"+shipment+":mm:1")
	require.NoError(t, err)
	again, err := svc.InitiateFor(ctx, payeur, PurposeShipment, shipment, 2_200, "parcel:"+shipment+":mm:1")
	require.NoError(t, err)
	assert.Equal(t, first.Payment.ID, again.Payment.ID, "même clé, même paiement")
	assert.Len(t, repo.byID, 1)
	require.NotNil(t, first.Payment.ExpiresAt, "l'échéance de l'attente est servie")

	// Une autre tentative, une autre clé : un autre paiement.
	other, err := svc.InitiateFor(ctx, payeur, PurposeShipment, shipment, 2_200, "parcel:"+shipment+":mm:2")
	require.NoError(t, err)
	assert.NotEqual(t, first.Payment.ID, other.Payment.ID)

	// ⚠️ ET LES AUTRES `purpose` NE CHANGENT PAS : une commande lancée par le
	// canal WhatsApp n'a toujours pas besoin de clé.
	_, err = svc.InitiateFor(ctx, payeur, PurposeOrder, primitive.NewObjectID().Hex(), 1_500, "")
	require.NoError(t, err)
}
