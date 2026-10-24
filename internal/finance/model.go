// Package finance : LA COMPTABILITÉ de la plateforme, et ses règles d'argent.
//
// Quatre choses vivent ici, parce qu'elles se lisent ensemble :
//
//   - la FACTURATION par pays et par métier (`Billing`) : comment la
//     plateforme se rémunère sur un agent — des JETONS à l'acceptation, ou
//     une COMMISSION en pourcentage — et À QUELLE ÉTAPE le client est
//     débité ;
//   - le JOURNAL en partie double (`Entry`) : chaque mouvement d'argent ou de
//     jetons du grand livre des portefeuilles y écrit une écriture qui
//     débite un compte et en crédite un autre, pour que l'on puisse répondre
//     « qu'est-ce que la plateforme a gagné, doit, détient ? » sans relire
//     chaque portefeuille ;
//   - l'INTÉGRITÉ (`Finding`) : un balayage qui recalcule chaque solde depuis
//     ses mouvements, vérifie que le journal est équilibré et que chaque
//     mouvement a son écriture — et alerte quand ce n'est pas le cas. Un
//     solde modifié sans mouvement se voit ; une écriture manquante aussi ;
//   - les DÉCLARATIONS (`Declaration`, voir `declaration.go`) : l'argent qui
//     entre et sort SANS passer par un portefeuille — un loyer, du carburant,
//     un apport d'associé, une subvention. Elles écrivent dans le MÊME
//     journal, en partie double, et se contre-passent au lieu de se modifier.
package finance

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collections.
const (
	billingCollection  = "finance_billing"
	entriesCollection  = "accounting_entries"
	findingsCollection = "finance_findings"
)

// --- la facturation ---

// Comment un AGENT paie la plateforme.
const (
	// ChargeTokens : des jetons à l'acceptation (la grille de la verticale).
	// Les frais de la course lui reviennent en entier.
	ChargeTokens = "tokens"
	// ChargeCommission : un pourcentage du prix, retenu sur ce que la
	// plateforme lui verse — ou porté à sa dette quand l'argent ne transite
	// pas par elle (espèces).
	ChargeCommission = "commission"
)

// À quelle ÉTAPE le client est débité (portefeuille Dira ; le mobile money
// se paie d'avance, les espèces à la remise).
const (
	// Courses.
	ClientAtRequest  = "request"  // à la commande
	ClientAtAccept   = "accept"   // quand un chauffeur/livreur s'engage
	ClientAtStart    = "start"    // passager à bord / commande retirée
	ClientAtComplete = "complete" // déposé / livré
)

// Verticales.
const (
	VerticalRides      = "rides"
	VerticalDeliveries = "deliveries"
)

// VerticalBilling : la règle d'un métier dans un pays.
type VerticalBilling struct {
	// AgentCharge : `tokens` ou `commission`.
	AgentCharge string `bson:"agent_charge" json:"agent_charge"`
	// CommissionPct : le pourcentage retenu quand `commission`. Pour les
	// courses, 0 = celui de la classe du véhicule ; pour la livraison, 0 =
	// rien de retenu.
	CommissionPct int `bson:"commission_pct" json:"commission_pct"`
	// Les JETONS quand `tokens` : un forfait par course, ou une grille au
	// kilomètre bornée. La livraison garde sa propre grille (`platform_pricing`)
	// quand ces trois-là sont à zéro.
	TokensPerRide int `bson:"tokens_per_ride" json:"tokens_per_ride"`
	TokensPerKm   int `bson:"tokens_per_km" json:"tokens_per_km"`
	TokensMin     int `bson:"tokens_min" json:"tokens_min"`
	TokensMax     int `bson:"tokens_max" json:"tokens_max"`
	// ClientChargeAt : `request` · `accept` · `start` · `complete`.
	ClientChargeAt string `bson:"client_charge_at" json:"client_charge_at"`
	// DebtLimitXOF : au-delà de cette dette (commission sur espèces non
	// réglée), l'agent ne reçoit plus d'appel. 0 = pas de plafond.
	DebtLimitXOF int `bson:"debt_limit_xof" json:"debt_limit_xof"`
}

// Billing : les règles d'un pays.
type Billing struct {
	Country    string          `bson:"_id" json:"country"`
	Rides      VerticalBilling `bson:"rides" json:"rides"`
	Deliveries VerticalBilling `bson:"deliveries" json:"deliveries"`
	UpdatedBy  string          `bson:"updated_by,omitempty" json:"updated_by,omitempty"`
	UpdatedAt  time.Time       `bson:"updated_at" json:"updated_at"`
}

// DefaultBilling : ce que la plateforme fait quand rien n'est réglé — ce
// qu'elle a toujours fait. Les courses se paient en commission à la classe,
// le client est débité à l'acceptation ; la livraison se paie en jetons, le
// client est débité à la commande.
func DefaultBilling(country string) Billing {
	return Billing{
		Country:    country,
		Rides:      VerticalBilling{AgentCharge: ChargeCommission, ClientChargeAt: ClientAtAccept, DebtLimitXOF: 10000},
		Deliveries: VerticalBilling{AgentCharge: ChargeTokens, ClientChargeAt: ClientAtRequest},
	}
}

// --- le journal ---

// Unités.
const (
	UnitXOF   = "xof"
	UnitToken = "token"
)

// Les COMPTES. Un plan court, en lecture directe : chaque nom dit ce qu'il
// contient. Les passifs sont ce que la plateforme DOIT, les actifs ce
// qu'elle DÉTIENT ou qu'on lui doit, les produits ce qu'elle a GAGNÉ.
const (
	// Actifs.
	AccMobileMoney     = "mobile_money"     // ce que les prestataires de paiement ont encaissé pour nous
	AccAgentReceivable = "agent_receivable" // ce que les agents et clients nous doivent (commission sur espèces, paiements dus)
	// Passifs.
	AccClientWallets    = "client_wallets"     // l'argent des clients sur leur Dira Cash
	AccPromoCredits     = "promo_credits"      // le crédit offert, pas encore dépensé
	AccMerchantPayables = "merchant_payables"  // ce que l'on doit aux marchands
	AccAgentPayables    = "agent_payables"     // ce que l'on doit aux livreurs et chauffeurs
	AccAgentTokens      = "agent_tokens"       // jetons prépayés non consommés (produit constaté d'avance)
	AccEquipmentDeposit = "equipment_deposits" // cautions à rendre
	// Transit : une commande ou une course payée, pas encore répartie.
	AccOrderClearing = "order_clearing"
	AccRideClearing  = "ride_clearing"
	// Produits.
	AccRevenueCommission = "revenue_commission"
	AccRevenueTokens     = "revenue_tokens"
	AccRevenueEquipment  = "revenue_equipment"
	AccRevenueFees       = "revenue_fees"
	// Charges.
	AccExpensePromo    = "expense_promo"
	AccExpenseCredits  = "expense_operator_credits"
	AccExpenseWriteoff = "expense_writeoff"

	// LES COMPTES DES DÉCLARATIONS (voir `declaration.go`). L'argent que la
	// plateforme dépense et reçoit HORS de ce qu'elle fait circuler : un
	// loyer, un plein, un apport d'associé, une subvention.
	//
	// ⚠️ La TRÉSORERIE d'abord, et elle manquait : le plan ne savait nommer
	// que `mobile_money`, donc un loyer payé en espèces n'avait aucune
	// contrepartie à créditer. Une caisse et une banque, c'est ce qui permet
	// de rapprocher une déclaration d'un relevé.
	AccCashBox = "cash_box" // la caisse, en espèces
	AccBank    = "bank"     // les comptes en banque

	AccExpenseRent        = "expense_rent"
	AccExpenseUtilities   = "expense_utilities"
	AccExpenseFuel        = "expense_fuel"
	AccExpenseMaintenance = "expense_maintenance"
	// ⚠️ AccExpenseSalaries est LA COUTURE comptable laissée à la paie des
	// salariés : le compte existe, la catégorie `salaries` le meut, et le
	// module de paie n'aura qu'à passer par `DeclareFromModule` pour que ses
	// versements apparaissent ici. Aucun code de la finance ne connaît la
	// paie, et réciproquement.
	AccExpenseSalaries = "expense_salaries"
	AccExpenseTax      = "expense_tax"
	AccExpenseFine     = "expense_fine"
	AccExpenseOther    = "expense_other"
	// Immobilisations : un véhicule acheté n'est pas une charge, c'est un
	// actif. L'y mettre aurait fait passer le résultat du mois en perte pour
	// un achat qui enrichit la plateforme.
	AccFixedAssets = "fixed_assets"
	// ⚠️ Les apports des associés sont en CAPITAUX PROPRES depuis la sixième
	// nature (`KindEquity`), et non plus en passif. Ils y étaient par défaut,
	// faute d'une nature pour les porter ; mais un passif est de l'argent dû à
	// un TIERS qui peut le réclamer, et mêler les deux rendait la ligne
	// « Passif » de l'aperçu illisible — on ne pouvait plus répondre « combien
	// devons-nous réellement à des gens hors de la maison ? ».
	AccOwnerContributions = "owner_contributions"
	AccRevenueSubsidy     = "revenue_subsidy"
	AccRevenueOther       = "revenue_other"
)

// Kinds de compte, pour l'aperçu.
const (
	KindAsset     = "asset"
	KindLiability = "liability"
	KindClearing  = "clearing"
	KindRevenue   = "revenue"
	KindExpense   = "expense"
	// KindEquity : LES CAPITAUX PROPRES — ce que les associés ont mis dans la
	// plateforme, et ce que la plateforme a gagné sans le distribuer.
	//
	// ⚠️ SIXIÈME NATURE, ET IL EN FALLAIT UNE. Les apports des associés étaient
	// rangés en PASSIF faute de mieux : un passif est de l'argent dû à un
	// TIERS — un marchand, un client, un livreur — qu'il peut réclamer demain.
	// Un apport d'associé n'est pas de cette nature, et les mêler rendait la
	// ligne « Passif » de l'aperçu illisible : on ne pouvait plus répondre
	// « combien devons-nous réellement à des gens hors de la maison ? ».
	//
	// ⚠️ ELLE SE SOLDE DU CÔTÉ CRÉDIT, comme un passif et un produit : c'est
	// pour cela qu'elle n'entre PAS dans la liste des natures dont le solde se
	// lit à l'envers (actif, charge). Et elle n'entre pas dans le résultat
	// (`Net = produits − charges`) : un apport n'est pas un gain.
	KindEquity = "equity"
)

// Account décrit un compte du plan.
type Account struct {
	Code  string `json:"code"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

// Chart est le plan comptable, dans l'ordre de lecture.
var Chart = []Account{
	{AccMobileMoney, KindAsset, "Mobile money encaissé"},
	{AccAgentReceivable, KindAsset, "Créances (agents, clients)"},
	{AccClientWallets, KindLiability, "Soldes clients (Dira Cash)"},
	{AccPromoCredits, KindLiability, "Crédits promotionnels"},
	{AccMerchantPayables, KindLiability, "Dû aux marchands"},
	{AccAgentPayables, KindLiability, "Dû aux livreurs et chauffeurs"},
	{AccAgentTokens, KindLiability, "Jetons prépayés"},
	{AccEquipmentDeposit, KindLiability, "Cautions de matériel"},
	{AccOrderClearing, KindClearing, "Commandes payées à répartir"},
	{AccRideClearing, KindClearing, "Courses payées à répartir"},
	{AccRevenueCommission, KindRevenue, "Commissions"},
	{AccRevenueTokens, KindRevenue, "Jetons consommés"},
	{AccRevenueEquipment, KindRevenue, "Matériel vendu et loué"},
	{AccRevenueFees, KindRevenue, "Pénalités et frais"},
	{AccExpensePromo, KindExpense, "Promotions offertes"},
	{AccExpenseCredits, KindExpense, "Crédits opérateur"},
	{AccExpenseWriteoff, KindExpense, "Pertes et abandons"},
	// La trésorerie et les comptes des déclarations.
	{AccCashBox, KindAsset, "Caisse (espèces)"},
	{AccBank, KindAsset, "Banque"},
	{AccFixedAssets, KindAsset, "Immobilisations"},
	{AccOwnerContributions, KindEquity, "Apports des associés"},
	{AccRevenueSubsidy, KindRevenue, "Subventions"},
	{AccRevenueOther, KindRevenue, "Autres produits"},
	{AccExpenseRent, KindExpense, "Loyers"},
	{AccExpenseUtilities, KindExpense, "Eau, électricité, internet"},
	{AccExpenseFuel, KindExpense, "Carburant"},
	{AccExpenseMaintenance, KindExpense, "Entretien et réparations"},
	{AccExpenseSalaries, KindExpense, "Salaires et charges"},
	{AccExpenseTax, KindExpense, "Impôts et taxes"},
	{AccExpenseFine, KindExpense, "Amendes et pénalités"},
	{AccExpenseOther, KindExpense, "Autres charges"},
}

// Line : un côté d'une écriture.
type Line struct {
	Account string `bson:"account" json:"account"`
	Debit   int    `bson:"debit,omitempty" json:"debit,omitempty"`
	Credit  int    `bson:"credit,omitempty" json:"credit,omitempty"`
}

// Entry : une écriture. Ses lignes s'équilibrent — c'est vérifié à
// l'écriture ET par le balayage.
type Entry struct {
	ID      primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Country string             `bson:"country" json:"country"`
	At      time.Time          `bson:"at" json:"at"`
	// Service : qui a produit l'écriture — `core` (le grand livre des
	// portefeuilles), `vtc` (le grand livre des chauffeurs), `food`.
	Service string `bson:"service" json:"service"`
	Reason  string `bson:"reason" json:"reason"`
	RefKind string `bson:"ref_kind,omitempty" json:"ref_kind,omitempty"`
	RefID   string `bson:"ref_id,omitempty" json:"ref_id,omitempty"`
	// TransactionID : le mouvement du grand livre des portefeuilles qui a
	// produit l'écriture — un par mouvement, jamais deux.
	TransactionID *primitive.ObjectID `bson:"transaction_id,omitempty" json:"transaction_id,omitempty"`
	WalletID      *primitive.ObjectID `bson:"wallet_id,omitempty" json:"wallet_id,omitempty"`
	OwnerID       string              `bson:"owner_id,omitempty" json:"owner_id,omitempty"`
	Unit          string              `bson:"unit" json:"unit"`
	// Amount : le montant de l'écriture, en francs — la somme des débits.
	// Tokens : la quantité de jetons quand l'écriture en déplace, valorisés
	// au prix du jeton au moment de l'écriture.
	Amount      int    `bson:"amount" json:"amount"`
	Tokens      int    `bson:"tokens,omitempty" json:"tokens,omitempty"`
	Description string `bson:"description" json:"description"`
	Lines       []Line `bson:"lines" json:"lines"`
}

// Balanced dit si les débits égalent les crédits.
func (e Entry) Balanced() bool {
	d, c := 0, 0
	for _, l := range e.Lines {
		d += l.Debit
		c += l.Credit
	}
	return d == c && d > 0
}

// --- l'intégrité ---

// Ce que le balayage peut trouver.
const (
	FindingWalletDrift    = "wallet_drift"     // le solde ne vaut plus la somme de ses mouvements
	FindingEntryUnbalance = "entry_unbalanced" // une écriture dont les débits ≠ crédits
	FindingMissingEntry   = "missing_entry"    // un mouvement sans écriture
	FindingTrialUnbalance = "trial_unbalanced" // la balance générale ne s'équilibre pas
	FindingNegative       = "negative_balance" // un solde négatif là où c'est interdit
)

// Statuts d'un constat.
const (
	FindingOpen         = "open"
	FindingAcknowledged = "acknowledged"
	FindingResolved     = "resolved"
)

// Finding : un constat du balayage. Dédoublonné par empreinte : le même
// écart, trouvé à chaque passage, reste UN constat dont on voit la dernière
// occurrence.
type Finding struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Fingerprint string             `bson:"fingerprint" json:"-"`
	Kind        string             `bson:"kind" json:"kind"`
	Severity    string             `bson:"severity" json:"severity"` // critical · warning
	Country     string             `bson:"country,omitempty" json:"country,omitempty"`
	WalletID    string             `bson:"wallet_id,omitempty" json:"wallet_id,omitempty"`
	OwnerID     string             `bson:"owner_id,omitempty" json:"owner_id,omitempty"`
	OwnerType   string             `bson:"owner_type,omitempty" json:"owner_type,omitempty"`
	Unit        string             `bson:"unit,omitempty" json:"unit,omitempty"`
	Expected    int                `bson:"expected" json:"expected"`
	Actual      int                `bson:"actual" json:"actual"`
	Detail      string             `bson:"detail" json:"detail"`
	Status      string             `bson:"status" json:"status"`
	FirstSeenAt time.Time          `bson:"first_seen_at" json:"first_seen_at"`
	LastSeenAt  time.Time          `bson:"last_seen_at" json:"last_seen_at"`
	Occurrences int                `bson:"occurrences" json:"occurrences"`
	AckedBy     string             `bson:"acked_by,omitempty" json:"acked_by,omitempty"`
	AckedAt     *time.Time         `bson:"acked_at,omitempty" json:"acked_at,omitempty"`
	Note        string             `bson:"note,omitempty" json:"note,omitempty"`
}

// Run : le compte rendu d'un balayage.
type Run struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Wallets    int       `json:"wallets"`
	Entries    int       `json:"entries"`
	Findings   int       `json:"findings"`
	New        int       `json:"new_findings"`
	Resolved   int       `json:"resolved"`
}
