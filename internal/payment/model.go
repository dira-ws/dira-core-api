// Package payment collects payments (orders and token purchases) through
// mobile-money providers hidden behind the PaymentProvider abstraction.
// Confirmation is asynchronous via signed provider webhooks and strictly
// idempotent.
package payment

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection is the MongoDB collection storing payments.
const Collection = "payments"

// Payment purposes.
const (
	PurposeOrder = "order"
	// PurposeRide paie une COURSE. Distinct de `order` : ce n'est pas la même
	// verticale qu'il faut prévenir quand le prestataire confirme, et confondre
	// les deux enverrait la confirmation d'une course à la livraison — qui ne
	// connaît aucune course et la refuserait.
	PurposeRide = "ride"
	// PurposeRideSubscription paie un ABONNEMENT de courses — un mois de
	// trajets réglé d'avance, pas une course.
	//
	// ⚠️ Distinct de `ride` alors que la MÊME verticale est prévenue : le
	// `ref_id` ne désigne pas le même objet. Confondus, la confirmation d'un
	// abonnement irait chercher une course de cet identifiant, ne la
	// trouverait pas, et le passager aurait payé son mois sans jamais voir
	// son abonnement s'activer.
	PurposeRideSubscription = "ride_subscription"
	PurposeTokenPurchase    = "token_purchase"
	// PurposeWalletTopup recharge le portefeuille d'ARGENT d'un client. Le
	// crédit n'a lieu qu'à la CONFIRMATION du prestataire : créditer sur la
	// réponse d'une initiation reviendrait à offrir l'argent.
	PurposeWalletTopup = "wallet_topup"
)

// Payment statuses.
const (
	StatusPending   = "pending"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	// StatusRefunded est posé par la RÉSOLUTION D'UN LITIGE, dans la
	// verticale : c'est elle qui juge qu'une commande doit être remboursée.
	//
	// ⚠️ Ce statut décrit le registre de la plateforme, PAS le prestataire.
	// Rendre l'argent chez l'opérateur mobile reste un geste manuel ; marquer
	// ici sans le faire là-bas laisserait un client remboursé sur l'écran et
	// pas sur son téléphone.
	StatusRefunded = "refunded"
)

// DefaultCurrency is the platform currency (smallest unit, integer amounts).
const DefaultCurrency = "XOF"

// PendingWindow est la durée pendant laquelle un paiement « en cours » vaut
// encore la peine d'être attendu.
//
// ⚠️ ELLE EST SERVIE, PAS DEVINÉE. Les applications la codaient en dur —
// dix minutes dans l'app complète, dix minutes recopiées dans Dira Lite — et
// retenaient le bouton « Payer » pendant ce temps-là. Une valeur recopiée
// dans trois applications ne peut plus bouger : la changer ici ne changerait
// rien chez elles, et la changer chez elles demanderait trois revues de
// magasin. Elle part donc sur le fil (`expires_at`), et les apps s'alignent.
const PendingWindow = 10 * time.Minute

// reservedRefPrefix préfixe la référence d'un paiement RÉSERVÉ — écrit avant
// que l'opérateur ne soit contacté, et qui n'a donc pas encore la sienne.
//
// ⚠️⚠️ UN PRÉFIXE, ET NON LA CHAÎNE VIDE, À CAUSE D'UN INDEX QU'ON NE PEUT PAS
// MIGRER EN SILENCE. `provider_ref` porte un index unique NON partiel : deux
// réservations sans référence entreraient en collision sur la valeur vide, et
// la seconde échouerait. Le rendre partiel serait la correction naturelle —
// sauf que `Indexes().CreateOne` sur un index EXISTANT aux options
// différentes échoue (`IndexOptionsConflict`), et que `db.EnsureIndexes` se
// contente de JOURNALISER cet échec. La préprod garderait donc son ancien
// index, le service démarrerait vert, et la deuxième réservation de chaque
// client serait refusée — un défaut qu'aucune épreuve locale ne voit, puisque
// localement la base est neuve et l'index se pose comme demandé.
//
// `pending:<identifiant du paiement>` est unique par construction, tient dans
// l'index tel qu'il est, et se lit.
const reservedRefPrefix = "pending:"

// reservedRef est la référence d'attente d'un paiement qu'on vient de
// réserver.
func reservedRef(id primitive.ObjectID) string { return reservedRefPrefix + id.Hex() }

// IsReservedRef reconnaît une référence d'attente.
//
// ⚠️ ELLE SERT À REFUSER UN RAPPEL QUI LA PORTERAIT. Un rappel de prestataire
// cherche un paiement PAR sa référence : s'il en nommait une référence
// d'attente, il tomberait sur un paiement que l'opérateur n'a jamais vu — et
// le ferait aboutir. La signature du rappel est vérifiée avant, mais une
// porte qui ne doit jamais s'ouvrir se ferme à clé aussi.
func IsReservedRef(ref string) bool { return strings.HasPrefix(ref, reservedRefPrefix) }

// Meta keys used for token purchases.
const (
	MetaWalletOwnerID = "wallet_owner_id"
	MetaTokens        = "tokens"
)

// Payment is one payment attempt tracked from initiation to provider
// confirmation.
type Payment struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	UserID   primitive.ObjectID `bson:"user_id"`
	Purpose  string             `bson:"purpose"` // "order" | "token_purchase"
	RefID    primitive.ObjectID `bson:"ref_id"`  // order id or wallet owner id
	Provider string             `bson:"provider"`
	Amount   int                `bson:"amount"`   // smallest currency unit
	Currency string             `bson:"currency"` // "XOF"
	Status   string             `bson:"status"`   // "pending" | "succeeded" | "failed"
	// Country est le pays de la requête qui a lancé le paiement — la borne
	// des listes de la console. Voir `pkg/country`.
	Country string `bson:"country,omitempty"`
	// ProviderRef is the provider-side identifier; unique index guarantees
	// webhook idempotency.
	//
	// Vaut `pending:<id>` entre la réservation de la ligne et la réponse de
	// l'opérateur — voir `reservedRefPrefix`.
	ProviderRef string `bson:"provider_ref"`
	// Reached dit si l'opérateur a RÉPONDU à la demande d'initiation.
	//
	// ⚠️⚠️ C'EST LA SEULE CHOSE HONNÊTE QU'ON PUISSE DIRE D'UN PAIEMENT DONT
	// L'INITIATION A ÉCHOUÉ. Le serveur contacte l'opérateur, et n'obtient ni
	// « parti » ni « pas parti » : un délai dépassé, une coupure, un `5xx`
	// peuvent très bien avoir laissé une demande de paiement sur le téléphone
	// du client. `reached:false` ne dit donc pas « rien n'est parti », il dit
	// « on a demandé et on ne sait pas » — et c'est ce que l'application doit
	// afficher, parce que proposer « Réessayer » comme si rien ne s'était
	// passé peut déclencher une SECONDE demande de paiement.
	Reached bool `bson:"reached"`
	// PaymentURL est l'endroit où le payeur termine le paiement chez
	// l'opérateur.
	//
	// ⚠️ GARDÉE, parce que c'est elle qu'on rend quand la réponse de
	// l'initiation s'est perdue. Sans elle, retrouver le paiement ne servait à
	// rien : l'application savait qu'il existait, et n'avait aucun moyen de le
	// terminer.
	PaymentURL string `bson:"payment_url,omitempty"`
	// IdempotencyKey est la clé que l'application a choisie pour CETTE
	// intention de paiement. Deux initiations de la même clé rendent le même
	// paiement, et n'en demandent qu'un à l'opérateur.
	IdempotencyKey string `bson:"idempotency_key,omitempty"`
	// ExpiresAt est l'instant après lequel un paiement en attente ne vaut plus
	// la peine d'être attendu — voir `PendingWindow`.
	//
	// ⚠️ UNE INFORMATION, PAS UN COUPERET : rien ne marque un paiement
	// « échoué » à cette date. Un rappel d'opérateur qui arrive après doit
	// encore pouvoir le faire aboutir, et `SetStatusIfPending` ne fait passer
	// que `pending` → final : un balayage qui aurait écrit `failed` entre-temps
	// aurait fait tomber le rappel tardif, et le client aurait payé pour rien.
	ExpiresAt time.Time `bson:"expires_at,omitempty"`
	// Meta carries purpose-specific context, e.g. wallet_owner_id and tokens
	// count for token purchases.
	Meta      map[string]any `bson:"meta,omitempty"`
	CreatedAt time.Time      `bson:"created_at"`
	UpdatedAt time.Time      `bson:"updated_at"`
}
