package ownfleet

import (
	"time"

	"github.com/kgtech-org/dira-core-api/internal/equipment"
)

// --- ce que l'exploitation envoie ---

// AssetInput est la fiche d'un véhicule de Dira.
//
// ⚠️ NI `status`, NI L'ATTRIBUTION. Le statut suit un cycle (`available` →
// `assigned` → `available`) tenu par `Reserve`/`Release` et par un geste nommé ;
// un champ libre dans la fiche aurait permis de poser « disponible » sur une
// voiture confiée à quelqu'un, et de la confier deux fois sans rien contourner.
type AssetInput struct {
	ClassKey  string   `json:"class_key" validate:"required,min=2,max=40"`
	Verticals []string `json:"verticals" validate:"omitempty,dive,oneof=food vtc"`
	Plate     string   `json:"plate" validate:"required,min=3,max=24"`
	Brand     string   `json:"brand" validate:"omitempty,max=60"`
	Model     string   `json:"model" validate:"omitempty,max=60"`
	Color     string   `json:"color" validate:"omitempty,max=40"`
	Year      int      `json:"year" validate:"omitempty,min=1950,max=2100"`
	Seats     int      `json:"seats" validate:"omitempty,min=1,max=60"`
	// OdometerKm : le compteur, relevé à la remise et au retour.
	OdometerKm         int        `json:"odometer_km" validate:"omitempty,min=0"`
	Photos             []string   `json:"photos" validate:"omitempty,max=8,dive,url"`
	AcquiredAt         *time.Time `json:"acquired_at"`
	AcquisitionXOF     int        `json:"acquisition_xof" validate:"omitempty,min=0"`
	InsuranceExpiresAt *time.Time `json:"insurance_expires_at"`
	TechnicalExpiresAt *time.Time `json:"technical_expires_at"`
	// Terms : les conditions par défaut. Facultatives à la création — un
	// véhicule peut entrer au parc avant que son loyer soit négocié.
	Terms *TermsInput `json:"terms"`
	Notes string      `json:"notes" validate:"omitempty,max=2000"`
}

// AssetPatch ne porte que ce qui change.
//
// ⚠️ CHAQUE CHAMP EST UN POINTEUR : sans cela, « ne pas toucher à la
// redevance » et « remettre la redevance à zéro » s'écrivent pareil — et
// corriger une faute de frappe dans le modèle effacerait les conditions. C'est
// la faute que la couche Immo a payée : un PUT partiel avait effacé trois prix.
type AssetPatch struct {
	ClassKey           *string     `json:"class_key" validate:"omitempty,min=2,max=40"`
	Verticals          *[]string   `json:"verticals" validate:"omitempty,dive,oneof=food vtc"`
	Plate              *string     `json:"plate" validate:"omitempty,min=3,max=24"`
	Brand              *string     `json:"brand" validate:"omitempty,max=60"`
	Model              *string     `json:"model" validate:"omitempty,max=60"`
	Color              *string     `json:"color" validate:"omitempty,max=40"`
	Year               *int        `json:"year" validate:"omitempty,min=1950,max=2100"`
	Seats              *int        `json:"seats" validate:"omitempty,min=1,max=60"`
	OdometerKm         *int        `json:"odometer_km" validate:"omitempty,min=0"`
	Photos             *[]string   `json:"photos" validate:"omitempty,max=8,dive,url"`
	AcquiredAt         *time.Time  `json:"acquired_at"`
	AcquisitionXOF     *int        `json:"acquisition_xof" validate:"omitempty,min=0"`
	InsuranceExpiresAt *time.Time  `json:"insurance_expires_at"`
	TechnicalExpiresAt *time.Time  `json:"technical_expires_at"`
	Terms              *TermsInput `json:"terms"`
	Notes              *string     `json:"notes" validate:"omitempty,max=2000"`
	// Status : seulement `available`, `maintenance` ou `retired`.
	//
	// ⚠️ `assigned` EST REFUSÉ ICI, et c'est la garde qui tient le registre :
	// confier un véhicule est un geste qui ouvre un contrat, pas un statut
	// qu'on tape. L'accepter aurait permis d'afficher « confié » sans redevance
	// — un véhicule dehors que rien ne facture.
	Status *string `json:"status" validate:"omitempty,oneof=available maintenance retired"`
}

// TermsInput : les conditions d'attribution par défaut d'un véhicule.
type TermsInput struct {
	// FeeXOF : LA REDEVANCE par période, en entier, dans la plus petite unité
	// de la monnaie du PAYS du véhicule.
	FeeXOF     int      `json:"fee_xof" validate:"min=0"`
	Period     string   `json:"period" validate:"omitempty,oneof=daily weekly biweekly monthly"`
	DepositXOF int      `json:"deposit_xof" validate:"min=0"`
	Days       int      `json:"days" validate:"omitempty,min=1,max=3650"`
	Included   []string `json:"included" validate:"omitempty,dive,oneof=maintenance repairs insurance fuel tracker"`
}

// AssignInput : LES CONDITIONS D'ATTRIBUTION, pour cette personne et ce
// véhicule.
//
// ⚠️ TOUT EST FACULTATIF SAUF LA PERSONNE ET LA VERTICALE : ce qui manque vient
// des conditions du véhicule, puis des réglages du pays. Rien n'est laissé à
// deviner, et tout peut être surchargé — la même cascade que le matériel.
type AssignInput struct {
	UserID   string `json:"user_id" validate:"required,len=24,hexadecimal"`
	Vertical string `json:"vertical" validate:"required,oneof=food vtc"`
	// FeeXOF, Period, DepositXOF, Days, Included : absents = ceux du véhicule.
	FeeXOF     *int      `json:"fee_xof" validate:"omitempty,min=1"`
	Period     string    `json:"period" validate:"omitempty,oneof=daily weekly biweekly monthly"`
	DepositXOF *int      `json:"deposit_xof" validate:"omitempty,min=0"`
	Days       *int      `json:"days" validate:"omitempty,min=1,max=3650"`
	Included   *[]string `json:"included" validate:"omitempty,dive,oneof=maintenance repairs insurance fuel tracker"`
	// Plan : la façon dont l'argent revient — retenue sur les gains, grâce,
	// pénalité, blocage. Absent = les défauts du pays (`equipment`).
	//
	// ⚠️ C'EST LE PLAN DU MATÉRIEL, PAS UN SECOND : un seul moteur de retenue
	// existe sur cette plateforme, et une voiture n'en mérite pas un autre.
	Plan  *equipment.Plan `json:"plan"`
	Notes string          `json:"notes" validate:"omitempty,max=2000"`
}

// --- ce que la console et l'application lisent ---

// TermsResponse sont les conditions, telles qu'elles s'affichent.
type TermsResponse struct {
	FeeXOF     int      `json:"fee_xof"`
	Period     string   `json:"period"`
	DepositXOF int      `json:"deposit_xof"`
	Days       int      `json:"days"`
	Included   []string `json:"included"`
	// Excluded : ce que la redevance NE couvre PAS.
	//
	// ⚠️ SERVI, ET C'EST VOLONTAIRE PLUTÔT QUE LAISSÉ À L'ÉCRAN. « Inclus :
	// entretien » laisse croire que l'assurance l'est aussi ; la question se
	// pose pour de bon le jour de la panne, devant quelqu'un qui n'a pas écrit
	// la liste. Les deux faces viennent donc du serveur, qui seul connaît le
	// vocabulaire complet.
	Excluded []string `json:"excluded"`
}

// AssetResponse est un véhicule de la flotte interne.
type AssetResponse struct {
	ID       string `json:"id"`
	Country  string `json:"country"`
	FleetID  string `json:"fleet_id"`
	ClassKey string `json:"class_key"`
	// Verticals : vide = les deux métiers.
	Verticals          []string   `json:"verticals"`
	Plate              string     `json:"plate"`
	Brand              string     `json:"brand,omitempty"`
	Model              string     `json:"model,omitempty"`
	Color              string     `json:"color,omitempty"`
	Year               int        `json:"year,omitempty"`
	Seats              int        `json:"seats,omitempty"`
	OdometerKm         int        `json:"odometer_km,omitempty"`
	Photos             []string   `json:"photos"`
	AcquiredAt         *time.Time `json:"acquired_at,omitempty"`
	AcquisitionXOF     int        `json:"acquisition_xof,omitempty"`
	InsuranceExpiresAt *time.Time `json:"insurance_expires_at,omitempty"`
	TechnicalExpiresAt *time.Time `json:"technical_expires_at,omitempty"`
	Status             string     `json:"status"`
	// Label est ce qu'on écrit sur une ligne et sur un contrat :
	// « Toyota Corolla · GA-1234-AB ».
	Label string        `json:"label"`
	Terms TermsResponse `json:"terms"`
	// L'ATTRIBUTION EN COURS.
	HolderUserID string     `json:"holder_user_id,omitempty"`
	HolderName   string     `json:"holder_name,omitempty"`
	ContractID   string     `json:"contract_id,omitempty"`
	AssignedAt   *time.Time `json:"assigned_at,omitempty"`
	// Fee est LA REDEVANCE EN COURS, telle que le contrat la porte — pas celle
	// de la fiche.
	//
	// ⚠️ LA DIFFÉRENCE COMPTE : les conditions du véhicule sont un DÉFAUT pour
	// la prochaine attribution ; ce qui engage l'agent est figé sur son
	// contrat. Servir la fiche à sa place aurait affiché un loyer qui n'est pas
	// celui qu'il paie, le jour où quelqu'un met le parc à jour.
	Fee *FeeResponse `json:"fee,omitempty"`
	// Attention : ce qui demande un geste — impayé, terme dépassé, papiers
	// périmés. Vide = rien à faire.
	//
	// ⚠️ CALCULÉ PAR LE SERVEUR, parce que deux écrans qui le calculent
	// eux-mêmes ne comptent jamais pareil — et que la console et l'application
	// doivent dire la même chose à la même personne.
	Attention []string  `json:"attention"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FeeResponse est ce que la redevance en cours doit — un EXTRAIT du contrat,
// pas une seconde comptabilité.
//
// ⚠️ TOUS CES NOMBRES VIENNENT DU CONTRAT (`internal/equipment`) ET NE SONT
// JAMAIS RECALCULÉS ICI. Un second calcul d'« impayé » aurait fini par
// contredire l'échéancier : c'est le genre d'écart qu'on découvre quand
// quelqu'un conteste, et il a raison deux fois sur trois.
type FeeResponse struct {
	ContractID     string     `json:"contract_id"`
	Status         string     `json:"status"`
	FeeXOF         int        `json:"fee_xof"`
	Period         string     `json:"period"`
	DepositXOF     int        `json:"deposit_xof"`
	PaidXOF        int        `json:"paid_xof"`
	OutstandingXOF int        `json:"outstanding_xof"`
	DueXOF         int        `json:"due_xof"`
	OverdueSince   *time.Time `json:"overdue_since,omitempty"`
	// Blocked : l'agent ne peut plus se mettre en ligne. ⚠️ C'est le MÊME
	// blocage que le matériel (`402 equipment_overdue`), pas un second.
	Blocked  bool       `json:"blocked"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
	TermOver bool       `json:"term_over"`
	Included []string   `json:"included"`
	HandedAt *time.Time `json:"handed_at,omitempty"`
}

// Les motifs d'attention, nommés pour que la console et l'application disent le
// même mot.
const (
	AttentionOverdue    = "fee_overdue"       // la redevance est en retard
	AttentionBlocked    = "agent_blocked"     // l'agent ne peut plus travailler
	AttentionTermOver   = "term_over"         // la durée convenue est passée
	AttentionNotHanded  = "not_handed"        // attribué, jamais remis
	AttentionInsurance  = "insurance_expired" // assurance périmée
	AttentionTechnical  = "technical_expired" // visite technique périmée
	AttentionNoContract = "assigned_no_fee"   // confié sans redevance lisible
)

// toAsset rend un véhicule tel que les écrans le lisent.
func toAsset(a *Asset) AssetResponse {
	photos, verticals := a.Photos, a.Verticals
	if photos == nil {
		photos = []string{}
	}
	if verticals == nil {
		// ⚠️ UN TABLEAU VIDE ET NON `null` : une console qui itère sur `null`
		// s'arrête, et « les deux métiers » se lit mieux comme une liste vide
		// que comme une absence. Même convention que les audiences d'un
		// article de matériel.
		verticals = []string{}
	}
	out := AssetResponse{
		ID: a.ID.Hex(), Country: a.Country, FleetID: a.FleetID.Hex(),
		ClassKey: a.ClassKey, Verticals: verticals, Plate: a.Plate,
		Brand: a.Brand, Model: a.Model, Color: a.Color, Year: a.Year, Seats: a.Seats,
		OdometerKm: a.OdometerKm, Photos: photos,
		AcquiredAt: a.AcquiredAt, AcquisitionXOF: a.AcquisitionXOF,
		InsuranceExpiresAt: a.InsuranceExpiresAt, TechnicalExpiresAt: a.TechnicalExpiresAt,
		Status: a.Status, Label: labelOf(a), Terms: toTerms(a.Terms),
		AssignedAt: a.AssignedAt, Notes: a.Notes,
		Attention: []string{},
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if a.HolderUserID != nil {
		out.HolderUserID = a.HolderUserID.Hex()
	}
	if a.ContractID != nil {
		out.ContractID = a.ContractID.Hex()
	}
	return out
}

// toTerms sert les conditions, et les DEUX FACES de ce qui est inclus.
func toTerms(t Terms) TermsResponse {
	inc := t.Included
	if inc == nil {
		inc = []string{}
	}
	out := TermsResponse{
		FeeXOF: t.FeeXOF, Period: t.Period, DepositXOF: t.DepositXOF,
		Days: t.Days, Included: inc, Excluded: excluded(inc),
	}
	return out
}

// excluded rend ce que la redevance NE couvre PAS, dans l'ordre du vocabulaire.
//
// ⚠️ CALCULÉ PAR LE SERVEUR, parce que lui seul connaît la liste complète. Une
// console qui la recopie afficherait une face incomplète le jour où un poste
// s'ajoute — et c'est la face qui coûte de l'argent à quelqu'un.
func excluded(included []string) []string {
	has := make(map[string]bool, len(included))
	for _, v := range included {
		has[v] = true
	}
	out := make([]string, 0, len(equipment.Included))
	for _, known := range equipment.Included {
		if !has[known] {
			out = append(out, known)
		}
	}
	return out
}

// feeOf extrait du CONTRAT ce que la redevance doit.
//
// ⚠️ AUCUN NOMBRE N'EST RECALCULÉ ICI. Tout vient du contrat : un second calcul
// d'« impayé » aurait fini par contredire l'échéancier, et celui qui conteste a
// raison deux fois sur trois.
func feeOf(c *equipment.ContractResponse) *FeeResponse {
	if c == nil {
		return nil
	}
	inc := c.Included
	if inc == nil {
		inc = []string{}
	}
	return &FeeResponse{
		ContractID: c.ID, Status: c.Status,
		FeeXOF: c.PriceXOF, Period: c.Plan.Period, DepositXOF: c.DepositXOF,
		PaidXOF: c.PaidXOF, OutstandingXOF: c.OutstandingXOF, DueXOF: c.DueXOF,
		OverdueSince: c.OverdueSince, Blocked: c.Blocked,
		EndsAt: c.EndsAt, TermOver: c.TermOver, Included: inc,
		HandedAt: c.HandedAt,
	}
}

// Vocabulary est ce que le code connaît — pour que la console propose sans
// deviner, et sans recopier une liste qui bougera.
type Vocabulary struct {
	Statuses []string `json:"statuses"`
	Included []string `json:"included"`
	Periods  []string `json:"periods"`
}

// Words rend le vocabulaire de la flotte interne.
//
// ⚠️ LES PÉRIODES ET LES POSTES INCLUS VIENNENT DU MATÉRIEL, pas d'une copie :
// c'est le même vocabulaire, parce que c'est le même moteur. Deux listes
// auraient divergé au premier ajout, et la console aurait proposé un pas que le
// contrat refuse.
func Words() Vocabulary {
	return Vocabulary{Statuses: Statuses, Included: equipment.Included, Periods: equipment.Periods}
}
