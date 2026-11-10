package appconfig

// L'IMMOBILIER DANS L'APPLICATION CLIENTE — l'onglet qui s'ouvre pays par pays.
//
// ⚠️⚠️ CELUI-LÀ S'ALLUME, IL NE SE COUPE PAS — la seconde exception de ce
// paquet, après le direct (`live.go`), et pour une raison différente. Le
// direct coûte à la seconde ; l'immobilier, lui, ne coûte rien, mais il n'a
// de sens QUE là où des annonces existent. Une verticale accepte les quatorze
// pays du catalogue, et n'a de biens qu'au Gabon et au Tchad : un défaut
// « disponible partout » aurait posé, le jour du déploiement, un onglet
// « Logement » dans douze pays où toute recherche répond « aucune annonce ».
// Un onglet vide se lit « Dira ne marche pas ici », et c'est faux pour les
// courses et les repas du même écran.
//
// Il s'allume donc par la PRÉSENCE du pays dans `Registry.Immo`, et la
// fonction du catalogue ne sert qu'à le retirer ENSUITE d'une édition —
// « l'immobilier au Gabon, mais pas dans Lite ».
//
// ⚠️ C'EST UN RÉGLAGE D'AFFICHAGE, PAS UN VERROU. Les routes de
// `dira-immo-api` ne le lisent pas : le site public sert les pays qu'il sert,
// et une recherche dans un pays fermé rend une liste vide, sans rien exposer.
// Le couper ne retire rien à personne d'autre que l'application qui le lit.

import (
	"sort"
	"strings"
)

// ImmoOpen dit si l'immobilier est ouvert dans ce pays, pour cette édition.
//
// ⚠️ LE PAYS D'ABORD, L'ÉDITION ENSUITE : une règle de catalogue qui ne vise
// pas ce pays ne l'ouvre pas — elle ne fait que le retirer d'une édition.
func (r *Registry) ImmoOpen(countryCode, ed string) bool {
	// ⚠️ `listed` rend VRAI sur une liste vide (« aucune restriction ») : ici,
	// une liste vide veut dire « ouvert nulle part », d'où le garde d'abord.
	if r == nil || len(r.Immo) == 0 || !listed(r.Immo, strings.ToUpper(strings.TrimSpace(countryCode))) {
		return false
	}
	return r.Allows(FeatureImmo, countryCode, ed)
}

// CleanCountries normalise une liste de pays ouverts : majuscules, sans
// doublon, triée. Rend le premier code qui n'a pas deux lettres.
//
// ⚠️ TRIÉE, pour qu'une relecture de la console compare deux réponses sans
// faux écart ; SANS DOUBLON, parce qu'un pays coché deux fois et décoché une
// fois resterait ouvert.
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
