package refkind

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Les routes de portefeuille prennent les genres de toutes les verticales — et
// le matériel, qui a sa route à lui, s'écrit au grand livre sans passer par
// elles.
func TestTheWalletRoutesTakeTheKindsOfEveryVertical(t *testing.T) {
	for _, kind := range []string{Order, Ride, Tip, RideAdjustment, RideSubscription, Shipment, ShipmentCharge, ShipmentLeg} {
		assert.True(t, OnServiceRoutes(kind), kind)
		assert.True(t, Writable(kind), kind)
	}
	assert.False(t, OnServiceRoutes(Equipment), "le matériel a sa propre route")
	assert.True(t, Writable(Equipment))
	// Un genre retiré, ou inventé, ne s'écrit plus.
	for _, kind := range []string{"listing", "listing_boost", "subscription", "", "Ride"} {
		assert.False(t, Writable(kind), "%q", kind)
	}
	// Une copie : la liste ne se modifie pas de l'extérieur.
	got := ServiceRoutes()
	got[0] = "x"
	assert.Equal(t, Order, ServiceRoutes()[0])
}
