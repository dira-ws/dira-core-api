package finance

// LE JOURNAL D'UN REPAS ET D'UNE COURSE, FIGÉ.
//
// ⚠️⚠️ CETTE ÉPREUVE EXISTE POUR QU'UN AJOUT NE CHANGE RIEN À CE QUI MARCHE. Le
// colis ajoute des genres, des comptes et des motifs au journal ; le journal
// d'une commande de repas et celui d'une course doivent rester, écriture par
// écriture, ce qu'ils étaient — les mêmes comptes, les mêmes montants, les mêmes
// libellés. Le fichier doré a été produit par le code d'AVANT ces ajouts : il
// est la preuve, pas une photographie de ce que le code fait aujourd'hui.
//
// Chaque mouvement qu'un repas ou une course écrit au grand livre des
// portefeuilles y est, et chaque événement que les courses déclarent au
// journal. Les identifiants et la date sont fixes : la sortie est stable d'une
// exécution à l'autre.
//
//	DIRA_GOLDEN_UPDATE=1 go test ./internal/finance/ -run TestTheJournalOfAMealAndOfARideIsUnchanged
//
// ⚠️ NE RÉGÉNÉREZ PAS CE FICHIER POUR FAIRE PASSER L'ÉPREUVE. S'il doit changer,
// c'est que le journal d'un repas ou d'une course change : une décision, qui se
// dit dans la PR.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
)

const journalGolden = "testdata/journal_meal_ride.golden.json"

// oid fabrique un identifiant FIXE, lisible : 24 caractères hexadécimaux.
func oid(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return id
}

type goldenCase struct {
	Name    string  `json:"name"`
	Entries []Entry `json:"entries"`
}

func TestTheJournalOfAMealAndOfARideIsUnchanged(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)
	wallet := func(kind, hex string) *token.Wallet {
		return &token.Wallet{ID: oid(t, "aaaaaaaaaaaaaaaaaaaa"+hex), OwnerID: oid(t, "bbbbbbbbbbbbbbbbbbbb"+hex), Type: kind, Country: "TG"}
	}
	client, courier, store, driver := wallet("client", "0001"), wallet("driver", "0002"), wallet("merchant", "0003"), wallet("driver", "0004")
	order, ride := oid(t, "0123456789abcdef01234567"), oid(t, "76543210fedcba9876543210")
	tip, adjustment, contract := oid(t, "1111111111111111111111aa"), oid(t, "2222222222222222222222bb"), oid(t, "3333333333333333333333cc")
	seq := 0
	mv := func(kind, reason, unit string, amount int, refKind string, refID *primitive.ObjectID, ref map[string]any) *token.Transaction {
		seq++
		return &token.Transaction{
			ID:   oid(t, fmt.Sprintf("cccccccccccccccccccc%04d", seq)),
			Kind: kind, Reason: reason, Unit: unit, Amount: amount,
			RefKind: refKind, RefID: refID, Ref: ref, CreatedAt: at,
		}
	}
	entry := func(tx *token.Transaction, w *token.Wallet) Entry {
		e := EntryFor(tx, w, 100)
		require.NotNil(t, e, tx.Reason)
		return *e
	}

	meal := []Entry{
		// La recharge mobile money, puis le paiement de la commande : le promotionnel d'abord.
		entry(mv(token.KindPurchase, token.ReasonWalletTopup, token.UnitXOF, 5000, "", nil, map[string]any{"payment_id": "p1"}), client),
		entry(mv(token.KindConsume, token.ReasonPayment, token.UnitXOF, 300, token.RefOrder, &order, map[string]any{"source": "promo"}), client),
		entry(mv(token.KindConsume, token.ReasonPayment, token.UnitXOF, 2700, token.RefOrder, &order, map[string]any{"source": "cash"}), client),
		// Le livreur accepte : ses jetons ; ils lui reviennent si la course échoue.
		entry(mv(token.KindConsume, token.ReasonOrderAccept, "", -2, token.RefOrder, &order, nil), courier),
		entry(mv(token.KindPurchase, "order_accept_refund", "", 2, "", nil, nil), courier),
		// Le retrait paie la boutique ; la fin de course paie le livreur, net de la commission.
		entry(mv(token.KindPurchase, token.ReasonOrderPayout, token.UnitXOF, 2500, token.RefOrder, &order, map[string]any{"payment_method": "wallet"}), store),
		entry(mv(token.KindPurchase, token.ReasonDeliveryFee, token.UnitXOF, 1000, token.RefOrder, &order, map[string]any{"delivery_fee": 1000}), courier),
		entry(mv(token.KindConsume, token.ReasonCommission, token.UnitXOF, 150, token.RefOrder, &order, map[string]any{"on": "delivery_fee"}), courier),
		entry(mv(token.KindConsume, token.ReasonDebtRepaid, token.UnitXOF, 100, token.RefOrder, &order, map[string]any{"debt_delta": -100}), courier),
		// Une commande en espèces : la commission prise sur le solde, le reste en dette.
		entry(mv(token.KindConsume, "commission", token.UnitXOF, 50, token.RefOrder, &order, nil), courier),
		entry(mv(token.KindConsume, token.ReasonCommissionDue, token.UnitXOF, 100, token.RefOrder, &order, map[string]any{"debt_delta": 100, "no_balance": true}), courier),
		// Une commande remboursée ; une autre que le solde n'a pas pu payer à la livraison.
		entry(mv(token.KindPurchase, token.ReasonRefund, token.UnitXOF, 3000, token.RefOrder, &order, nil), client),
		entry(mv(token.KindConsume, token.ReasonPayment, token.UnitXOF, 500, token.RefOrder, &order, nil), client),
		entry(mv(token.KindConsume, token.ReasonPaymentDue, token.UnitXOF, 1500, token.RefOrder, &order, map[string]any{"debt_delta": 1500, "no_balance": true}), client),
		entry(mv(token.KindConsume, token.ReasonDebtRepaid, token.UnitXOF, 1500, "", nil, map[string]any{"debt_delta": -1500}), client),
		// Ce qui entoure : un geste commercial, des jetons offerts ou achetés, le matériel.
		entry(mv(token.KindPurchase, token.ReasonPromoCredit, token.UnitXOF, 1000, "", nil, map[string]any{"source": "operator"}), client),
		entry(mv(token.KindPurchase, token.ReasonOperatorCredit, "", 5, "", nil, nil), courier),
		entry(mv(token.KindPurchase, token.ReasonTopup, "", 10, "", nil, nil), courier),
		entry(mv(token.KindConsume, token.ReasonEquipment, token.UnitXOF, 300, token.RefEquipment, &contract, nil), courier),
		entry(mv(token.KindPurchase, token.ReasonEquipment, token.UnitXOF, 5000, token.RefEquipment, &contract, nil), courier),
		entry(mv(token.KindConsume, token.ReasonDebtRepaid, token.UnitXOF, 200, "", nil, map[string]any{"debt_delta": -200, "no_balance": true, "settled": "agency"}), courier),
		// Un motif inconnu : à classer, comme toujours.
		entry(mv(token.KindConsume, "mystery", token.UnitXOF, 42, "", nil, nil), client),
	}

	rideEntries := []Entry{
		// Le prix d'une course, son ajustement en route, son pourboire, son remboursement.
		entry(mv(token.KindConsume, token.ReasonPayment, token.UnitXOF, 800, token.RefRide, &ride, map[string]any{"source": "promo"}), client),
		entry(mv(token.KindConsume, token.ReasonPayment, token.UnitXOF, 1200, token.RefRide, &ride, map[string]any{"source": "cash"}), client),
		entry(mv(token.KindConsume, token.ReasonPayment, token.UnitXOF, 400, token.RefRideAdjustment, &adjustment, map[string]any{"source": "cash"}), client),
		entry(mv(token.KindPurchase, token.ReasonRefund, token.UnitXOF, 400, token.RefRideAdjustment, &adjustment, nil), client),
		entry(mv(token.KindConsume, token.ReasonTip, token.UnitXOF, 500, token.RefTip, &tip, map[string]any{"source": "cash"}), client),
		entry(mv(token.KindPurchase, token.ReasonRefund, token.UnitXOF, 2000, token.RefRide, &ride, nil), client),
		entry(mv(token.KindConsume, token.ReasonPaymentDue, token.UnitXOF, 2000, token.RefRide, &ride, map[string]any{"debt_delta": 2000, "no_balance": true}), client),
		// Le chauffeur : ses jetons à l'acceptation, rendus si elle n'aboutit pas.
		entry(mv(token.KindConsume, "ride_accept", "", -3, token.RefRide, &ride, nil), driver),
		entry(mv(token.KindPurchase, "ride_accept_refund", "", 3, "", nil, nil), driver),
	}
	// Ce que les courses déclarent au journal : le règlement, en ligne et en espèces, et leur grand livre.
	for _, ev := range []Event{
		{Kind: "ride_settled", Country: "TG", Vertical: "vtc", RefKind: "ride", RefID: ride.Hex(), OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), PaymentMethod: "wallet", FareXOF: 2000, CommissionXOF: 400, DriverXOF: 1600},
		{Kind: "ride_settled", Country: "TG", Vertical: "vtc", RefKind: "ride", RefID: ride.Hex(), OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), PaymentMethod: "cash", FareXOF: 2000, CommissionXOF: 400, DriverXOF: 1600},
		{Kind: "ledger_entry", Country: "TG", Vertical: "vtc", OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), EntryKind: "payout", AmountXOF: -5000, Note: "retrait"},
		{Kind: "ledger_entry", Country: "TG", Vertical: "vtc", OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), EntryKind: "settlement", AmountXOF: 700, Note: "dette réglée"},
		{Kind: "ledger_entry", Country: "TG", Vertical: "vtc", OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), EntryKind: "adjustment", AmountXOF: 250, Note: "geste"},
		{Kind: "ledger_entry", Country: "TG", Vertical: "vtc", OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), EntryKind: "adjustment", AmountXOF: -250, Note: "abandon"},
		{Kind: "ledger_entry", Country: "TG", Vertical: "vtc", OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), EntryKind: "equipment", AmountXOF: -300, Note: "matériel"},
		{Kind: "ledger_entry", Country: "TG", Vertical: "vtc", OwnerID: driver.OwnerID.Hex(), At: at.Format(time.RFC3339), EntryKind: "commission", AmountXOF: 120, Note: "commission"},
	} {
		rideEntries = append(rideEntries, EntriesForEvent(ev, at)...)
	}

	got := []goldenCase{{Name: "repas", Entries: meal}, {Name: "course", Entries: rideEntries}}
	raw, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	raw = append(raw, '\n')

	if os.Getenv("DIRA_GOLDEN_UPDATE") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(journalGolden), 0o755))
		require.NoError(t, os.WriteFile(journalGolden, raw, 0o644))
		t.Logf("fichier doré réécrit : %s", journalGolden)
		return
	}
	want, err := os.ReadFile(journalGolden)
	require.NoError(t, err, "le fichier doré manque : il se produit avec le code d'AVANT, jamais pour faire passer l'épreuve")
	assert.Equal(t, string(want), string(raw),
		"le journal d'un repas ou d'une course a changé — comptes, montants ou libellés : c'est une décision, pas un effet de bord")
}
