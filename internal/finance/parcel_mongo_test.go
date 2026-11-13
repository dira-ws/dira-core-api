package finance

// L'ARGENT D'UN COLIS DE BOUT EN BOUT, SUR UNE VRAIE BASE.
//
// ⚠️⚠️ CE QU'UNE DOUBLURE NE PEUT PAS MONTRER : le mouvement et son écriture
// dans la MÊME transaction Mongo, les clés d'idempotence tenues par l'unicité
// de `_id`, et le balayage d'intégrité qui recalcule chaque solde depuis ses
// mouvements. Les flux du plan — l'envoi payé par mobile money puis débité,
// livré, annulé après l'acceptation, échoué puis rendu, l'entreprise endettée
// puis approvisionnée — doivent laisser un journal ÉQUILIBRÉ, un passage des
// colis VIDE une fois tout réparti, rien « à classer », et un balayage MUET.
//
// Lancée seulement si `DIRA_TEST_MONGO_URI` est posé, sur une base jetable.
// ⚠️ Pays d'essai : le Togo — jamais le Sénégal.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

func TestTheParcelMoneyBalancesOnARealDatabase(t *testing.T) {
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	base, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	m, err := db.Connect(base, uri, fmt.Sprintf("dira_test_colis_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })

	finRepo := NewRepository(m)
	tokRepo := token.NewRepository(m)
	tokRepo.SetJournal(NewJournal(finRepo, 100))
	tok := token.NewService(tokRepo, audit.NewRecorder(m.DB), nil, nil, nil, 100, 5, nil)
	ctx := country.WithCountry(base, "TG", country.SourceHeader)
	hex := func() string { return primitive.NewObjectID().Hex() }

	client, courier, atelier, atelierEndette := hex(), hex(), hex(), hex()
	require.NoError(t, tok.CreateClientWallet(ctx, client))
	require.NoError(t, tok.CreateWallet(ctx, courier, token.WalletTypeDriver))
	require.NoError(t, tok.CreateWallet(ctx, atelier, token.WalletTypeBusiness))
	require.NoError(t, tok.CreateWallet(ctx, atelierEndette, token.WalletTypeBusiness))
	require.NoError(t, tok.Credit(ctx, courier, 10, token.ReasonOperatorCredit), "les jetons du livreur")

	// F8 + F2 — un particulier paie par mobile money : la recharge que fait la
	// confirmation, puis le débit de l'envoi (rejoué : une fois). Livré : le
	// gain de l'étape, net de 15 %.
	envoi1, aller1 := hex(), hex()
	require.NoError(t, tok.TopUp(ctx, client, 2200, map[string]any{"payment_id": hex()}))
	require.NoError(t, tok.Pay(ctx, client, 2200, token.RefShipment, envoi1))
	require.NoError(t, tok.Pay(ctx, client, 2200, token.RefShipment, envoi1), "le rappel rejoué ne débite pas deux fois")
	_, _, err = tok.CreditEarningsNet(ctx, courier, 2200, 330, token.ReasonDeliveryFee, token.RefShipmentLeg, aller1, nil)
	require.NoError(t, err)

	// L'entreprise approvisionnée sur pièce, puis F4 — annulé après
	// l'acceptation : 1 760 rendus, 440 de compensation (66 de commission), et
	// les jetons de l'acceptation rendus, rejoués sans être rendus deux fois.
	_, err = tok.ProvisionBusiness(ctx, hex(), atelier, token.ProvisionInput{
		Amount: 50_000, Means: "bank", Reference: "VIR-2026-11-0042", Justification: "virement reçu le 13 nov.",
	})
	require.NoError(t, err)
	// La même pièce, saisie une seconde fois : refusée par la VRAIE unicité, et
	// la transaction annulée ne laisse ni argent ni ligne derrière elle.
	_, err = tok.ProvisionBusiness(ctx, hex(), atelier, token.ProvisionInput{
		Amount: 50_000, Means: "bank", Reference: "vir-2026-11-0042", Justification: "double saisie",
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, apperr.From(err).HTTPStatus)
	provisions, err := m.Collection("token_transactions").CountDocuments(ctx, bson.M{"reason": token.ReasonBusinessProvision})
	require.NoError(t, err)
	assert.EqualValues(t, 1, provisions, "une pièce, un approvisionnement")
	envoi2, aller2 := hex(), hex()
	require.NoError(t, tok.Pay(ctx, atelier, 2200, token.RefShipment, envoi2))
	require.NoError(t, tok.Consume(ctx, courier, 2, token.ReasonOrderAccept, token.RefShipmentLeg, aller2, nil))
	require.NoError(t, tok.Refund(ctx, atelier, 1760, token.RefShipment, envoi2))
	_, _, err = tok.CreditEarningsNet(ctx, courier, 440, 66, token.ReasonParcelCancelCompensation, token.RefShipmentLeg, aller2, nil)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, tok.CreditRef(ctx, courier, 2, "order_accept_refund", token.RefShipmentLeg, aller2))
	}

	// F6 — échoué puis rendu, chez une entreprise SANS solde : l'aller payé par
	// la première, le frais de retour porté à la dette de la seconde, puis
	// remboursé d'office par son approvisionnement ; le retour payé au livreur.
	envoi3, aller3, retour3, frais3 := hex(), hex(), hex(), hex()
	require.NoError(t, tok.Pay(ctx, atelier, 2200, token.RefShipment, envoi3))
	_, _, err = tok.CreditEarningsNet(ctx, courier, 2200, 330, token.ReasonDeliveryFee, token.RefShipmentLeg, aller3, nil)
	require.NoError(t, err)
	paid, owed, err := tok.Owe(ctx, atelierEndette, 950, token.ReasonParcelReturnFee, token.ReasonPaymentDue, token.RefShipmentCharge, frais3)
	require.NoError(t, err)
	assert.Zero(t, paid)
	assert.Equal(t, 950, owed)
	_, _, err = tok.CreditEarningsNet(ctx, courier, 950, 143, token.ReasonDeliveryFee, token.RefShipmentLeg, retour3, nil)
	require.NoError(t, err)
	_, err = tok.ProvisionBusiness(ctx, hex(), atelierEndette, token.ProvisionInput{
		Amount: 5_000, Means: "mobile_money", Reference: "MM-778899", Justification: "versement mobile reçu",
	})
	require.NoError(t, err)

	// Les soldes.
	w, err := tok.WalletOf(ctx, client)
	require.NoError(t, err)
	assert.Zero(t, w.BalanceXOF, "le client a payé ce qu'il a rechargé, une fois")
	w, err = tok.WalletOf(ctx, atelier)
	require.NoError(t, err)
	assert.Equal(t, 50_000-2200+1760-2200, w.BalanceXOF)
	w, err = tok.WalletOf(ctx, atelierEndette)
	require.NoError(t, err)
	assert.Zero(t, w.DebtXOF, "la dette remboursée sur l'approvisionnement")
	assert.Equal(t, 5_000-950, w.BalanceXOF)
	w, err = tok.WalletOf(ctx, courier)
	require.NoError(t, err)
	assert.Equal(t, (2200-330)+(440-66)+(2200-330)+(950-143), w.BalanceXOF, "chaque étape payée une fois, nette")
	assert.Equal(t, 10, w.Balance, "les jetons de l'acceptation rendus une fois")

	// Le journal : équilibré, rien à classer, le passage des colis vidé, les
	// avances égales à ce que les entreprises détiennent.
	totals, err := finRepo.Totals(ctx, EntryFilter{})
	require.NoError(t, err)
	byAcc := map[string]AccountTotal{}
	d, c := 0, 0
	for _, at := range totals {
		byAcc[at.Account] = at
		d += at.Debit
		c += at.Credit
	}
	assert.Equal(t, d, c, "la balance générale")
	assert.Zero(t, byAcc[AccSuspense].Debit+byAcc[AccSuspense].Credit, "un mouvement du colis est tombé « à classer »")
	assert.Zero(t, byAcc[AccParcelClearing].Credit-byAcc[AccParcelClearing].Debit, "le passage des colis n'est pas vidé")
	assert.Positive(t, byAcc[AccParcelClearing].Credit, "rien n'est passé par le compte des colis")
	assert.Zero(t, byAcc[AccOrderClearing].Debit+byAcc[AccOrderClearing].Credit, "le colis a touché le compte des commandes")
	assert.Equal(t, (50_000-2200+1760-2200)+(5_000-950), byAcc[AccBusinessAdvances].Credit-byAcc[AccBusinessAdvances].Debit,
		"les avances des entreprises valent ce qu'elles détiennent")
	assert.Equal(t, 50_000, byAcc[AccBank].Debit, "le virement est entré par la banque")
	assert.Zero(t, byAcc[AccAgentReceivable].Debit-byAcc[AccAgentReceivable].Credit, "la créance du retour s'est éteinte")

	// Et le balayage ne relève rien.
	run, err := NewService(finRepo, nil, time.Time{}).RunIntegrity(ctx)
	require.NoError(t, err)
	var findings []bson.M
	cur, err := m.Collection(findingsCollection).Find(ctx, bson.M{})
	require.NoError(t, err)
	require.NoError(t, cur.All(ctx, &findings))
	assert.Zero(t, run.Findings, "le balayage a relevé : %v", findings)
	assert.Equal(t, 4, run.Wallets)
}
