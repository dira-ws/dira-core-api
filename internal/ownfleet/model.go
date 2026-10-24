// Package ownfleet est LA FLOTTE INTERNE : les véhicules que DIRA POSSÈDE et
// confie à un chauffeur ou à un livreur contre une REDEVANCE.
//
// ═══════════════════════════════════════════════════════════════════════════
// LA DÉCISION DE CONCEPTION, ET POURQUOI
// ═══════════════════════════════════════════════════════════════════════════
//
// Trois mécanismes existaient déjà, et chacun couvrait une PART du sujet. Ce
// paquet n'en réinvente aucun ; il tient exactement ce que les trois ne
// tenaient pas.
//
//  1. UNE FLOTTE INTERNE EST UNE FLOTTE (`internal/fleet`), marquée
//     `internal: true`. La propriété d'un véhicule est déjà un sujet résolu :
//     `vehicles.fleet_id` pointe la société, la console affiche son nom sur la
//     ligne du véhicule, `release-fleet` la détache, `/admin/fleets/{id}/drivers`
//     dit qui conduit son parc. Inventer une seconde notion de propriétaire
//     aurait dupliqué ces quatre chemins — et le cinquième, écrit l'an
//     prochain, n'aurait été branché que sur l'un des deux.
//
//     ⚠️⚠️ MAIS SON ARGENT N'EST PAS CELUI D'UNE FLOTTE PARTENAIRE, et c'est
//     la ligne de fracture du sujet. Ce qu'un chauffeur rend à Sodigaz est un
//     accord entre eux deux : Dira l'enregistre (`FleetPay`) et ne prélève
//     RIEN — « cet argent n'est pas celui de Dira », dit le fichier qui le
//     calcule. La redevance d'un véhicule de Dira, elle, EST de l'argent de
//     Dira : elle se prélève, elle se retarde, elle bloque la mise en ligne.
//     La console partenaire est donc fermée sur une flotte interne, ce qui
//     ferme du même coup `PUT /partner/drivers/{id}/pay` — sans quoi Dira
//     aurait pris une part de chaque course EN PLUS de la redevance, sans que
//     rien ne le signale : les deux nombres sont plausibles.
//
//  2. LE PRÉLÈVEMENT EST CELUI DU MATÉRIEL (`internal/equipment`), sans une
//     ligne d'arithmétique nouvelle. Une attribution de véhicule OUVRE UN
//     CONTRAT DE LOCATION chez lui : même échéancier, même retenue sur les
//     gains, mêmes plafonds jour et semaine, même minimum laissé à l'agent,
//     même grâce, même pénalité, même blocage, mêmes QR de remise et de
//     retour, même file d'exploitation.
//
//     ⚠️⚠️ PARCE QU'IL NE PEUT EXISTER QU'UN SEUL MOTEUR DE RETENUE. `Collect`
//     parcourt TOUS les contrats d'une personne avec un budget COMMUN et
//     applique `MinLeftXOF` à l'ensemble. Un second moteur n'aurait rien su de
//     ce budget : le casque aurait pris sa part, la voiture la sienne, et
//     l'agent serait rentré avec zéro un soir de petite recette — en ayant
//     respecté les deux plafonds séparément.
//
//  3. LE TYPE DE VÉHICULE EST LA CLASSE TARIFAIRE DE LA VERTICALE — `moto`,
//     `tricycle`, `eco`, `confort`, `van`. Aucune seconde taxonomie : une
//     classe décrit un véhicule, c'est elle qui décide de ce qu'il peut
//     prendre, et deux listes auraient divergé au premier ajout (le tricycle
//     est arrivé après les quatre autres).
//
// ═══════════════════════════════════════════════════════════════════════════
// CE QUE CE PAQUET TIENT, ET LUI SEUL
// ═══════════════════════════════════════════════════════════════════════════
//
// Le REGISTRE DES BIENS : un véhicule de Dira est un bien UNIQUE, pas un
// article de catalogue. Il n'y a pas « trois Corolla en stock », il y a LA
// Corolla GA-1234-AB. D'où les deux choses qu'aucun des trois mécanismes ne
// pouvait tenir :
//
//   - un véhicule EXISTE AVANT SON CONDUCTEUR. Le véhicule d'une verticale
//     appartient à un chauffeur (`vehicles.driver_id`) ; un véhicule de Dira
//     qui attend au parc n'en a aucun. Il lui faut donc son propre document, au
//     socle — comme la flotte, et pour la même raison : les deux métiers ont
//     besoin de la même DONNÉE, puisque la même moto peut livrer ou faire des
//     courses ;
//   - une DOUBLE ATTRIBUTION doit être impossible, et cette garantie ne peut
//     vivre que là où l'on choisit le véhicule : dans UNE écriture
//     conditionnelle sur son document (voir `Repository.Reserve`). Un contrôle
//     « est-il libre ? » suivi d'une écriture aurait laissé passer les deux
//     attributions du matin où quelqu'un double-clique.
//
// ⚠️ ET LE CHEMIN INVERSE EXISTE. Le piège maison de cette plateforme est un
// rattachement que RIEN ne défaisait : le champ existait, un seul chemin le
// remplissait, aucun ne l'effaçait, et les écrans paraissaient normaux. Ici,
// les TROIS sorties du contrat — rendu, impayé constaté, annulé avant la
// remise — rendent le véhicule au parc, par le même appel
// (`equipment.AssetDesk.Release`), et une épreuve lit le document écrit.
package ownfleet

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Collection des biens de la flotte interne.
const Collection = "own_fleet_assets"

// États d'un véhicule de la flotte interne.
//
// ⚠️ CE QUI PERMET D'ÊTRE ATTRIBUÉ EST UNE LISTE BLANCHE — `available`, et
// rien d'autre —, et non « tout sauf immobilisé ». C'est la leçon de
// `Drivable` dans les courses : les deux lectures du dispatch testaient
// `!= maintenance`, donc tout statut AJOUTÉ ensuite devenait permissif par
// défaut. Un véhicule « vendu » aurait continué à être attribuable, et
// l'écran, lui, affichait bien « vendu ».
const (
	StatusAvailable   = "available"   // au parc, attribuable
	StatusAssigned    = "assigned"    // confié à quelqu'un, contrat ouvert
	StatusMaintenance = "maintenance" // au garage : ni attribuable, ni perdu
	StatusRetired     = "retired"     // vendu, détruit, sorti du parc
)

// Statuses est ce que le code connaît — pour que la console propose sans
// deviner.
var Statuses = []string{StatusAvailable, StatusAssigned, StatusMaintenance, StatusRetired}

// Terms sont les CONDITIONS D'ATTRIBUTION par défaut d'un véhicule.
//
// ⚠️ SUR LE VÉHICULE, MAIS SEULEMENT COMME DÉFAUT : ce qui engage l'agent est
// ce qui est écrit sur SON contrat, figé à l'ouverture. Changer la redevance
// d'un véhicule au parc ne doit pas réévaluer l'échéancier de celui qui roule
// avec — « une écriture dont le montant bouge n'est pas une écriture », et
// c'est la règle que le grand livre des flottes a déjà payée une fois.
type Terms struct {
	// FeeXOF est LA REDEVANCE, par période, dans la PLUS PETITE UNITÉ DE LA
	// MONNAIE DU PAYS du véhicule.
	//
	// ⚠️ L'UNITÉ N'EST PAS LA MÊME PARTOUT : recopier une redevance du Togo
	// vers la Guinée la divise par quinze. C'est pour cela qu'un véhicule porte
	// son pays et que le parc se lit pays par pays.
	FeeXOF int `bson:"fee_xof"`
	// Period est le pas de la redevance — `daily`, `weekly`, `biweekly`,
	// `monthly`, le vocabulaire du matériel.
	Period string `bson:"period"`
	// DepositXOF est la caution. ⚠️ Rendue au retour moins les dégâts, et
	// c'est le plan du contrat qui le dit (`DepositRefundable`) — pas ce champ.
	DepositXOF int `bson:"deposit_xof,omitempty"`
	// Days est la durée convenue, en jours. Zéro = sans terme.
	//
	// ⚠️ ELLE NE FERME RIEN TOUTE SEULE : une voiture ne rentre pas au parc
	// parce qu'une date est passée. Le terme est servi, l'exploitation le voit
	// dans sa liste, et c'est elle qui reprend.
	Days int `bson:"days,omitempty"`
	// Included : ce que la redevance COUVRE — entretien, réparations,
	// assurance, carburant, boîtier. Le vocabulaire est celui du contrat
	// (`equipment.Included`), parce que c'est là qu'il engage quelqu'un.
	//
	// ⚠️ CE QUI N'Y EST PAS EST À LA CHARGE DE L'AGENT. L'écran doit l'écrire
	// DANS LES DEUX SENS : une liste « entretien » seule laisse croire que
	// l'assurance est comprise, et la question se pose pour de bon le jour de
	// la panne.
	Included []string `bson:"included,omitempty"`
}

// Asset est UN véhicule de Dira.
type Asset struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	Country string             `bson:"country"`
	// FleetID est la flotte INTERNE du pays — la société « Dira GA ».
	//
	// ⚠️ RENSEIGNÉ À LA CRÉATION, jamais plus tard : c'est ce qui permet au
	// véhicule de la verticale de porter le MÊME `fleet_id` et à toute la
	// plateforme d'afficher un propriétaire sans rien savoir de ce paquet.
	FleetID primitive.ObjectID `bson:"fleet_id"`
	// ClassKey est la CLASSE TARIFAIRE de la verticale : `moto`, `tricycle`,
	// `eco`, `confort`, `van`.
	//
	// ⚠️ UNE CHAÎNE, ET AUCUNE ÉNUMÉRATION AU SOCLE. La taxonomie des classes
	// appartient aux courses (`dira-vtc-api/internal/tariff`) : c'est elle qui
	// décide de ce qu'un véhicule peut prendre, et elle change (le tricycle est
	// arrivé après les quatre autres). La recopier ici aurait fait une seconde
	// liste, et la console aurait proposé une classe que le dispatch ignore.
	ClassKey string `bson:"class_key"`
	// Verticals : à qui ce véhicule peut être confié — `vtc`, `food`. Vide =
	// les deux. Même convention que les audiences d'un article de matériel.
	//
	// ⚠️ LA MÊME MOTO SERT LES DEUX MÉTIERS, et c'est précisément pourquoi ce
	// registre est au socle. L'attribution, elle, choisit UNE verticale : c'est
	// elle qui dit où les gains seront retenus.
	Verticals []string `bson:"verticals,omitempty"`
	// Plate est L'IDENTITÉ du véhicule — ce qu'on lit sur le pare-chocs.
	Plate string `bson:"plate"`
	Brand string `bson:"brand,omitempty"`
	Model string `bson:"model,omitempty"`
	Color string `bson:"color,omitempty"`
	Year  int    `bson:"year,omitempty"`
	Seats int    `bson:"seats,omitempty"`
	// OdometerKm : l'usure, relevée à la remise et au retour. Zéro = non
	// renseigné, acceptable tant qu'aucune règle ne s'appuie dessus.
	OdometerKm int      `bson:"odometer_km,omitempty"`
	Photos     []string `bson:"photos,omitempty"`
	// LA PROPRIÉTÉ : ce que Dira a payé, et quand. Ce sont les deux nombres
	// qu'on vient chercher pour décider d'un renouvellement.
	AcquiredAt     *time.Time `bson:"acquired_at,omitempty"`
	AcquisitionXOF int        `bson:"acquisition_xof,omitempty"`
	// Les ÉCHÉANCES ADMINISTRATIVES du véhicule — assurance, visite technique.
	//
	// ⚠️ ELLES NE COUPENT RIEN D'ELLES-MÊMES, et il faut le dire ici : c'est
	// `vehicles.status` dans la verticale qui immobilise, et l'exploitation qui
	// le pose. Écrire un blocage automatique depuis le socle aurait mis un
	// chauffeur à l'arrêt sur une date mal saisie.
	InsuranceExpiresAt *time.Time `bson:"insurance_expires_at,omitempty"`
	TechnicalExpiresAt *time.Time `bson:"technical_expires_at,omitempty"`
	Status             string     `bson:"status"`
	// Terms : les conditions par DÉFAUT. Voir `Terms`.
	Terms Terms `bson:"terms"`
	// L'ATTRIBUTION EN COURS — les trois champs qui vont ensemble.
	//
	// ⚠️⚠️ ILS SONT ÉCRITS ET EFFACÉS PAR UNE SEULE PAIRE DE CHEMINS,
	// `Reserve` et `Release`, et jamais par un PATCH de fiche. C'est ce qui
	// rend le registre fiable : un champ qu'on peut aussi écrire « à la main »
	// finit par dire autre chose que le contrat qu'il désigne, et c'est l'écran
	// qui a l'air normal.
	HolderUserID *primitive.ObjectID `bson:"holder_user_id,omitempty"`
	ContractID   *primitive.ObjectID `bson:"contract_id,omitempty"`
	AssignedAt   *time.Time          `bson:"assigned_at,omitempty"`
	Notes        string              `bson:"notes,omitempty"`
	CreatedBy    string              `bson:"created_by,omitempty"`
	CreatedAt    time.Time           `bson:"created_at"`
	UpdatedAt    time.Time           `bson:"updated_at"`
}

// Assignable dit si le véhicule peut être confié.
//
// ⚠️ UNE LISTE BLANCHE, et les DEUX conditions : le statut ET l'absence de
// contrat. Le statut seul aurait suffi tant que les deux champs changent
// ensemble — c'est-à-dire jusqu'au premier incident qui les désynchronise, et
// ce jour-là la voiture serait partie deux fois.
func (a *Asset) Assignable() bool {
	return a.Status == StatusAvailable && a.ContractID == nil
}

var (
	errNotFound = apperr.NotFound("own_fleet_vehicle_not_found", "this vehicle is not in the internal fleet")
	// errPlateTaken : deux véhicules de la même plaque dans un pays.
	//
	// ⚠️ REFUSÉ PAR L'INDEX UNIQUE, pas par une lecture préalable : deux
	// exploitants qui enregistrent la même livraison passeraient tous les deux
	// un contrôle « cette plaque existe-t-elle ? », et le parc porterait deux
	// fois la même voiture — dont une seule serait attribuée, et l'autre
	// resterait « disponible » pour toujours.
	errPlateTaken = apperr.Conflict("own_fleet_plate_taken",
		"a vehicle with this plate is already in the internal fleet of this country")
	// errBusy : le véhicule n'est pas attribuable.
	//
	// ⚠️ C'EST LE REFUS QUI PROTÈGE L'AGENT, et il vient de l'écriture
	// conditionnelle, pas d'une lecture : deux attributions simultanées du même
	// véhicule se terminent par un seul contrat. Sans lui, deux chauffeurs
	// auraient payé la même voiture, et le second l'aurait appris en arrivant au
	// comptoir.
	errBusy = apperr.Conflict("own_fleet_vehicle_busy",
		"this vehicle is already assigned or not available")
	errBadStatus    = apperr.Validation("status must be available, assigned, maintenance or retired")
	errNotAssigned  = apperr.Conflict("own_fleet_vehicle_not_assigned", "this vehicle is not assigned to anyone")
	errNoFleetDesk  = apperr.Conflict("own_fleet_unavailable", "the internal fleet is not available on this deployment")
	errNoRedevance  = apperr.Conflict("own_fleet_unavailable", "the fee engine is not available on this deployment")
	errRetiredTerms = apperr.Conflict("own_fleet_vehicle_retired", "a vehicle out of the fleet cannot be assigned")
)
