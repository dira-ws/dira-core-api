package finance

// LE DÉPÔT DES DÉCLARATIONS — et la transaction qui tient l'écriture et la
// déclaration ensemble.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

var (
	errDeclarationNotFound = apperr.NotFound("declaration_not_found", "declaration not found")
	// errDeclarationDuplicate : la même pièce, ou la même référence de module,
	// déclarée deux fois.
	errDeclarationDuplicate = apperr.Conflict("declaration_duplicate", "a declaration already carries this reference")
)

// declarationStore : ce que le service attend du stockage.
//
// ⚠️ L'INTERFACE EXISTE POUR QUE LE JSON SERVI SOIT ÉPROUVABLE SANS MONGO,
// pas pour abstraire la base. Elle est donc étroite, et la logique comptable
// (quelle écriture, quel compte, quel signe dans la synthèse) n'est PAS
// dedans : elle est en fonctions pures, pour qu'aucune doublure ne puisse
// être plus fidèle que la base.
type declarationStore interface {
	Catalog(ctx context.Context, cc string) (*DeclarationCatalog, error)
	SaveCatalog(ctx context.Context, c *DeclarationCatalog) error
	// InsertDeclaration pose l'ÉCRITURE puis la déclaration, d'un seul tenant.
	InsertDeclaration(ctx context.Context, d *Declaration, e *Entry) error
	FindDeclaration(ctx context.Context, id primitive.ObjectID) (*Declaration, error)
	ListDeclarations(ctx context.Context, f DeclarationFilter, limit int, cursor string) ([]Declaration, string, error)
	// ReverseDeclaration pose la contre-passation et ferme la déclaration.
	ReverseDeclaration(ctx context.Context, d *Declaration, e *Entry) error
	LinkCorrection(ctx context.Context, corrected, by primitive.ObjectID) error
	// DeclarationsInWindow rend les déclarations dont la date de VALEUR ou la
	// date de CONTRE-PASSATION tombe dans la fenêtre.
	DeclarationsInWindow(ctx context.Context, from, to time.Time) ([]Declaration, error)
}

// --- le catalogue ---

func (r *Repository) Catalog(ctx context.Context, cc string) (*DeclarationCatalog, error) {
	var c DeclarationCatalog
	err := r.declCatalog.FindOne(ctx, bson.M{"_id": cc}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		d := DefaultDeclarationCatalog(cc)
		return &d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finance: declaration catalog: %w", err)
	}
	return &c, nil
}

func (r *Repository) SaveCatalog(ctx context.Context, c *DeclarationCatalog) error {
	_, err := r.declCatalog.ReplaceOne(ctx, bson.M{"_id": c.Country}, c, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("finance: save declaration catalog: %w", err)
	}
	return nil
}

// --- les déclarations ---

// InsertDeclaration écrit L'ÉCRITURE D'ABORD, puis la déclaration.
//
// ⚠️ CET ORDRE EST UNE DÉCISION, et il compte là où les transactions n'ont
// pas lieu (Mongo autonome, la machine de développement — voir
// `db.WithTransaction`). Si la seconde écriture échoue, il reste une écriture
// du journal dont la référence ne désigne rien : la balance générale reste
// équilibrée, le balayage d'intégrité reste vert, et la synthèse compte un
// peu MOINS que le journal. L'ordre inverse aurait laissé une déclaration
// sans écriture — la synthèse aurait alors annoncé un loyer payé dont le
// journal n'a aucune preuve, et c'est la seule des deux erreurs qui mente.
func (r *Repository) InsertDeclaration(ctx context.Context, d *Declaration, e *Entry) error {
	if d.ID.IsZero() {
		d.ID = primitive.NewObjectID()
	}
	return r.mongo.WithTransaction(ctx, func(txCtx context.Context) error {
		// ⚠️ LA PIÈCE EST VÉRIFIÉE AVANT QUE L'ÉCRITURE NE SOIT POSÉE. Poser
		// d'abord et se faire refuser ensuite par l'index laisserait, LÀ OÙ IL
		// N'Y A PAS DE TRANSACTION, une écriture orpheline du montant exact de
		// la dépense — un module de paie qui réessaie aurait gonflé le journal
		// d'un salaire sans qu'aucune déclaration ne le dise. L'index reste la
		// garantie contre la course ; cette lecture évite l'orphelin du cas
		// ordinaire, qui est le réessai.
		if err := r.assertDeclarationFree(txCtx, d); err != nil {
			return err
		}
		if e != nil {
			if err := r.InsertEntry(txCtx, e); err != nil {
				return err
			}
			id := e.ID
			d.EntryID = &id
		}
		if _, err := r.declarations.InsertOne(txCtx, d); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return errDeclarationDuplicate
			}
			return fmt.Errorf("finance: insert declaration: %w", err)
		}
		return nil
	})
}

// assertDeclarationFree dit si la pièce, ou la référence du module, a déjà
// été déclarée dans ce pays.
func (r *Repository) assertDeclarationFree(ctx context.Context, d *Declaration) error {
	var taken []bson.M
	if d.Reference != "" {
		taken = append(taken, bson.M{"reference": d.Reference})
	}
	if d.SourceRef != "" {
		taken = append(taken, bson.M{"source": d.Source, "source_ref": d.SourceRef})
	}
	if len(taken) == 0 {
		return nil
	}
	q := country.Restrict(ctx, bson.M{})
	q["$or"] = taken
	n, err := r.declarations.CountDocuments(ctx, q)
	if err != nil {
		return fmt.Errorf("finance: declaration reference: %w", err)
	}
	if n > 0 {
		return errDeclarationDuplicate
	}
	return nil
}

func (r *Repository) ReverseDeclaration(ctx context.Context, d *Declaration, e *Entry) error {
	return r.mongo.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := r.InsertEntry(txCtx, e); err != nil {
			return err
		}
		id := e.ID
		d.ReversalEntryID = &id
		// ⚠️ La mise à jour est CONDITIONNÉE au statut `posted` : deux
		// annulations lancées en même temps poseraient sinon deux
		// contre-passations pour une seule écriture, et la balance générale
		// basculerait de la valeur de la déclaration. Le filtre fait la
		// course à notre place.
		res, err := r.declarations.UpdateOne(txCtx,
			bson.M{"_id": d.ID, "status": DeclarationPosted},
			bson.M{"$set": bson.M{
				"status":            DeclarationReversed,
				"reversal_entry_id": id,
				"reversal_reason":   d.ReversalReason,
				"reversed_by":       d.ReversedBy,
				"reversed_at":       d.ReversedAt,
			}})
		if err != nil {
			return fmt.Errorf("finance: reverse declaration: %w", err)
		}
		if res.MatchedCount == 0 {
			return errDeclarationAlreadyReversed
		}
		return nil
	})
}

func (r *Repository) LinkCorrection(ctx context.Context, corrected, by primitive.ObjectID) error {
	_, err := r.declarations.UpdateOne(ctx, bson.M{"_id": corrected}, bson.M{"$set": bson.M{"corrected_by": by}})
	if err != nil {
		return fmt.Errorf("finance: link correction: %w", err)
	}
	return nil
}

// FindDeclaration rend une déclaration par son identifiant — BORNÉE PAR LE
// PAYS de la requête, parce qu'un identifiant deviné ne doit pas ouvrir la
// comptabilité d'un autre pays.
func (r *Repository) FindDeclaration(ctx context.Context, id primitive.ObjectID) (*Declaration, error) {
	q := country.Restrict(ctx, bson.M{"_id": id})
	var d Declaration
	err := r.declarations.FindOne(ctx, q).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errDeclarationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("finance: declaration: %w", err)
	}
	return &d, nil
}

// DeclarationFilter : ce que la console demande à la liste.
type DeclarationFilter struct {
	Direction string
	Category  string
	Means     string
	Status    string
	Source    string
	Search    string
	From      time.Time
	To        time.Time
}

// query rend le filtre, BORNÉ PAR LE PAYS de la requête — voir
// `EntryFilter.query` pour le pourquoi de `country.Restrict` ici plutôt que
// d'un `country.FromContext` recopié.
func (f DeclarationFilter) query(ctx context.Context) bson.M {
	q := country.Restrict(ctx, bson.M{})
	if f.Direction != "" {
		q["direction"] = f.Direction
	}
	if f.Category != "" {
		q["category"] = f.Category
	}
	if f.Means != "" {
		q["means"] = f.Means
	}
	if f.Status != "" {
		q["status"] = f.Status
	}
	if f.Source != "" {
		q["source"] = f.Source
	}
	if f.Search != "" {
		// La contrepartie et la pièce, cherchées littéralement : ce sont les
		// deux choses qu'on a sous les yeux quand on cherche une dépense.
		rx := primitive.Regex{Pattern: regexEscape(f.Search), Options: "i"}
		q["$or"] = bson.A{bson.M{"counterparty": rx}, bson.M{"reference": rx}}
	}
	if !f.From.IsZero() || !f.To.IsZero() {
		at := bson.M{}
		if !f.From.IsZero() {
			at["$gte"] = f.From
		}
		if !f.To.IsZero() {
			at["$lt"] = f.To
		}
		q["value_date"] = at
	}
	return q
}

// regexEscape neutralise ce qu'un terme de recherche pourrait contenir de
// significatif pour une expression régulière. ⚠️ Sans cela, un `(` tapé dans
// la barre de recherche fait répondre 500 au serveur, et un `.*` fait lire
// toute la collection.
func regexEscape(s string) string {
	const special = `\.+*?()|[]{}^$`
	out := make([]rune, 0, len(s)+8)
	for _, r := range s {
		for _, c := range special {
			if r == c {
				out = append(out, '\\')
				break
			}
		}
		out = append(out, r)
	}
	return string(out)
}

func (r *Repository) ListDeclarations(ctx context.Context, f DeclarationFilter, limit int, cursor string) ([]Declaration, string, error) {
	q := f.query(ctx)
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			q["_id"] = bson.M{"$lt": cid}
		}
	}
	cur, err := r.declarations.Find(ctx, q, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", fmt.Errorf("finance: list declarations: %w", err)
	}
	var out []Declaration
	if err := cur.All(ctx, &out); err != nil {
		return nil, "", fmt.Errorf("finance: list declarations decode: %w", err)
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID.Hex()
	}
	if out == nil {
		out = []Declaration{}
	}
	return out, next, nil
}

// DeclarationsInWindow rend les déclarations qui ont un FAIT dans la fenêtre :
// leur date de valeur, ou leur contre-passation.
//
// ⚠️ On rend les documents plutôt qu'un agrégat Mongo, et c'est délibéré. La
// règle de synthèse (quel signe, à quelle date, quelle catégorie) est la
// partie qu'on peut se tromper en écrivant ; l'avoir en Go la rend éprouvable
// ligne par ligne, et il n'y a qu'UNE implémentation à vérifier. Le volume le
// permet : une déclaration est un geste humain, pas une transaction.
func (r *Repository) DeclarationsInWindow(ctx context.Context, from, to time.Time) ([]Declaration, error) {
	window := func(field string) bson.M {
		at := bson.M{}
		if !from.IsZero() {
			at["$gte"] = from
		}
		if !to.IsZero() {
			at["$lt"] = to
		}
		return bson.M{field: at}
	}
	q := country.Restrict(ctx, bson.M{})
	q["$or"] = bson.A{window("value_date"), window("reversed_at")}
	cur, err := r.declarations.Find(ctx, q, options.Find().SetLimit(declarationSummaryMax))
	if err != nil {
		return nil, fmt.Errorf("finance: declarations in window: %w", err)
	}
	var out []Declaration
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("finance: declarations in window decode: %w", err)
	}
	return out, nil
}

// declarationSummaryMax borne la synthèse. ⚠️ Un plafond atteint est DIT dans
// la réponse (`truncated`) : une synthèse silencieusement tronquée est un
// faux total, et un faux total vaut moins que pas de total.
const declarationSummaryMax = 5000

// EnsureDeclarationIndexes pose les index des déclarations.
func (r *Repository) EnsureDeclarationIndexes(ctx context.Context) error {
	_, err := r.declarations.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// La liste de la console, bornée au pays.
		{Keys: bson.D{{Key: "country", Value: 1}, {Key: "_id", Value: -1}}},
		// La synthèse : les deux fenêtres de dates.
		{Keys: bson.D{{Key: "country", Value: 1}, {Key: "value_date", Value: -1}}},
		{Keys: bson.D{{Key: "country", Value: 1}, {Key: "reversed_at", Value: -1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "country", Value: 1}, {Key: "category", Value: 1}, {Key: "value_date", Value: -1}}},
		// ⚠️ LA PIÈCE EST UNIQUE PAR PAYS, et c'est CET index qui l'empêche
		// d'être déclarée deux fois — pas la vérification du service, qui ne
		// sert qu'à rendre un message lisible. Lire puis écrire laisse une
		// fenêtre : deux saisies simultanées liraient toutes deux « absente ».
		//
		// ⚠️ LE FILTRE PARTIEL DOIT MORDRE SUR CE QU'ON ÉCRIT. `reference` est
		// marquée `omitempty` : une pièce vide est ABSENTE du document, donc
		// hors du filtre, donc non contrainte — c'est exactement ce qu'on veut,
		// et c'est faux dès que quelqu'un retire le `omitempty`. (`$gt: ""`
		// sélectionne les chaînes non vides ; `$ne` n'est pas admis dans un
		// filtre partiel.)
		{
			Keys:    bson.D{{Key: "country", Value: 1}, {Key: "reference", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.D{{Key: "reference", Value: bson.D{{Key: "$gt", Value: ""}}}}),
		},
		// La COUTURE des modules : une référence de module ne se déclare
		// qu'une fois, pour qu'un réessai ne double pas un salaire.
		{
			Keys:    bson.D{{Key: "country", Value: 1}, {Key: "source", Value: 1}, {Key: "source_ref", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.D{{Key: "source_ref", Value: bson.D{{Key: "$gt", Value: ""}}}}),
		},
	})
	if err != nil {
		return err
	}
	return nil
}
