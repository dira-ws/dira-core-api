package ownfleet

// LES ÉPREUVES DE LA FLOTTE INTERNE.
//
// ⚠️ ELLES LISENT LE DOCUMENT ÉCRIT ET LE JSON SERVI, jamais deux structures Go
// comparées entre elles. Le sujet de ce paquet est précisément un champ qui
// EXISTE et que personne ne remplit : seul le document relu peut le dire.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/equipment"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// --- doublures ---------------------------------------------------------------

// memStore tient les documents en mémoire.
//
// ⚠️⚠️ `Reserve` ET `Release` Y PORTENT LEURS CONDITIONS, EXACTEMENT COMME LA
// BASE. C'est tout l'intérêt de cette doublure : une réservation qui réussirait
// toujours ferait passer au vert l'épreuve de la double attribution en ne
// vérifiant rien, et une libération sans condition sur le contrat cacherait
// qu'un retour en retard reprend la voiture du conducteur suivant. Cette
// plateforme a déjà vu une doublure dire la vérité qu'une base ne disait pas —
// on reproduit le mécanisme, on ne l'esquive pas.
type memStore struct {
	assets map[primitive.ObjectID]*Asset
}

func newMemStore() *memStore { return &memStore{assets: map[primitive.ObjectID]*Asset{}} }

func (m *memStore) Create(_ context.Context, a *Asset) error {
	for _, got := range m.assets {
		if got.Country == a.Country && got.Plate == a.Plate {
			// La même unicité que l'index : pays + plaque.
			return errPlateTaken
		}
	}
	a.ID = primitive.NewObjectID()
	now := time.Now().UTC()
	a.CreatedAt, a.UpdatedAt = now, now
	cp := *a
	m.assets[a.ID] = &cp
	return nil
}

func (m *memStore) ByID(_ context.Context, id primitive.ObjectID) (*Asset, error) {
	a, ok := m.assets[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memStore) List(_ context.Context, f Filter, _ primitive.ObjectID, limit int) ([]Asset, error) {
	var out []Asset
	for _, a := range m.assets {
		if f.Status != "" && a.Status != f.Status {
			continue
		}
		if !f.Holder.IsZero() && (a.HolderUserID == nil || *a.HolderUserID != f.Holder) {
			continue
		}
		out = append(out, *a)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *memStore) ByHolder(_ context.Context, userID primitive.ObjectID) ([]Asset, error) {
	var out []Asset
	for _, a := range m.assets {
		if a.HolderUserID != nil && *a.HolderUserID == userID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *memStore) Update(_ context.Context, id primitive.ObjectID, set bson.M) (*Asset, error) {
	a, ok := m.assets[id]
	if !ok {
		return nil, nil
	}
	// ⚠️ LA DOUBLURE REFUSE CE QUE LE DÉPÔT NE DOIT JAMAIS RECEVOIR. Si un jour
	// quelqu'un fait passer l'attribution par la fiche, l'épreuve le dira ici
	// plutôt que six mois plus tard sur un parc incohérent.
	for _, forbidden := range []string{"contract_id", "holder_user_id", "assigned_at", "fleet_id"} {
		if _, bad := set[forbidden]; bad {
			return nil, errors.New("ownfleet: la fiche ne doit pas écrire " + forbidden)
		}
	}
	if v, ok := set["status"].(string); ok {
		a.Status = v
	}
	if v, ok := set["plate"].(string); ok {
		a.Plate = v
	}
	if v, ok := set["notes"].(string); ok {
		a.Notes = v
	}
	if v, ok := set["terms"].(Terms); ok {
		a.Terms = v
	}
	a.UpdatedAt = time.Now().UTC()
	cp := *a
	return &cp, nil
}

func (m *memStore) Reserve(_ context.Context, assetID, contractID, holder primitive.ObjectID, at time.Time) (*Asset, error) {
	a, ok := m.assets[assetID]
	// LE MÊME FILTRE QUE LA BASE : disponible ET sans contrat.
	if !ok || a.Status != StatusAvailable || a.ContractID != nil {
		return nil, nil
	}
	a.Status = StatusAssigned
	a.ContractID, a.HolderUserID, a.AssignedAt = &contractID, &holder, &at
	cp := *a
	return &cp, nil
}

func (m *memStore) Release(_ context.Context, assetID, contractID primitive.ObjectID, at time.Time) (bool, error) {
	a, ok := m.assets[assetID]
	// LE MÊME FILTRE QUE LA BASE : le contrat NOMMÉ, et pas « le contrat en
	// cours ».
	if !ok || a.ContractID == nil || *a.ContractID != contractID {
		return false, nil
	}
	a.Status = StatusAvailable
	a.ContractID, a.HolderUserID, a.AssignedAt = nil, nil, nil
	a.UpdatedAt = at
	return true, nil
}

// memFleets rend la flotte interne du pays.
type memFleets struct {
	id    primitive.ObjectID
	calls int
}

func (f *memFleets) EnsureInternal(context.Context) (string, error) {
	f.calls++
	return f.id.Hex(), nil
}

// memRedevance imite le moteur de prélèvement : elle garde les contrats
// ouverts et sait refuser.
type memRedevance struct {
	contracts map[string]*equipment.ContractResponse
	opens     int
	fail      error
}

func newRedevance() *memRedevance {
	return &memRedevance{contracts: map[string]*equipment.ContractResponse{}}
}

func (m *memRedevance) OpenAssetRental(_ context.Context, in equipment.AssetRentalInput) (*equipment.ContractResponse, error) {
	m.opens++
	if m.fail != nil {
		return nil, m.fail
	}
	c := &equipment.ContractResponse{
		ID: in.ContractID.Hex(), Country: "GA", UserID: in.UserID.Hex(), Vertical: in.Vertical,
		ItemName: in.Label, ItemKind: equipment.KindVehicle,
		AssetID: in.AssetID.Hex(), AssetLabel: in.Label, Included: in.Included,
		Mode: equipment.ModeRental, Status: equipment.StatusDraft,
		PriceXOF: in.FeeXOF, DepositXOF: in.DepositXOF,
		OutstandingXOF: 0, Plan: equipment.Plan{Schedule: equipment.SchedulePerPeriod, Period: in.Plan.Period},
	}
	if in.Days > 0 {
		ends := day0.AddDate(0, 0, in.Days)
		c.EndsAt = &ends
	}
	m.contracts[c.ID] = c
	return c, nil
}

func (m *memRedevance) ListContracts(_ context.Context, f equipment.ContractFilter, _ httpx.Page) ([]equipment.ContractResponse, string, error) {
	var out []equipment.ContractResponse
	for _, c := range m.contracts {
		if len(f.Statuses) > 0 && !has(f.Statuses, c.Status) {
			continue
		}
		out = append(out, *c)
	}
	return out, "", nil
}

func (m *memRedevance) MyContracts(_ context.Context, userID string) ([]equipment.ContractResponse, error) {
	var out []equipment.ContractResponse
	for _, c := range m.contracts {
		if c.UserID == userID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// memAccounts : un conducteur gabonais.
type memAccounts struct{ role string }

func (a memAccounts) CountryOf(context.Context, string) (string, error) { return "GA", nil }
func (a memAccounts) RoleOf(context.Context, string) (string, error) {
	if a.role == "" {
		return "driver", nil
	}
	return a.role, nil
}
func (memAccounts) UserNames(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		out[id] = "Ndong"
	}
	return out, nil
}

var day0 = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

const holderID = "6a9f000000000000000000d2"

// ⚠️ LE GABON : les pays réels de cette plateforme déclenchent des alertes qui
// préviennent six personnes, et une épreuve n'a aucune raison de les nommer.
func desk(t *testing.T) (*Service, *memStore, *memFleets, *memRedevance, context.Context) {
	t.Helper()
	store := newMemStore()
	fleets := &memFleets{id: primitive.NewObjectID()}
	rede := newRedevance()
	svc := NewService(store)
	svc.SetFleets(fleets)
	svc.SetRedevance(rede)
	svc.SetAccounts(memAccounts{})
	svc.now = func() time.Time { return day0 }
	ctx := country.WithCountry(context.Background(), "GA", country.SourceClaims)
	return svc, store, fleets, rede, ctx
}

func corollaInput() AssetInput {
	return AssetInput{
		ClassKey: "eco", Verticals: []string{"vtc"}, Plate: "ga 1234 ab",
		Brand: "Toyota", Model: "Corolla", Year: 2020, Seats: 4,
		Terms: &TermsInput{
			// 150 000 F par semaine, caution 100 000 F : des entiers, dans la
			// plus petite unité de la monnaie du pays.
			FeeXOF: 150_000, Period: equipment.PeriodWeekly, DepositXOF: 100_000,
			Days: 180, Included: []string{equipment.IncludedInsurance},
		},
	}
}

func jsonOf(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// --- le parc ------------------------------------------------------------------

// ⚠️⚠️ L'ÉPREUVE DU PIÈGE MAISON : un véhicule qui entre au parc PORTE son
// propriétaire. Le défaut qu'on reproduit ici est resté invisible longtemps —
// le champ `fleet_id` existait, aucun chemin ne le remplissait, et les écrans
// paraissaient normaux parce qu'ils affichaient « aucun propriétaire ».
func TestAVehicleEnteringTheFleetCarriesItsOwner(t *testing.T) {
	svc, store, fleets, _, ctx := desk(t)

	out, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	got := jsonOf(t, out)

	assert.Equal(t, fleets.id.Hex(), got["fleet_id"], "la flotte interne du pays, écrite et servie")
	assert.Equal(t, StatusAvailable, got["status"], "un véhicule entre au parc disponible, jamais confié")
	assert.Equal(t, "GA 1234 AB", got["plate"], "la plaque est normalisée : l'unicité par pays en dépend")
	assert.Equal(t, "Toyota Corolla · GA 1234 AB", got["label"])
	assert.Equal(t, "eco", got["class_key"], "la classe tarifaire de la verticale, pas une seconde taxonomie")

	// ET LE DOCUMENT ÉCRIT, pas seulement la réponse.
	stored := store.assets[mustID(t, out.ID)]
	require.NotNil(t, stored)
	assert.Equal(t, fleets.id, stored.FleetID)

	// Les DEUX FACES de ce qui est inclus.
	terms, _ := got["terms"].(map[string]any)
	require.NotNil(t, terms)
	assert.Equal(t, []any{"insurance"}, terms["included"])
	assert.Equal(t, []any{"maintenance", "repairs", "fuel", "tracker"}, terms["excluded"],
		"⚠️ servir la seule face « inclus » laisse croire que le reste l'est aussi")
}

func TestTheSamePlateCannotEnterTwice(t *testing.T) {
	svc, _, _, _, ctx := desk(t)
	_, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	_, err = svc.Create(ctx, "admin", corollaInput())
	require.Error(t, err)
	assert.Equal(t, "own_fleet_plate_taken", apperr.From(err).Code,
		"sinon le parc porte deux lignes pour une voiture : l'une confiée, l'autre disponible pour toujours")
}

// --- confier -------------------------------------------------------------------

func TestAssigningWritesTheAttachmentAndOpensTheFee(t *testing.T) {
	svc, store, _, rede, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)

	out, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)
	got := jsonOf(t, out)

	assert.Equal(t, StatusAssigned, got["status"])
	assert.Equal(t, holderID, got["holder_user_id"])
	assert.Equal(t, "Ndong", got["holder_name"])
	require.NotEmpty(t, got["contract_id"])

	// LE DOCUMENT ÉCRIT porte le contrat, et c'est LE contrat ouvert.
	stored := store.assets[mustID(t, created.ID)]
	require.NotNil(t, stored.ContractID)
	assert.Equal(t, stored.ContractID.Hex(), got["contract_id"])
	require.Contains(t, rede.contracts, stored.ContractID.Hex(),
		"⚠️ la réservation désigne un contrat qui EXISTE : c'est l'ordre réserver-puis-ouvrir")

	// LA REDEVANCE SERVIE vient du contrat, pas de la fiche.
	fee, _ := got["fee"].(map[string]any)
	require.NotNil(t, fee)
	assert.Equal(t, float64(150_000), fee["fee_xof"])
	assert.Equal(t, float64(100_000), fee["deposit_xof"])
	assert.Equal(t, equipment.PeriodWeekly, fee["period"])
	assert.Equal(t, []any{"insurance"}, fee["included"])
	assert.NotNil(t, fee["ends_at"], "la durée convenue de 180 jours")

	// Attribué, jamais remis : l'exploitation doit le voir.
	assert.Contains(t, got["attention"], AttentionNotHanded)
}

// ⚠️⚠️ UN VÉHICULE NE PART PAS DEUX FOIS. La garantie vient de l'écriture
// conditionnelle, pas d'une lecture : sans elle, les deux attributions du matin
// où quelqu'un double-clique passeraient, deux chauffeurs paieraient la même
// voiture, et le second l'apprendrait au comptoir.
func TestAVehicleCannotBeAssignedTwice(t *testing.T) {
	svc, store, _, rede, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	first, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)

	const other = "6a9f000000000000000000d3"
	_, err = svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: other, Vertical: "vtc"})
	require.Error(t, err)
	assert.Equal(t, "own_fleet_vehicle_busy", apperr.From(err).Code)

	// Et le DOCUMENT n'a pas bougé : c'est le premier qui l'a.
	stored := store.assets[mustID(t, created.ID)]
	require.NotNil(t, stored.HolderUserID)
	assert.Equal(t, holderID, stored.HolderUserID.Hex())
	assert.Equal(t, first.ContractID, stored.ContractID.Hex())
	assert.Equal(t, 1, rede.opens, "⚠️ AUCUNE SECONDE REDEVANCE : refusée AVANT d'engager quelqu'un")
}

// ⚠️⚠️ SI LA REDEVANCE NE S'OUVRE PAS, LA RÉSERVATION EST DÉFAITE. Sans ce
// retour en arrière, un refus du moteur immobiliserait le véhicule pour
// toujours : réservé par un contrat qui n'existe pas, et aucun écran pour dire
// pourquoi il n'est plus attribuable.
func TestARefusedFeeRollsTheReservationBack(t *testing.T) {
	svc, store, _, rede, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	rede.fail = apperr.Conflict("equipment_mode_not_allowed", "la location n'est pas proposée ici")

	_, err = svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.Error(t, err)
	assert.Equal(t, "equipment_mode_not_allowed", apperr.From(err).Code, "le refus du moteur passe tel quel")

	stored := store.assets[mustID(t, created.ID)]
	assert.Equal(t, StatusAvailable, stored.Status)
	assert.Nil(t, stored.ContractID)
	assert.Nil(t, stored.HolderUserID)

	// Et la voiture est de nouveau attribuable pour de bon.
	rede.fail = nil
	_, err = svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)
}

func TestAssigningRefusesWhatItShould(t *testing.T) {
	t.Run("un compte qui n'est pas conducteur", func(t *testing.T) {
		svc, _, _, _, ctx := desk(t)
		svc.SetAccounts(memAccounts{role: "client"})
		created, err := svc.Create(ctx, "admin", corollaInput())
		require.NoError(t, err)
		_, err = svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
		require.Error(t, err)
		// ⚠️ Sans ce refus, la redevance se retiendrait sur des gains qui
		// n'existent pas, et personne ne saurait pourquoi elle ne rentre jamais.
		assert.Equal(t, "own_fleet_not_a_driver", apperr.From(err).Code)
	})
	t.Run("un métier que le véhicule ne sert pas", func(t *testing.T) {
		svc, _, _, _, ctx := desk(t)
		created, err := svc.Create(ctx, "admin", corollaInput()) // verticals: vtc
		require.NoError(t, err)
		_, err = svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "food"})
		require.Error(t, err)
		assert.Equal(t, "own_fleet_vertical_not_offered", apperr.From(err).Code)
	})
	t.Run("sans redevance nulle part", func(t *testing.T) {
		svc, _, _, _, ctx := desk(t)
		in := corollaInput()
		in.Terms = nil
		created, err := svc.Create(ctx, "admin", in)
		require.NoError(t, err)
		_, err = svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
		require.Error(t, err, "une voiture confiée sans loyer serait un véhicule dehors que rien ne facture")
	})
	t.Run("un véhicule sorti de la flotte", func(t *testing.T) {
		svc, _, _, _, ctx := desk(t)
		created, err := svc.Create(ctx, "admin", corollaInput())
		require.NoError(t, err)
		retired := StatusRetired
		_, err = svc.Update(ctx, "admin", created.ID, AssetPatch{Status: &retired})
		require.NoError(t, err)
		_, err = svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
		require.Error(t, err)
		assert.Equal(t, "own_fleet_vehicle_retired", apperr.From(err).Code)
	})
}

// --- le chemin inverse ---------------------------------------------------------

func TestReleaseGivesTheVehicleBack(t *testing.T) {
	svc, store, _, _, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	assigned, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)

	require.NoError(t, svc.Release(ctx, created.ID, assigned.ContractID, equipment.OutcomeReturned))

	stored := store.assets[mustID(t, created.ID)]
	assert.Equal(t, StatusAvailable, stored.Status)
	assert.Nil(t, stored.ContractID, "⚠️ le champ est EFFACÉ, pas laissé derrière")
	assert.Nil(t, stored.HolderUserID)
	assert.Nil(t, stored.AssignedAt)
}

// ⚠️⚠️ UN RETOUR EN RETARD NE REPREND PAS LA VOITURE DU SUIVANT. C'est la
// condition sur l'identifiant du contrat : sans elle, la libération d'un contrat
// ANCIEN aurait rendu au parc un véhicule déjà reconfié, et le second chauffeur
// se serait vu retirer sa voiture par le retour du premier.
func TestAStaleReleaseDoesNotTakeTheNextDriversVehicle(t *testing.T) {
	svc, store, _, _, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	first, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)
	require.NoError(t, svc.Release(ctx, created.ID, first.ContractID, equipment.OutcomeReturned))

	const next = "6a9f000000000000000000d4"
	second, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: next, Vertical: "vtc"})
	require.NoError(t, err)

	// Le retour du PREMIER arrive maintenant — en retard.
	require.NoError(t, svc.Release(ctx, created.ID, first.ContractID, equipment.OutcomeReturned),
		"il ne se plaint pas : c'est un rejeu, pas une faute")

	stored := store.assets[mustID(t, created.ID)]
	require.NotNil(t, stored.HolderUserID)
	assert.Equal(t, next, stored.HolderUserID.Hex(), "la voiture reste au second conducteur")
	assert.Equal(t, second.ContractID, stored.ContractID.Hex())
}

// ⚠️ LE GESTE DE CORRECTION REFUSE DE DÉFAIRE UNE ATTRIBUTION VIVANTE. Sinon une
// redevance courrait sur une voiture que le parc croit libre, et le prochain
// exploitant l'aurait confiée à un deuxième chauffeur.
func TestForceReleaseRefusesWhileTheFeeContractLives(t *testing.T) {
	svc, store, _, rede, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	assigned, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)

	_, err = svc.ForceRelease(ctx, "admin", created.ID, "la voiture est au garage depuis trois semaines")
	require.Error(t, err)
	assert.Equal(t, "own_fleet_contract_open", apperr.From(err).Code)

	// Une fois le contrat clos, la correction passe.
	rede.contracts[assigned.ContractID].Status = equipment.StatusDefaulted
	out, err := svc.ForceRelease(ctx, "admin", created.ID, "registre resté en arrière après un impayé")
	require.NoError(t, err)
	assert.Equal(t, StatusAvailable, jsonOf(t, out)["status"])
	assert.Nil(t, store.assets[mustID(t, created.ID)].ContractID)
}

// --- la fiche n'écrit jamais l'attribution -------------------------------------

// ⚠️⚠️ `contract_id`, `holder_user_id` et `assigned_at` N'ONT QUE DEUX AUTEURS :
// `Reserve` et `Release`. Un champ qu'on peut aussi poser depuis une fiche finit
// par dire autre chose que le contrat qu'il désigne, et c'est l'écran qui a
// l'air normal. La doublure refuse ces clés ; cette épreuve vérifie qu'aucun
// chemin ne les envoie.
func TestTheVehicleSheetNeverWritesTheAttachment(t *testing.T) {
	set, err := patchOf(&Asset{Status: StatusAvailable}, AssetPatch{
		Plate: ptr("GA 9 ZZ"), Notes: ptr("pneus arrière à changer"),
		Terms: &TermsInput{FeeXOF: 90_000, Period: equipment.PeriodWeekly},
	})
	require.NoError(t, err)
	for _, forbidden := range []string{"contract_id", "holder_user_id", "assigned_at", "fleet_id"} {
		assert.NotContains(t, set, forbidden)
	}

	t.Run("et le statut d'un véhicule confié ne se change pas", func(t *testing.T) {
		cid := primitive.NewObjectID()
		_, err := patchOf(&Asset{Status: StatusAssigned, ContractID: &cid},
			AssetPatch{Status: ptr(StatusMaintenance)})
		require.Error(t, err)
		// ⚠️ Poser « au garage » sur une voiture dont la redevance court aurait
		// arrêté le parc sans arrêter le loyer : l'agent continuerait de payer un
		// véhicule que la console affiche immobilisé.
		assert.Equal(t, "own_fleet_vehicle_assigned", apperr.From(err).Code)
	})
	t.Run("et `assigned` n'est pas un statut qu'on tape", func(t *testing.T) {
		_, err := patchOf(&Asset{Status: StatusAvailable}, AssetPatch{Status: ptr(StatusAssigned)})
		require.Error(t, err)
	})
}

// ⚠️ CHANGER LA FICHE NE RÉÉVALUE PAS UN CONTRAT EN COURS. « Une écriture dont
// le montant bouge n'est pas une écriture » — la règle que le grand livre des
// flottes a déjà payée une fois.
func TestChangingTheSheetDoesNotRepriceALiveFee(t *testing.T) {
	svc, _, _, rede, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	assigned, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)

	// Le parc est mis à jour : la redevance de CE modèle passe à 200 000.
	_, err = svc.Update(ctx, "admin", created.ID, AssetPatch{
		Terms: &TermsInput{FeeXOF: 200_000, Period: equipment.PeriodWeekly},
	})
	require.NoError(t, err)

	assert.Equal(t, 150_000, rede.contracts[assigned.ContractID].PriceXOF,
		"le contrat du conducteur garde ce qui a été figé à son ouverture")
	again, err := svc.Get(ctx, created.ID)
	require.NoError(t, err)
	got := jsonOf(t, again)
	assert.Equal(t, float64(200_000), got["terms"].(map[string]any)["fee_xof"], "la fiche, elle, a changé")
	assert.Equal(t, float64(150_000), got["fee"].(map[string]any)["fee_xof"], "la redevance servie reste celle du contrat")
}

// --- ce que l'agent voit --------------------------------------------------------

func TestWhatTheAgentSees(t *testing.T) {
	svc, _, _, rede, ctx := desk(t)
	created, err := svc.Create(ctx, "admin", corollaInput())
	require.NoError(t, err)
	assigned, err := svc.Assign(ctx, "admin", created.ID, AssignInput{UserID: holderID, Vertical: "vtc"})
	require.NoError(t, err)

	// Le contrat vit, trois semaines sont dues, rien n'a été payé.
	c := rede.contracts[assigned.ContractID]
	c.Status = equipment.StatusActive
	handed := day0
	c.HandedAt = &handed
	c.OutstandingXOF, c.DueXOF, c.PaidXOF = 450_000, 450_000, 150_000
	overdue := day0.AddDate(0, 0, -5)
	c.OverdueSince = &overdue
	c.Blocked = true

	items, err := svc.Mine(ctx, holderID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	got := jsonOf(t, items[0])

	assert.Equal(t, "GA 1234 AB", got["plate"])
	fee, _ := got["fee"].(map[string]any)
	require.NotNil(t, fee)
	assert.Equal(t, float64(150_000), fee["fee_xof"], "ce qu'il paie par semaine")
	assert.Equal(t, float64(450_000), fee["outstanding_xof"], "ce qu'il doit")
	assert.Equal(t, float64(150_000), fee["paid_xof"], "ce qui a été prélevé")
	assert.Equal(t, true, fee["blocked"])
	assert.Equal(t, []any{"insurance"}, fee["included"])
	assert.Equal(t, []any{AttentionOverdue, AttentionBlocked}, got["attention"])

	// ⚠️ NI LE NOM DU PORTEUR, NI LES NOTES D'EXPLOITATION sur son écran : il
	// sait qui il est, et les notes du parc sont des échanges internes.
	assert.NotContains(t, got, "holder_name")
	assert.NotContains(t, got, "notes")
}

// ⚠️ CONFIÉ SANS REDEVANCE LISIBLE EST UN SIGNAL, PAS UN SILENCE : c'est le
// signe qu'un contrat manque ou qu'un retour en arrière a échoué, et une voiture
// dehors que rien ne facture ne doit pas s'afficher comme un véhicule normal.
func TestAssignedWithoutAReadableFeeIsFlagged(t *testing.T) {
	cid := primitive.NewObjectID()
	flags := attentionOf(&Asset{Status: StatusAssigned, ContractID: &cid}, nil, day0)
	assert.Equal(t, []string{AttentionNoContract}, flags)
}

func TestExpiredPapersAreFlagged(t *testing.T) {
	past := day0.AddDate(0, 0, -1)
	flags := attentionOf(&Asset{Status: StatusAvailable, InsuranceExpiresAt: &past, TechnicalExpiresAt: &past}, nil, day0)
	assert.Equal(t, []string{AttentionInsurance, AttentionTechnical}, flags)
	// ⚠️ ET ILS NE COUPENT RIEN : c'est `vehicles.status` dans la verticale qui
	// immobilise, et l'exploitation qui le pose. Un blocage automatique depuis
	// le socle aurait mis un chauffeur à l'arrêt sur une date mal saisie.
}

// --- les conditions en cascade ---------------------------------------------------

func TestTermsCascade(t *testing.T) {
	vehicle := Terms{FeeXOF: 150_000, Period: equipment.PeriodWeekly, DepositXOF: 100_000, Days: 180,
		Included: []string{equipment.IncludedInsurance}}

	t.Run("rien de demandé : celles du véhicule", func(t *testing.T) {
		assert.Equal(t, vehicle, termsFrom(nil, vehicle))
	})
	t.Run("ce qui est demandé l'emporte", func(t *testing.T) {
		got := termsFrom(&TermsInput{FeeXOF: 90_000, Period: equipment.PeriodMonthly}, vehicle)
		assert.Equal(t, 90_000, got.FeeXOF)
		assert.Equal(t, equipment.PeriodMonthly, got.Period)
		assert.Equal(t, 100_000, got.DepositXOF, "le reste reste celui du véhicule")
	})
	t.Run("une liste vide explicite veut dire « rien n'est inclus »", func(t *testing.T) {
		empty := []string{}
		got := termsFrom(termsInputOf(AssignInput{Included: &empty}), vehicle)
		assert.Equal(t, []string{}, got.Included,
			"⚠️ confondre « je n'ai rien dit » avec « rien n'est inclus » offrirait l'assurance à quelqu'un qui ne l'a pas négociée — ou l'inverse, qui se découvre le jour de la panne")
	})
	t.Run("la semaine par défaut", func(t *testing.T) {
		assert.Equal(t, equipment.PeriodWeekly, termsFrom(&TermsInput{FeeXOF: 1}, Terms{}).Period)
	})
}

// ⚠️ LE PAS DE LA REDEVANCE EST CELUI DU PLAN, ET IL NE PEUT PAS Y EN AVOIR
// DEUX : une redevance « mensuelle » prélevée chaque semaine est quatre fois
// trop.
func TestThePlanCarriesTheFeePeriod(t *testing.T) {
	p := planWithPeriod(&equipment.Plan{Period: equipment.PeriodDaily, EarningsPercent: 20}, equipment.PeriodMonthly)
	assert.Equal(t, equipment.PeriodMonthly, p.Period)
	assert.Equal(t, 20, p.EarningsPercent, "le reste du plan est respecté")
	assert.Equal(t, equipment.PeriodWeekly, planWithPeriod(nil, equipment.PeriodWeekly).Period)
}

func TestTheVocabularyIsTheEquipmentOne(t *testing.T) {
	w := Words()
	// ⚠️ PAS UNE COPIE : deux listes auraient divergé au premier ajout, et la
	// console aurait proposé un pas que le contrat refuse.
	assert.Equal(t, equipment.Periods, w.Periods)
	assert.Equal(t, equipment.Included, w.Included)
	assert.Equal(t, Statuses, w.Statuses)
}

func ptr[T any](v T) *T { return &v }

func mustID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return id
}
