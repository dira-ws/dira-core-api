package payout

// LE RETRAIT MOBILE MONEY — ce qu'un chauffeur ou un livreur emporte.
//
// ⚠️⚠️ RÉSERVÉ AUX AGENTS, ET CE N'EST PAS UNE RESTRICTION TECHNIQUE. Un
// client n'a pas de revenu à retirer : son solde vient de recharges et de
// remboursements, et lui ouvrir un retrait ferait de la plateforme un service
// de transfert d'argent — un métier qui demande une licence que nous n'avons
// pas. Un marchand est payé par virement, sur contrat. Restent le chauffeur et
// le livreur, qui gagnent de l'argent course après course et doivent pouvoir
// le sortir.

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection des demandes de retrait.
const Collection = "payouts"

// LES TROIS MODES DE RETRAIT, réglés par pays.
//
// ⚠️⚠️ TROIS, PARCE QUE TROIS RÉALITÉS D'EXPLOITATION DIFFÉRENTES — et pas
// parce qu'on hésite. Le mode se choisit selon ce que l'exploitation peut
// tenir, pas selon ce qui est le plus confortable pour l'agent :
//
//   - un pays qui démarre, sans flottant chez l'agrégateur, ne peut pas verser
//     à la demande : il lui faut `request`, pour décider au cas par cas ;
//   - un pays installé, avec un flottant approvisionné, sert `direct` — et
//     c'est ce que l'agent préfère de loin ;
//   - une société qui paie ses chauffeurs à la semaine veut `periodic`, sans
//     que personne n'ait à demander quoi que ce soit.
const (
	// ModeOff : aucun retrait. ⚠️ C'EST LE DÉFAUT, et c'est délibéré : un pays
	// dont les contrats avec l'agrégateur ne sont pas signés ne doit pas
	// proposer un bouton qui échouera.
	ModeOff = "off"
	// ModeDirect : l'agent demande, l'argent part tout de suite.
	ModeDirect = "direct"
	// ModeRequest : l'agent demande, l'exploitation valide, puis l'argent part.
	ModeRequest = "request"
	// ModePeriodic : personne ne demande rien. Le solde part tout seul, au
	// rythme réglé.
	//
	// ⚠️ L'AGENT NE PEUT PAS DEMANDER EN MODE PÉRIODIQUE, et l'écran doit le
	// DIRE — « vos gains partent chaque lundi » — plutôt que d'afficher un
	// bouton grisé. Un bouton grisé fait chercher ce qui ne va pas.
	ModePeriodic = "periodic"
)

// Modes est la liste admise, dans l'ordre où la console les montre.
var Modes = []string{ModeOff, ModeDirect, ModeRequest, ModePeriodic}

func ValidMode(m string) bool {
	for _, v := range Modes {
		if v == m {
			return true
		}
	}
	return false
}

// LES ÉTATS D'UNE DEMANDE.
//
// ⚠️ L'ARGENT EST DÉJÀ SORTI DU SOLDE DÈS `pending`. Voir
// `token.HoldForWithdrawal` : retenir au moment de la demande est la seule
// façon qu'une validation donnée trois heures plus tard trouve encore de quoi
// payer. Les états ci-dessous décrivent où en est le VERSEMENT, pas où en est
// l'argent — il a déjà quitté le solde.
const (
	// StatePending : en attente de validation (mode `request`).
	StatePending = "pending"
	// StateSending : l'ordre est parti chez l'agrégateur.
	StateSending = "sending"
	// StatePaid : l'agrégateur confirme que le bénéficiaire a reçu.
	StatePaid = "paid"
	// StateRejected : l'exploitation a refusé. L'argent est remis au solde.
	StateRejected = "rejected"
	// StateFailed : l'agrégateur n'a pas pu verser. L'argent est remis au
	// solde.
	//
	// ⚠️ `failed` ET `rejected` SONT DEUX CHOSES, et les confondre ferait
	// croire à un agent qu'on lui a refusé son argent alors que c'est son
	// numéro qui n'est pas enregistré. L'écran ne dit pas la même phrase.
	StateFailed = "failed"
)

// Payout est une demande de retrait.
type Payout struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID primitive.ObjectID `bson:"owner_id"`
	Country string             `bson:"country,omitempty"`
	// Mode : celui qui était en vigueur quand la demande est née.
	//
	// ⚠️ FIGÉ SUR LA DEMANDE, pas relu au moment de verser. Changer le mode
	// d'un pays ne doit pas transformer rétroactivement une demande en attente
	// de validation en versement automatique.
	Mode     string `bson:"mode"`
	Amount   int    `bson:"amount_xof"`
	Currency string `bson:"currency,omitempty"`
	// Phone est le numéro QUI REÇOIT.
	//
	// ⚠️⚠️ C'EST CELUI DU COMPTE, ET IL EST COPIÉ ICI AU MOMENT DE LA DEMANDE.
	// Pas saisi par l'agent, pas relu plus tard. Un champ libre est la porte
	// ouverte au détournement : quelqu'un qui obtient une session — un
	// téléphone prêté, un mot de passe donné de vive voix, ce qui arrive
	// souvent ici — n'aurait qu'à changer le numéro pour vider le compte.
	// Copié sur la demande, il reste aussi la trace de l'endroit où l'argent
	// est parti, même si le compte change de numéro ensuite.
	Phone string `bson:"phone"`
	State string `bson:"state"`
	// ProviderRef : l'identifiant chez l'agrégateur. C'est ce qu'on cite au
	// support quand l'argent n'arrive pas.
	ProviderRef string `bson:"provider_ref,omitempty"`
	// FailureReason : la phrase de l'agrégateur, telle quelle.
	FailureReason string `bson:"failure_reason,omitempty"`
	// DecidedBy, DecidedAt : qui a validé ou refusé, et quand.
	DecidedBy *primitive.ObjectID `bson:"decided_by,omitempty"`
	DecidedAt *time.Time          `bson:"decided_at,omitempty"`
	// RejectReason : ce que l'exploitation a répondu. ⚠️ Obligatoire sur un
	// refus : « refusé » sans raison fait rappeler le support, et l'agent n'a
	// aucune idée de ce qu'il doit corriger.
	RejectReason string     `bson:"reject_reason,omitempty"`
	CreatedAt    time.Time  `bson:"created_at"`
	SentAt       *time.Time `bson:"sent_at,omitempty"`
	SettledAt    *time.Time `bson:"settled_at,omitempty"`
}

// Open dit que cette demande attend encore quelque chose.
func (p Payout) Open() bool {
	return p.State == StatePending || p.State == StateSending
}

// Response est la demande telle que l'application et la console la lisent.
type Response struct {
	ID       string `json:"id"`
	Mode     string `json:"mode"`
	Amount   int    `json:"amount_xof"`
	Currency string `json:"currency,omitempty"`
	// Phone est MASQUÉ : les quatre derniers chiffres.
	//
	// ⚠️ L'AGENT SAIT QUEL EST SON NUMÉRO ; ce qu'il veut vérifier, c'est que
	// c'est bien le sien. Quatre chiffres suffisent pour ça, et un numéro
	// complet dans une réponse finit dans une capture d'écran partagée au
	// support, puis dans un canal de discussion.
	PhoneMasked   string     `json:"phone_masked"`
	State         string     `json:"state"`
	ProviderRef   string     `json:"provider_ref,omitempty"`
	FailureReason string     `json:"failure_reason,omitempty"`
	RejectReason  string     `json:"reject_reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	SettledAt     *time.Time `json:"settled_at,omitempty"`
}

func maskPhone(p string) string {
	if len(p) <= 4 {
		return p
	}
	return "••••" + p[len(p)-4:]
}

func toResponse(p Payout) Response {
	return Response{
		ID: p.ID.Hex(), Mode: p.Mode, Amount: p.Amount, Currency: p.Currency,
		PhoneMasked: maskPhone(p.Phone), State: p.State,
		ProviderRef: p.ProviderRef, FailureReason: p.FailureReason,
		RejectReason: p.RejectReason, CreatedAt: p.CreatedAt, SettledAt: p.SettledAt,
	}
}
