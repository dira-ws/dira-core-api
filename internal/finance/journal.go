package finance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kgtech-org/dira-core-api/internal/token"
)

// LE JOURNAL : d'un mouvement de portefeuille à une écriture en partie double.
//
// La règle de traduction est ICI, pure, et testée ligne par ligne : c'est
// elle qui dit ce que la plateforme gagne, doit et détient. Un motif inconnu
// n'est pas deviné : il produit une écriture « à classer » sur un compte
// d'attente, et le balayage la signale.

// Journal écrit une écriture par mouvement, dans la même transaction Mongo
// que le mouvement. Implémente `token.Journal`.
type Journal struct {
	repo          *Repository
	tokenPriceXOF int
	now           func() time.Time
}

func NewJournal(repo *Repository, tokenPriceXOF int) *Journal {
	if tokenPriceXOF <= 0 {
		tokenPriceXOF = token.DefaultTokenPriceXOF
	}
	return &Journal{repo: repo, tokenPriceXOF: tokenPriceXOF, now: func() time.Time { return time.Now().UTC() }}
}

// Post traduit et écrit. Une traduction impossible n'annule PAS le mouvement
// (l'argent a bougé, c'est ce qui compte) : elle écrit une écriture en
// attente que l'aperçu et le balayage montrent.
func (j *Journal) Post(ctx context.Context, tx *token.Transaction, w *token.Wallet) error {
	entry := EntryFor(tx, w, j.tokenPriceXOF)
	if entry == nil {
		return nil
	}
	return j.repo.InsertEntry(ctx, entry)
}

// AccSuspense : le compte d'attente des mouvements que la règle ne sait pas
// classer. Vide en régime normal ; tout solde ici est un constat.
const AccSuspense = "suspense"

// EntryFor rend l'écriture d'un mouvement de portefeuille, ou nil quand il
// ne déplace rien.
func EntryFor(tx *token.Transaction, w *token.Wallet, tokenPriceXOF int) *Entry {
	if tx == nil || tx.Amount == 0 {
		return nil
	}
	e := &Entry{
		At: tx.CreatedAt, Service: "core", Reason: tx.Reason, RefKind: tx.RefKind,
		Unit: tx.Unit, Description: describe(tx, w),
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if e.Unit == "" {
		e.Unit = UnitToken
	}
	if w != nil {
		e.Country = w.Country
		e.OwnerID = w.OwnerID.Hex()
		wid := w.ID
		e.WalletID = &wid
	}
	if !tx.ID.IsZero() {
		id := tx.ID
		e.TransactionID = &id
	}
	if tx.RefID != nil {
		e.RefID = tx.RefID.Hex()
	}
	amount := tx.Amount
	if amount < 0 {
		amount = -amount
	}
	credit := tx.Kind == token.KindPurchase // un crédit du portefeuille
	source, _ := tx.Ref["source"].(string)

	if e.Unit == UnitToken {
		// Les JETONS : valorisés au prix du jeton. Le passif « jetons
		// prépayés » monte quand un agent en achète (ou en reçoit), descend
		// et devient un produit quand il les consomme.
		e.Tokens = amount
		e.Amount = amount * tokenPriceXOF
		switch {
		case tx.Reason == token.ReasonTopup && credit:
			e.Lines = lines(AccMobileMoney, AccAgentTokens, e.Amount)
		case tx.Reason == token.ReasonOperatorCredit && credit:
			e.Lines = lines(AccExpenseCredits, AccAgentTokens, e.Amount)
		case strings.HasSuffix(tx.Reason, "_refund") && credit:
			// Ce qu'une acceptation avait pris et qui revient.
			e.Lines = lines(AccRevenueTokens, AccAgentTokens, e.Amount)
		case !credit:
			e.Lines = lines(AccAgentTokens, AccRevenueTokens, e.Amount)
		default:
			e.Lines = lines(AccSuspense, AccAgentTokens, e.Amount)
		}
		return e
	}

	e.Amount = amount
	clearing := clearingOf(tx.RefKind)
	walletAcc := walletAccount(w)
	// payer : le compte d'où part un paiement, et où revient son
	// remboursement. Les soldes clients, comme toujours — sauf pour le
	// portefeuille d'une entreprise, dont l'argent est une avance qu'elle a
	// versée (`AccBusinessAdvances`).
	//
	// ⚠️ POUR CE TYPE SEULEMENT. Un client, un livreur ou un marchand qui paie
	// garde la ligne d'aujourd'hui : c'est ce qui fait que le journal d'un repas
	// et d'une course ne change pas d'un octet.
	payer := AccClientWallets
	if w != nil && w.Type == token.WalletTypeBusiness {
		payer = walletAcc
	}
	switch tx.Reason {
	case token.ReasonWalletTopup:
		e.Lines = lines(AccMobileMoney, AccClientWallets, amount)
	case token.ReasonPromoCredit:
		e.Lines = lines(AccExpensePromo, AccPromoCredits, amount)
	case token.ReasonPayment:
		from := payer
		if source == "promo" {
			from = AccPromoCredits
		}
		e.Lines = lines(from, clearing, amount)
	case token.ReasonTip:
		from := AccClientWallets
		if source == "promo" {
			from = AccPromoCredits
		}
		e.Lines = lines(from, AccAgentPayables, amount)
	case token.ReasonRefund:
		e.Lines = lines(clearing, payer, amount)
	case token.ReasonOrderPayout:
		e.Lines = lines(AccOrderClearing, AccMerchantPayables, amount)
	case token.ReasonDeliveryFee:
		// Le gain d'une course débite le passage de ce qu'elle a servi : celui
		// des colis pour une étape d'envoi, celui des commandes pour tout le
		// reste — la ligne de toujours.
		e.Lines = lines(deliveryClearingOf(tx.RefKind), AccAgentPayables, amount)
	case token.ReasonParcelCancelCompensation:
		// Le trajet d'un livreur annulé sous lui : pris sur ce que
		// l'expéditeur n'a pas récupéré, resté au passage des colis.
		e.Lines = lines(clearing, AccAgentPayables, amount)
	case token.ReasonParcelCancelGoodwill:
		// Le même trajet, quand l'équipe a tout rendu : une dépense de la
		// plateforme, un geste comme un crédit accordé.
		e.Lines = lines(AccExpenseCredits, AccAgentPayables, amount)
	case token.ReasonParcelReturnFee:
		// Le frais d'un retour, pris sur le solde de l'expéditeur. Ce que le
		// solde n'a pas couvert devient une créance (`payment_due`, plus bas).
		if credit {
			e.Lines = lines(clearing, payer, amount)
		} else {
			e.Lines = lines(payer, clearing, amount)
		}
	case token.ReasonBusinessProvision:
		// L'argent qu'une entreprise a versé : la trésorerie par où il est
		// arrivé (le moyen de la pièce), vers ses avances.
		if treasury := TreasuryAccount(refString(tx.Ref, "means")); treasury != "" && credit {
			e.Lines = lines(treasury, walletAcc, amount)
		} else {
			e.Lines = lines(AccSuspense, walletAcc, amount)
		}
	case token.ReasonEquipment:
		if credit {
			// La caution rendue.
			e.Lines = lines(AccRevenueEquipment, walletAcc, amount)
		} else {
			e.Lines = lines(walletAcc, AccRevenueEquipment, amount)
		}
	case token.ReasonCommission:
		// Une commission retenue sur ce que l'on versait à l'agent, ou prise
		// sur son solde pour une course en espèces.
		e.Lines = lines(walletAcc, AccRevenueCommission, amount)
	case token.ReasonCommissionDue:
		// Le solde n'a pas suffi : la commission devient une créance.
		e.Lines = lines(AccAgentReceivable, AccRevenueCommission, amount)
	case token.ReasonPaymentDue:
		// Un paiement refusé à l'étape réglée : la course ou la commande est
		// due par le client.
		e.Lines = lines(AccAgentReceivable, clearing, amount)
	case token.ReasonDebtRepaid:
		if settled, _ := tx.Ref["settled"].(string); settled != "" {
			// Réglé à l'agence : de l'argent entre, la créance s'éteint.
			e.Lines = lines(AccMobileMoney, AccAgentReceivable, amount)
		} else {
			e.Lines = lines(walletAcc, AccAgentReceivable, amount)
		}
	default:
		if credit {
			e.Lines = lines(AccSuspense, walletAcc, amount)
		} else {
			e.Lines = lines(walletAcc, AccSuspense, amount)
		}
	}
	return e
}

func lines(debit, credit string, amount int) []Line {
	return []Line{{Account: debit, Debit: amount}, {Account: credit, Credit: amount}}
}

// clearingOf : le compte de passage de ce qu'un mouvement paie.
//
// ⚠️ LES TROIS GENRES DU COLIS ONT LE LEUR. Sans eux, ils tomberaient ici
// « à classer » — et le prix d'un envoi, son remboursement, le frais de son
// retour finiraient dans le compte d'attente que le balayage signale.
//
// ⚠️ UN ABONNEMENT DE COURSES PASSE PAR CELUI DES COURSES. Ses courses ne se
// paient pas une à une : chacune se règle au chauffeur sur ce passage
// (`ride_settled`, moyen `subscription`), comme une course payée en ligne. Payé
// au solde ou par paiement mobile, c'est le même argent, au même endroit.
func clearingOf(refKind string) string {
	switch refKind {
	case token.RefRide, token.RefRideAdjustment, token.RefTip, token.RefRideSubscription:
		return AccRideClearing
	case token.RefOrder:
		return AccOrderClearing
	case token.RefShipment, token.RefShipmentCharge, token.RefShipmentLeg:
		return AccParcelClearing
	}
	return AccSuspense
}

// deliveryClearingOf : le passage que débite le GAIN d'un livreur.
//
// ⚠️⚠️ LE COMPTE DES COMMANDES ÉTAIT ÉCRIT EN DUR, et il le reste pour tout ce
// qui n'est pas un colis : la ligne d'un repas ne bouge pas d'un octet. Mais le
// gain d'une étape d'envoi débité du passage des commandes aurait vidé un
// compte que le colis n'a jamais rempli, et laissé plein celui qu'il a rempli.
func deliveryClearingOf(refKind string) string {
	if c := clearingOf(refKind); c == AccParcelClearing {
		return c
	}
	return AccOrderClearing
}

// walletAccount : le passif que porte un portefeuille selon son propriétaire.
//
// ⚠️ `business` A LE SIEN : sans ce cas, le portefeuille d'une entreprise
// tombait dans le défaut — le compte des agents — et son approvisionnement se
// serait lu comme une dette envers les livreurs.
func walletAccount(w *token.Wallet) string {
	if w == nil {
		return AccSuspense
	}
	switch w.Type {
	case token.WalletTypeClient:
		return AccClientWallets
	case token.WalletTypeMerchant:
		return AccMerchantPayables
	case token.WalletTypeBusiness:
		return AccBusinessAdvances
	default:
		return AccAgentPayables
	}
}

// refString lit une chaîne de la référence d'un mouvement, vide sinon.
func refString(ref map[string]any, key string) string {
	s, _ := ref[key].(string)
	return s
}

// parcelWords : ce que l'exploitation LIT d'un mouvement du colis, au lieu du
// genre technique.
//
// ⚠️ « COLIS », JAMAIS « COMMANDE ». C'est le mot qui permet à qui relit le
// journal de reconnaître un envoi ; les autres genres gardent le libellé
// d'aujourd'hui.
var parcelWords = map[string]string{
	token.RefShipment:       "colis",
	token.RefShipmentCharge: "frais de colis",
	token.RefShipmentLeg:    "étape de colis",
}

func describe(tx *token.Transaction, w *token.Wallet) string {
	who := "portefeuille"
	if w != nil {
		switch w.Type {
		case token.WalletTypeClient:
			who = "client"
		case token.WalletTypeMerchant:
			who = "marchand"
		case token.WalletTypeBusiness:
			who = "entreprise"
		default:
			who = "agent"
		}
	}
	ref := ""
	if tx.RefKind != "" && tx.RefID != nil {
		kind := tx.RefKind
		if word, ok := parcelWords[kind]; ok {
			kind = word
		}
		ref = " · " + kind + " #" + strings.ToUpper(tx.RefID.Hex()[len(tx.RefID.Hex())-5:])
	}
	return fmt.Sprintf("%s · %s%s", tx.Reason, who, ref)
}

// --- les événements des verticales ---

// Event : ce qu'une verticale dit au journal quand de l'argent bouge chez
// elle sans passer par un portefeuille du socle — le grand livre des
// chauffeurs, surtout.
type Event struct {
	Kind          string `json:"kind" validate:"required,oneof=ride_settled ledger_entry"`
	Country       string `json:"country" validate:"required,len=2"`
	Vertical      string `json:"vertical" validate:"required,oneof=vtc food"`
	RefKind       string `json:"ref_kind"`
	RefID         string `json:"ref_id"`
	OwnerID       string `json:"owner_id"`
	At            string `json:"at"`
	PaymentMethod string `json:"payment_method"`
	// ride_settled
	FareXOF       int `json:"fare_xof"`
	CommissionXOF int `json:"commission_xof"`
	DriverXOF     int `json:"driver_xof"`
	// ledger_entry : `payout` · `settlement` · `adjustment` · `equipment` · `tip` · `commission`
	EntryKind string `json:"entry_kind"`
	AmountXOF int    `json:"amount_xof"`
	Note      string `json:"note"`
}

// EntriesForEvent traduit un événement de verticale. Plusieurs écritures
// possibles (une course en ligne répartit le prix en deux).
func EntriesForEvent(ev Event, now time.Time) []Entry {
	at := now
	if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
		at = t
	}
	base := func(reason, desc string) Entry {
		return Entry{Country: ev.Country, At: at, Service: ev.Vertical, Reason: reason, RefKind: ev.RefKind, RefID: ev.RefID, OwnerID: ev.OwnerID, Unit: UnitXOF, Description: desc}
	}
	var out []Entry
	switch ev.Kind {
	case "ride_settled":
		switch ev.PaymentMethod {
		case "cash":
			// L'argent est dans la poche du chauffeur ; il doit la commission.
			if ev.CommissionXOF > 0 {
				e := base("ride_commission_cash", "commission due sur course en espèces")
				e.Amount = ev.CommissionXOF
				e.Lines = lines(AccAgentReceivable, AccRevenueCommission, ev.CommissionXOF)
				out = append(out, e)
			}
		default:
			// La plateforme a encaissé le prix (transit) : elle le répartit.
			if ev.DriverXOF > 0 {
				e := base("ride_earning", "part du chauffeur sur course payée en ligne")
				e.Amount = ev.DriverXOF
				e.Lines = lines(AccRideClearing, AccAgentPayables, ev.DriverXOF)
				out = append(out, e)
			}
			if ev.CommissionXOF > 0 {
				e := base("ride_commission", "commission sur course payée en ligne")
				e.Amount = ev.CommissionXOF
				e.Lines = lines(AccRideClearing, AccRevenueCommission, ev.CommissionXOF)
				out = append(out, e)
			}
		}
	case "ledger_entry":
		amount := ev.AmountXOF
		if amount == 0 {
			return nil
		}
		abs := amount
		if abs < 0 {
			abs = -abs
		}
		e := base("ledger_"+ev.EntryKind, ev.Note)
		e.Amount = abs
		switch ev.EntryKind {
		case "payout":
			// La plateforme verse au chauffeur ce qu'elle lui doit.
			e.Lines = lines(AccAgentPayables, AccMobileMoney, abs)
		case "settlement":
			// Le chauffeur règle sa dette.
			e.Lines = lines(AccMobileMoney, AccAgentReceivable, abs)
		case "adjustment":
			if amount > 0 {
				e.Lines = lines(AccExpenseCredits, AccAgentPayables, abs)
			} else {
				e.Lines = lines(AccAgentPayables, AccExpenseWriteoff, abs)
			}
		case "equipment":
			if amount < 0 {
				e.Lines = lines(AccAgentPayables, AccRevenueEquipment, abs)
			} else {
				e.Lines = lines(AccRevenueEquipment, AccAgentPayables, abs)
			}
		case "tip":
			// Déjà écrit par le socle au débit du passager : rien de plus.
			return nil
		case "commission":
			e.Lines = lines(AccAgentReceivable, AccRevenueCommission, abs)
		default:
			e.Lines = lines(AccSuspense, AccAgentPayables, abs)
		}
		out = append(out, e)
	}
	return out
}
