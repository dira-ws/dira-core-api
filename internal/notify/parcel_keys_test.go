package notify

// LES MESSAGES DU COLIS — chacun a son gabarit, et aucun ne porte le
// destinataire.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var parcelKeys = []string{
	KeyParcelConfirmed, KeyParcelAccepted, KeyParcelPickedUp, KeyParcelCourierNearby,
	KeyParcelDelivered, KeyParcelFailedAttempt, KeyParcelReturning, KeyParcelReturned,
	KeyParcelCancelled, KeyParcelPaymentKept, KeyParcelReturnAssigned,
	KeyStaffParcelCodeLocked, KeyStaffParcelIncident,
}

// ⚠️⚠️ UNE CLÉ SANS GABARIT EST UNE NOTIFICATION QUI NE PART JAMAIS — et
// `/internal/notifications/send` la refuse en `422 unknown_template`. Chaque
// message du colis a donc le sien, allumé, en français ET en anglais, avec les
// variables qu'il annonce.
func TestEveryParcelKeyHasItsTemplate(t *testing.T) {
	for _, key := range parcelKeys {
		tmpl, ok := defaults[key]
		require.True(t, ok, "gabarit manquant pour %s", key)
		assert.True(t, tmpl.Enabled, key)
		assert.NotEmpty(t, tmpl.Description, key)
		for _, loc := range []string{LocaleFR, LocaleEN} {
			txt, ok := tmpl.Locales[loc]
			require.True(t, ok, "%s sans %s", key, loc)
			assert.NotEmpty(t, txt.Title, "%s/%s", key, loc)
			assert.NotEmpty(t, txt.Body, "%s/%s", key, loc)
			assert.Empty(t, unknownVariables(key, txt.Title, txt.Body), "%s/%s : une variable que personne ne remplit", key, loc)
		}
		assert.NotEmpty(t, Provided(key), key)
	}
	// Les clés se nomment comme le contrat les nommera : l'expéditeur et le
	// livreur sous `parcel_`, l'équipe sous `staff_parcel_`.
	for _, key := range parcelKeys {
		assert.True(t, strings.HasPrefix(key, "parcel_") || strings.HasPrefix(key, "staff_parcel_"), key)
	}
}

// ⚠️⚠️ AUCUN MESSAGE DU COLIS NE PORTE LE DESTINATAIRE. Il n'a pas de compte, il
// ne nous a confié ni son nom ni son numéro : rien de lui ne s'affiche sur un
// écran verrouillé, ni ne se déclare comme variable qu'un gabarit de la console
// pourrait se mettre à afficher.
func TestNoParcelMessageNamesTheRecipient(t *testing.T) {
	for _, key := range parcelKeys {
		for _, v := range Provided(key) {
			for _, forbidden := range []string{"recipient", "destinataire", "phone", "address", "dropoff", "name", "code"} {
				assert.NotContains(t, v, forbidden, "%s déclare `%s`", key, v)
			}
		}
	}
}

// L'équipe ne coupe pas ses alertes ; un paiement conservé n'est pas une mise à
// jour qu'on coupe ; le reste se range avec les mises à jour, comme un repas.
func TestTheParcelMessagesHaveTheirCategories(t *testing.T) {
	assert.Equal(t, CategoryStaff, categoryOf(KeyStaffParcelCodeLocked))
	assert.Equal(t, CategoryStaff, categoryOf(KeyStaffParcelIncident))
	assert.False(t, Muteable(CategoryStaff))
	assert.Equal(t, CategorySupport, categoryOf(KeyParcelPaymentKept), "de l'argent payé qui n'a rien acheté")
	assert.False(t, Muteable(categoryOf(KeyParcelPaymentKept)))
	for _, key := range []string{KeyParcelConfirmed, KeyParcelDelivered, KeyParcelCancelled, KeyParcelReturnAssigned} {
		assert.Equal(t, CategoryOrderUpdates, categoryOf(key), key)
	}
}

// ⚠️ LES MESSAGES D'UN MÊME ENVOI SE REMPLACENT sur l'écran verrouillé, ceux de
// deux envois jamais — et rien ne change pour les autres objets.
func TestTheMessagesOfOneShipmentReplaceEachOther(t *testing.T) {
	assert.Equal(t, "shipment-s1", collapseOf(map[string]string{"type": "parcel_status", "shipment_id": "s1"}))
	assert.NotEqual(t, collapseOf(map[string]string{"shipment_id": "s1"}), collapseOf(map[string]string{"shipment_id": "s2"}))
	assert.Equal(t, "order-o1", collapseOf(map[string]string{"order_id": "o1", "delivery_id": "d1"}), "une commande reste regroupée par commande")
	assert.Equal(t, "delivery-d1", collapseOf(map[string]string{"delivery_id": "d1"}))
}
