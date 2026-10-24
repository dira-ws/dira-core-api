package equipment

// LA LOCATION D'UN BIEN UNIQUE — une voiture, une moto, un tricycle que DIRA
// POSSÈDE et confie à un chauffeur ou à un livreur contre une REDEVANCE.
//
// ⚠️⚠️ POURQUOI ICI ET NON DANS UN SECOND MODULE : PARCE QU'IL NE PEUT EXISTER
// QU'UN SEUL MOTEUR DE RETENUE SUR LES GAINS. `Collect` est appelé au règlement
// de chaque course et de chaque commande ; il parcourt TOUS les contrats de la
// personne avec un budget commun — `already` — et applique `MinLeftXOF`, le
// plafond du jour et celui de la semaine à l'ensemble. Une redevance calculée
// par un second moteur n'aurait rien su de ce budget : le casque aurait pris sa
// part, la voiture la sienne, et l'agent serait rentré chez lui avec zéro un
// soir de petite recette — en ayant respecté les deux plafonds séparément.
// C'est exactement la divergence qu'une seconde implémentation produit, et
// c'est l'agent qui l'aurait payée.
//
// Ce qu'une voiture ajoute à un casque est donc PEU DE CHOSE, et c'est tout ce
// que ce fichier contient :
//
//   - elle n'est pas un article de CATALOGUE : il n'y a pas « des Corolla » en
//     stock, il y a LA Corolla GA-1234-AB. Le contrat ne porte donc pas
//     d'`item_id`, mais un `asset_id` — un bien unique, dont le registre vit
//     dans `internal/ownfleet` ;
//   - deux attributions du même bien doivent être IMPOSSIBLES, et cette
//     garantie ne peut pas vivre ici : c'est le registre qui la tient, par une
//     écriture conditionnelle (voir `ownfleet.Repository.Reserve`) ;
//   - ce qui est INCLUS dans la redevance — entretien, réparations, assurance,
//     carburant — se dit sur le contrat, parce que c'est une condition
//     d'attribution et non une propriété du véhicule : la même voiture peut
//     partir avec l'entretien inclus chez l'un et pas chez l'autre ;
//   - la DURÉE convenue (`EndsAt`), qui ne ferme rien toute seule — voir plus
//     bas.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// KindVehicle est le genre porté par un contrat de redevance.
//
// ⚠️ VOLONTAIREMENT ABSENT DE `Kinds`, la liste du CATALOGUE. Un véhicule ne
// s'enregistre pas comme un gilet : il n'a pas de stock, il a une plaque. L'y
// ajouter aurait fait apparaître « véhicule » dans le formulaire d'article de
// la console, et quelqu'un aurait fini par créer un article « Corolla » avec
// trois unités en stock — trois voitures que rien ne distingue, et une
// attribution qui ne dit pas laquelle est partie.
const KindVehicle = "vehicle"

// Ce que la redevance PEUT inclure.
//
// ⚠️ UNE LISTE FERMÉE ET NON UN CHAMP LIBRE, parce que c'est la première
// question posée quand une voiture tombe en panne : « qui paie ? ». Une phrase
// libre se lit de deux façons le jour où il faut trancher, et c'est l'agent qui
// perd l'arbitrage — il n'a pas écrit la phrase. Ce qui N'EST PAS dans la liste
// d'un contrat est à la charge de l'agent, et l'écran le dit dans les deux
// sens.
const (
	IncludedMaintenance = "maintenance" // l'entretien courant (vidange, filtres)
	IncludedRepairs     = "repairs"     // les réparations : la panne, pas l'usure
	IncludedInsurance   = "insurance"   // l'assurance du véhicule
	IncludedFuel        = "fuel"        // le carburant
	IncludedTracker     = "tracker"     // le boîtier de suivi et son abonnement
)

// Included est ce que le code connaît — pour que la console propose sans
// deviner.
var Included = []string{IncludedMaintenance, IncludedRepairs, IncludedInsurance, IncludedFuel, IncludedTracker}

// AssetDesk est le REGISTRE DES BIENS UNIQUES, vu depuis le contrat.
//
// ⚠️ DÉCLARÉE ICI, CÔTÉ CONSOMMATEUR, comme `Fleets` : ce module dit ce dont il
// a besoin — rendre le bien au parc quand le contrat se termine — et ignore qui
// le fait. Le registre, lui, ne connaît pas l'argent.
//
// ⚠️⚠️ ET IL N'Y A QUE `Release`, PAS DE `Reserve`. La réservation doit être la
// MÊME écriture que le choix du bien, sinon deux attributions simultanées
// passent toutes les deux : c'est donc l'appelant qui réserve, puis ouvre le
// contrat. Ici, on ne connaît que le chemin du RETOUR — et il doit exister,
// parce que le piège maison de cette plateforme est précisément un champ que
// personne ne remplissait et un rattachement que rien ne défaisait.
type AssetDesk interface {
	Release(ctx context.Context, assetID, contractID, outcome string) error
}

// SetAssets branche le registre (câblage). Sans lui, un contrat de bien unique
// ne peut pas être ouvert — `OpenAssetRental` refuse plutôt que de créer une
// redevance sur un véhicule que personne ne saura rendre au parc.
func (s *Service) SetAssets(a AssetDesk) { s.assets = a }

// Les issues d'un contrat de bien, telles qu'elles arrivent au registre.
const (
	OutcomeReturned  = "returned"  // rendu : le bien revient au parc
	OutcomeDefaulted = "defaulted" // impayé constaté, contrat clos
	OutcomeCancelled = "cancelled" // annulé avant la remise
)

var (
	errNoAssetDesk = apperr.Conflict("equipment_asset_desk_unavailable",
		"the internal fleet desk is not available on this deployment")
	errAssetNeedsRental = apperr.Validation("an asset contract is a rental").
				WithMeta(map[string]any{"fields": []string{"mode"}, "reason": "asset_rental_only"})
)

// AssetRentalInput : les CONDITIONS D'ATTRIBUTION, telles que la flotte interne
// les fixe.
type AssetRentalInput struct {
	// ContractID est fourni par l'APPELANT, et c'est délibéré.
	//
	// ⚠️ C'EST CE QUI REND LA DOUBLE ATTRIBUTION IMPOSSIBLE. Le registre
	// réserve le bien et inscrit l'identifiant du contrat dans la MÊME écriture
	// conditionnelle, AVANT que ce contrat existe ; si deux attributions
	// partent en même temps, la seconde ne réserve rien et n'ouvre donc aucune
	// redevance. L'ordre inverse — ouvrir puis réserver — aurait laissé un
	// contrat orphelin sur un véhicule déjà parti, c'est-à-dire une dette sans
	// voiture.
	ContractID primitive.ObjectID
	AssetID    primitive.ObjectID
	// Label nomme le bien sur le contrat : « Toyota Corolla · GA-1234-AB ».
	//
	// ⚠️ FIGÉ À L'OUVERTURE, comme le nom d'un article : la plaque d'un véhicule
	// change (réimmatriculation, revente), et un contrat de redevance se relit
	// deux ans plus tard devant quelqu'un qui contexte un prélèvement.
	Label      string
	UserID     primitive.ObjectID
	Vertical   string
	FeeXOF     int // LA REDEVANCE, par période
	DepositXOF int
	// Days : la durée convenue en jours. Zéro = sans terme.
	Days     int
	Included []string
	Plan     *Plan
	Notes    string
	ActorID  string
}

// OpenAssetRental ouvre la redevance d'un bien unique — un contrat de LOCATION
// comme un autre, au même échéancier et au même moteur de retenue.
//
// ⚠️ IL N'Y A PAS DE `HandOverNow` ICI, et c'est la seule différence de parcours
// avec le matériel. Un casque se tend par-dessus un comptoir ; une voiture se
// remet avec un état des lieux, des clés et des papiers. La remise passe donc
// par le même geste prouvé que le matériel — le QR scanné au comptoir
// (`MintHandoverCode` / `ScanHandover`) —, et c'est elle qui démarre
// l'échéancier. Ouvrir ET remettre en un appel aurait fait courir la redevance
// d'un véhicule que l'agent n'a pas encore.
func (s *Service) OpenAssetRental(ctx context.Context, in AssetRentalInput) (*ContractResponse, error) {
	if s.assets == nil {
		return nil, errNoAssetDesk
	}
	if in.AssetID.IsZero() || in.ContractID.IsZero() {
		return nil, apperr.Validation("asset_id and contract_id are required")
	}
	cc := country.FromContext(ctx)
	st, err := s.repo.Settings(ctx, cc)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	// ⚠️ LE MÊME INTERRUPTEUR DE PAYS QUE POUR UN CASQUE : un pays qui n'ouvre
	// pas la LOCATION n'a pas à la voir arriver par la flotte interne. Un
	// réglage contourné par une seconde porte n'est plus un réglage.
	if !contains(st.AllowedModes, ModeRental) {
		return nil, errModeNotAllowed
	}
	// La personne existe, et elle est de ce pays.
	//
	// ⚠️ LE MÊME CONTRÔLE QUE `CreateContract`, et il compte double ici : la
	// redevance est libellée dans la monnaie du pays du véhicule. Confier une
	// voiture gabonaise à un compte guinéen aurait inscrit un loyer de 150 000
	// sur un échéancier lu en francs guinéens — quinze fois moins, et personne
	// ne l'aurait vu passer.
	if s.accounts != nil {
		who, err := s.accounts.CountryOf(ctx, in.UserID.Hex())
		if err != nil {
			return nil, err
		}
		if who != "" && who != cc {
			return nil, apperr.Validation("this account belongs to another country").
				WithMeta(map[string]any{"fields": []string{"user_id"}, "reason": "other_country"})
		}
	}
	plan := st.Defaults
	if in.Plan != nil {
		plan = merge(*in.Plan, plan)
	}
	plan = fitPlanToMode(plan, ModeRental)
	if plan.Schedule != SchedulePerPeriod {
		// fitPlanToMode l'impose déjà ; la garde dit pourquoi on y tient.
		return nil, errAssetNeedsRental
	}
	if err := validatePlan(plan, st.MaxEarningsPercent); err != nil {
		return nil, err
	}
	if in.FeeXOF <= 0 {
		return nil, apperr.Validation("the fee must be a positive amount").
			WithMeta(map[string]any{"fields": []string{"fee_xof"}})
	}
	now := s.now()
	c := &Contract{
		ID: in.ContractID, Country: cc, UserID: in.UserID, Vertical: in.Vertical,
		// ⚠️ `ItemID` RESTE NUL : il n'y a pas d'article de catalogue derrière
		// une voiture. C'est ce qui fait que l'ajustement de stock de la remise
		// ne touche rien (voir `handOver`) et que la console n'affiche pas un
		// identifiant de vingt-quatre zéros (voir `toContract`).
		ItemName: strings.TrimSpace(in.Label), ItemKind: KindVehicle,
		AssetID: &in.AssetID, AssetLabel: strings.TrimSpace(in.Label),
		Included: normaliseIncluded(in.Included),
		Quantity: 1, Mode: ModeRental, Plan: plan, Status: StatusDraft,
		PriceXOF: in.FeeXOF, DepositXOF: in.DepositXOF,
		Notes: strings.TrimSpace(in.Notes), CreatedBy: in.ActorID,
	}
	if in.Days > 0 {
		ends := now.AddDate(0, 0, in.Days)
		c.EndsAt = &ends
	}
	// ⚠️ L'ACCEPTATION SUIT LE RÉGLAGE DU PAYS, comme pour le matériel : là où
	// elle n'est pas exigée, l'exploitation remet directement.
	if !st.RequireAcceptance {
		c.Status = StatusAccepted
		c.AcceptedAt = &now
	}
	if err := s.repo.InsertContract(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	s.record(ctx, "equipment.asset_rental", c, nil)
	s.notify(ctx, c.UserID.Hex(), KeyContractProposed,
		map[string]string{"item": c.ItemName, "mode": modeLabel(c.Mode)}, s.data(c))
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// normaliseIncluded garde ce que le code connaît, une fois chacun, dans
// l'ordre déclaré.
//
// ⚠️ L'ORDRE DÉCLARÉ ET NON CELUI DE LA SAISIE : cette liste s'affiche sur
// l'écran de l'agent, et deux contrats du même pays doivent se comparer d'un
// coup d'œil. « Assurance, entretien » contre « entretien, assurance » fait
// chercher une différence qui n'existe pas.
func normaliseIncluded(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		seen[strings.ToLower(strings.TrimSpace(v))] = true
	}
	out := make([]string, 0, len(in))
	for _, known := range Included {
		if seen[known] {
			out = append(out, known)
		}
	}
	return out
}

// releaseAsset rend le bien au parc — LE CHEMIN INVERSE, appelé par le retour,
// le constat d'impayé et l'annulation.
//
// ⚠️⚠️ APPELÉ DEPUIS LES TROIS SORTIES, ET C'EST LE POINT DE CE FICHIER. Le
// piège maison de cette plateforme est un rattachement que RIEN ne défaisait :
// le champ existait, un seul chemin le remplissait, aucun ne l'effaçait, et
// l'écran paraissait normal. Un véhicule dont le contrat est clos et qui reste
// « attribué » est un véhicule que l'exploitation ne peut plus confier à
// personne — et la seule issue connue serait une écriture directe en base.
//
// ⚠️ ET L'ÉCHEC EST BRUYANT MAIS NON BLOQUANT. Refuser le retour parce que le
// registre n'a pas répondu laisserait un contrat vivant sur un véhicule
// physiquement rendu : la redevance continuerait de courir. Mieux vaut un parc à
// recaler — le journal dit lequel — qu'un agent qui paie une voiture qu'il a
// rendue.
func (s *Service) releaseAsset(ctx context.Context, c *Contract, outcome string) {
	if c.AssetID == nil {
		return
	}
	if s.assets == nil {
		slog.ErrorContext(ctx, "equipment: asset not released, no desk wired",
			"contract_id", c.ID.Hex(), "asset_id", c.AssetID.Hex(), "outcome", outcome)
		return
	}
	if err := s.assets.Release(ctx, c.AssetID.Hex(), c.ID.Hex(), outcome); err != nil {
		slog.ErrorContext(ctx, "equipment: asset not released",
			"contract_id", c.ID.Hex(), "asset_id", c.AssetID.Hex(), "outcome", outcome, "error", err)
	}
}

// TermOver dit si la durée convenue est passée.
//
// ⚠️ ELLE NE FERME RIEN, et c'est une décision assumée. Une voiture ne revient
// pas au parc parce qu'une date est passée : il faut que quelqu'un la reprenne,
// constate son état et solde la caution. Clore le contrat tout seul au terme
// aurait arrêté la redevance d'un véhicule toujours dehors — exactement
// l'inverse de ce qu'on veut. Le terme est donc SERVI, l'exploitation le voit
// dans sa liste, et c'est elle qui reprend.
func (c *Contract) TermOver(now time.Time) bool {
	return c.EndsAt != nil && !c.EndsAt.After(now)
}
