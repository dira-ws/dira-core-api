// Package appconfig sert et règle CE QU'UNE APPLICATION VOIT — par pays et
// par édition.
//
// La résolution elle-même vit dans `pkg/appconfig`, pure : c'est elle qui
// décide ce qui disparaît d'un écran, et elle doit s'éprouver sans base. Ce
// module-ci ne fait que la ranger, la servir, et la laisser modifier depuis la
// console.
//
// ⚠️ UN SEUL DOCUMENT, comme les autres réglages de plateforme. Le registre
// tient dans quelques kilooctets et se lit à chaque appel de `/app/config` :
// une collection par règle aurait multiplié les lectures sans rien gagner.
package appconfig

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/appconfig"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Collection garde le registre.
const Collection = "app_config"

// docID : le registre est UNIQUE, et son identifiant est fixe.
const docID = "registry"

// Document est le registre tel qu'il est rangé.
type Document struct {
	ID                 string `bson:"_id"`
	appconfig.Registry `bson:",inline"`
	UpdatedAt          time.Time `bson:"updated_at"`
	UpdatedBy          string    `bson:"updated_by,omitempty"`
}

// Repository lit et écrit le registre.
type Repository struct{ col *mongo.Collection }

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{col: m.Collection(Collection)}
}

// Load rend le registre — VIDE s'il n'existe pas encore.
//
// ⚠️⚠️ UN REGISTRE ABSENT N'EST PAS UNE ERREUR, et c'est ce qui rend ce module
// sûr à déployer. Tant que personne n'a rien restreint, il n'y a pas de
// document : rendre une erreur aurait fait échouer `/app/config` sur toute
// plateforme qui n'a pas encore touché à cet écran — c'est-à-dire toutes, le
// jour du déploiement.
func (r *Repository) Load(ctx context.Context) (*appconfig.Registry, error) {
	var doc Document
	err := r.col.FindOne(ctx, bson.M{"_id": docID}).Decode(&doc)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return &appconfig.Registry{}, nil
	case err != nil:
		return nil, fmt.Errorf("appconfig: load: %w", err)
	}
	return &doc.Registry, nil
}

// Save remplace le registre.
//
// ⚠️ IL REMPLACE, et l'écran de la console doit le savoir : c'est la même
// règle que les réglages d'IA, et elle a déjà surpris une fois. Un écran qui
// n'envoie que ce qu'il a modifié EFFACE tout le reste.
func (r *Repository) Save(ctx context.Context, reg appconfig.Registry, actor string) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": docID}, bson.M{
		"$set": bson.M{
			"features": reg.Features,
			"lists":    reg.ListRules,
			// ⚠️ CE `$set` NOMME SES CHAMPS UN PAR UN : un champ ajouté à la
			// `Registry` et oublié ici ne s'écrit JAMAIS, et rien n'échoue —
			// l'écran de la console affiche le réglage qu'il vient d'envoyer,
			// jusqu'au prochain rechargement. Une épreuve de parité tient
			// cette liste à jour.
			"live":       reg.Live,
			"updated_at": time.Now().UTC(),
			"updated_by": actor,
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("appconfig: save: %w", err)
	}
	return nil
}

// Meta rend qui a touché au registre, et quand — pour l'écran de la console.
func (r *Repository) Meta(ctx context.Context) (at *time.Time, by string) {
	var doc Document
	if err := r.col.FindOne(ctx, bson.M{"_id": docID}).Decode(&doc); err != nil {
		return nil, ""
	}
	if doc.UpdatedAt.IsZero() {
		// ⚠️ UNE DATE NULLE NE SORT PAS : Go la sérialise en `0001-01-01`, et
		// un écran qui la formate affiche « 31/12/0000 ». Troisième fois que
		// ce piège se présente sur cette plateforme.
		return nil, doc.UpdatedBy
	}
	return &doc.UpdatedAt, doc.UpdatedBy
}
