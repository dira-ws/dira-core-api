package middleware

// REFUSER UNE ROUTE QUE CETTE ÉDITION N'A PLUS — déclaré à côté du rôle et de
// la portée, pas éparpillé dans les poignées.
//
// ⚠️⚠️ UN INTERGICIEL, ET C'EST LA DÉCISION DE CE FICHIER. Écrire `Require`
// dans chaque poignée revient à s'en souvenir à chaque fois : la route ajoutée
// demain au même groupe ne l'aurait pas, et le trou serait invisible — une
// route qui répond `200` n'a l'air de rien. Monté sur le GROUPE, le garde
// couvre ce qui existe et ce qui viendra.
//
// ⚠️ ET IL NE SERT À RIEN TANT QU'IL N'EST PAS MONTÉ. C'est exactement ce qui
// vient d'arriver au socle : `appconfig.Service` savait refuser depuis le
// premier jour, personne ne l'appelait, et `/app/config` annonçait
// `wallet: false` pendant que `GET /wallet` rendait le solde (#251). Les
// épreuves de ce garde regardent donc les ROUTES MONTÉES, pas l'intergiciel
// seul.

import (
	"context"
	"net/http"

	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// FeatureGate dit si l'appel a encore droit à une fonction.
//
// ⚠️ UNE INTERFACE, POUR QUE LES VERTICALES S'EN SERVENT AUSSI. Le socle la
// satisfait avec `internal/appconfig`, les courses et la livraison avec leur
// pont vers `GET /internal/app-config` : un seul intergiciel, un seul refus,
// une seule façon de se tromper plutôt que trois.
type FeatureGate interface {
	// Require rend le refus NOMMÉ du paquet partagé, ou nil.
	Require(ctx context.Context, feature string) error
}

// RequireFeature refuse si cette édition (et ce pays) n'ont plus la fonction.
//
// ⚠️ UNE PORTE NULLE LAISSE PASSER. Un service monté sans registre — un
// développement sans socle, une verticale dont le pont n'est pas configuré —
// ne doit pas refuser TOUT : le défaut de cette couche est « disponible »,
// partout et toujours.
func RequireFeature(gate FeatureGate, feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if gate == nil {
				next.ServeHTTP(w, r)
				return
			}
			if err := gate.Require(r.Context(), feature); err != nil {
				// ⚠️ LE REFUS DU PAQUET PARTAGÉ REMONTE TEL QUEL : son code est
				// ce que les applications lisent pour cacher un bouton, et
				// `meta.feature` dit lequel. L'aplatir en « 403 » ferait
				// proposer une reconnexion pour un compte qui n'a rien.
				httpx.Error(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
