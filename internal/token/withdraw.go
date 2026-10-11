package token

// RETIRER DE L'ARGENT DU PORTEFEUILLE — le sens SORTANT.
//
// ⚠️⚠️ C'EST L'OPÉRATION LA PLUS DANGEREUSE DE TOUTE LA PLATEFORME. Tout le
// reste déplace des chiffres entre deux comptes que nous tenons ; ici l'argent
// QUITTE la plateforme et ne revient pas. Trois règles en découlent, et aucune
// n'est négociable.
//
//  1. ON NE RETIRE PAS UN CRÉDIT OFFERT. `promo_xof` est un geste commercial,
//     dépensable dans l'application — pas de l'argent à emporter. L'autoriser
//     ferait du budget promotionnel un distributeur de billets, et la première
//     campagne un peu large viderait la caisse en une nuit. Le retrait ne
//     touche QUE `balance_xof`, et c'est pour ça qu'il ne passe pas par
//     `SpendMoney`, qui prend le promotionnel en premier.
//
//  2. LA DETTE PASSE AVANT. Ce qu'on doit à la plateforme n'est pas à soi : le
//     retirable est `balance_xof − debt_xof`. Sans cette borne, un chauffeur
//     endetté retire tout et la dette ne se rembourse jamais.
//
//  3. LA CONDITION ET LE DÉBIT SONT UN SEUL GESTE. Deux demandes simultanées
//     qui lisent « 10 000 » puis débitent chacune 10 000 font sortir 20 000.
//     `$expr` dans le filtre : Mongo ne met à jour que si la condition tient
//     AU MOMENT de l'écriture.

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// withdrawable est l'expression du RETIRABLE : l'argent réel, moins la dette.
//
// ⚠️ Écrite une seule fois et réutilisée par les deux chemins (le débit et la
// lecture), parce qu'un retirable calculé à deux endroits finit par donner
// deux chiffres — et c'est l'écran qui annonce un montant que le serveur
// refuse ensuite.
func withdrawableExpr() bson.M {
	return bson.M{"$subtract": []any{
		bson.M{"$ifNull": []any{"$balance_xof", 0}},
		bson.M{"$ifNull": []any{"$debt_xof", 0}},
	}}
}

// HoldForWithdrawal retire le montant du solde, de façon atomique et
// conditionnelle. Rend `false` quand le retirable ne suffit pas.
//
// ⚠️ « HOLD », PAS « SEND » : l'argent quitte le solde AU MOMENT DE LA DEMANDE,
// avant même d'être envoyé. Sinon une demande validée trois heures plus tard
// trouverait un portefeuille vidé entre-temps — et il faudrait soit refuser une
// validation déjà promise, soit laisser le solde devenir négatif. Les deux sont
// pires que de retenir tout de suite.
func (r *Repository) HoldForWithdrawal(ctx context.Context, walletID primitive.ObjectID, amount int) (bool, error) {
	if amount <= 0 {
		return false, fmt.Errorf("token: withdraw: amount must be positive")
	}
	res, err := r.wallets.UpdateOne(ctx,
		bson.M{
			"_id": walletID,
			// ⚠️ LA CONDITION PORTE SUR LE RETIRABLE, pas sur le solde : la
			// dette n'est pas à celui qui la doit.
			"$expr": bson.M{"$gte": []any{withdrawableExpr(), amount}},
		},
		bson.M{"$inc": bson.M{"balance_xof": -amount}},
	)
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// ReleaseWithdrawal remet le montant au solde — un retrait refusé ou échoué.
//
// ⚠️ UNE REMISE, PAS UNE ANNULATION. Le mouvement de retenue reste au relevé,
// et celui-ci s'ajoute : un relevé où l'argent disparaît puis réapparaît sans
// trace est un relevé qu'un chauffeur conteste, et il a raison.
func (r *Repository) ReleaseWithdrawal(ctx context.Context, walletID primitive.ObjectID, amount int) error {
	if amount <= 0 {
		return fmt.Errorf("token: release: amount must be positive")
	}
	_, err := r.wallets.UpdateOne(ctx,
		bson.M{"_id": walletID}, bson.M{"$inc": bson.M{"balance_xof": amount}})
	return err
}

// WithdrawableOf rend ce qu'un propriétaire peut réellement retirer.
func (r *Repository) WithdrawableOf(ctx context.Context, walletID primitive.ObjectID) (int, error) {
	cur, err := r.wallets.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"_id": walletID}}},
		{{Key: "$project", Value: bson.M{"w": withdrawableExpr()}}},
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = cur.Close(ctx) }()
	var row struct {
		W int `bson:"w"`
	}
	if cur.Next(ctx) {
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
	}
	// ⚠️ JAMAIS NÉGATIF VERS L'ÉCRAN. « Retirable : −1 500 F » se lit comme un
	// bug ; une dette supérieure au solde veut dire « rien à retirer ».
	if row.W < 0 {
		return 0, nil
	}
	return row.W, nil
}
