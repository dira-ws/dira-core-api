package appconfig

// CE QUE VOIT UNE APPLICATION — la résolution, pure et éprouvable sans base.
//
// ⚠️ PURE ET À PART, parce que c'est elle qui décide de ce qui disparaît d'un
// écran. Une fonction retirée par erreur ne lève aucune erreur : elle laisse
// un menu plus court, et personne ne s'en plaint avant des semaines. Elle doit
// donc s'éprouver ligne par ligne, sans Mongo et sans requête HTTP.

import (
	"sort"
	"strings"

	"github.com/kgtech-org/dira-core-api/pkg/edition"
)

// Rule restreint UNE fonction ou UNE liste.
//
// ⚠️⚠️ UNE RÈGLE DIT QUI PERD QUELQUE CHOSE, ET OÙ — jamais « qui y a droit ».
// La première version disait l'inverse : deux listes de PERMISSIONS, éditions
// et pays, qu'il fallait satisfaire toutes les deux. Écrit comme ça, le
// réglage que cette couche existe pour porter — « retirer la location de Lite,
// au Tchad » — n'est pas exprimable : il faudrait dire « disponible dans
// l'édition normale, OU dans n'importe quelle édition hors du Tchad », une
// disjonction qui n'a pas de place dans le schéma.
//
// Et ce n'est pas resté théorique. En vérifiant la préprod, j'ai écrit ce que
// tout le monde écrira :
//
//	{"rental": {"editions": ["full"], "countries": ["GA"]}}
//
// en croyant dire « pas de location dans Lite, au Gabon ». Ça disait « la
// location n'existe QUE pour l'édition normale, et QUE au Gabon » : l'app
// NORMALE a perdu la location au Togo, au Sénégal et en Guinée — trois pays
// réels, par une écriture qui se lisait comme un réglage gabonais. Voir #245.
//
// Avec « qui perd, et où », nommer un pays ne peut plus que RÉTRÉCIR la portée
// d'une coupure. Le défaut ci-dessus devient inexprimable.
type Rule struct {
	// Editions : les éditions QUI PERDENT cette fonction — ou, pour une
	// liste, celles qui ne voient que `Allow`.
	//
	// ⚠️ VIDE = AUCUNE, et cette valeur par défaut n'est pas négociable : la
	// `Rule` nulle est ce qu'on obtient pour une clé ABSENTE de la carte, et
	// elle doit alors ne rien retirer. Si vide voulait dire « toutes », chaque
	// fonction du catalogue serait coupée partout dès qu'une seule autre est
	// réglée.
	Editions []string `bson:"editions,omitempty" json:"editions,omitempty"`
	// Countries : les pays OÙ CETTE RESTRICTION S'APPLIQUE. VIDE = partout.
	//
	// ⚠️ UNE PORTÉE, PAS UNE PERMISSION. Hors de ces pays, la règle ne retire
	// rien du tout — ni à Lite, ni à l'application normale.
	//
	// ⚠️ ET EN PLUS des réglages de pays que chaque module tient déjà. Celui-ci
	// ne remplace pas « le partage de trajet est éteint au Togo » : il permet
	// de dire « et il est éteint dans Lite, partout ». Les deux se cumulent,
	// et le plus restrictif gagne — une fonction que son module éteint reste
	// éteinte quoi qu'on écrive ici.
	Countries []string `bson:"countries,omitempty" json:"countries,omitempty"`
	// Allow : pour une LISTE, les seules entrées que les éditions nommées
	// voient. Vide = la règle ne restreint aucune valeur.
	Allow []string `bson:"allow,omitempty" json:"allow,omitempty"`
}

// Restricts dit si cette règle retire quelque chose à quelqu'un.
//
// ⚠️ C'EST CE QUI REFUSE UN RÉGLAGE MORT À L'ÉCRITURE. Une règle sans édition
// nommée ne retire rien ; une règle de liste sans `Allow` non plus. Les deux
// s'enregistraient sans broncher : la case était décochée à l'écran et le
// serveur servait tout. Tout #245 est fait de réglages posés qui ne
// s'appliquent pas — celui-là se refuse maintenant par le nom.
func (rule Rule) Restricts(isList bool) bool {
	if len(rule.Editions) == 0 {
		return false
	}
	if isList {
		return len(rule.Allow) > 0
	}
	return true
}

// Registry est l'ensemble des règles — un seul document, comme les autres
// réglages de plateforme.
type Registry struct {
	// Features : clé du catalogue → sa règle. Absente = disponible partout.
	Features map[string]Rule `bson:"features,omitempty" json:"features,omitempty"`
	// ListRules : clé de liste → sa règle (c'est `Allow` qui y compte).
	ListRules map[string]Rule `bson:"lists,omitempty" json:"lists,omitempty"`
}

// Allows dit si une fonction est disponible pour ce pays et cette édition.
//
// ⚠️ LE DÉFAUT EST OUI, ET C'EST LA PROPRIÉTÉ QUI REND CETTE COUCHE SÛRE À
// AJOUTER. Un registre vide, une clé inconnue, un registre nil : tout est
// disponible. La plateforme se comporte donc exactement comme avant jusqu'à ce
// que quelqu'un restreigne explicitement.
func (r *Registry) Allows(key, countryCode, ed string) bool {
	if r == nil {
		return true
	}
	// ⚠️ UNE CLÉ ABSENTE DU REGISTRE PASSE PAR LA RÈGLE NULLE, et c'est voulu :
	// une `Rule` vide ne nomme aucune édition, et `cuts` ne retire alors rien.
	// Un test explicite `if !ok { return true }` aurait dit la même chose deux
	// fois — et une mutation l'a montré en passant au vert. Une garde qu'on ne
	// peut pas faire échouer ne garde rien.
	return !r.Features[key].cuts(countryCode, ed)
}

// cuts dit si cette règle mord ICI : dans ce pays, sur cette édition.
func (rule Rule) cuts(countryCode, ed string) bool {
	// ⚠️ LA PORTÉE D'ABORD. Hors des pays nommés, la règle ne retire rien —
	// c'est tout l'objet du correctif de #245 : nommer un pays RÉTRÉCIT une
	// coupure, il ne peut plus en créer une ailleurs.
	if !listed(rule.Countries, strings.ToUpper(strings.TrimSpace(countryCode))) {
		return false
	}
	// ⚠️ ET `listed` NE CONVIENT PAS POUR LES ÉDITIONS : une liste vide y veut
	// dire « toutes », alors qu'ici elle veut dire « aucune ». La `Rule` nulle
	// doit rester inerte.
	if len(rule.Editions) == 0 {
		return false
	}
	return listed(rule.Editions, edition.Normalize(ed))
}

// listed : vide = tout est permis ; sinon il faut y être.
//
// ⚠️ LA CASSE NE DOIT PAS DÉCIDER. Un pays écrit `tg` dans la console et `TG`
// dans un jeton est le MÊME pays : comparer tel quel aurait retiré la fonction
// à moitié des requêtes, de façon parfaitement reproductible et parfaitement
// inexplicable.
func listed(allowed []string, want string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), want) {
			return true
		}
	}
	return false
}

// AllowedIn réduit une liste servie par un module à ce que cette édition et ce
// pays ont le droit de voir.
//
// ⚠️⚠️ UNE INTERSECTION, ET L'ORDRE DU MODULE EST CONSERVÉ. Le module range sa
// liste exprès — le mode mis en tête est celui qu'on propose d'abord, la
// classe la moins chère vient avant. Rendre l'ordre de la liste blanche aurait
// réordonné des écrans que personne n'a demandé à réordonner.
//
// ⚠️ ET UNE LISTE BLANCHE QUI NE CROISE RIEN REND LA LISTE ENTIÈRE, PAS UNE
// LISTE VIDE. C'est le garde-fou le plus important de ce fichier : une faute
// de frappe dans la console (`moto,` avec une virgule, `Eco` au lieu de `eco`)
// aurait rendu zéro mode de véhicule — une application passager sans aucune
// voiture à commander, pour un pays entier, sans aucune erreur nulle part.
// Mieux vaut une restriction qui ne s'applique pas qu'un produit injouable.
func (r *Registry) AllowedIn(list, countryCode, ed string, served []string) []string {
	if r == nil || len(served) == 0 {
		return served
	}
	// ⚠️ MÊME RAISON QUE DANS `Allows` : une liste absente du registre, ou une
	// règle sans édition nommée, passe par la règle nulle et ne restreint
	// rien. Les deux cas se traitaient explicitement ici, et la mutation qui
	// retirait ce traitement passait au vert.
	rule := r.ListRules[list]
	// ⚠️ LA MÊME POLARITÉ QUE LES FONCTIONS, et c'est le second correctif de
	// #245. Avant, `editions` nommait ici les éditions RESTREINTES et là-haut
	// celles qui GARDAIENT la fonction : un seul document, deux sens opposés
	// pour un seul nom de champ. La console écrivait la polarité des fonctions
	// dans les deux moitiés — « retirer les modes de Lite » s'enregistrait et
	// ne faisait rien, la case décochée à l'écran et tous les modes servis.
	if !rule.cuts(countryCode, ed) {
		return served
	}
	out := make([]string, 0, len(served))
	for _, s := range served {
		if listed(rule.Allow, s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return served
	}
	return out
}

// Resolved est ce qu'une application lit au démarrage.
type Resolved struct {
	Edition string `json:"edition"`
	Country string `json:"country"`
	// Features : chaque clé du catalogue, avec sa disponibilité.
	//
	// ⚠️ TOUTES LES CLÉS, Y COMPRIS CELLES À `true`. Ne servir que les
	// fonctions coupées obligerait chaque application à connaître le
	// catalogue pour deviner le reste — et une fonction ajoutée au serveur
	// serait absente de la réponse, donc lue comme coupée par une
	// application prudente.
	Features map[string]bool `json:"features"`
	// Lists : les listes blanches, quand il y en a. Une liste absente veut
	// dire « tout ce que le métier sert ».
	Lists map[string][]string `json:"lists,omitempty"`
}

// Resolve rend ce que cette édition voit dans ce pays.
func (r *Registry) Resolve(countryCode, ed string) Resolved {
	out := Resolved{
		Edition:  edition.Normalize(ed),
		Country:  strings.ToUpper(strings.TrimSpace(countryCode)),
		Features: make(map[string]bool, len(Features)),
	}
	for _, f := range Features {
		out.Features[f.Key] = r.Allows(f.Key, countryCode, ed)
	}
	if r == nil {
		return out
	}
	for _, l := range Lists {
		rule, ok := r.ListRules[l.Key]
		if !ok || len(rule.Allow) == 0 || !rule.cuts(countryCode, ed) {
			continue
		}
		allow := make([]string, len(rule.Allow))
		copy(allow, rule.Allow)
		sort.Strings(allow)
		if out.Lists == nil {
			out.Lists = map[string][]string{}
		}
		out.Lists[l.Key] = allow
	}
	return out
}
