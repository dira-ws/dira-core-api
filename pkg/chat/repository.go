package chat

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

type Repository struct {
	col *mongo.Collection
}

// NewRepository builds the store on a collection the VERTICALE names.
//
// ⚠️ Le nom de la collection est un ARGUMENT, pas une constante de ce paquet :
// chaque verticale garde ses conversations chez elle. Les mêler dans une seule
// collection n'apporterait rien — une conversation de commande et une
// conversation de course ne se lisent jamais ensemble — et obligerait les deux
// services à écrire dans la même table.
func NewRepository(m *db.Mongo, collection string) *Repository {
	return &Repository{col: m.Collection(collection)}
}

func (r *Repository) Insert(ctx context.Context, m *Message) error {
	res, err := r.col.InsertOne(ctx, m)
	if err != nil {
		return apperr.Internal(fmt.Errorf("chat: insert: %w", err))
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		m.ID = id
	}
	return nil
}

// ListByOrder returns a page of the conversation, OLDEST FIRST.
//
// Une conversation se lit dans l'ordre où elle s'est tenue. La pagination
// remonte donc le fil par le haut : `after` est le dernier message déjà connu,
// ce qui rend aussi le rattrapage après une coupure — l'appelant redemande la
// suite de ce qu'il a.
func (r *Repository) ListByRef(ctx context.Context, refID primitive.ObjectID, after *primitive.ObjectID, limit int) ([]Message, error) {
	filter := bson.M{"ref_id": refID}
	if after != nil {
		filter["_id"] = bson.M{"$gt": *after}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit))
	cur, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("chat: list: %w", err))
	}
	var out []Message
	if err := cur.All(ctx, &out); err != nil {
		return nil, apperr.Internal(fmt.Errorf("chat: decode: %w", err))
	}
	return out, nil
}

// MarkRead marks every message of the order NOT sent by `role` as read.
//
// Par RÔLE et non par identifiant d'expéditeur : c'est « tout ce que l'autre
// m'a envoyé » qu'on marque, et un livreur remplacé en cours de course
// laisserait sinon des messages que personne ne pourrait plus marquer lus.
func (r *Repository) MarkRead(ctx context.Context, refID primitive.ObjectID, role string, at time.Time) (int64, error) {
	res, err := r.col.UpdateMany(ctx,
		bson.M{"ref_id": refID, "sender_role": bson.M{"$ne": role}, "read_at": nil},
		bson.M{"$set": bson.M{"read_at": at}})
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("chat: mark read: %w", err))
	}
	return res.ModifiedCount, nil
}

// CountUnread counts what the other side sent and `role` has not read.
func (r *Repository) CountUnread(ctx context.Context, refID primitive.ObjectID, role string) (int, error) {
	n, err := r.col.CountDocuments(ctx, bson.M{
		"ref_id": refID, "sender_role": bson.M{"$ne": role}, "read_at": nil,
	})
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("chat: count unread: %w", err))
	}
	return int(n), nil
}

// PurgeOf jette tout ce que cette personne a ÉCRIT, dans toutes ses
// conversations — la part de l'effacement d'un compte qui vit ici.
//
// ⚠️ ON SUPPRIME SES MESSAGES, PAS LA CONVERSATION. Le fil appartient à DEUX
// personnes : effacer le tout retirerait au chauffeur ce qu'il a écrit lui-même
// et dont il peut avoir besoin — « j'ai déposé le sac chez le gardien » est sa
// trace, pas celle du client. Ce qui part, c'est le texte de la personne
// effacée ; ce qui reste n'est plus adressé à personne.
//
// ⚠️ IDEMPOTENTE, parce que l'annonce du socle arrive « au moins une fois » :
// supprimer des messages déjà supprimés ne supprime rien, et rend zéro.
func (r *Repository) PurgeOf(ctx context.Context, senderID primitive.ObjectID) (int64, error) {
	res, err := r.col.DeleteMany(ctx, bson.M{"sender_id": senderID})
	if err != nil {
		return 0, fmt.Errorf("chat: purge of %s: %w", senderID.Hex(), err)
	}
	return res.DeletedCount, nil
}

// --- LA MODÉRATION ---------------------------------------------------------

// One rend un message par son identifiant.
func (r *Repository) One(ctx context.Context, id primitive.ObjectID) (*Message, error) {
	var m Message
	switch err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&m); {
	case err == mongo.ErrNoDocuments:
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &m, nil
}

// ListFlagged rend la file de modération.
//
// ⚠️⚠️ LES SIGNALEMENTS HUMAINS D'ABORD (`reported: -1`), puis les plus
// récents. Une file triée par date seule noie les trois messages qu'une
// personne a pris la peine de signaler sous les quatre cents que l'automate a
// marqués — et ce sont précisément les trois qui méritent un humain.
//
// ⚠️ ET LES MESSAGES DÉJÀ TRAITÉS SORTENT DE LA FILE : un modérateur qui
// revoit cent fois les mêmes lignes cesse de la regarder.
func (r *Repository) ListFlagged(ctx context.Context, limit int, after *primitive.ObjectID) ([]Message, error) {
	q := bson.M{
		"$or":       []bson.M{{"flagged": true}, {"reported": true}},
		"moderated": bson.M{"$ne": true},
	}
	if after != nil {
		q["_id"] = bson.M{"$lt": *after}
	}
	cur, err := r.col.Find(ctx, q, options.Find().
		SetSort(bson.D{{Key: "reported", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []Message
	return out, cur.All(ctx, &out)
}

// SetReported marque un message comme signalé par son destinataire.
func (r *Repository) SetReported(ctx context.Context, id primitive.ObjectID, at time.Time) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$set": bson.M{"reported": true, "reported_at": at}})
	return err
}

// SetHidden retire un message de la conversation.
//
// ⚠️ IL RESTE EN BASE. Voir `Message.Hidden` : effacer réécrirait une histoire
// que l'autre partie a vécue, et priverait l'enquête de la seule pièce.
func (r *Repository) SetHidden(ctx context.Context, id, by primitive.ObjectID, reason string, at time.Time) error {
	set := bson.M{"hidden": true, "hidden_at": at, "moderated": true}
	if !by.IsZero() {
		set["hidden_by"] = by
	}
	if reason != "" {
		set["hidden_reason"] = reason
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

// SetKept sort un message de la file sans le retirer de la conversation.
func (r *Repository) SetKept(ctx context.Context, id primitive.ObjectID, at time.Time) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$set": bson.M{"moderated": true, "moderated_at": at}})
	return err
}
