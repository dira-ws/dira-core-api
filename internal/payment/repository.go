package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

var errPaymentNotFound = apperr.NotFound("payment_not_found", "payment not found")

// Repository provides MongoDB access to the payments collection.
type Repository struct {
	col *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{col: m.Collection(Collection)}
}

// Create inserts a new payment. The unique index on provider_ref enforces
// idempotency at initiation time.
func (r *Repository) Create(ctx context.Context, p *Payment) error {
	if p.ID.IsZero() {
		p.ID = primitive.NewObjectID()
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if _, err := r.col.InsertOne(ctx, p); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperr.Conflict("duplicate_provider_ref", "a payment with this provider reference already exists").WithCause(err)
		}
		return fmt.Errorf("payment: create: %w", err)
	}
	return nil
}

// FindByID returns the payment with the given id.
func (r *Repository) FindByID(ctx context.Context, id primitive.ObjectID) (*Payment, error) {
	var p Payment
	if err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&p); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errPaymentNotFound
		}
		return nil, fmt.Errorf("payment: find by id: %w", err)
	}
	return &p, nil
}

// FindByProviderRef returns the payment carrying the given provider reference.
//
// ⚠️⚠️ UNE RÉFÉRENCE VIDE OU D'ATTENTE EST REFUSÉE ICI, à la seule porte.
// Depuis que la ligne est écrite AVANT l'appel à l'opérateur, il existe en
// base des paiements portant une référence d'attente (`pending:<id>`) que
// l'opérateur n'a jamais vue. Un rappel qui en nommerait une ferait aboutir
// le paiement de quelqu'un d'autre. Et une référence vide, elle, matcherait
// n'importe quel document à qui ce champ manquerait.
//
// Le refus est ici et non chez l'appelant parce que c'est l'unique entrée :
// une règle écrite chez les appelants est perdue au premier appelant suivant.
func (r *Repository) FindByProviderRef(ctx context.Context, ref string) (*Payment, error) {
	if !usableProviderRef(ref) {
		return nil, errPaymentNotFound
	}
	var p Payment
	if err := r.col.FindOne(ctx, bson.M{"provider_ref": ref}).Decode(&p); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errPaymentNotFound
		}
		return nil, fmt.Errorf("payment: find by provider ref: %w", err)
	}
	return &p, nil
}

// SetStatusIfPending atomically transitions a payment from "pending" to
// status. It returns false when the payment was not pending anymore, which is
// the idempotency gate for duplicated webhooks.
func (r *Repository) SetStatusIfPending(ctx context.Context, id primitive.ObjectID, status string) (bool, error) {
	res, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "status": StatusPending},
		bson.M{"$set": bson.M{"status": status, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return false, fmt.Errorf("payment: set status: %w", err)
	}
	return res.MatchedCount > 0, nil
}

// usableProviderRef dit si une référence venue d'un rappel désigne quelque
// chose de légitime.
//
// Extraite et pure pour qu'elle s'éprouve, et parce que la doublure des
// épreuves l'APPELLE au lieu de la recopier : une règle écrite deux fois est
// perdue une fois, et une doublure plus permissive que la base certifie
// exactement le défaut qu'elle devait attraper.
func usableProviderRef(ref string) bool {
	return ref != "" && !IsReservedRef(ref)
}

// attachFilter désigne LE paiement dont la référence d'attente peut encore
// être remplacée.
//
// ⚠️ EXTRAITE ET PURE PARCE QUE C'EST ELLE QUI PROTÈGE. Sans la clause
// `provider_ref`, une seconde initiation de la même clé d'idempotence
// écraserait la référence déjà obtenue, et le rappel de l'opérateur ne
// retrouverait plus rien : un paiement encaissé, jamais confirmé. Un filtre
// qui vit dans l'appel à Mongo ne s'éprouve qu'avec une base ; celui-ci
// s'éprouve en une ligne.
func attachFilter(id primitive.ObjectID) bson.M {
	return bson.M{"_id": id, "provider_ref": reservedRef(id)}
}

// AttachProviderRef pose la référence de l'opérateur sur un paiement réservé,
// avec l'adresse où le payeur termine.
//
// ⚠️ LE FILTRE EXIGE LA RÉFÉRENCE D'ATTENTE. Sans lui, une seconde initiation
// de la même clé d'idempotence écraserait la référence déjà obtenue, et le
// rappel de l'opérateur ne retrouverait plus rien — un paiement encaissé,
// jamais confirmé. Ne rien mettre à jour n'est donc pas une erreur : cela dit
// que l'opérateur a déjà répondu, et l'appelant relit la ligne.
func (r *Repository) AttachProviderRef(ctx context.Context, id primitive.ObjectID, ref, paymentURL string) (bool, error) {
	res, err := r.col.UpdateOne(ctx,
		attachFilter(id),
		bson.M{"$set": bson.M{
			"provider_ref": ref,
			"payment_url":  paymentURL,
			"reached":      true,
			"updated_at":   time.Now().UTC(),
		}},
	)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, apperr.Conflict("duplicate_provider_ref",
				"a payment with this provider reference already exists").WithCause(err)
		}
		return false, fmt.Errorf("payment: attach provider ref: %w", err)
	}
	return res.MatchedCount > 0, nil
}

// FindByIdempotencyKey rend le paiement que CE compte a déjà lancé sous cette
// clé, ou (nil, nil).
//
// ⚠️ « RIEN » N'EST PAS UNE ERREUR ICI, et c'est voulu : c'est le cas NORMAL,
// celui d'une première initiation. Le signaler par une erreur obligerait
// chaque appelant à distinguer « pas trouvé » de « la base est tombée » —
// une comparaison que `WithCause` rend fragile, puisqu'elle clone la
// sentinelle.
//
// ⚠️ LA CLÉ EST BORNÉE AU COMPTE. Une clé est choisie par l'application : deux
// clients peuvent tirer la même, et une clé mondiale rendrait alors le
// paiement d'autrui.
func (r *Repository) FindByIdempotencyKey(ctx context.Context, userID primitive.ObjectID, key string) (*Payment, error) {
	if key == "" {
		return nil, nil
	}
	var p Payment
	err := r.col.FindOne(ctx, bson.M{"user_id": userID, "idempotency_key": key}).Decode(&p)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("payment: find by idempotency key: %w", err)
	}
	return &p, nil
}

// PaymentQuery borne la recherche d'un payeur dans SES paiements.
type PaymentQuery struct {
	// UserID est le payeur, tiré du jeton — jamais d'un paramètre.
	//
	// ⚠️ C'EST LA BORNE DE LA RECHERCHE, et elle est toujours au filtre. Un
	// identifiant nul ne l'ouvre pas : il devient `user_id: ObjectID("0…0")`,
	// qu'aucun compte ne porte. La recherche échoue alors FERMÉE — vide — là
	// où un filtre bâti « seulement si non nul » aurait rendu les paiements de
	// toute la plateforme.
	UserID  primitive.ObjectID
	Purpose string
	RefID   primitive.ObjectID
	Status  string
	Limit   int
	Cursor  primitive.ObjectID
}

// ownerFilter monte le filtre d'une recherche de paiements.
//
// Extraite et pure pour la même raison que celle du grand livre des jetons :
// c'est elle qui décide de ce qu'un compte voit, et une mutation qui
// l'affaiblit doit faire rougir une épreuve sans base ni requête HTTP.
//
// ⚠️ LE PAYEUR EST POSÉ D'ABORD ET SANS CONDITION. Les autres clauses sont des
// filtres de confort ; celle-là est la seule qui protège, et une écriture
// conditionnelle (`if !q.UserID.IsZero()`) l'aurait rendue facultative.
func ownerFilter(q PaymentQuery) bson.M {
	filter := bson.M{"user_id": q.UserID}
	if q.Purpose != "" {
		filter["purpose"] = q.Purpose
	}
	if !q.RefID.IsZero() {
		filter["ref_id"] = q.RefID
	}
	if q.Status != "" {
		filter["status"] = q.Status
	}
	return filter
}

// ListOwn rend les paiements du payeur, LE PLUS RÉCENT D'ABORD, avec une ligne
// de plus que demandé pour que l'appelant sache s'il en reste.
func (r *Repository) ListOwn(ctx context.Context, q PaymentQuery) ([]Payment, error) {
	filter := ownerFilter(q)
	if err := db.AfterNewest(ctx, r.col, filter, q.Cursor); err != nil {
		return nil, err
	}
	cur, err := r.col.Find(ctx, filter,
		options.Find().SetSort(db.NewestFirst).SetLimit(int64(q.Limit+1)))
	if err != nil {
		return nil, fmt.Errorf("payment: list own: %w", err)
	}
	var out []Payment
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("payment: list own: %w", err)
	}
	return out, nil
}
