package token

// L'HISTORIQUE DES JETONS — #237.
//
// Trois demandes, venues de l'app marchand : le plus récent d'abord, un filtre
// d'unité, et un schéma publié.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// grandLivre pose n mouvements dans l'ordre d'écriture, avec leurs unités.
//
// ⚠️ LES IDENTIFIANTS SONT CROISSANTS, comme ceux de Mongo : le curseur du
// grand livre s'appuie sur `_id`, et des identifiants tirés au hasard auraient
// fait passer une pagination qui ne marche pas.
func grandLivre(t *testing.T, unités ...string) (*Service, string) {
	t.Helper()
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := seedWallet(t, repo, WalletTypeDriver, 100)

	w, err := repo.FindWalletByOwner(context.Background(), owner)
	require.NoError(t, err)
	for i, u := range unités {
		repo.transactions = append(repo.transactions, Transaction{
			ID: primitive.NewObjectID(), WalletID: w.ID,
			Kind: KindPurchase, Amount: i + 1, Unit: u,
		})
		time.Sleep(time.Millisecond) // des identifiants strictement croissants
	}
	return svc, owner.Hex()
}

// ⚠️⚠️ LE PLUS RÉCENT D'ABORD. Ascendant, une application devait lire TOUTES
// les pages puis retourner la liste pour montrer les derniers mouvements — et
// ne pouvait pas proposer « afficher plus » vers le passé. La fiche de la
// console le faisait déjà bien ; seule la route de la personne était à
// l'envers.
func TestTheLedgerReadsNewestFirst(t *testing.T) {
	svc, owner := grandLivre(t, "", "", "")

	page, _, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 10})
	require.NoError(t, err)
	require.Len(t, page, 3)
	assert.Equal(t, 3, page[0].Amount, "le dernier mouvement doit être en tête")
	assert.Equal(t, 1, page[2].Amount)
}

// ⚠️ ET `order=asc` GARDE L'ANCIEN SENS, pour qui rejoue un relevé du début.
func TestAscendingIsStillAvailableForAStatement(t *testing.T) {
	svc, owner := grandLivre(t, "", "", "")

	page, _, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 10, Oldest: true})
	require.NoError(t, err)
	require.Len(t, page, 3)
	assert.Equal(t, 1, page[0].Amount, "le relevé se rejoue du début")
}

// ⚠️⚠️ LE FILTRE D'UNITÉ, ET SON PIÈGE : L'ABSENCE DU CHAMP VAUT « JETON ».
//
// `unit` est `omitempty` — tous les mouvements écrits AVANT l'arrivée de
// l'argent n'ont pas ce champ, et aucune migration ne les a touchés. Un filtre
// naïf `{"unit": "token"}` rendrait donc ZÉRO ligne sur l'historique d'un
// compte ancien : la page paraîtrait vide alors que le solde, lui, est juste.
func TestFilteringOnTokensKeepsTheOldRowsThatHaveNoUnit(t *testing.T) {
	// Deux anciens (sans unité), un jeton explicite, deux en argent.
	svc, owner := grandLivre(t, "", "", UnitToken, UnitXOF, UnitXOF)

	jetons, _, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 10, Unit: UnitToken})
	require.NoError(t, err)
	assert.Len(t, jetons, 3,
		"les mouvements d'avant l'argent ont disparu : ils n'ont pas de champ `unit`")

	argent, _, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 10, Unit: UnitXOF})
	require.NoError(t, err)
	assert.Len(t, argent, 2)

	// Sans filtre, le grand livre entier.
	tout, _, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, tout, 5)
}

// ⚠️ LE CURSEUR RECULE VERS LE PASSÉ, et une page ne se répète pas.
func TestTheCursorWalksBackwardsWithoutRepeating(t *testing.T) {
	svc, owner := grandLivre(t, "", "", "", "", "")

	p1, next, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 2})
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.NotEmpty(t, next)
	assert.Equal(t, 5, p1[0].Amount)

	p2, _, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 2, Cursor: next})
	require.NoError(t, err)
	require.Len(t, p2, 2)
	assert.Equal(t, 3, p2[0].Amount, "la page suivante doit aller vers le PASSÉ")

	vus := map[int]bool{}
	for _, t2 := range append(p1, p2...) {
		assert.False(t, vus[t2.Amount], "le mouvement %d est servi deux fois", t2.Amount)
		vus[t2.Amount] = true
	}
}

// ⚠️ ET LE FILTRE SURVIT À LA PAGINATION : le curseur ne doit pas le perdre,
// sinon la deuxième page ramène ce que la première avait écarté.
func TestTheUnitFilterSurvivesPagination(t *testing.T) {
	svc, owner := grandLivre(t, UnitToken, UnitXOF, UnitToken, UnitXOF, UnitToken)

	p1, next, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 2, Unit: UnitToken})
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.NotEmpty(t, next)

	p2, _, err := svc.ListTransactions(context.Background(), owner, auth.RoleDriver, "",
		TransactionPage{Limit: 2, Unit: UnitToken, Cursor: next})
	require.NoError(t, err)
	for _, t2 := range append(p1, p2...) {
		assert.NotEqual(t, UnitXOF, t2.Unit, "une ligne d'argent a traversé le filtre")
	}
}

// --- ET MAINTENANT LA REQUÊTE ELLE-MÊME ------------------------------------
//
// ⚠️⚠️ CE BLOC EXISTE PARCE QUE LES ÉPREUVES D'AU-DESSUS NE PROUVAIENT RIEN DU
// CODE RÉEL. Mesuré : **0 mutation sur 7** attrapée — on pouvait casser le
// filtre d'unité, inverser l'ordre, laisser le curseur écraser le filtre, et
// tout restait vert. La raison est simple et vaut pour toute doublure : mes
// épreuves passaient par `fakeRepo`, que j'avais écrit pour appliquer la bonne
// règle. Elles éprouvaient ma doublure, pas la requête de Mongo.
//
// Les deux décisions qui comptent sont donc PURES, et vérifiées ici sans base.

// ⚠️⚠️ L'ABSENCE DU CHAMP VAUT « JETON ». `unit` est `omitempty` : tous les
// mouvements écrits avant l'arrivée de l'argent n'ont pas ce champ, et aucune
// migration ne les a touchés. Un filtre `{"unit": "token"}` rendrait ZÉRO ligne
// sur l'historique d'un compte ancien — page vide, solde juste, et personne ne
// comprend.
func TestTheTokenFilterKeepsTheRowsWithNoUnitField(t *testing.T) {
	f := unitFilter(UnitToken)
	require.NotNil(t, f)

	ou, ok := f["$or"].([]bson.M)
	require.True(t, ok, "le filtre jeton doit être un `$or` : sinon il oublie les anciens")

	var existe bool
	for _, clause := range ou {
		if u, ok := clause["unit"].(bson.M); ok {
			if v, ok := u["$exists"].(bool); ok && !v {
				existe = true
			}
		}
	}
	assert.True(t, existe,
		"`$exists: false` manque : tout l'historique d'avant l'argent disparaît")
}

// L'argent, lui, est toujours écrit explicitement : un filtre simple suffit.
func TestTheMoneyFilterIsExact(t *testing.T) {
	assert.Equal(t, bson.M{"unit": UnitXOF}, unitFilter(UnitXOF))
}

// Sans unité demandée, rien n'est borné — le grand livre entier.
func TestNoUnitAskedFiltersNothing(t *testing.T) {
	for _, rien := range []string{"", "tokens", "XOF", "argent"} {
		assert.Nil(t, unitFilter(rien), "« %s » a borné le grand livre", rien)
	}
}

// ⚠️⚠️ LE SENS DU TRI ET LA BORNE DU CURSEUR VONT ENSEMBLE. Trier du plus
// récent au plus ancien avec une borne `$gt` rend une page VIDE au deuxième
// appel — et rien ne le signale : l'application croit être arrivée au bout de
// l'historique.
func TestTheSortAndTheCursorBoundAlwaysAgree(t *testing.T) {
	sens, borne := orderOf(false)
	assert.Equal(t, -1, sens, "le défaut est le plus récent d'abord")
	assert.Equal(t, "$lt", borne, "vers le passé, le curseur recule")

	sens, borne = orderOf(true)
	assert.Equal(t, 1, sens, "`order=asc` rejoue le relevé du début")
	assert.Equal(t, "$gt", borne)
}

// ⚠️⚠️ LE CURSEUR NE DOIT PAS ÉCRASER LE FILTRE D'UNITÉ.
//
// Les deux portent un `$or` ou un `_id` à la racine du même document : posés à
// plat, le second efface le premier. Le symptôme serait sournois — la PREMIÈRE
// page respecte le filtre, la DEUXIÈME ramène l'argent qu'on venait d'écarter.
// D'où le `$and` explicite, vérifié ici sur la requête construite.
func TestTheCursorAndTheUnitFilterCoexist(t *testing.T) {
	q := transactionsFilter(primitive.NewObjectID(),
		TransactionPage{Unit: UnitToken, Cursor: primitive.NewObjectID().Hex()})

	et, ok := q["$and"].([]bson.M)
	require.True(t, ok, "sans `$and`, le curseur écrase le filtre d'unité")

	var unité, curseur bool
	var walk func(bson.M)
	walk = func(m bson.M) {
		if _, ok := m["$or"]; ok {
			unité = true
		}
		if _, ok := m["_id"]; ok {
			curseur = true
		}
		if sous, ok := m["$and"].([]bson.M); ok {
			for _, s := range sous {
				walk(s)
			}
		}
	}
	for _, clause := range et {
		walk(clause)
	}
	assert.True(t, unité, "le filtre d'unité a disparu de la requête")
	assert.True(t, curseur, "la borne du curseur a disparu de la requête")
}

// ⚠️ ET LA ROUTE LIT BIEN `order` ET `unit`. Mes épreuves passaient par le
// SERVICE : retirer la lecture des paramètres laissait tout au vert.
func TestTheRouteReadsOrderAndUnit(t *testing.T) {
	for _, c := range []struct {
		query  string
		unit   string
		oldest bool
	}{
		{"", "", false},
		{"?order=asc", "", true},
		{"?order=ASC", "", true},
		{"?order=desc", "", false},
		{"?unit=token", UnitToken, false},
		{"?unit=xof&order=asc", UnitXOF, true},
		// ⚠️ TOLÉRANT : une valeur inconnue vaut « pas de filtre », pas un
		// `422`. Casser l'écran d'historique pour un pluriel serait absurde —
		// la liste est seulement plus longue.
		{"?unit=tokens", "", false},
		{"?order=croissant", "", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "/wallet/transactions"+c.query, nil)
		got := transactionPageFromRequest(req)
		assert.Equal(t, c.unit, got.Unit, "query %q", c.query)
		assert.Equal(t, c.oldest, got.Oldest, "query %q", c.query)
	}
}
