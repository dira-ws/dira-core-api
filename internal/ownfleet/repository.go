package ownfleet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Repository est l'accès Mongo au registre des biens.
type Repository struct {
	assets *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{assets: m.Collection(Collection)}
}

// ⚠️ LES INDEX DE CE PAQUET SONT DÉCLARÉS DANS `internal/indexes`, ET NULLE
// PART AILLEURS. C'est cette liste-là qui est réellement appliquée au
// démarrage ; une seconde copie ici n'aurait servi qu'à documenter — et à
// mentir le jour où l'une des deux change. L'unicité de la plaque par pays en
// dépend : elle n'est pas vérifiée par une lecture applicative.

// Filter borne une liste d'exploitation.
type Filter struct {
	Status   string
	ClassKey string
	Vertical string
	// Holder : les véhicules confiés à UNE personne.
	//
	// ⚠️ UN `ObjectID` ET NON UNE CHAÎNE, pour la même raison que le filtre de
	// flotte des courses : une chaîne illisible se traduit en clause vide, et
	// une clause vide rend LE PARC ENTIER sur un écran qui demandait les
	// véhicules d'une personne. Le type force l'analyse à avoir lieu avant.
	Holder primitive.ObjectID
}

// Create insère un véhicule.
func (r *Repository) Create(ctx context.Context, a *Asset) error {
	now := time.Now().UTC()
	a.CreatedAt, a.UpdatedAt = now, now
	res, err := r.assets.InsertOne(ctx, a)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return errPlateTaken
		}
		return fmt.Errorf("ownfleet: create: %w", err)
	}
	a.ID = res.InsertedID.(primitive.ObjectID)
	return nil
}

// ByID rend un véhicule du pays de la requête, ou nil.
//
// ⚠️ BORNÉ PAR LE PAYS en plus de l'identifiant : un véhicule d'un autre pays
// doit être INTROUVABLE, pas refusé. La console de Libreville ne doit pas
// pouvoir apprendre, en essayant des identifiants, ce que contient le parc de
// Lomé — ni y confier une voiture dont la redevance est libellée dans une
// autre monnaie.
func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Asset, error) {
	var a Asset
	err := r.assets.FindOne(ctx, country.Restrict(ctx, bson.M{"_id": id})).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ownfleet: by id: %w", err)
	}
	return &a, nil
}

// List rend le parc, le plus récent d'abord.
func (r *Repository) List(ctx context.Context, f Filter, cursor primitive.ObjectID, limit int) ([]Asset, error) {
	filter := country.Restrict(ctx, bson.M{})
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.ClassKey != "" {
		filter["class_key"] = f.ClassKey
	}
	if f.Vertical != "" {
		// ⚠️ UN PARC SANS VERTICALE DÉCLARÉE SERT LES DEUX, exactement comme un
		// article de matériel sans audience. Filtrer sur l'égalité seule aurait
		// masqué tous les véhicules polyvalents — c'est-à-dire la majorité des
		// motos.
		filter["$or"] = []bson.M{
			{"verticals": f.Vertical},
			{"verticals": bson.M{"$in": []any{nil, []string{}}}},
		}
	}
	if !f.Holder.IsZero() {
		filter["holder_user_id"] = f.Holder
	}
	if !cursor.IsZero() {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	cur, err := r.assets.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("ownfleet: list: %w", err)
	}
	var out []Asset
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("ownfleet: decode list: %w", err)
	}
	return out, nil
}

// ByHolder rend les véhicules confiés à une personne.
//
// ⚠️ SANS BORNE PAYS, comme toutes les lectures « les miens » : c'est
// l'identifiant du compte connecté qui désigne les documents, et il est plus
// étroit qu'un pays. Un chauffeur gabonais qui ouvre son application en voyage
// doit voir la voiture qu'il doit payer.
func (r *Repository) ByHolder(ctx context.Context, userID primitive.ObjectID) ([]Asset, error) {
	cur, err := r.assets.Find(ctx, bson.M{"holder_user_id": userID},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(20))
	if err != nil {
		return nil, fmt.Errorf("ownfleet: by holder: %w", err)
	}
	var out []Asset
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("ownfleet: decode by holder: %w", err)
	}
	return out, nil
}

// Update applique les champs d'une fiche et rend le véhicule tel qu'il est
// stocké.
//
// ⚠️ IL NE TOUCHE JAMAIS À L'ATTRIBUTION. `contract_id`, `holder_user_id` et
// `assigned_at` ne s'écrivent que par `Reserve` et `Release` : un champ qu'on
// peut aussi poser « à la main » depuis une fiche finit par dire autre chose
// que le contrat qu'il désigne, et c'est l'écran qui a l'air normal. Le service
// construit `set`, et son test vérifie que ces trois clés n'y entrent pas.
func (r *Repository) Update(ctx context.Context, id primitive.ObjectID, set bson.M) (*Asset, error) {
	set["updated_at"] = time.Now().UTC()
	var a Asset
	err := r.assets.FindOneAndUpdate(ctx,
		country.Restrict(ctx, bson.M{"_id": id}),
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, errPlateTaken
		}
		return nil, fmt.Errorf("ownfleet: update: %w", err)
	}
	return &a, nil
}

// Reserve CONFIE le véhicule : une seule écriture, condition et effet
// ensemble.
//
// ⚠️⚠️ C'EST LA GARANTIE CONTRE LA DOUBLE ATTRIBUTION, et elle ne peut vivre
// nulle part ailleurs. Lire « est-il libre ? » puis écrire aurait laissé passer
// les deux attributions du matin où quelqu'un double-clique — ou les deux
// exploitants qui traitent la même demande. Le filtre exige `available` ET
// l'absence de contrat ; la seconde requête ne trouve rien, et le service en
// fait un refus nommé.
//
// ⚠️ L'IDENTIFIANT DU CONTRAT EST ÉCRIT ICI, AVANT QUE LE CONTRAT EXISTE. C'est
// l'ordre qui compte : réserver d'abord, ouvrir la redevance ensuite. L'inverse
// — ouvrir puis réserver — aurait laissé un contrat vivant sur un véhicule déjà
// parti, c'est-à-dire une dette sans voiture.
//
// Rend nil quand rien n'a été réservé.
func (r *Repository) Reserve(ctx context.Context, assetID, contractID, holderUserID primitive.ObjectID, at time.Time) (*Asset, error) {
	var a Asset
	err := r.assets.FindOneAndUpdate(ctx,
		country.Restrict(ctx, bson.M{
			"_id":    assetID,
			"status": StatusAvailable,
			// ⚠️ `nil` PLUTÔT QUE `$exists: false` : il attrape le champ absent
			// ET le champ à null. `Release` l'efface, mais un document écrit par
			// une version antérieure peut le porter à null — et un véhicule
			// qu'on ne peut plus réserver se règle par une écriture en base.
			"contract_id": nil,
		}),
		bson.M{"$set": bson.M{
			"status":         StatusAssigned,
			"contract_id":    contractID,
			"holder_user_id": holderUserID,
			"assigned_at":    at,
			"updated_at":     at,
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ownfleet: reserve: %w", err)
	}
	return &a, nil
}

// Release REND le véhicule au parc — le chemin INVERSE de `Reserve`.
//
// ⚠️⚠️ CONDITIONNÉ PAR L'IDENTIFIANT DU CONTRAT, et c'est ce qui le rend
// rejouable sans dégât. Un retour enregistré en retard, un balayage qui repasse,
// une annulation tardive : sans cette condition, la libération d'un contrat
// ANCIEN aurait rendu au parc un véhicule déjà reconfié à quelqu'un d'autre — et
// le second chauffeur se serait vu retirer sa voiture par le retour du premier.
//
// ⚠️ ET SANS BORNE PAYS : le filtre porte `_id` ET `contract_id`, ce qui est
// bien plus étroit qu'un pays. Y ajouter le pays de la REQUÊTE aurait fait
// échouer la libération sur le retour scanné par un agent qui n'est pas chez
// lui — et le véhicule serait resté attribué à quelqu'un qui l'a rendu.
//
// Rend `false` quand rien n'a été libéré : déjà rendu, ou déjà reconfié.
func (r *Repository) Release(ctx context.Context, assetID, contractID primitive.ObjectID, at time.Time) (bool, error) {
	res, err := r.assets.UpdateOne(ctx,
		bson.M{"_id": assetID, "contract_id": contractID},
		bson.M{
			"$set":   bson.M{"status": StatusAvailable, "updated_at": at},
			"$unset": bson.M{"contract_id": "", "holder_user_id": "", "assigned_at": ""},
		})
	if err != nil {
		return false, fmt.Errorf("ownfleet: release: %w", err)
	}
	return res.ModifiedCount > 0, nil
}
