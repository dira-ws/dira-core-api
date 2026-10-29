package payment

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// DefaultProvider is used when the caller does not name a provider.
const DefaultProvider = "mock"

var (
	errUnknownProvider  = apperr.New("unknown_provider", "unknown payment provider", http.StatusUnprocessableEntity)
	errInvalidSignature = apperr.Unauthorized("invalid_signature", "invalid webhook signature")
)

// repository is the storage contract the service needs (consumer-side).
type repository interface {
	Create(ctx context.Context, p *Payment) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*Payment, error)
	FindByProviderRef(ctx context.Context, ref string) (*Payment, error)
	SetStatusIfPending(ctx context.Context, id primitive.ObjectID, status string) (bool, error)
	AttachProviderRef(ctx context.Context, id primitive.ObjectID, ref, paymentURL string) (bool, error)
	FindByIdempotencyKey(ctx context.Context, userID primitive.ObjectID, key string) (*Payment, error)
	ListOwn(ctx context.Context, q PaymentQuery) ([]Payment, error)
}

// auditRecorder is satisfied by *audit.Recorder (injected at wiring time).
type auditRecorder interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// TopupLimits rend les bornes de recharge du pays de la requête.
//
// ⚠️⚠️ ELLE EXISTE PARCE QU'UNE BORNE SERVIE QU'AUCUNE ROUTE N'APPLIQUE EST LE
// DÉFAUT DE #251, À L'IDENTIQUE. `GET /wallet` annonce désormais un minimum et
// un maximum ; sans ce contrôle, l'application cacherait le bouton pour un
// montant que le serveur accepterait quand même, et un appel direct passerait
// outre. Une borne qui ne refuse rien n'est pas une borne, c'est une
// suggestion.
//
// Facultative : sans elle, seul `amount > 0` s'applique, comme avant.
type TopupLimits interface {
	// ⚠️ UN NOM À ELLE, et non `TopupOf` : le portefeuille a besoin des
	// montants proposés et des décimales pour AFFICHER, le paiement n'a besoin
	// que des deux bornes pour REFUSER. Deux signatures sous le même nom
	// obligeraient à deux adaptateurs — et deux adaptateurs lisant la même
	// politique finissent par la lire différemment, ce qui est exactement
	// l'écart qu'on cherche à empêcher.
	TopupLimitsOf(ctx context.Context, code string) (min, max int, currency string)
}

// Service implements the payment business logic.
// PaymentPreferences rend l'opérateur mobile money habituel d'un client.
// Défini ICI, côté consommateur ; le câblage injecte le service des comptes.
type PaymentPreferences interface {
	PreferredPaymentProvider(ctx context.Context, userID string) string
}

type Service struct {
	repo      repository
	providers map[string]PaymentProvider
	audit     auditRecorder

	// OnOrderPaid is invoked exactly once when an order payment succeeds.
	// Wiring sets it to mark the order paid AND create the delivery
	// (order.MarkPaid + delivery.CreateForOrder). Nil-safe: skipped with a
	// warning when unset.
	// OnRefPaid est invoqué EXACTEMENT UNE FOIS quand le paiement d'un objet
	// d'une verticale aboutit. Le `purpose` dit laquelle prévenir — le socle
	// ne sait ni ce qu'est une commande, ni ce qu'est une course.
	OnRefPaid func(ctx context.Context, purpose, refID, paymentID string) error
	// OnRefPaidDuplicate est invoqué quand un webhook ARRIVE EN DOUBLON sur un
	// paiement déjà abouti.
	//
	// ⚠️ Il existe pour une raison précise, et son absence était un trou. Le
	// rappel de la verticale part désormais en FILE : si la mise en file
	// échoue, le paiement est pourtant déjà marqué « abouti ». Le prestataire
	// réessaie, tombe sur la branche « doublon » — et sans ce point
	// d'accroche, personne ne serait jamais prévenu. La commande resterait
	// payée et jamais confirmée.
	//
	// Nil-safe : sans lui, le doublon reste un simple no-op, ce qu'il était.
	OnRefPaidDuplicate func(ctx context.Context, purpose, refID, paymentID string) error
	// OnTokensPurchased is invoked exactly once when a token purchase
	// succeeds. Wiring sets it to credit the wallet (token.Credit). Nil-safe:
	// skipped with a warning when unset.
	OnTokensPurchased func(ctx context.Context, walletOwnerID string, tokens int) error
	// OnWalletToppedUp crédite le portefeuille d'argent d'un client après
	// confirmation du prestataire.
	OnWalletToppedUp func(ctx context.Context, userID string, amountXOF int, paymentID string) error
	// preferences rend l'opérateur habituel d'un client. Facultatif : sans
	// lui, le défaut s'applique comme avant.
	preferences PaymentPreferences
	// topup borne une recharge de portefeuille. Facultatif.
	topup TopupLimits
}

// SetTopupLimits branche les bornes de recharge (câblage).
func (s *Service) SetTopupLimits(l TopupLimits) { s.topup = l }

// LES DEUX REFUS D'UNE RECHARGE HORS BORNES.
//
// ⚠️ NOMMÉS, ET NON UN `validation_failed` GÉNÉRIQUE. C'est la septième fois
// sur cette plateforme qu'un refus perd son détail en route : l'application
// doit pouvoir dire « le minimum est de 500 F » plutôt que « montant
// invalide », et pour cela il lui faut le CODE du refus. Les chiffres voyagent
// dans la phrase traduite (`{{.min}}`, `{{.max}}`, `{{.currency}}`), là où les
// chiffres d'un refus voyagent sur cette plateforme.
var (
	errTopupTooSmall = apperr.New("topup_below_minimum",
		"the amount is below this country's minimum top-up", http.StatusUnprocessableEntity)
	errTopupTooLarge = apperr.New("topup_above_maximum",
		"the amount is above this country's maximum top-up", http.StatusUnprocessableEntity)
)

// checkTopup borne une recharge de portefeuille.
//
// ⚠️ LES AUTRES `purpose` NE SONT PAS CONCERNÉS, et c'est une décision : le
// prix d'une course, d'un panier ou d'un abonnement est décidé par la verticale
// qui l'a calculé, et le plafond d'une recharge n'a rien à y dire. Le borner
// ici aurait fait refuser une commande de restaurant au motif qu'elle dépasse
// le plafond de recharge du pays.
func (s *Service) checkTopup(ctx context.Context, purpose string, amount int) error {
	if s.topup == nil || purpose != PurposeWalletTopup {
		return nil
	}
	min, max, currency := s.topup.TopupLimitsOf(ctx, country.FromContext(ctx))
	détail := map[string]any{
		"fields":   []string{"amount"},
		"min":      min,
		"max":      max,
		"currency": currency,
	}
	if min > 0 && amount < min {
		return errTopupTooSmall.WithMeta(détail)
	}
	if max > 0 && amount > max {
		return errTopupTooLarge.WithMeta(détail)
	}
	return nil
}

// NewService builds the service with a provider registry keyed by provider
// name (e.g. {"mock": NewMockProvider("")}).
func NewService(repo repository, providers map[string]PaymentProvider, rec auditRecorder) *Service {
	return &Service{repo: repo, providers: providers, audit: rec}
}

// Initiate creates a pending payment and starts the provider flow.
func (s *Service) Initiate(ctx context.Context, userID string, req InitiatePaymentRequest) (*InitiatePaymentResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	refID, err := primitive.ObjectIDFromHex(req.RefID)
	if err != nil {
		return nil, apperr.Validation("invalid ref_id").WithCause(err)
	}

	providerName := req.Provider
	if providerName == "" && s.preferences != nil {
		// L'opérateur habituel du client, quand il en a déclaré un. Une
		// PRÉSÉLECTION, pas un paiement automatique : aucun jeton n'est
		// conservé, et le prestataire demandera confirmation comme toujours.
		//
		// Le repli sur le défaut ne vaut QUE pour cet opérateur-là. Un
		// opérateur EXPLICITEMENT demandé et inconnu reste une erreur : le
		// client a choisi, et le faire payer ailleurs sans le dire serait
		// pire qu'un refus.
		if remembered := s.preferences.PreferredPaymentProvider(ctx, userID); remembered != "" {
			if _, known := s.providers[remembered]; known {
				providerName = remembered
			}
		}
	}
	if providerName == "" {
		providerName = DefaultProvider
	}
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, errUnknownProvider
	}

	var meta map[string]any
	if req.Purpose == PurposeTokenPurchase {
		if req.Tokens <= 0 {
			return nil, apperr.Validation("tokens count is required for token purchases")
		}
		meta = map[string]any{
			MetaWalletOwnerID: refID.Hex(),
			MetaTokens:        req.Tokens,
		}
	}

	// ⚠️ AVANT DE RÉSERVER QUOI QUE CE SOIT : un montant hors bornes ne doit
	// laisser aucune ligne derrière lui — et surtout pas une ligne que la clé
	// d'idempotence rendrait ensuite pour toujours.
	if err := s.checkTopup(ctx, req.Purpose, req.Amount); err != nil {
		return nil, err
	}

	key := strings.TrimSpace(req.IdempotencyKey)

	// ⚠️ MÊME CLÉ = MÊME PAIEMENT, et on ne redemande rien à l'opérateur pour
	// un paiement qui a déjà la sienne. C'est tout l'objet de la clé : une
	// application qui réessaie après une réponse perdue ne doit pas déclencher
	// une SECONDE demande de paiement sur le téléphone du client.
	if existing, err := s.repo.FindByIdempotencyKey(ctx, uid, key); err != nil {
		return nil, err
	} else if existing != nil {
		return s.resume(ctx, existing, provider, userID)
	}

	id := primitive.NewObjectID()
	p := &Payment{
		ID:       id,
		UserID:   uid,
		Purpose:  req.Purpose,
		RefID:    refID,
		Provider: providerName,
		Amount:   req.Amount,
		Currency: DefaultCurrency,
		Status:   StatusPending,
		Country:  country.FromContext(ctx),
		Meta:     meta,
		// ⚠️⚠️ LA RÉFÉRENCE D'ATTENTE, PARCE QUE LA LIGNE EST ÉCRITE AVANT
		// D'APPELER L'OPÉRATEUR — voir `reservedRefPrefix`.
		ProviderRef:    reservedRef(id),
		IdempotencyKey: key,
		ExpiresAt:      time.Now().UTC().Add(PendingWindow),
	}

	// ⚠️⚠️ ON RÉSERVE LA LIGNE AVANT DE CONTACTER L'OPÉRATEUR, ET C'EST
	// L'INVERSION QUI COMPTE DANS TOUT CE FICHIER.
	//
	// Avant, l'ordre était : appeler l'opérateur, PUIS écrire. Un `5xx`, un
	// délai dépassé, une réponse illisible — et la demande de paiement
	// pouvait très bien être partie sur le téléphone du client, sans qu'aucune
	// ligne n'existe chez nous. L'application, qui avait perdu l'`id` avec la
	// réponse, n'avait alors AUCUN moyen de savoir qu'un paiement était en
	// cours : « Réessayer » déclenchait une deuxième demande, et les apps Dira
	// Lite se protégeaient en retenant le bouton dix minutes à l'aveugle.
	//
	// Et la correction demandée — une route pour RETROUVER un paiement perdu —
	// n'aurait rien trouvé dans ce cas précis, le seul pour lequel elle
	// existe. L'ordre devait changer d'abord.
	if err := s.repo.Create(ctx, p); err != nil {
		// Course sur la clé d'idempotence : deux initiations simultanées de la
		// même intention. L'index a tranché, et le perdant relit le gagnant
		// plutôt que de rendre une erreur à un client qui n'a rien fait de mal.
		if existing, lookupErr := s.repo.FindByIdempotencyKey(ctx, uid, key); lookupErr == nil && existing != nil {
			return s.resume(ctx, existing, provider, userID)
		}
		return nil, err
	}

	return s.ask(ctx, p, provider, userID)
}

// ask demande le paiement à l'opérateur pour une ligne DÉJÀ réservée.
func (s *Service) ask(ctx context.Context, p *Payment, provider PaymentProvider, userID string) (*InitiatePaymentResponse, error) {
	result, err := provider.Initiate(ctx, InitiateRequest{
		// ⚠️ L'IDENTIFIANT EST CELUI DE LA LIGNE, ET IL SURVIT AUX REPRISES.
		// C'est ce qui donne à l'opérateur une chance de dédoublonner : avant,
		// chaque tentative portait un identifiant NEUF, et deux tentatives
		// étaient pour lui deux paiements sans rapport.
		PaymentID: p.ID.Hex(),
		UserID:    userID,
		Purpose:   p.Purpose,
		Amount:    p.Amount,
		Currency:  p.Currency,
		Meta:      p.Meta,
	})
	if err != nil {
		// ⚠️ LA LIGNE RESTE, `pending` ET `reached:false` : « on a demandé et
		// on ne sait pas ». C'est précisément ce que l'application doit
		// pouvoir retrouver, et c'était ce qui disparaissait avant.
		slog.WarnContext(ctx, "payment: the operator did not answer the initiation; the payment stays pending and unreached",
			"payment_id", p.ID.Hex(), "provider", p.Provider, "error", err)
		return nil, apperr.Internal(err)
	}

	attached, err := s.repo.AttachProviderRef(ctx, p.ID, result.ProviderRef, result.InstructionsURL)
	if err != nil {
		// ⚠️ TROU RÉSIDUEL, ASSUMÉ ET NOMMÉ : l'opérateur a répondu, et sa
		// référence n'est pas écrite — son rappel ne retrouvera donc pas ce
		// paiement. La ligne existe pourtant, `pending`, et l'exploitation
		// peut la rapprocher : c'est strictement mieux qu'avant, où il n'y
		// avait aucune ligne du tout. Le fermer vraiment demande que le rappel
		// puisse aussi désigner notre identifiant de paiement, ce que le
		// contrat des prestataires réels n'est pas encore en état de promettre.
		slog.ErrorContext(ctx, "payment: the operator answered but its reference could not be stored; its webhook will not match",
			"payment_id", p.ID.Hex(), "provider", p.Provider, "provider_ref", result.ProviderRef, "error", err)
		return nil, err
	}
	if !attached {
		// Quelqu'un a attaché avant nous (une reprise concurrente) : on relit
		// plutôt que d'écraser.
		fresh, readErr := s.repo.FindByID(ctx, p.ID)
		if readErr != nil {
			return nil, readErr
		}
		p = fresh
	} else {
		p.ProviderRef, p.PaymentURL, p.Reached = result.ProviderRef, result.InstructionsURL, true
	}

	return &InitiatePaymentResponse{
		Payment:    toPaymentResponse(p),
		PaymentURL: p.PaymentURL,
	}, nil
}

// resume rend un paiement déjà lancé sous la même clé d'idempotence.
//
// ⚠️ IL NE REDEMANDE À L'OPÉRATEUR QUE CE QU'IL NE LUI A JAMAIS DEMANDÉ. Un
// paiement qui porte déjà sa référence est rendu tel quel — redemander
// déclencherait la deuxième demande que la clé existe pour éviter. Un paiement
// jamais abouti chez l'opérateur, lui, est repris : sans cela, une initiation
// tombée en panne laisserait un paiement mort que la clé rendrait éternellement.
func (s *Service) resume(ctx context.Context, p *Payment, provider PaymentProvider, userID string) (*InitiatePaymentResponse, error) {
	if p.Reached || p.Status != StatusPending {
		resp := toPaymentResponse(p)
		return &InitiatePaymentResponse{Payment: resp, PaymentURL: resp.PaymentURL}, nil
	}
	return s.ask(ctx, p, provider, userID)
}

// defaultListLimit et maxListLimit bornent `GET /payments`.
const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// ListOwn rend les paiements du demandeur, le plus récent d'abord.
//
// ⚠️ TOUJOURS LES SIENS, SANS EXCEPTION NI RÔLE PRIVILÉGIÉ. La console a sa
// propre route (`/admin/payments`), bornée au pays ; celle-ci est bornée au
// COMPTE, et le `user_id` ne vient que du jeton. Un paramètre `user_id` aurait
// fait de cette route un annuaire des paiements de la plateforme.
func (s *Service) ListOwn(ctx context.Context, userID string, q PaymentQuery) (*PaymentListResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	q.UserID = uid
	if q.Limit <= 0 || q.Limit > maxListLimit {
		q.Limit = defaultListLimit
	}
	rows, err := s.repo.ListOwn(ctx, q)
	if err != nil {
		return nil, err
	}
	out := &PaymentListResponse{Items: make([]PaymentResponse, 0, len(rows))}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		out.HasMore = true
	}
	for i := range rows {
		out.Items = append(out.Items, toPaymentResponse(&rows[i]))
	}
	if out.HasMore && len(out.Items) > 0 {
		out.NextCursor = out.Items[len(out.Items)-1].ID
	}
	return out, nil
}

// InitiatePurchase is the provider contract used by the token module's
// POST /wallet/purchase: it starts a token-purchase payment with the default
// provider and returns the payment id.
func (s *Service) InitiatePurchase(ctx context.Context, userID, walletOwnerID string, tokens int, amountXOF int) (string, error) {
	resp, err := s.Initiate(ctx, userID, InitiatePaymentRequest{
		Purpose: PurposeTokenPurchase,
		Amount:  amountXOF,
		RefID:   walletOwnerID,
		Tokens:  tokens,
	})
	if err != nil {
		return "", err
	}
	return resp.Payment.ID, nil
}

// Get returns a payment; only its owner or an admin may read it.
func (s *Service) Get(ctx context.Context, requesterID, requesterRole, paymentID string) (*PaymentResponse, error) {
	id, err := primitive.ObjectIDFromHex(paymentID)
	if err != nil {
		return nil, errPaymentNotFound.WithCause(err)
	}
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if requesterRole != auth.RoleAdmin && p.UserID.Hex() != requesterID {
		return nil, apperr.Forbidden("forbidden", "not the payment owner")
	}
	resp := toPaymentResponse(p)
	return &resp, nil
}

// HandleWebhook verifies and processes a provider webhook. It is strictly
// idempotent: a payment already confirmed is acknowledged without any side
// effect, and the pending->final status transition is the atomic gate that
// guarantees hooks run at most once.
func (s *Service) HandleWebhook(ctx context.Context, providerName string, payload []byte, signature string) error {
	provider, ok := s.providers[providerName]
	if !ok {
		return apperr.NotFound("unknown_provider", "unknown payment provider")
	}

	event, err := provider.VerifyWebhook(payload, signature)
	if err != nil {
		return errInvalidSignature.WithCause(err)
	}

	p, err := s.repo.FindByProviderRef(ctx, event.ProviderRef)
	if err != nil {
		return err
	}
	// Idempotency: already confirmed -> 200.
	//
	// ⚠️ On RÉ-ENFILE le rappel au passage. C'est ce qui rend récupérable un
	// échec de mise en file : le paiement est abouti, mais rien ne dit que la
	// verticale l'a su. Le rappel étant idempotent chez elle, en envoyer un de
	// trop ne coûte rien — en oublier un coûte une commande perdue.
	if p.Status == StatusSucceeded {
		s.replayRefPaid(ctx, p)
		return nil
	}

	switch event.Status {
	case StatusSucceeded:
		return s.confirmSucceeded(ctx, p)
	case StatusFailed:
		return s.confirmFailed(ctx, p)
	default:
		return apperr.Validation("unknown webhook event status")
	}
}

// replayRefPaid remet en file l'annonce d'un paiement déjà abouti.
//
// ⚠️ AU MIEUX, et volontairement silencieux en cas d'échec : on est sur la
// branche « doublon », le prestataire a déjà eu sa réponse la première fois.
// Faire échouer le webhook ici le ferait réessayer indéfiniment pour un
// paiement qui, lui, est bien enregistré.
func (s *Service) replayRefPaid(ctx context.Context, p *Payment) {
	if s.OnRefPaidDuplicate == nil {
		return
	}
	if !notifiesVertical(p.Purpose) {
		return
	}
	if err := s.OnRefPaidDuplicate(ctx, p.Purpose, p.RefID.Hex(), p.ID.Hex()); err != nil {
		slog.WarnContext(ctx, "payment: could not re-queue the vertical callback for a duplicate webhook",
			"payment_id", p.ID.Hex(), "purpose", p.Purpose, "error", err)
	}
}

// notifiesVertical dit si ce `purpose` désigne un objet qui vit dans une
// VERTICALE — par opposition au portefeuille et aux jetons, que le socle
// crédite lui-même.
//
// ⚠️ Une seule liste, lue par la confirmation ET par le rejeu d'un doublon.
// Séparées, elles ont divergé : un `purpose` ajouté d'un côté seulement
// confirmait le premier webhook et laissait tomber les reprises — l'échec le
// plus difficile à voir, puisqu'il ne se produit que quand le prestataire
// réessaie.
func notifiesVertical(purpose string) bool {
	switch purpose {
	case PurposeOrder, PurposeRide, PurposeRideSubscription:
		return true
	}
	return false
}

func (s *Service) confirmSucceeded(ctx context.Context, p *Payment) error {
	transitioned, err := s.repo.SetStatusIfPending(ctx, p.ID, StatusSucceeded)
	if err != nil {
		return err
	}
	if !transitioned {
		// Course perdue avec un webhook concurrent : c'est un doublon, et il
		// ré-enfile pour la même raison que ci-dessus.
		s.replayRefPaid(ctx, p)
		return nil
	}

	s.record(ctx, "payment.succeeded", p, StatusSucceeded)

	switch p.Purpose {
	case PurposeOrder, PurposeRide, PurposeRideSubscription:
		if s.OnRefPaid == nil {
			slog.WarnContext(ctx, "payment: OnRefPaid hook not configured, skipping",
				"payment_id", p.ID.Hex(), "purpose", p.Purpose, "ref_id", p.RefID.Hex())
			return nil
		}
		// L'identifiant du PAIEMENT part avec : la verticale en a besoin pour
		// sa trace d'audit, et il est le seul moyen de remonter au
		// prestataire depuis une commande ou une course contestée.
		if err := s.OnRefPaid(ctx, p.Purpose, p.RefID.Hex(), p.ID.Hex()); err != nil {
			return apperr.Internal(err)
		}
	case PurposeWalletTopup:
		if s.OnWalletToppedUp == nil {
			slog.WarnContext(ctx, "payment: OnWalletToppedUp hook not configured, skipping", "payment_id", p.ID.Hex())
			return nil
		}
		// Le montant CRÉDITÉ est celui du paiement confirmé, jamais celui
		// qu'annonçait la requête : entre les deux, c'est le prestataire qui
		// fait foi.
		if err := s.OnWalletToppedUp(ctx, p.RefID.Hex(), p.Amount, p.ID.Hex()); err != nil {
			return apperr.Internal(err)
		}
	case PurposeTokenPurchase:
		if s.OnTokensPurchased == nil {
			slog.WarnContext(ctx, "payment: OnTokensPurchased hook not configured, skipping", "payment_id", p.ID.Hex())
			return nil
		}
		ownerID, tokens := tokenPurchaseContext(p)
		if tokens <= 0 {
			return apperr.Internal(errInvalidTokenMeta(p))
		}
		if err := s.OnTokensPurchased(ctx, ownerID, tokens); err != nil {
			return apperr.Internal(err)
		}
	}
	return nil
}

func (s *Service) confirmFailed(ctx context.Context, p *Payment) error {
	transitioned, err := s.repo.SetStatusIfPending(ctx, p.ID, StatusFailed)
	if err != nil {
		return err
	}
	if !transitioned {
		return nil
	}
	// The order (or purchase intent) is left untouched: the payment can be
	// retried with a new initiation.
	s.record(ctx, "payment.failed", p, StatusFailed)
	return nil
}

func (s *Service) record(ctx context.Context, action string, p *Payment, newStatus string) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "payment", p.ID.Hex(),
		map[string]any{"status": p.Status},
		map[string]any{"status": newStatus, "purpose": p.Purpose, "amount": p.Amount, "provider_ref": p.ProviderRef},
	)
}

// tokenPurchaseContext extracts the wallet owner and tokens count from the
// payment meta, tolerating the numeric types Mongo decodes into.
func tokenPurchaseContext(p *Payment) (string, int) {
	ownerID := p.RefID.Hex()
	if v, ok := p.Meta[MetaWalletOwnerID].(string); ok && v != "" {
		ownerID = v
	}
	return ownerID, asInt(p.Meta[MetaTokens])
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

type invalidTokenMetaError struct{ paymentID string }

func (e invalidTokenMetaError) Error() string {
	return "payment: token purchase " + e.paymentID + " has no valid tokens count in meta"
}

func errInvalidTokenMeta(p *Payment) error { return invalidTokenMetaError{paymentID: p.ID.Hex()} }

// ConfirmSandboxPayment forces a pending payment to succeed by forging the
// provider webhook it would have received.
//
// ⚠️ CETTE MÉTHODE SIMULE DE L'ARGENT REÇU. Elle existe pour dérouler le
// parcours de recharge de bout en bout hors production — sans elle, un achat
// de jetons reste éternellement en attente sur un environnement de test.
//
// Deux verrous INDÉPENDANTS la protègent :
//   - la route qui l'expose n'est montée qu'en dehors de la production ;
//   - seul un fournisseur capable de signer ses propres webhooks est
//     acceptable. Un vrai prestataire ne l'est pas : sa signature ne peut pas
//     être forgée, et c'est exactement la propriété qu'on veut conserver.
//
// Le chemin emprunté est le VRAI : même vérification de signature, même
// traitement, même idempotence. Rien n'est court-circuité.
func (s *Service) ConfirmSandboxPayment(ctx context.Context, paymentID string) error {
	id, err := primitive.ObjectIDFromHex(paymentID)
	if err != nil {
		return errPaymentNotFound.WithCause(err)
	}
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if p.Status == StatusSucceeded {
		return nil // déjà encaissé : rejouer ne doit rien changer
	}

	provider, ok := s.providers[p.Provider]
	if !ok {
		return apperr.NotFound("unknown_provider", "unknown payment provider")
	}
	signer, ok := provider.(interface{ Sign([]byte) string })
	if !ok {
		return apperr.New("provider_not_simulatable",
			"ce prestataire ne peut pas être simulé : sa signature de webhook n'est pas forgeable",
			http.StatusConflict)
	}

	payload, err := json.Marshal(map[string]string{
		"provider_ref": p.ProviderRef,
		"status":       StatusSucceeded,
	})
	if err != nil {
		return apperr.Internal(err)
	}
	return s.HandleWebhook(ctx, p.Provider, payload, signer.Sign(payload))
}

// SetPreferences branche l'opérateur habituel des clients (câblage).
func (s *Service) SetPreferences(p PaymentPreferences) { s.preferences = p }

// InitiateFor starts a payment ON BEHALF OF a client, at a vertical's request.
//
// ⚠️ Sert au canal WhatsApp : une commande passée hors de l'application n'a pas
// de session, et personne pour appuyer sur « payer ». La verticale déclenche,
// le client reçoit un lien.
//
// Rend l'URL SEULE et non la réponse entière : l'appelant n'a besoin que de
// cela, et lui rendre l'objet complet ferait traverser au réseau des champs
// qu'aucune verticale n'a à connaître.
func (s *Service) InitiateFor(ctx context.Context, clientID, purpose, refID string, amountXOF int) (string, error) {
	resp, err := s.Initiate(ctx, clientID, InitiatePaymentRequest{
		Purpose: purpose, RefID: refID, Amount: amountXOF,
	})
	if err != nil {
		return "", err
	}
	return resp.PaymentURL, nil
}
