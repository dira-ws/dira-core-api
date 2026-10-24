package equipment

// LES ÉPREUVES DE LA REDEVANCE D'UN VÉHICULE — la flotte interne vue depuis le
// moteur de prélèvement.
//
// ⚠️ ELLES LISENT CE QUI SORT, et pas deux structures Go comparées entre elles :
// le JSON servi passe par `encoding/json`, et le document écrit est relu dans la
// doublure. Cette plateforme s'est fait prendre trois fois par des épreuves qui
// validaient une structure pendant que le fil portait autre chose.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// --- doublures ---------------------------------------------------------------

// gaAccounts : des comptes GABONAIS.
//
// ⚠️ LE GABON ET NON LE TOGO : les pays réels de cette plateforme déclenchent
// des alertes qui préviennent six personnes, et une épreuve n'a aucune raison de
// nommer l'un d'eux. `memAccounts` répond « TG » ; la flotte interne vérifie le
// pays du porteur contre celui de la requête, donc il faut les deux cohérents.
type gaAccounts struct{}

func (gaAccounts) CountryOf(context.Context, string) (string, error) { return "GA", nil }
func (gaAccounts) UserNames(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		out[id] = "Ndong"
	}
	return out, nil
}

// memAssets est le REGISTRE DES BIENS, vu depuis le contrat.
//
// ⚠️ ELLE N'ENREGISTRE QUE LES LIBÉRATIONS, parce que c'est tout ce que le
// contrat connaît du registre : la réservation est faite par l'appelant, AVANT
// l'ouverture du contrat (voir `asset.go`). Une doublure qui aurait aussi porté
// `Reserve` aurait décrit une interface que le code n'a pas.
type memAssets struct {
	released []string // "assetID:outcome"
	fail     error
}

func (m *memAssets) Release(_ context.Context, assetID, _, outcome string) error {
	if m.fail != nil {
		return m.fail
	}
	m.released = append(m.released, assetID+":"+outcome)
	return nil
}

// gaDesk monte le comptoir du matériel pour un pays d'ESSAI, avec son registre
// de biens.
func gaDesk(t *testing.T) (*Service, *memStore, *memAssets, *memNotifier, context.Context) {
	t.Helper()
	store := newMemStore()
	svc := NewService(store, &memPurse{balance: map[string]int{}, keys: map[string]bool{}}, gaAccounts{})
	assets := &memAssets{}
	n := &memNotifier{}
	svc.SetAssets(assets)
	svc.SetNotifier(n)
	clock := day0
	svc.now = func() time.Time { return clock }
	ctx := country.WithCountry(context.Background(), "GA", country.SourceClaims)
	return svc, store, assets, n, ctx
}

// corolla ouvre la redevance d'une voiture confiée à un chauffeur.
func corolla(t *testing.T, svc *Service, ctx context.Context, assetID primitive.ObjectID, plan *Plan) (*ContractResponse, primitive.ObjectID) {
	t.Helper()
	contractID := primitive.NewObjectID()
	out, err := svc.OpenAssetRental(ctx, AssetRentalInput{
		ContractID: contractID, AssetID: assetID,
		Label: "Toyota Corolla · GA-1234-AB", UserID: mustID(driverID), Vertical: VerticalVTC,
		// 150 000 F par semaine : un ordre de grandeur réel pour une berline.
		FeeXOF: 150_000, DepositXOF: 100_000, Days: 180,
		Included: []string{IncludedInsurance, IncludedMaintenance},
		Plan:     plan, ActorID: "admin",
	})
	require.NoError(t, err)
	return out, contractID
}

// jsonOf rend le JSON SERVI, décodé en carte — ce que l'application reçoit
// vraiment.
func jsonOf(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// --- le contrat d'un bien unique ---------------------------------------------

// ⚠️ CE QUE CETTE ÉPREUVE PROTÈGE : un véhicule n'est PAS un article de
// catalogue. Un `item_id` servi à vingt-quatre zéros se lirait comme un article
// sur la console, et quelqu'un finirait par chercher « l'article » d'une
// voiture.
func TestAVehicleFeeIsARentalWithNoCatalogueItem(t *testing.T) {
	svc, _, _, n, ctx := gaDesk(t)
	assetID := primitive.NewObjectID()

	c, contractID := corolla(t, svc, ctx, assetID, nil)
	got := jsonOf(t, c)

	assert.Equal(t, contractID.Hex(), got["id"], "l'identifiant du contrat est celui qui a scellé la réservation")
	assert.Equal(t, assetID.Hex(), got["asset_id"])
	assert.Equal(t, "Toyota Corolla · GA-1234-AB", got["asset_label"])
	assert.Equal(t, KindVehicle, got["item_kind"])
	assert.NotContains(t, got, "item_id", "un bien unique n'a pas d'article de catalogue")
	assert.Equal(t, ModeRental, got["mode"])
	assert.Equal(t, float64(150_000), got["price_xof"], "la redevance par période")
	assert.Equal(t, float64(100_000), got["deposit_xof"])
	assert.Equal(t, []any{"maintenance", "insurance"}, got["included"],
		"dans l'ordre du vocabulaire, pas dans celui de la saisie")
	assert.NotNil(t, got["ends_at"], "la durée convenue est servie")
	assert.NotContains(t, got, "term_over", "le terme n'est pas passé : le champ est omis")
	assert.Equal(t, StatusDraft, got["status"], "l'acceptation est exigée par défaut")
	// Le plan a pris le pas de la redevance.
	plan, _ := got["plan"].(map[string]any)
	require.NotNil(t, plan)
	assert.Equal(t, SchedulePerPeriod, plan["schedule"])

	// L'agent est prévenu qu'une attribution l'attend.
	require.NotEmpty(t, n.sent)
	assert.Equal(t, KeyContractProposed, n.sent[len(n.sent)-1].key)
}

// ⚠️ LA REMISE DÉMARRE L'ÉCHÉANCIER, ET NE TOUCHE AUCUN STOCK. Un véhicule n'a
// pas d'unités : l'ajustement de stock doit être sauté, pas « ne rien trouver
// par chance ».
func TestHandingOverAVehicleStartsTheFeeAndTouchesNoStock(t *testing.T) {
	svc, store, _, _, ctx := gaDesk(t)
	assetID := primitive.NewObjectID()
	c, _ := corolla(t, svc, ctx, assetID, nil)

	_, err := svc.Accept(ctx, driverID, c.ID)
	require.NoError(t, err)
	handed, err := svc.HandOver(ctx, "admin", c.ID)
	require.NoError(t, err)

	got := jsonOf(t, handed)
	assert.Equal(t, StatusActive, got["status"])
	assert.Equal(t, HandedViaStaff, got["handed_via"], "remise par l'exploitation : la preuve manque, et c'est écrit")
	// Caution + première période : 100 000 + 150 000.
	assert.Equal(t, float64(250_000), got["outstanding_xof"])
	lines, _ := got["schedule"].([]any)
	require.Len(t, lines, 2)
	assert.Equal(t, LineDeposit, lines[0].(map[string]any)["kind"])
	assert.Equal(t, LinePeriod, lines[1].(map[string]any)["kind"])
	assert.Empty(t, store.items, "aucun article n'a été créé ni touché")
}

// --- la retenue ne vide jamais les gains -------------------------------------

// ⚠️⚠️ L'ÉPREUVE QUI JUSTIFIE TOUTE LA CONCEPTION : la voiture et le casque
// partagent le MÊME budget de retenue. C'est précisément ce qu'un second moteur
// de prélèvement aurait perdu — chacun aurait respecté le minimum laissé à
// l'agent de son côté, et l'agent serait rentré avec zéro.
func TestTheFeeAndTheHelmetShareOneBudgetAndLeaveTheAgentHisMinimum(t *testing.T) {
	svc, _, _, _, ctx := gaDesk(t)

	// Un casque vendu, retenue de 30 %, minimum laissé 3 000.
	helmet, err := svc.CreateItem(ctx, ItemInput{
		Kind: KindHelmet, Name: "Casque", SalePriceXOF: 20_000, Stock: 5, TrackStock: true,
	})
	require.NoError(t, err)
	hard := Plan{
		Schedule: ScheduleInstallments, Installments: 2, Period: PeriodWeekly, FirstDueDays: 0,
		CollectFromEarnings: true, EarningsPercent: 30, MinLeftXOF: 3_000,
	}
	hc, err := svc.CreateContract(ctx, "admin", ContractInput{
		UserID: driverID, Vertical: VerticalVTC, ItemID: helmet.ID, Mode: ModeSale, Plan: &hard,
	})
	require.NoError(t, err)
	_, err = svc.Accept(ctx, driverID, hc.ID)
	require.NoError(t, err)
	_, err = svc.HandOver(ctx, "admin", hc.ID)
	require.NoError(t, err)

	// La voiture, même retenue, même minimum.
	vc, _ := corolla(t, svc, ctx, primitive.NewObjectID(), &hard)
	_, err = svc.Accept(ctx, driverID, vc.ID)
	require.NoError(t, err)
	_, err = svc.HandOver(ctx, "admin", vc.ID)
	require.NoError(t, err)

	// Une course rapporte 5 000 F.
	const earning = 5_000
	out, err := svc.Collect(ctx, driverID, VerticalVTC, earning, "ride", "r1")
	require.NoError(t, err)
	got := jsonOf(t, out)

	taken := int(got["amount_xof"].(float64))
	assert.LessOrEqual(t, taken, earning-3_000,
		"⚠️ TOUS CONTRATS CONFONDUS : le chauffeur garde au moins 3 000 F de ses 5 000 F")
	assert.Positive(t, taken, "la retenue a bien eu lieu")
	// Et les deux contrats ont bien été servis par le MÊME passage.
	lines, _ := got["lines"].([]any)
	require.NotEmpty(t, lines)
	sum := 0
	for _, l := range lines {
		sum += int(l.(map[string]any)["amount_xof"].(float64))
	}
	assert.Equal(t, taken, sum, "le total servi est la somme des lignes servies")
}

// --- les trois sorties rendent le véhicule au parc ----------------------------

// ⚠️⚠️ LE CHEMIN INVERSE, ÉPROUVÉ SUR LES TROIS SORTIES. Le piège maison de
// cette plateforme est un rattachement que rien ne défaisait : le champ
// existait, un seul chemin le remplissait, aucun ne l'effaçait, et les écrans
// paraissaient normaux.
func TestEachExitGivesTheVehicleBackToThePool(t *testing.T) {
	t.Run("le retour", func(t *testing.T) {
		svc, _, assets, _, ctx := gaDesk(t)
		assetID := primitive.NewObjectID()
		c, _ := corolla(t, svc, ctx, assetID, nil)
		_, err := svc.Accept(ctx, driverID, c.ID)
		require.NoError(t, err)
		_, err = svc.HandOver(ctx, "admin", c.ID)
		require.NoError(t, err)

		_, err = svc.Return(ctx, "admin", c.ID, ReturnInput{Condition: "rayure portière"})
		require.NoError(t, err)
		assert.Equal(t, []string{assetID.Hex() + ":" + OutcomeReturned}, assets.released)
	})
	t.Run("l'impayé constaté", func(t *testing.T) {
		svc, _, assets, _, ctx := gaDesk(t)
		assetID := primitive.NewObjectID()
		c, _ := corolla(t, svc, ctx, assetID, nil)
		_, err := svc.Accept(ctx, driverID, c.ID)
		require.NoError(t, err)
		_, err = svc.HandOver(ctx, "admin", c.ID)
		require.NoError(t, err)

		_, err = svc.Default(ctx, "admin", c.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{assetID.Hex() + ":" + OutcomeDefaulted}, assets.released)
	})
	t.Run("l'annulation avant la remise", func(t *testing.T) {
		svc, _, assets, _, ctx := gaDesk(t)
		assetID := primitive.NewObjectID()
		c, _ := corolla(t, svc, ctx, assetID, nil)

		_, err := svc.Cancel(ctx, "admin", c.ID, "le chauffeur a refusé les conditions")
		require.NoError(t, err)
		assert.Equal(t, []string{assetID.Hex() + ":" + OutcomeCancelled}, assets.released,
			"sinon la voiture resterait réservée par un contrat annulé, pour toujours")
	})
}

// ⚠️ UN REGISTRE MUET NE BLOQUE PAS LE RETOUR. Refuser le retour parce que le
// registre n'a pas répondu laisserait un contrat vivant sur un véhicule
// physiquement rendu : la redevance continuerait de courir. Mieux vaut un parc
// à recaler qu'un agent qui paie une voiture qu'il a rendue.
func TestAMuteRegistryDoesNotBlockTheReturn(t *testing.T) {
	svc, _, assets, _, ctx := gaDesk(t)
	assets.fail = apperr.Internal(assert.AnError)
	c, _ := corolla(t, svc, ctx, primitive.NewObjectID(), nil)
	_, err := svc.Accept(ctx, driverID, c.ID)
	require.NoError(t, err)
	_, err = svc.HandOver(ctx, "admin", c.ID)
	require.NoError(t, err)

	out, err := svc.Return(ctx, "admin", c.ID, ReturnInput{Condition: "bon état"})
	require.NoError(t, err, "le retour aboutit malgré le registre muet")
	assert.Equal(t, StatusReturned, jsonOf(t, out)["status"])
}

// --- une reprise qui laisse une dette ----------------------------------------

// ⚠️⚠️ REPRENDRE LA VOITURE N'EFFACE PAS CE QUI EST DÛ, et c'est le cas qu'on
// se trompe le plus facilement : un contrat `returned` sort des lectures
// « actives », et une dette qui disparaît d'un écran disparaît de la mémoire de
// l'exploitation. Les deux faits doivent tenir ENSEMBLE — la voiture revient au
// parc, et l'échéancier garde l'impayé.
func TestATakeBackLeavesTheDebtOwed(t *testing.T) {
	svc, _, assets, _, ctx := gaDesk(t)
	assetID := primitive.NewObjectID()
	// Pas de retenue sur les gains : personne ne paie, et la dette s'accumule.
	c, _ := corolla(t, svc, ctx, assetID, &Plan{
		Period: PeriodWeekly, GraceDays: 2, DepositRefundable: true,
	})
	_, err := svc.Accept(ctx, driverID, c.ID)
	require.NoError(t, err)
	_, err = svc.HandOver(ctx, "admin", c.ID)
	require.NoError(t, err)

	// Trois semaines passent sans un franc versé : le balayage ouvre les
	// périodes et vieillit les lignes.
	for i := 0; i < 3; i++ {
		svc.advance(7 * 24 * time.Hour)
		svc.RunDue(ctx)
	}

	out, err := svc.Return(ctx, "admin", c.ID, ReturnInput{Condition: "reprise pour impayé"})
	require.NoError(t, err)
	got := jsonOf(t, out)

	assert.Equal(t, StatusReturned, got["status"])
	owed := int(got["outstanding_xof"].(float64))
	assert.Positive(t, owed, "⚠️ LA DETTE RESTE DUE : la reprise ne solde rien")
	// Caution (100 000) + 4 périodes de 150 000 = 700 000, moins la caution
	// rendue, qui est un MOUVEMENT et non une remise de dette.
	assert.GreaterOrEqual(t, owed, 4*150_000, "les quatre périodes ouvertes restent dues")
	assert.Equal(t, []string{assetID.Hex() + ":" + OutcomeReturned}, assets.released,
		"et la voiture est bien revenue au parc : les deux faits tiennent ensemble")

	// ⚠️ ET LA DETTE RESTE VISIBLE DANS CE QUE L'AGENT LIT. `standing` compte
	// les contrats `returned` : s'arrêter aux contrats actifs aurait fait
	// disparaître de son écran, et de celui du support, exactement ce qu'il
	// reste à recouvrer.
	st, err := svc.Standing(ctx, driverID)
	require.NoError(t, err)
	assert.Equal(t, owed, int(jsonOf(t, st)["outstanding_xof"].(float64)))
}

// --- les refus ----------------------------------------------------------------

func TestOpeningAVehicleFeeRefusesWhatItCannotHonour(t *testing.T) {
	t.Run("sans registre branché", func(t *testing.T) {
		store := newMemStore()
		svc := NewService(store, nil, gaAccounts{})
		svc.now = func() time.Time { return day0 }
		ctx := country.WithCountry(context.Background(), "GA", country.SourceClaims)
		_, err := svc.OpenAssetRental(ctx, AssetRentalInput{
			ContractID: primitive.NewObjectID(), AssetID: primitive.NewObjectID(),
			UserID: mustID(driverID), Vertical: VerticalVTC, FeeXOF: 1_000,
		})
		// ⚠️ ON REFUSE PLUTÔT QUE D'OUVRIR UNE REDEVANCE QUE PERSONNE NE SAURA
		// RENDRE AU PARC : un véhicule confié par un déploiement sans registre
		// serait un véhicule dehors que rien ne libère.
		require.Error(t, err)
		assert.Equal(t, "equipment_asset_desk_unavailable", apperr.From(err).Code)
	})
	t.Run("sans redevance", func(t *testing.T) {
		svc, _, _, _, ctx := gaDesk(t)
		_, err := svc.OpenAssetRental(ctx, AssetRentalInput{
			ContractID: primitive.NewObjectID(), AssetID: primitive.NewObjectID(),
			UserID: mustID(driverID), Vertical: VerticalVTC, FeeXOF: 0,
		})
		require.Error(t, err, "un loyer nul n'est pas un prêt : il se dit par un plan sans échéance")
	})
	t.Run("un porteur d'un autre pays", func(t *testing.T) {
		store := newMemStore()
		svc := NewService(store, nil, memAccounts{}) // memAccounts répond « TG »
		svc.SetAssets(&memAssets{})
		svc.now = func() time.Time { return day0 }
		ctx := country.WithCountry(context.Background(), "GA", country.SourceClaims)
		_, err := svc.OpenAssetRental(ctx, AssetRentalInput{
			ContractID: primitive.NewObjectID(), AssetID: primitive.NewObjectID(),
			UserID: mustID(driverID), Vertical: VerticalVTC, FeeXOF: 150_000,
		})
		// ⚠️ LA REDEVANCE EST LIBELLÉE DANS LA MONNAIE DU PAYS DU VÉHICULE :
		// confier une voiture gabonaise à un compte d'ailleurs aurait inscrit un
		// loyer lu dans une autre unité, et personne ne l'aurait vu passer.
		require.Error(t, err)
		assert.Equal(t, "validation_failed", apperr.From(err).Code)
	})
}
