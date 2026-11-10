package notify

// L'APPLICATION DIRA IMMO — un second téléphone pour le MÊME compte client.
//
// Le chercheur de logement a son application, Dira Immo, distincte de
// l'application cliente des repas et des courses, et il y entre avec le MÊME
// compte `client` (architecture de l'immobilier : aucun nouveau compte). Sans
// rien pour les distinguer, chaque notification partait vers TOUS ses jetons :
// « votre course est acceptée » s'affichait dans Dira Immo, « visite
// confirmée » dans l'application des courses — c'est-à-dire, chaque fois, dans
// une application qui ne sait pas l'ouvrir.
//
// ⚠️ C'EST LE JETON QUI DIT SON APPLICATION, comme son édition : une même
// personne peut avoir les deux sur le même téléphone. Et c'est LA CLÉ du
// gabarit qui dit à quelle application un message s'adresse — le préfixe
// `immo_`, qu'aucun autre métier n'emploie.

import (
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// AppImmo : le jeton appartient à l'application Dira Immo (le chercheur).
//
// ⚠️ LA SEULE VALEUR. Absent = l'application du rôle du compte, c'est-à-dire
// le comportement d'avant : tous les jetons déjà enregistrés en sont, et ils ne
// doivent rien perdre. L'application des annonceurs (`immo_pro`) n'en a pas
// besoin : un compte `publisher` n'a qu'elle.
const AppImmo = "immo"

// immoKeyPrefix : les clés de gabarit de l'immobilier.
const immoKeyPrefix = "immo_"

// AppOfKey dit quelle application sert ce message : `immo` pour une clé de
// l'immobilier, "" pour toutes les autres.
func AppOfKey(key string) string {
	if strings.HasPrefix(key, immoKeyPrefix) {
		return AppImmo
	}
	return ""
}

// routeByApp garde, parmi les appareils d'un compte, ceux qui savent ouvrir ce
// message.
//
// ⚠️⚠️ L'ASYMÉTRIE EST VOULUE, et c'est toute la règle :
//   - un message de l'immobilier va aux jetons de Dira Immo, et REPLIE sur les
//     autres quand il n'y en a aucun : quelqu'un qui a demandé sa visite depuis
//     le site, sans l'application, doit quand même apprendre qu'elle est
//     confirmée — mieux vaut une notification qu'il ouvre dans la boîte qu'un
//     silence (la règle de `NotifyEdition`) ;
//   - un message d'un AUTRE métier ne va JAMAIS à Dira Immo, même quand elle est
//     le seul téléphone : « votre livreur arrive » dans une application qui ne
//     montre ni livreur ni commande est un message qu'on ne peut que balayer.
//     Il reste dans la boîte du compte.
func routeByApp(devices []Device, key string) []Device {
	immo := AppOfKey(key) == AppImmo
	mine := make([]Device, 0, len(devices))
	others := make([]Device, 0, len(devices))
	for _, d := range devices {
		if d.App == AppImmo {
			mine = append(mine, d)
		} else {
			others = append(others, d)
		}
	}
	if immo && len(mine) > 0 {
		return mine
	}
	return others
}

// ParseApp valide le paramètre `app` d'une lecture de la boîte.
//
// ⚠️ UNE VALEUR INCONNUE EST REFUSÉE, PAS IGNORÉE : `?app=immobilier` qui
// rendrait toute la boîte ferait afficher, dans Dira Immo, les commandes de
// repas de la personne — l'écart que ce paramètre existe pour fermer.
func ParseApp(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return "", nil
	case AppImmo:
		return AppImmo, nil
	}
	return "", apperr.Validation("unknown app: " + raw).
		WithMeta(map[string]any{"fields": []string{"app"}, "reason": raw})
}

// inboxScope borne un filtre de la boîte à une application.
//
// ⚠️ PAR LE PRÉFIXE DE LA CLÉ, sans champ ajouté aux entrées : les
// notifications déjà archivées se rangent ainsi d'elles-mêmes, sans migration.
// Sans `app`, la boîte reste ENTIÈRE — celle de l'application cliente.
func inboxScope(filter bson.M, app string) bson.M {
	if app == AppImmo {
		filter["key"] = primitive.Regex{Pattern: "^" + immoKeyPrefix}
	}
	return filter
}
