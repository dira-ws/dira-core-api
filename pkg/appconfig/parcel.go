package appconfig

// LE COLIS — la livraison de colis, ouverte pays par pays.
//
// ⚠️⚠️ CELUI-LÀ S'ALLUME, IL NE SE COUPE PAS, comme le direct (`live.go`) : un
// pays n'est ouvert que s'il est NOMMÉ dans `Registry.Parcel`, et la fonction
// du catalogue ne sert qu'à le retirer ENSUITE d'une édition — « le colis au
// Togo, mais pas dans Lite ». Un défaut « disponible partout » aurait ouvert,
// le jour du déploiement, la vente d'envois dans quatorze pays où il n'y a ni
// barème, ni livreur qui l'ait activé.
//
// ⚠️⚠️ ET C'EST UN VERROU, PAS UN RÉGLAGE D'AFFICHAGE. Toutes les routes du colis
// le lisent — celles du particulier, de l'entreprise et la page publique : un
// pays fermé ne vend rien, même à une application qui ne lit pas
// `/app/config`.
//
// ⚠️ D'OÙ UNE RÈGLE DE LECTURE : `ParcelOpen`, JAMAIS `Allows`. `Allows` (et
// `Require`, et `middleware.RequireFeature`) dit si une ÉDITION a perdu la
// fonction, et répond « oui » quand rien n'est réglé — la bonne réponse pour
// tout le catalogue, la mauvaise pour un verrou.

import (
	"sort"
	"strings"
)

// ParcelOpen dit si le colis est ouvert dans ce pays, pour cette édition.
//
// ⚠️ FERMÉ PAR DÉFAUT : un registre nil, une liste vide, un pays absent ou vide
// — c'est « pas ici ». Et le pays d'abord, l'édition ensuite : une règle de
// catalogue qui ne vise pas ce pays ne l'ouvre pas, elle ne fait que le
// retirer d'une édition.
func (r *Registry) ParcelOpen(countryCode, ed string) bool {
	// ⚠️ `listed` rend VRAI sur une liste vide (« aucune restriction ») : ici,
	// une liste vide veut dire « ouvert nulle part », d'où le garde d'abord.
	code := strings.ToUpper(strings.TrimSpace(countryCode))
	if r == nil || code == "" || len(r.Parcel) == 0 || !listed(r.Parcel, code) {
		return false
	}
	return r.Allows(FeatureParcel, code, ed)
}

// CleanCountries normalise une liste de pays OUVERTS : majuscules, sans
// doublon, triée. Rend le premier code qui n'a pas deux lettres.
//
// ⚠️ TRIÉE, pour qu'une relecture de la console compare deux réponses sans faux
// écart ; SANS DOUBLON, parce qu'un pays coché deux fois et décoché une fois
// resterait ouvert.
func CleanCountries(in []string) ([]string, string) {
	if len(in) == 0 {
		return nil, ""
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		code := strings.ToUpper(strings.TrimSpace(raw))
		if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
			return nil, raw
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	sort.Strings(out)
	return out, ""
}
