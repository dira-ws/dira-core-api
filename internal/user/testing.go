package user

// LES COMPTES D'ESSAI.
//
// Un pays peut être réservé aux essais (`testing` dans `GET /countries`) : tout
// ce qui s'y passe sort de la supervision, des rapports et des classements. Ce
// fichier porte la même idée à l'échelle d'un COMPTE : l'équipe marque un
// livreur émulé, un client de recette, et ce que ce compte fait naître est un
// essai — dans un pays qui, lui, reste réel pour tous les autres.
//
// ⚠️ LA MARQUE VOYAGE DANS LE JETON (`tst`, voir `grantFor`) : la verticale qui
// crée une commande la lit sans demander au socle, et la commande la recopie.
// Tout ce qui en naît ensuite — la course, ses faits, ses gains — la recopie de
// l'objet, jamais de la requête d'un autre (`auth.Claims.Testing`).

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// errTestingNotApplicable : la marque n'a de sens que pour un compte qui FAIT
// NAÎTRE de l'activité — un client qui commande, un livreur ou un chauffeur
// qui la porte.
//
// ⚠️ REFUSÉE AILLEURS PLUTÔT QUE POSÉE POUR RIEN. Sur un compte de staff ou de
// marchand, rien ne la lit : une case cochée qui ne fait rien se lit, à la
// relecture, comme une garantie qui existe.
var errTestingNotApplicable = apperr.Conflict("testing_not_applicable",
	"only client and driver accounts can be marked as testing")

// SetAccountTesting marque un compte d'essai, ou l'en sort, et rend l'état
// d'AVANT.
//
// IDEMPOTENTE : poser ce qui est déjà posé réussit sans rien écrire et sans rien
// tracer — un geste d'administration qui échoue parce qu'il a déjà été fait
// pousse à s'acharner.
//
// ⚠️ TRACÉE au journal d'audit : sortir un compte des chiffres est un geste qui
// fait DISPARAÎTRE de l'activité des rapports. « Qui a marqué ce compte, et
// quand ? » est une question qui sera posée le jour où un chiffre manque.
//
// ⚠️ ELLE NE COUPE AUCUNE SESSION. Le marqueur s'applique au prochain jeton —
// la connexion suivante ou le prochain rafraîchissement, quinze minutes au
// plus : marquer d'abord, puis faire la recette.
func (s *Service) SetAccountTesting(ctx context.Context, id string, testing bool) (bool, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false, errUserNotFound.WithCause(err)
	}
	u, err := s.repo.FindByID(ctx, oid)
	if err != nil {
		return false, apperr.Internal(err)
	}
	if u == nil {
		return false, errUserNotFound
	}
	before := u.Testing
	if before == testing {
		return before, nil
	}
	// ⚠️ SEULE LA POSE SE REFUSE : retirer la marque d'un compte qui l'aurait
	// gardée d'avant un changement de rôle doit toujours rester possible.
	if testing && u.Role != auth.RoleClient && u.Role != auth.RoleDriver {
		return before, errTestingNotApplicable.WithMeta(map[string]any{"reason": u.Role})
	}
	if err := s.repo.SetTesting(ctx, oid, testing); err != nil {
		return before, apperr.Internal(err)
	}
	if s.auditor != nil {
		s.auditor.Record(ctx, "user.testing", "user", id,
			map[string]any{"testing": before}, map[string]any{"testing": testing})
	}
	return before, nil
}
