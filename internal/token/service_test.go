package token

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// ⚠️⚠️ UN SOCLE SANS ANNUAIRE DE BOUTIQUES NE PANIQUE PAS, IL REFUSE.
//
// `stores` est branché à `nil` depuis l'extraction du socle : les points de
// vente appartiennent à la livraison. Ce chemin l'appelait sans regarder, et
// chaque lecture de portefeuille par un MARCHAND déréférençait un pointeur nul
// — 500, et l'alerte « un service a paniqué » : quarante-deux fois en une heure
// sur la préproduction avant qu'on ne le voie.
//
// ⚠️ Et le refus est un 503 NOMMÉ, pas un 403 : le marchand n'a rien fait de
// mal et ne peut rien corriger. C'est un câblage qui manque.
func TestAMerchantWalletWithoutAStoreDirectoryRefusesInsteadOfPanicking(t *testing.T) {
	// ⚠️ Identifiants LISIBLES et non des ObjectID : ces chemins ne les
	// analysent pas, et une chaîne de 24 hexadécimaux fait sonner le
	// détecteur de secrets de la CI (entropie) sur un test qui n'en porte pas.
	svc := &Service{} // comme le socle le branche : ni booster, ni annuaire
	_, err := svc.resolveOwner(context.Background(), "marchand-1", auth.RoleMerchant, "boutique-1")
	require.Error(t, err)
	e := apperr.From(err)
	assert.Equal(t, "store_directory_unavailable", e.Code)
	assert.Equal(t, 503, e.HTTPStatus)

	// Les deux autres portes qui demandent la boutique refusent de même,
	// plutôt que de paniquer : propulser un plat et acheter une option.
	_, err = svc.BoostDish(context.Background(), "u", auth.RoleMerchant, "s", "d")
	require.Error(t, err)
	assert.Equal(t, "store_directory_unavailable", apperr.From(err).Code)
	_, err = svc.BuyOption(context.Background(), "u", "s", "whatever")
	require.Error(t, err)
	assert.Equal(t, "store_directory_unavailable", apperr.From(err).Code)

	// ⚠️ ET UN ADMIN PASSE TOUJOURS : la dérogation d'exploitation ne dépend
	// pas de l'annuaire — il nomme la boutique explicitement.
	owner, err := svc.resolveOwner(context.Background(), "admin", auth.RoleAdmin, "boutique-1")
	require.NoError(t, err)
	assert.Equal(t, "boutique-1", owner)
}
