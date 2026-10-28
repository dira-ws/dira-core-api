package appconfig

// LE REFUS D'UNE FONCTION RETIRÉE — ici, et nulle part ailleurs.
//
// ⚠️⚠️ AU PAQUET PARTAGÉ, PAS AU SOCLE, et c'est tout l'objet de ce fichier.
// Trois services refusent cette même chose : le socle pour le portefeuille et
// les codes promo, les courses pour la location et le partage de trajet, la
// livraison pour le fil des plats. Écrit trois fois, le code finirait par
// différer d'un service à l'autre — et une application qui sait cacher un
// bouton sur `feature_unavailable_in_edition` ne saurait rien faire du
// `feature_disabled` qu'une verticale aurait inventé de son côté.
//
// ⚠️ ET CE N'EST PAS UNE INQUIÉTUDE DE PRINCIPE : une règle écrite deux fois
// est perdue une fois. Celle-ci a déjà vécu dans `internal/appconfig`, hors
// d'atteinte des verticales, qui s'apprêtaient donc à la recopier.

import (
	"net/http"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// ErrFeatureUnavailable refuse une action vers une fonction retirée de cette
// édition ou de ce pays.
//
// ⚠️⚠️ UN REFUS NOMMÉ, et il faut qu'il le reste. « 403 » tout court se lit
// comme un problème de droits : l'application proposerait de se reconnecter,
// et la personne appellerait le support pour un compte qui n'a rien. Le code
// dit la vraie raison — cette fonction n'existe pas dans cette application —
// et `meta.feature` dit laquelle, pour que l'écran la cache au prochain
// démarrage plutôt que de reproposer un bouton qui échoue.
//
// ⚠️ ET LA PHRASE EST COURTE EXPRÈS : Lite la LIT À VOIX HAUTE (#239). Une
// phrase de trois lignes, lue par une synthèse, perd la personne avant la fin.
var ErrFeatureUnavailable = apperr.New("feature_unavailable_in_edition",
	"this feature is not available in this app", http.StatusForbidden)

// Refuse compose le refus d'une fonction, en nommant laquelle.
func Refuse(feature string) *apperr.Error {
	return ErrFeatureUnavailable.WithMeta(map[string]any{"feature": feature})
}
