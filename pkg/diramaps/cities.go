package diramaps

// LA VILLE D'UN PAYS — pour que CHERCHER UNE ADRESSE cherche au bon endroit.
//
// ⚠️⚠️ CE FICHIER EXISTE À CAUSE D'UN DÉFAUT MESURÉ. Le 29 octobre 2026, un
// client du GABON a demandé « un taxi pour aller à Glass depuis Louis » — deux
// quartiers de Libreville. Il a reçu cinq devis de LOMÉ, à 2 000 km, en
// `country: "TG"` : « Residence Louise, Boulevard des armées, Lomé » et
// « Festival des Glaces, Rue de L'Entente, Lomé ». L'assistant en direct a
// prononcé le prix à voix haute et demandé « je confirme ? ».
//
// La cause : la ville de recherche venait d'une VARIABLE D'ENVIRONNEMENT du
// service (`MAPS_CITY`, « lome » par défaut), la même pour les quatorze pays.
// Le champ était documenté comme une clé de CACHE — « elle ne change pas le
// calcul » —, ce qui est vrai pour un itinéraire, dont les coordonnées sont
// données, et FAUX pour un géocodage, où elle décide de la zone cherchée. Un
// seul nom pour deux rôles opposés.
//
// ⚠️ ET LA TABLE NE RÉPOND PAS POUR LES PAYS QU'ELLE NE CONNAÎT PAS. C'est la
// décision importante : hors de ces pays, il n'y a PAS de ville de repli.
// Chercher « Louis » à Lomé pour quelqu'un au Bénin rendrait un résultat
// plausible, précis, et faux — et un résultat faux voyage ensuite jusqu'au
// prix débité. Aucune réponse se dit ; une mauvaise réponse se paie.

import "strings"

// cities : le pays (ISO 3166-1 alpha-2) → la ville servie par dira-maps.
//
// ⚠️ UNE VILLE PAR PAYS, PARCE QUE C'EST CE QUE LE SIG SERT. dira-maps tient
// cinq villes, une par pays ouvert. Le jour où un pays en aura deux, cette
// table devra rendre celle de la POSITION de la personne, pas celle du pays —
// et ce sera un changement de signature, visible, plutôt qu'un défaut muet.
var cities = map[string]string{
	"SN": "dakar",
	"TG": "lome",
	"GN": "conakry",
	"TD": "ndjamena",
	"GA": "libreville",
}

// CityOf rend la ville de ce pays, et dit si on la connaît.
//
// ⚠️⚠️ LE SECOND RETOUR N'EST PAS DÉCORATIF. Un appelant qui l'ignore et passe
// la chaîne vide au SIG retombe sur le défaut du service — c'est-à-dire sur le
// défaut qu'on vient de corriger. Les deux usages se distinguent :
//
//   - pour un ITINÉRAIRE, la ville est une clé de cache : une ville inconnue
//     coûte un calcul de plus, et rien d'autre ;
//   - pour une RECHERCHE D'ADRESSE, elle est la zone cherchée : une ville
//     inconnue doit faire RENONCER à chercher.
func CityOf(countryCode string) (string, bool) {
	city, ok := cities[strings.ToUpper(strings.TrimSpace(countryCode))]
	return city, ok
}

// Cities rend les pays servis par le SIG — pour un diagnostic, un écran
// d'exploitation, ou un refus qui dit où le service existe.
func Cities() map[string]string {
	out := make(map[string]string, len(cities))
	for k, v := range cities {
		out[k] = v
	}
	return out
}
