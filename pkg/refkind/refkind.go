// Package refkind nomme les GENRES DE RÉFÉRENCE du grand livre des
// portefeuilles : à quoi un mouvement d'argent se rattache — une commande, une
// course, un abonnement de courses, un envoi de colis.
//
// ⚠️⚠️ UNE LISTE, ET ELLE EST ICI POUR ÊTRE LUE DES DEUX CÔTÉS. Le socle refuse
// un genre qu'il ne connaît pas : une verticale qui en envoie un autre voit son
// mouvement refusé en `422`. Une liste recopiée chez elle ne prouverait rien —
// elle dirait ce que la verticale croit, pas ce que le socle applique. Elle
// éprouve donc ce qu'elle envoie contre CETTE liste, celle du socle.
package refkind

import "slices"

// Les genres.
const (
	Order = "order"
	Ride  = "ride"
	// Tip : le pourboire d'une course — un second mouvement sur une course
	// déjà payée, sous sa propre clé.
	Tip = "tip"
	// RideAdjustment : la différence de prix d'une course dont le trajet a
	// changé en route, sous l'identifiant de l'ajustement.
	RideAdjustment = "ride_adjustment"
	// RideSubscription : un ABONNEMENT de courses, payé d'avance — au solde
	// comme par paiement mobile, où il porte le même nom (`purpose`).
	RideSubscription = "ride_subscription"
	// Equipment : un contrat de matériel. Il a sa propre route
	// (`/internal/equipment/collect`), et n'est pas accepté par les routes de
	// portefeuille.
	Equipment = "equipment"
	// Les trois genres du colis : l'envoi, un frais décidé après coup, une
	// étape.
	Shipment       = "shipment"
	ShipmentCharge = "shipment_charge"
	ShipmentLeg    = "shipment_leg"
)

// serviceRoutes : les genres qu'acceptent les routes de portefeuille de la
// surface de service (`/internal/wallets/pay`, `refund`, `consume`, `credit`,
// `credit-earnings`, `owe`).
var serviceRoutes = []string{Order, Ride, Tip, RideAdjustment, RideSubscription, Shipment, ShipmentCharge, ShipmentLeg}

// ServiceRoutes rend ces genres, dans un ordre stable. Une copie : la liste ne
// se modifie pas de l'extérieur.
func ServiceRoutes() []string { return slices.Clone(serviceRoutes) }

// OnServiceRoutes dit si les routes de portefeuille acceptent ce genre.
func OnServiceRoutes(kind string) bool { return slices.Contains(serviceRoutes, kind) }

// Writable dit si une écriture NOUVELLE du grand livre peut porter ce genre :
// ceux des routes de portefeuille, et le matériel, qui a sa route à lui.
//
// ⚠️ UN GENRE RETIRÉ RESTE LISIBLE au grand livre — des écritures passées le
// portent —, mais il n'est plus ici : aucune écriture nouvelle ne s'y rattache.
func Writable(kind string) bool { return kind == Equipment || OnServiceRoutes(kind) }
