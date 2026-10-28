package middleware

// L'ÉDITION DE L'APPLICATION QUI APPELLE — voir `pkg/edition`.

import (
	"net/http"

	"github.com/kgtech-org/dira-core-api/pkg/edition"
	"github.com/kgtech-org/dira-core-api/pkg/obs"
)

// Edition lit `X-Dira-Edition` et l'inscrit dans le contexte.
//
// ⚠️ SANS JAMAIS REFUSER. L'édition est DÉCLARATIVE : une valeur absente,
// inconnue, ou venue d'une version future retombe sur `full` — l'application
// complète, c'est-à-dire le comportement d'avant. Un refus ici aurait cassé
// tout le parc installé le jour du déploiement, pour un en-tête qui ne sert
// qu'à adapter une liste et à compter.
//
// ⚠️ ET ELLE EST REPUBLIÉE DANS LA RÉPONSE, comme le pays. C'est ce qui permet
// à une application de voir ce que le serveur a RETENU : une application qui
// se croit Lite et reçoit `full` sait immédiatement que son en-tête n'arrive
// pas (une passerelle qui le filtre, un cache qui le mange), au lieu de
// chercher pendant deux jours pourquoi sa configuration n'est pas réduite.
func Edition() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get(edition.Header)
			if raw == "" {
				// ⚠️ LE PARAMÈTRE D'URL EN SECOND, pour le WebSocket du suivi
				// et les liens : un navigateur ne pose pas d'en-tête sur une
				// ouverture de socket.
				raw = r.URL.Query().Get(edition.QueryParam)
			}
			ed := edition.Normalize(raw)
			ctx := edition.WithEdition(r.Context(),
				ed, r.Header.Get(edition.VersionHeader), r.Header.Get(edition.PlatformHeader))
			w.Header().Set(edition.Header, ed)
			// ⚠️ PUBLIÉE POUR LES JOURNAUX, parce que « Lite ne marche pas »
			// est une phrase qu'on recevra, et qu'il faudra pouvoir isoler
			// les appels de Lite dans la supervision sans deviner.
			ctx = obs.WithField(ctx, "edition", ed)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
