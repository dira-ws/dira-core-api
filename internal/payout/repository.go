package payout

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

type Repository struct{ col *mongo.Collection }

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{col: m.Collection(Collection)}
}

// EnsureIndexes pose ce que les écrans demandent.
func (r *Repository) EnsureIndexes(ctx context.Context) error {
	_, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Les retraits d'une personne, du plus récent au plus ancien.
		{Keys: bson.D{{Key: "owner_id", Value: 1}, {Key: "_id", Value: -1}}},
		// ⚠️ LA DEMANDE OUVERTE D'UNE PERSONNE — l'index qui tient la règle
		// « une seule à la fois ». Sans lui, la vérification balaie.
		{Keys: bson.D{{Key: "owner_id", Value: 1}, {Key: "state", Value: 1}}},
		// La file de validation d'un pays.
		{Keys: bson.D{{Key: "state", Value: 1}, {Key: "country", Value: 1}, {Key: "_id", Value: -1}}},
	})
	return err
}

func (r *Repository) Insert(ctx context.Context, p *Payout) error {
	res, err := r.col.InsertOne(ctx, p)
	if err != nil {
		return err
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		p.ID = id
	}
	return nil
}

// OpenOf rend la demande en cours d'une personne, s'il y en a une.
func (r *Repository) OpenOf(ctx context.Context, ownerID string) (*Payout, error) {
	oid, err := primitive.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, nil
	}
	var p Payout
	err = r.col.FindOne(ctx, bson.M{
		"owner_id": oid,
		"state":    bson.M{"$in": []string{StatePending, StateSending}},
	}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SentTodaySince somme ce qui est SORTI depuis `since`.
//
// ⚠️ ON COMPTE CE QUI EST PARTI OU EN COURS, pas ce qui est revenu. Un retrait
// refusé ou échoué remet l'argent au solde : le compter dans le plafond du
// jour punirait quelqu'un pour un numéro mal enregistré chez son opérateur.
func (r *Repository) SentTodaySince(ctx context.Context, ownerID string, since time.Time) (int, error) {
	oid, err := primitive.ObjectIDFromHex(ownerID)
	if err != nil {
		return 0, nil
	}
	cur, err := r.col.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"owner_id":   oid,
			"created_at": bson.M{"$gte": since},
			"state":      bson.M{"$in": []string{StatePending, StateSending, StatePaid}},
		}}},
		{{Key: "$group", Value: bson.M{"_id": nil, "total": bson.M{"$sum": "$amount_xof"}}}},
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = cur.Close(ctx) }()
	var row struct {
		Total int `bson:"total"`
	}
	if cur.Next(ctx) {
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
	}
	return row.Total, nil
}

// Decide passe une demande `pending` à l'état voulu, et rend `nil` si elle ne
// l'était plus.
//
// ⚠️ LA CONDITION SUR `pending` EST LE VERROU. Deux opérateurs qui valident en
// même temps : le second ne trouve rien, et on ne verse pas deux fois.
func (r *Repository) Decide(ctx context.Context, id primitive.ObjectID, state, reason string, by primitive.ObjectID, at time.Time) (*Payout, error) {
	set := bson.M{"state": state, "decided_at": at}
	if !by.IsZero() {
		set["decided_by"] = by
	}
	if reason != "" {
		set["reject_reason"] = reason
	}
	if state == StateRejected {
		set["settled_at"] = at
	}
	var p Payout
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": id, "state": StatePending},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) MarkSending(ctx context.Context, id primitive.ObjectID, ref string, at time.Time) error {
	set := bson.M{"state": StateSending, "sent_at": at}
	if ref != "" {
		set["provider_ref"] = ref
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

func (r *Repository) MarkPaid(ctx context.Context, id primitive.ObjectID, ref string, at time.Time) error {
	set := bson.M{"state": StatePaid, "settled_at": at}
	if ref != "" {
		set["provider_ref"] = ref
	}
	// ⚠️ CONDITIONNÉE SUR UN ÉTAT NON FINAL : un rappel d'agrégateur rejoué ne
	// doit pas ressusciter une demande déjà close, ni réécrire son instant.
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "state": bson.M{"$in": []string{StatePending, StateSending}}},
		bson.M{"$set": set})
	return err
}

func (r *Repository) MarkFailed(ctx context.Context, id primitive.ObjectID, reason string, at time.Time) error {
	set := bson.M{"state": StateFailed, "settled_at": at}
	if reason != "" {
		set["failure_reason"] = reason
	}
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "state": bson.M{"$in": []string{StatePending, StateSending}}},
		bson.M{"$set": set})
	return err
}

// ByID rend une demande.
func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Payout, error) {
	var p Payout
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&p)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) ListOf(ctx context.Context, ownerID string, limit int) ([]Payout, error) {
	oid, err := primitive.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	cur, err := r.col.Find(ctx, bson.M{"owner_id": oid},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []Payout
	return out, cur.All(ctx, &out)
}

// ListPending rend la file de validation, BORNÉE AU PAYS de la requête.
func (r *Repository) ListPending(ctx context.Context, limit int) ([]Payout, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := country.Restrict(ctx, bson.M{"state": StatePending})
	cur, err := r.col.Find(ctx, q,
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []Payout
	return out, cur.All(ctx, &out)
}

// DueForPeriodic rend les propriétaires à payer au tour périodique.
//
// ⚠️ C'EST LE PORTEFEUILLE QUI SAIT QUI A DE L'ARGENT, pas ce module : cette
// fonction ne sert qu'à écarter ceux qui ont déjà une demande ouverte.
func (r *Repository) OpenOwners(ctx context.Context) (map[string]bool, error) {
	cur, err := r.col.Find(ctx,
		country.Restrict(ctx, bson.M{"state": bson.M{"$in": []string{StatePending, StateSending}}}),
		options.Find().SetProjection(bson.M{"owner_id": 1}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()
	out := map[string]bool{}
	for cur.Next(ctx) {
		var row struct {
			OwnerID primitive.ObjectID `bson:"owner_id"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out[row.OwnerID.Hex()] = true
	}
	return out, cur.Err()
}
