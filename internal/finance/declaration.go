package finance

// LES DÉCLARATIONS D'ENTRÉE ET DE SORTIE D'ARGENT.
//
// La plateforme savait compter l'argent QU'ELLE FAIT CIRCULER — une course
// payée, des jetons consommés, une commission retenue, un retrait. Elle ne
// savait rien dire d'un loyer payé, d'un plein de carburant, d'un apport
// d'associé ou d'une amende : cet argent-là bougeait sans laisser de trace,
// et l'aperçu comptable montrait donc une plateforme qui gagne et ne dépense
// rien.
//
// Une déclaration comble ce trou. Elle n'ouvre PAS une seconde comptabilité :
// elle écrit dans LE journal (`Entry`), en partie double, sur le plan de
// comptes existant. C'est la raison pour laquelle le balayage d'intégrité
// reste vert après une déclaration — il ne voit rien de nouveau, juste une
// écriture de plus, équilibrée, comme toutes les autres.
//
// ⚠️ TROIS RÈGLES PORTENT TOUT LE RESTE :
//
//  1. UNE DÉCLARATION NE TOUCHE AUCUN COMPTE ADOSSÉ À UN PORTEFEUILLE.
//     `client_wallets`, `agent_payables`, `agent_tokens`, les cautions, les
//     comptes de transit : ces soldes sont la contrepartie comptable de
//     portefeuilles RÉELS, que le balayage recalcule depuis leurs
//     mouvements. Une déclaration qui les mouvrait ferait dire au journal que
//     la plateforme doit une somme que personne ne détient. Aucun constat ne
//     le signalerait — le balayage compare les portefeuilles à LEURS
//     mouvements, pas au journal — et c'est précisément ce qui rend l'erreur
//     coûteuse. La liste des comptes déclarables est donc NOMINATIVE
//     (`declarableAccounts`).
//
//  2. UNE ÉCRITURE NE SE MODIFIE PAS, ELLE SE CONTRE-PASSE. Corriger une
//     déclaration fautive en réécrivant son écriture effacerait le fait qu'on
//     s'est trompé — et un journal dont on peut réécrire le passé ne prouve
//     plus rien. Une déclaration fautive est donc contre-passée (une seconde
//     écriture, les mêmes comptes, les côtés échangés), elle reste visible
//     avec son motif d'annulation, et la correction est une NOUVELLE
//     déclaration qui la désigne.
//
//  3. LE COMPTE EST FIGÉ À LA DÉCLARATION. La catégorie dit quel compte elle
//     meut, mais c'est le compte RETENU AU MOMENT DE LA DÉCLARATION qui est
//     écrit sur elle. Repointer « Carburant » vers un autre compte demain ne
//     doit pas réécrire ce que l'on a déclaré hier : l'écriture, elle, est
//     déjà posée et ne bougera plus.

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Collections.
const (
	declarationsCollection = "finance_declarations"
	declCatalogCollection  = "finance_declaration_categories"
)

// RefDeclaration : le genre de référence que porte l'écriture d'une
// déclaration — `GET /admin/finance/journal?ref_kind=declaration&ref_id=…`
// rend l'écriture et sa contre-passation.
const RefDeclaration = "declaration"

// Le SENS d'un mouvement déclaré.
const (
	DeclarationIn  = "in"  // de l'argent ENTRE : subvention, apport, cession
	DeclarationOut = "out" // de l'argent SORT : loyer, carburant, amende
	// DeclarationBoth ne qualifie pas un mouvement mais une CATÉGORIE qui
	// marche dans les deux sens — on achète une immobilisation, on la cède ;
	// un associé apporte, un associé retire.
	DeclarationBoth = "both"
)

// Par OÙ l'argent a passé. ⚠️ Ce n'est pas un détail d'affichage : c'est la
// contrepartie comptable de l'écriture, et sans elle une déclaration serait
// inrapprochable d'un relevé.
const (
	MeansCash        = "cash"         // la caisse, en espèces
	MeansBank        = "bank"         // un compte en banque
	MeansMobileMoney = "mobile_money" // le flottant mobile money
)

// Statuts d'une déclaration. ⚠️ Pas de « supprimée » : voir la règle 2.
const (
	DeclarationPosted   = "posted"
	DeclarationReversed = "reversed"
)

// D'où vient une déclaration.
const (
	// SourceConsole : saisie à la main par l'exploitation. Le handler le pose
	// lui-même — un appelant ne choisit pas sa propre provenance.
	SourceConsole = "console"
	// SourcePayroll : LA COUTURE laissée à la paie des salariés (voir
	// `DeclareFromModule`). Déclarée ici pour que le nom soit stable des deux
	// côtés de la jonction ; AUCUN code de ce module ne connaît la paie.
	SourcePayroll = "payroll"
)

// Les motifs portés par les écritures d'une déclaration.
const (
	ReasonDeclarationIn       = "declaration_in"
	ReasonDeclarationOut      = "declaration_out"
	ReasonDeclarationReversed = "declaration_reversed"
)

// TreasuryAccount : le compte d'actif que meut un moyen de paiement.
//
// ⚠️ `mobile_money` réemploie le compte EXISTANT, et c'est voulu : payer un
// plein depuis le flottant mobile money fait réellement baisser ce que les
// prestataires détiennent pour nous. Lui inventer un compte parallèle aurait
// donné deux soldes pour un seul avoir réel.
func TreasuryAccount(means string) string {
	switch means {
	case MeansCash:
		return AccCashBox
	case MeansBank:
		return AccBank
	case MeansMobileMoney:
		return AccMobileMoney
	}
	return ""
}

// declarableAccounts : les comptes qu'une CATÉGORIE peut mouvoir — la règle 1,
// écrite une fois.
//
// ⚠️ C'EST UNE LISTE, PAS UN FILTRE PAR NATURE. « Tout compte de charge ou de
// produit » aurait semblé équivalent et laissé passer `agent_tokens` (un
// passif), `order_clearing` (du transit) ou `suspense` le jour où quelqu'un
// aurait voulu « classer une déclaration à part ». Ajouter un compte ici est
// un geste qu'on fait exprès.
var declarableAccounts = map[string]bool{
	AccExpenseRent:        true,
	AccExpenseUtilities:   true,
	AccExpenseFuel:        true,
	AccExpenseMaintenance: true,
	AccExpenseSalaries:    true,
	AccExpenseTax:         true,
	AccExpenseFine:        true,
	AccExpenseOther:       true,
	AccFixedAssets:        true,
	AccOwnerContributions: true,
	AccRevenueSubsidy:     true,
	AccRevenueOther:       true,
	// Deux comptes d'avant les déclarations qu'il est légitime de mouvoir à la
	// main : une pénalité encaissée hors plateforme, une perte constatée.
	AccRevenueFees:     true,
	AccExpenseWriteoff: true,
}

// DeclarableAccounts rend les comptes déclarables, dans l'ordre du plan — ce
// que la console propose quand on règle une catégorie.
func DeclarableAccounts() []Account {
	out := make([]Account, 0, len(declarableAccounts))
	for _, a := range Chart {
		if declarableAccounts[a.Code] {
			out = append(out, a)
		}
	}
	return out
}

// --- le catalogue des catégories ---

// DeclarationCategory : une catégorie du catalogue d'un pays.
type DeclarationCategory struct {
	// Code est ce que PORTENT les déclarations : il ne se renomme pas sans
	// casser la synthèse des périodes passées.
	Code  string `bson:"code" json:"code"`
	Label string `bson:"label" json:"label"`
	// Direction : `in`, `out`, ou `both` quand la catégorie marche dans les
	// deux sens.
	Direction string `bson:"direction" json:"direction"`
	// Account : le compte du plan que la catégorie meut.
	Account string `bson:"account" json:"account"`
	// Disabled retire la catégorie des choix SANS la faire disparaître des
	// déclarations déjà faites — les supprimer aurait rendu illisible la
	// synthèse de l'an dernier.
	Disabled bool `bson:"disabled,omitempty" json:"disabled,omitempty"`
}

// Allows dit si la catégorie admet ce sens.
func (c DeclarationCategory) Allows(direction string) bool {
	return c.Direction == DeclarationBoth || c.Direction == direction
}

// DeclarationCatalog : les catégories d'un pays. Un document par pays, comme
// la facturation — le pays EST la clé, donc aucune lecture de ce catalogue ne
// peut déborder sur un autre pays.
type DeclarationCatalog struct {
	Country    string                `bson:"_id" json:"country"`
	Categories []DeclarationCategory `bson:"categories" json:"categories"`
	UpdatedBy  string                `bson:"updated_by,omitempty" json:"updated_by,omitempty"`
	UpdatedAt  *time.Time            `bson:"updated_at,omitempty" json:"updated_at,omitempty"`
}

// Find rend une catégorie par son code.
func (c DeclarationCatalog) Find(code string) (DeclarationCategory, bool) {
	for _, x := range c.Categories {
		if x.Code == code {
			return x, true
		}
	}
	return DeclarationCategory{}, false
}

// DefaultDeclarationCategories : le catalogue d'un pays qui n'a rien réglé.
//
// Un pays qui ouvre doit pouvoir déclarer un loyer le premier jour, sans
// qu'on lui demande d'abord de construire un plan de comptes. L'exploitation
// renomme, désactive et ajoute ensuite depuis la console.
func DefaultDeclarationCategories() []DeclarationCategory {
	return []DeclarationCategory{
		{Code: "rent", Label: "Loyer", Direction: DeclarationOut, Account: AccExpenseRent},
		{Code: "utilities", Label: "Eau, électricité, internet", Direction: DeclarationOut, Account: AccExpenseUtilities},
		{Code: "fuel", Label: "Carburant", Direction: DeclarationOut, Account: AccExpenseFuel},
		{Code: "maintenance", Label: "Entretien et réparations", Direction: DeclarationOut, Account: AccExpenseMaintenance},
		// ⚠️ LA COUTURE DE LA PAIE passe par cette catégorie et par son compte.
		// Elle existe pour qu'un versement de salaire ait une place AVANT que
		// le module de paie n'existe : une déclaration à la main aujourd'hui,
		// la même déclaration posée par la paie demain, au même endroit.
		{Code: "salaries", Label: "Salaires et charges", Direction: DeclarationOut, Account: AccExpenseSalaries},
		{Code: "tax", Label: "Impôts et taxes", Direction: DeclarationOut, Account: AccExpenseTax},
		{Code: "fine", Label: "Amendes et pénalités", Direction: DeclarationOut, Account: AccExpenseFine},
		{Code: "asset", Label: "Immobilisation (achat, cession)", Direction: DeclarationBoth, Account: AccFixedAssets},
		{Code: "contribution", Label: "Apport d'associé", Direction: DeclarationBoth, Account: AccOwnerContributions},
		{Code: "subsidy", Label: "Subvention", Direction: DeclarationIn, Account: AccRevenueSubsidy},
		{Code: "other_in", Label: "Autre entrée", Direction: DeclarationIn, Account: AccRevenueOther},
		{Code: "other_out", Label: "Autre sortie", Direction: DeclarationOut, Account: AccExpenseOther},
	}
}

// DefaultDeclarationCatalog : le catalogue par défaut d'un pays.
func DefaultDeclarationCatalog(cc string) DeclarationCatalog {
	return DeclarationCatalog{Country: cc, Categories: DefaultDeclarationCategories()}
}

// --- la déclaration ---

// Declaration : un mouvement d'argent déclaré à la main (ou par un module qui
// passe par la couture), et l'écriture qu'il a produite.
type Declaration struct {
	ID      primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Country string             `bson:"country" json:"country"`
	// Direction : `in` ou `out`.
	Direction string `bson:"direction" json:"direction"`
	Category  string `bson:"category" json:"category"`
	// CategoryLabel : le libellé TEL QU'IL ÉTAIT. Figé pour la même raison
	// que le compte : la synthèse d'un trimestre clos ne doit pas changer de
	// vocabulaire parce qu'on a renommé une catégorie depuis.
	CategoryLabel string `bson:"category_label" json:"category_label"`
	// Account : le compte du plan que l'écriture a mouvementé — figé (règle 3).
	Account string `bson:"account" json:"account"`
	// Means : `cash`, `bank`, `mobile_money`.
	Means string `bson:"means" json:"means"`
	// Amount : un ENTIER dans la plus petite unité de la monnaie du pays.
	Amount int `bson:"amount" json:"amount"`
	// Currency : la monnaie du pays, retenue à la déclaration. Rien n'est
	// converti — un montant déclaré en Guinée est en francs guinéens, et le
	// dire évite qu'on l'additionne un jour à des francs CFA.
	Currency string `bson:"currency" json:"currency"`
	// ValueDate : la date de VALEUR — le jour où l'argent a bougé, pas celui
	// de la saisie. C'est elle que porte l'écriture, donc elle qui décide de
	// la période où la dépense apparaît.
	ValueDate time.Time `bson:"value_date" json:"value_date"`
	// Counterparty : qui a payé, ou qui a été payé. Obligatoire — une sortie
	// sans bénéficiaire nommé n'est pas une dépense, c'est un trou.
	Counterparty string `bson:"counterparty" json:"counterparty"`
	// Reference : le numéro de la pièce (facture, reçu, quittance).
	//
	// ⚠️ UNIQUE PAR PAYS quand elle est renseignée, et c'est un index qui le
	// garantit : c'est ce qui empêche de régler deux fois la même facture.
	// Vide, il n'y a aucune protection — deux pleins de 20 000 F le même jour
	// sont deux dépenses, et rien ne peut les distinguer d'un double clic.
	Reference string `bson:"reference,omitempty" json:"reference,omitempty"`
	// Justification : POURQUOI cet argent a bougé. Obligatoire.
	Justification string `bson:"justification" json:"justification"`
	// AttachmentURL : la pièce jointe (photo du reçu), facultative.
	AttachmentURL string `bson:"attachment_url,omitempty" json:"attachment_url,omitempty"`

	Status string `bson:"status" json:"status"`
	// EntryID : l'écriture du journal. Toujours posée AVANT la déclaration —
	// voir `InsertDeclaration`.
	EntryID *primitive.ObjectID `bson:"entry_id,omitempty" json:"entry_id,omitempty"`

	// La CONTRE-PASSATION.
	ReversalEntryID *primitive.ObjectID `bson:"reversal_entry_id,omitempty" json:"reversal_entry_id,omitempty"`
	ReversalReason  string              `bson:"reversal_reason,omitempty" json:"reversal_reason,omitempty"`
	ReversedBy      string              `bson:"reversed_by,omitempty" json:"reversed_by,omitempty"`
	ReversedAt      *time.Time          `bson:"reversed_at,omitempty" json:"reversed_at,omitempty"`
	// Corrects : la déclaration contre-passée que celle-ci remplace, et
	// CorrectedBy la déclaration qui l'a remplacée. Les deux sens sont écrits
	// parce qu'un seul ne se remonte pas : sans l'inverse, on tient la
	// correction et on ne retrouve pas la faute.
	Corrects    *primitive.ObjectID `bson:"corrects,omitempty" json:"corrects,omitempty"`
	CorrectedBy *primitive.ObjectID `bson:"corrected_by,omitempty" json:"corrected_by,omitempty"`

	// Source : `console`, ou le nom du module qui a déclaré (la couture).
	Source string `bson:"source" json:"source"`
	// SourceRef : la référence de l'objet du module (un bulletin de paie).
	// ⚠️ UNIQUE PAR (PAYS, SOURCE) quand elle est renseignée : c'est elle qui
	// rend la couture idempotente, pour qu'un module qui réessaie ne déclare
	// pas deux fois.
	SourceRef string `bson:"source_ref,omitempty" json:"source_ref,omitempty"`

	DeclaredBy string    `bson:"declared_by,omitempty" json:"declared_by,omitempty"`
	CreatedAt  time.Time `bson:"created_at" json:"created_at"`
}

// --- la traduction en partie double ---

// EntryForDeclaration rend L'ÉCRITURE d'une déclaration, ou nil si elle n'est
// pas en état d'en produire une.
//
// Une SORTIE constate la charge (ou l'immobilisation) au débit et vide la
// trésorerie au crédit ; une ENTRÉE fait l'inverse. C'est tout — et c'est
// pour cela que la balance générale reste équilibrée après n'importe quelle
// déclaration.
func EntryForDeclaration(d Declaration) *Entry {
	treasury := TreasuryAccount(d.Means)
	if d.Amount <= 0 || d.Account == "" || treasury == "" || d.ID.IsZero() {
		return nil
	}
	at := d.ValueDate
	if at.IsZero() {
		at = d.CreatedAt
	}
	e := &Entry{
		Country: d.Country, At: at, Service: "core",
		Reason: ReasonDeclarationOut, RefKind: RefDeclaration, RefID: d.ID.Hex(),
		Unit: UnitXOF, Amount: d.Amount, Description: describeDeclaration(d),
	}
	if d.Direction == DeclarationIn {
		e.Reason = ReasonDeclarationIn
		e.Lines = lines(treasury, d.Account, d.Amount)
		return e
	}
	e.Lines = lines(d.Account, treasury, d.Amount)
	return e
}

// ReversalEntryForDeclaration rend la CONTRE-PASSATION : les mêmes comptes,
// les côtés échangés.
//
// ⚠️ ELLE EST DATÉE DU JOUR OÙ ON LA FAIT, pas de la date de valeur de la
// déclaration fautive. Une contre-passation antidatée réécrirait une période
// déjà lue, déjà rapportée, peut-être déjà close — c'est exactement ce qu'une
// contre-passation existe pour éviter. La conséquence est à connaître : une
// déclaration de janvier annulée en mars laisse janvier tel qu'il était, et
// la synthèse le dit (voir `SummariseDeclarations`).
func ReversalEntryForDeclaration(d Declaration, at time.Time) *Entry {
	e := EntryForDeclaration(d)
	if e == nil {
		return nil
	}
	e.At = at
	e.Reason = ReasonDeclarationReversed
	e.Description = "contre-passation · " + e.Description
	e.Lines = reverseLines(e.Lines)
	return e
}

// reverseLines échange débit et crédit, et remet la ligne de débit en tête —
// l'ordre que le journal de la console lit.
func reverseLines(ls []Line) []Line {
	out := make([]Line, len(ls))
	for i, l := range ls {
		out[len(ls)-1-i] = Line{Account: l.Account, Debit: l.Credit, Credit: l.Debit}
	}
	return out
}

func describeDeclaration(d Declaration) string {
	label := d.CategoryLabel
	if label == "" {
		label = d.Category
	}
	parts := []string{label}
	if d.Counterparty != "" {
		parts = append(parts, d.Counterparty)
	}
	if d.Reference != "" {
		parts = append(parts, "pièce "+d.Reference)
	}
	return strings.Join(parts, " · ")
}

// currencyOf : la monnaie du pays, telle que le catalogue la déclare.
func currencyOf(cc string) string {
	if info, ok := country.Lookup(cc); ok {
		return info.Currency
	}
	return ""
}
