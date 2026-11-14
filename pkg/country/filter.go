package country

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
)

// Field est le nom du champ `country` sur tout document borné par pays :
// compte, commande, course, enseigne, chauffeur, campagne. Le même nom
// partout, pour que la migration et les index s'écrivent une fois.
const Field = "country"

// TestingField est le nom du champ qui marque un ESSAI sur tout ce qu'un essai
// produit : une commande, une course, un envoi, un fait, un gain.
//
// ⚠️ LE MÊME MOT QUE LE PAYS D'ESSAI (`testing` dans `GET /countries`), et ce
// n'est pas une coïncidence : c'est la même idée, portée par l'objet au lieu
// du pays. Un pays d'essai sort TOUTE son activité de ce qui compte ; un compte
// d'essai n'en sort que la sienne, et le pays reste réel pour tous les autres.
//
// ⚠️ ABSENT = RÉEL. Le marqueur ne s'écrit que sur un essai (`omitempty`) :
// rien n'est à migrer, et un document d'avant compte comme avant.
const TestingField = "testing"

type countingKey struct{}

// Counting marque une lecture qui COMPTE : la supervision, les rapports, les
// classements — et donc les alertes, qui ne lisent que la supervision.
//
// Sous ce contexte, `Restrict` écarte les essais EXACTEMENT comme la liste des
// pays réels écarte un pays d'essai : ce qui s'y passe existe, se relit dans
// les listes de l'exploitation, et n'entre dans aucun chiffre.
//
// ⚠️ UNE LECTURE ORDINAIRE NE LES ÉCARTE PAS, et il ne le faut surtout pas. Le
// livreur d'une course d'essai doit la voir dans son application, et la console
// doit pouvoir suivre la recette. C'est le CALCUL d'un chiffre qui les ignore,
// pas la base qui les cache.
//
// ⚠️ LA DÉCISION EST À L'ENTRÉE, L'APPLICATION DANS LE DÉPÔT : la boucle d'une
// jauge, le calcul d'un rapport posent ce contexte une fois ; chaque lecture
// qui passe par `Restrict` en hérite, y compris celle qu'on ajoutera demain.
func Counting(ctx context.Context) context.Context {
	return context.WithValue(ctx, countingKey{}, true)
}

// IsCounting dit si la lecture compte — voir `Counting`.
func IsCounting(ctx context.Context) bool {
	on, _ := ctx.Value(countingKey{}).(bool)
	return on
}

// realOnly est la condition « ce n'est pas un essai ».
//
// ⚠️ `$ne: true`, et non `false` : un document sans le champ — tous ceux
// d'avant, et tous les vrais — doit passer. `testing: false` ne le laisserait
// pas passer, et les tableaux se videraient le jour du déploiement.
func realOnly() bson.M { return bson.M{"$ne": true} }

// Restrict ajoute le pays effectif de la requête à un filtre Mongo.
//
// ⚠️ C'est LA borne « haut niveau » de la plateforme : toute liste que
// l'exploitation ou le public consulte doit passer par ici, sans quoi la
// console de Lomé affiche les courses de Cotonou. La borne est posée dans le
// DÉPÔT, pas dans le handler : un handler oublié laisserait passer, un dépôt
// oublié n'existe pas — c'est lui qu'on relit.
//
// Sans pays dans le contexte (middleware absent, ou tâche de fond sans
// requête), le filtre est rendu tel quel : une tâche de fond qui consolide
// tous les pays est légitime, et un filtre sur `country: ""` n'aurait rien
// rendu du tout — ce qui se serait lu comme une base vide.
//
// Sous un contexte qui COMPTE (`Counting`), les essais sont écartés aussi —
// avec ou sans pays : le marqueur est sur l'objet, il ne dépend d'aucune liste.
func Restrict(ctx context.Context, filter bson.M) bson.M {
	if filter == nil {
		filter = bson.M{}
	}
	if code := FromContext(ctx); code != "" {
		filter[Field] = code
	}
	if IsCounting(ctx) {
		filter[TestingField] = realOnly()
	}
	return filter
}

// RestrictD est `Restrict` pour un filtre ordonné.
func RestrictD(ctx context.Context, filter bson.D) bson.D {
	if code := FromContext(ctx); code != "" {
		filter = append(filter, bson.E{Key: Field, Value: code})
	}
	if IsCounting(ctx) {
		filter = append(filter, bson.E{Key: TestingField, Value: realOnly()})
	}
	return filter
}
