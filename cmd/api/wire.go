package main

import (
	"context"

	"github.com/kgtech-org/dira-core-api/internal/callback"
	"github.com/kgtech-org/dira-core-api/internal/config"
	"github.com/kgtech-org/dira-core-api/internal/payment"
	"github.com/kgtech-org/dira-core-api/internal/staff"
	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/internal/user"
)

// paymentCallbacks : QUELLE VERTICALE prévenir pour chaque objet payé.
//
// ⚠️⚠️ CHAQUE `purpose` DE `payment.VerticalPurposes` DOIT Y ÊTRE, et une
// épreuve le vérifie (`wire_callbacks_test.go`). Un `purpose` que la
// confirmation annonce et que ce registre ne route pas est un paiement abouti
// que personne n'apprend : la tâche part, ne trouve pas de destinataire, et
// s'abandonne — la commande ou l'envoi reste impayé alors que l'argent est
// encaissé.
//
// Extraite de `main` pour être éprouvée sans démarrer la plateforme.
func paymentCallbacks(cfg *config.Config) map[string]*callback.Client {
	food := func() *callback.Client { return callback.New("food", cfg.FoodBaseURL, cfg.FoodCallbackToken) }
	vtc := func() *callback.Client { return callback.New("vtc", cfg.VTCBaseURL, cfg.VTCCallbackToken) }
	return map[string]*callback.Client{
		payment.PurposeOrder: food(),
		payment.PurposeRide:  vtc(),
		// L'abonnement de courses va chez les COURSES, comme une course —
		// mais sous son propre nom, parce que son `ref_id` n'est pas une
		// course.
		payment.PurposeRideSubscription: vtc(),
		// L'ENVOI de colis va chez la LIVRAISON, qui porte le colis — sous
		// son propre nom : son `ref_id` est un envoi, pas une commande, et
		// `purpose` est ce qui le dit à la porte d'entrée.
		payment.PurposeShipment: food(),
	}
}

// backOffice réunit les LECTURES financières de deux modules en une seule
// dépendance pour la surface de service.
//
// C'est une TRADUCTION, pas un module : elle ne décide de rien, ne stocke
// rien, et ne fait que router trois appels vers le dépôt qui les sert. Elle
// existe parce que les portefeuilles et les paiements sont deux modules, alors
// que le back-office les lit d'un même écran — et parce que `serviceapi` ne
// doit pas avoir à connaître deux dépôts pour servir trois listes.
//
// Aucun champ n'est recopié ici : les lignes traversent TELLES QUELLES. Un
// champ ajouté à un portefeuille arrive au back-office sans qu'on touche à ce
// fichier.
// staffEntitlements adapte le staff à ce que les comptes en attendent.
type staffEntitlements struct{ svc *staff.Service }

func (a staffEntitlements) EntitlementsOf(ctx context.Context, userID string) (user.Entitlements, error) {
	scopes, direction, err := a.svc.EntitlementsOf(ctx, userID)
	return user.Entitlements{Scopes: scopes, Direction: direction}, err
}

type backOffice struct {
	tokens   *token.Repository
	payments *payment.Repository
}

func (b backOffice) ListWallets(ctx context.Context, walletType, ownerID, cursor string, limit int) ([]token.WalletRow, string, error) {
	return b.tokens.ListWallets(ctx, walletType, ownerID, cursor, limit)
}

func (b backOffice) ListLedger(ctx context.Context, walletID, cursor string, limit int) ([]token.LedgerRow, string, error) {
	return b.tokens.ListLedger(ctx, walletID, cursor, limit)
}

func (b backOffice) ListPayments(ctx context.Context, status, purpose, cursor string, limit int) ([]payment.PaymentRow, string, error) {
	return b.payments.List(ctx, status, purpose, cursor, limit)
}

func (b backOffice) RefundOrderPayment(ctx context.Context, orderID string) (string, error) {
	return b.payments.RefundOrderPayment(ctx, orderID)
}
