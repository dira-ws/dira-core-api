package main

// LE REGISTRE DES RAPPELS — chaque objet payé qu'une verticale doit apprendre.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/internal/config"
	"github.com/kgtech-org/dira-core-api/internal/payment"
)

// ⚠️⚠️ CHAQUE `purpose` QUE LA CONFIRMATION ANNONCE A SA VERTICALE.
//
// La confirmation et le rejeu lisent `payment.VerticalPurposes` ; ce registre
// dit à QUI l'annonce part. Un `purpose` présent dans la première liste et
// absent d'ici est un paiement abouti que personne n'apprend : la tâche part,
// ne trouve pas de destinataire, s'abandonne — et l'objet reste impayé alors
// que l'argent est encaissé. L'épreuve lit la liste au lieu de la recopier :
// un `purpose` ajouté demain échoue ici tant qu'il n'est pas routé.
func TestEveryAnnouncedPurposeReachesItsVertical(t *testing.T) {
	cfg := &config.Config{
		FoodBaseURL: "http://food:8080", FoodCallbackToken: "jeton-food",
		VTCBaseURL: "http://vtc:8080", VTCCallbackToken: "jeton-vtc",
	}
	routes := paymentCallbacks(cfg)
	require.NotEmpty(t, payment.VerticalPurposes)
	for _, purpose := range payment.VerticalPurposes {
		c, ok := routes[purpose]
		require.True(t, ok, "`%s` est annoncé et routé nulle part", purpose)
		assert.True(t, c.Enabled(), purpose)
	}
	// Et chacun va chez celle qui possède l'objet.
	want := map[string]string{
		payment.PurposeOrder: "food", payment.PurposeShipment: "food",
		payment.PurposeRide: "vtc", payment.PurposeRideSubscription: "vtc",
	}
	for purpose, target := range want {
		assert.Equal(t, target, routes[purpose].Target(), purpose)
	}
	assert.Len(t, routes, len(payment.VerticalPurposes), "un `purpose` routé que la confirmation n'annonce jamais")
}
