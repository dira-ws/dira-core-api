package finance

// LES ÉPREUVES DES DÉCLARATIONS.
//
// ⚠️ ELLES LISENT CE QUI SORT, pas ce que des structures Go se racontent entre
// elles : les LIGNES D'ÉCRITURE réellement posées au journal, et le JSON
// réellement servi par le routeur. La plateforme s'est fait prendre trois fois
// par des épreuves qui validaient une structure pendant que le fil portait
// autre chose.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// ⚠️ GABON. Jamais le Togo ni le Sénégal : ce sont des pays RÉELS, et leurs
// alertes préviennent six personnes.
const testCountry = "GA"

// --- la doublure de stockage ---

// fakeDeclStore : un stockage en mémoire, VOLONTAIREMENT bête.
//
// ⚠️ Elle ne porte AUCUNE règle comptable. Le 10 octobre, une doublure plus
// fidèle que la base avait fait passer au vert un dépôt de documents qui
// n'avait jamais marché : tout ce qui peut se tromper (quelle écriture, quel
// signe, quelle date) est donc en fonction PURE, éprouvé directement, et
// celle-ci ne fait que garder ce qu'on lui donne.
type fakeDeclStore struct {
	catalogs map[string]*DeclarationCatalog
	decls    []*Declaration
	// entries : les écritures RÉELLEMENT posées, dans l'ordre.
	entries []Entry
	refs    map[string]bool
}

func newFakeStore() *fakeDeclStore {
	return &fakeDeclStore{catalogs: map[string]*DeclarationCatalog{}, refs: map[string]bool{}}
}

func (f *fakeDeclStore) Catalog(_ context.Context, cc string) (*DeclarationCatalog, error) {
	if c := f.catalogs[cc]; c != nil {
		cp := *c
		return &cp, nil
	}
	d := DefaultDeclarationCatalog(cc)
	return &d, nil
}

func (f *fakeDeclStore) SaveCatalog(_ context.Context, c *DeclarationCatalog) error {
	cp := *c
	f.catalogs[c.Country] = &cp
	return nil
}

func (f *fakeDeclStore) InsertDeclaration(_ context.Context, d *Declaration, e *Entry) error {
	// Le MÊME ORDRE que le dépôt réel : la pièce d'abord, l'écriture ensuite.
	// Une doublure qui poserait l'écriture avant de refuser le doublon aurait
	// caché l'écriture orpheline que cet ordre existe pour éviter.
	keys := []string{}
	if d.Reference != "" {
		keys = append(keys, d.Country+":"+d.Reference)
	}
	if d.SourceRef != "" {
		keys = append(keys, d.Country+":"+d.Source+":"+d.SourceRef)
	}
	for _, k := range keys {
		if f.refs[k] {
			return errDeclarationDuplicate
		}
	}
	// Le même refus que le dépôt réel : une écriture déséquilibrée n'entre pas.
	if e != nil {
		if !e.Balanced() {
			return errUnbalanced
		}
		if e.ID.IsZero() {
			e.ID = primitive.NewObjectID()
		}
		f.entries = append(f.entries, *e)
		id := e.ID
		d.EntryID = &id
	}
	for _, k := range keys {
		f.refs[k] = true
	}
	cp := *d
	f.decls = append(f.decls, &cp)
	return nil
}

func (f *fakeDeclStore) FindDeclaration(_ context.Context, id primitive.ObjectID) (*Declaration, error) {
	for _, d := range f.decls {
		if d.ID == id {
			cp := *d
			return &cp, nil
		}
	}
	return nil, errDeclarationNotFound
}

func (f *fakeDeclStore) ListDeclarations(_ context.Context, fl DeclarationFilter, limit int, _ string) ([]Declaration, string, error) {
	out := []Declaration{}
	for i := len(f.decls) - 1; i >= 0; i-- {
		d := *f.decls[i]
		if fl.Direction != "" && d.Direction != fl.Direction {
			continue
		}
		if fl.Category != "" && d.Category != fl.Category {
			continue
		}
		if fl.Status != "" && d.Status != fl.Status {
			continue
		}
		out = append(out, d)
		if len(out) >= limit {
			break
		}
	}
	return out, "", nil
}

func (f *fakeDeclStore) ReverseDeclaration(_ context.Context, d *Declaration, e *Entry) error {
	for _, cur := range f.decls {
		if cur.ID != d.ID {
			continue
		}
		if cur.Status != DeclarationPosted {
			return errDeclarationAlreadyReversed
		}
		if !e.Balanced() {
			return errUnbalanced
		}
		if e.ID.IsZero() {
			e.ID = primitive.NewObjectID()
		}
		f.entries = append(f.entries, *e)
		id := e.ID
		d.ReversalEntryID = &id
		cur.Status = DeclarationReversed
		cur.ReversalEntryID = &id
		cur.ReversalReason, cur.ReversedBy, cur.ReversedAt = d.ReversalReason, d.ReversedBy, d.ReversedAt
		return nil
	}
	return errDeclarationNotFound
}

func (f *fakeDeclStore) LinkCorrection(_ context.Context, corrected, by primitive.ObjectID) error {
	for _, d := range f.decls {
		if d.ID == corrected {
			id := by
			d.CorrectedBy = &id
			return nil
		}
	}
	return errDeclarationNotFound
}

func (f *fakeDeclStore) DeclarationsInWindow(_ context.Context, from, to time.Time) ([]Declaration, error) {
	out := []Declaration{}
	hits := func(t time.Time) bool {
		return !t.IsZero() && !t.Before(from) && t.Before(to)
	}
	for _, d := range f.decls {
		if hits(d.ValueDate) || (d.ReversedAt != nil && hits(*d.ReversedAt)) {
			out = append(out, *d)
		}
	}
	return out, nil
}

// --- le montage ---

type harness struct {
	r     chi.Router
	store *fakeDeclStore
	svc   *Service
	now   time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store := newFakeStore()
	svc := NewService(nil, nil, time.Time{})
	svc.declStore = store
	now := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	r := chi.NewRouter()
	NewHandler(svc).Mount(r, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), auth.Claims{
				UserID: "staff-1", Role: auth.RoleAdmin, Scopes: []string{auth.ScopeCore}, Country: testCountry,
			})
			ctx = country.WithCountry(ctx, testCountry, country.SourceClaims)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	return &harness{r: r, store: store, svc: svc, now: now}
}

// call joue une requête et rend le code et le corps DÉCODÉ — le JSON servi,
// lu comme une application le lirait.
func (h *harness) call(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, url, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.r.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), "le corps servi n'est pas du JSON : %s", rec.Body.String())
	}
	return rec.Code, out
}

// declareBody : un corps de déclaration valable, qu'un cas modifie.
func declareBody(patch map[string]any) map[string]any {
	body := map[string]any{
		"direction":     "out",
		"category":      "rent",
		"means":         "bank",
		"amount":        450000,
		"value_date":    "2026-10-01",
		"counterparty":  "Immobilière du Golfe",
		"reference":     "FAC-2026-10-014",
		"justification": "Loyer du bureau de Libreville, octobre 2026",
	}
	for k, v := range patch {
		if v == nil {
			delete(body, k)
			continue
		}
		body[k] = v
	}
	return body
}

// --- L'ÉCRITURE EN PARTIE DOUBLE ---

func declFor(direction, category, account, means string, amount int, value time.Time) Declaration {
	return Declaration{
		ID: primitive.NewObjectID(), Country: testCountry, Direction: direction,
		Category: category, CategoryLabel: strings.ToUpper(category), Account: account,
		Means: means, Amount: amount, Currency: "XAF", ValueDate: value,
		Counterparty: "Quelqu'un", Justification: "parce que", Status: DeclarationPosted,
	}
}

// Chaque sens et chaque moyen a SA traduction, équilibrée, sur les bons
// comptes. C'est cette table qui fait que l'aperçu comptable dit la vérité
// sur ce que la plateforme dépense.
func TestEachDeclarationPostsItsBalancedEntry(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name           string
		decl           Declaration
		debit, credit  string
		reason, amount any
	}{
		{"loyer payé en espèces", declFor(DeclarationOut, "rent", AccExpenseRent, MeansCash, 450000, day),
			AccExpenseRent, AccCashBox, ReasonDeclarationOut, 450000},
		{"carburant payé en mobile money", declFor(DeclarationOut, "fuel", AccExpenseFuel, MeansMobileMoney, 25000, day),
			AccExpenseFuel, AccMobileMoney, ReasonDeclarationOut, 25000},
		{"salaires payés par la banque", declFor(DeclarationOut, "salaries", AccExpenseSalaries, MeansBank, 1800000, day),
			AccExpenseSalaries, AccBank, ReasonDeclarationOut, 1800000},
		{"véhicule acheté (immobilisation)", declFor(DeclarationOut, "asset", AccFixedAssets, MeansBank, 9500000, day),
			AccFixedAssets, AccBank, ReasonDeclarationOut, 9500000},
		{"subvention reçue en banque", declFor(DeclarationIn, "subsidy", AccRevenueSubsidy, MeansBank, 3000000, day),
			AccBank, AccRevenueSubsidy, ReasonDeclarationIn, 3000000},
		{"apport d'associé en espèces", declFor(DeclarationIn, "contribution", AccOwnerContributions, MeansCash, 5000000, day),
			AccCashBox, AccOwnerContributions, ReasonDeclarationIn, 5000000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := EntryForDeclaration(c.decl)
			require.NotNil(t, e)
			assert.True(t, e.Balanced(), "l'écriture s'équilibre")
			require.Len(t, e.Lines, 2)
			assert.Equal(t, c.debit, e.Lines[0].Account, "le compte débité")
			assert.Equal(t, c.amount, e.Lines[0].Debit)
			assert.Zero(t, e.Lines[0].Credit)
			assert.Equal(t, c.credit, e.Lines[1].Account, "le compte crédité")
			assert.Equal(t, c.amount, e.Lines[1].Credit)
			assert.Zero(t, e.Lines[1].Debit)
			assert.Equal(t, c.reason, e.Reason)
			assert.Equal(t, c.amount, e.Amount)
			assert.Equal(t, testCountry, e.Country)
			assert.Equal(t, "core", e.Service)
			assert.Equal(t, UnitXOF, e.Unit)
			assert.Equal(t, RefDeclaration, e.RefKind)
			assert.Equal(t, c.decl.ID.Hex(), e.RefID)
			// ⚠️ La date de VALEUR, pas celle de la saisie : c'est elle qui
			// décide de la période où la dépense apparaît.
			assert.Equal(t, day, e.At)
			// ⚠️ AUCUN portefeuille, AUCUN mouvement : sans cela le balayage
			// d'intégrité chercherait un mouvement qui n'existe pas.
			assert.Nil(t, e.TransactionID)
			assert.Nil(t, e.WalletID)
		})
	}
	// Ce qui ne peut pas produire d'écriture n'en produit pas.
	assert.Nil(t, EntryForDeclaration(declFor(DeclarationOut, "rent", AccExpenseRent, MeansCash, 0, day)), "montant nul")
	assert.Nil(t, EntryForDeclaration(declFor(DeclarationOut, "rent", AccExpenseRent, "wallet", 100, day)), "moyen inconnu")
	assert.Nil(t, EntryForDeclaration(declFor(DeclarationOut, "rent", "", MeansCash, 100, day)), "sans compte")
}

// UNE ÉCRITURE NE SE MODIFIE PAS, ELLE SE CONTRE-PASSE : les mêmes comptes,
// les côtés échangés, et la paire s'annule compte par compte.
func TestAReversalIsACounterEntryNotADeletion(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	reversedAt := time.Date(2026, 10, 24, 9, 30, 0, 0, time.UTC)
	d := declFor(DeclarationOut, "fuel", AccExpenseFuel, MeansCash, 25000, day)

	e := EntryForDeclaration(d)
	rev := ReversalEntryForDeclaration(d, reversedAt)
	require.NotNil(t, rev)

	assert.True(t, rev.Balanced())
	assert.Equal(t, ReasonDeclarationReversed, rev.Reason)
	assert.Equal(t, 25000, rev.Amount)
	assert.Equal(t, RefDeclaration, rev.RefKind)
	assert.Equal(t, d.ID.Hex(), rev.RefID, "la contre-passation désigne LA MÊME déclaration")
	// ⚠️ DATÉE DU JOUR OÙ ON L'A FAITE, pas de la date de valeur : une
	// contre-passation antidatée réécrirait une période déjà lue.
	assert.Equal(t, reversedAt, rev.At)
	assert.NotEqual(t, day, rev.At)

	// Les côtés sont échangés : la caisse est débitée, la charge créditée.
	assert.Equal(t, AccCashBox, rev.Lines[0].Account)
	assert.Equal(t, 25000, rev.Lines[0].Debit)
	assert.Equal(t, AccExpenseFuel, rev.Lines[1].Account)
	assert.Equal(t, 25000, rev.Lines[1].Credit)

	// Et la PAIRE s'annule : chaque compte retrouve zéro.
	net := map[string]int{}
	for _, x := range []*Entry{e, rev} {
		for _, l := range x.Lines {
			net[l.Account] += l.Debit - l.Credit
		}
	}
	for acc, v := range net {
		assert.Zero(t, v, "le compte %s doit revenir à zéro après contre-passation", acc)
	}
}

// walletBackedAccounts : les comptes qu'une déclaration NE DOIT JAMAIS
// toucher, parce que leur solde est la contrepartie de portefeuilles réels.
//
// ⚠️ Une déclaration qui les mouvrait ferait dire au journal que la plateforme
// doit une somme que personne ne détient — et RIEN ne le signalerait : le
// balayage compare les portefeuilles à LEURS mouvements, jamais au journal.
var walletBackedAccounts = []string{
	AccClientWallets, AccPromoCredits, AccMerchantPayables, AccAgentPayables,
	AccAgentTokens, AccEquipmentDeposit, AccOrderClearing, AccRideClearing, AccSuspense,
}

// LE BALAYAGE D'INTÉGRITÉ RESTE VERT. Les trois choses qu'il regarde du côté
// du journal sont vérifiées ici sur les écritures que les déclarations posent
// réellement.
func TestDeclarationsKeepTheIntegritySweepGreen(t *testing.T) {
	h := newHarness(t)
	// Une poignée de déclarations dans les deux sens, et une annulation.
	for i, body := range []map[string]any{
		declareBody(nil),
		declareBody(map[string]any{"category": "fuel", "amount": 25000, "means": "cash", "reference": "RC-77"}),
		declareBody(map[string]any{"category": "subsidy", "direction": "in", "amount": 3000000, "reference": "SUB-1"}),
		declareBody(map[string]any{"category": "contribution", "direction": "in", "amount": 5000000, "means": "cash", "reference": nil}),
		declareBody(map[string]any{"category": "asset", "amount": 9500000, "reference": "BL-9"}),
	} {
		code, got := h.call(t, http.MethodPost, "/admin/finance/declarations", body)
		require.Equal(t, http.StatusCreated, code, "déclaration %d : %v", i, got)
	}
	first := h.store.decls[0]
	code, _ := h.call(t, http.MethodPost, "/admin/finance/declarations/"+first.ID.Hex()+"/reverse",
		map[string]any{"reason": "le loyer avait déjà été réglé en septembre"})
	require.Equal(t, http.StatusOK, code)

	require.Len(t, h.store.entries, 6, "cinq écritures et une contre-passation")

	debits, credits := 0, 0
	for _, e := range h.store.entries {
		// `entry_unbalanced` : chaque écriture s'équilibre.
		assert.True(t, e.Balanced(), "écriture %s déséquilibrée", e.Reason)
		// `missing_entry` et `wallet_drift` : rien ne rattache l'écriture à un
		// portefeuille, donc le balayage n'a rien à y chercher.
		assert.Nil(t, e.TransactionID)
		assert.Nil(t, e.WalletID)
		assert.Empty(t, e.OwnerID)
		for _, l := range e.Lines {
			debits += l.Debit
			credits += l.Credit
			for _, forbidden := range walletBackedAccounts {
				assert.NotEqual(t, forbidden, l.Account,
					"une déclaration a touché %s : le journal dirait que la plateforme doit un argent que personne ne détient", forbidden)
			}
		}
	}
	// `trial_unbalanced` : la balance générale tient.
	assert.Equal(t, debits, credits, "Σ débits = Σ crédits")
	assert.Positive(t, debits)
}

// Le catalogue ne peut pointer QUE des comptes déclarables — et la règle est
// tenue à l'écriture du catalogue, pas au premier usage.
func TestOnlyDeclarableAccountsCanBeACategory(t *testing.T) {
	ok, err := validateCategories(DefaultDeclarationCategories())
	require.NoError(t, err, "le catalogue par défaut est valable")
	assert.Len(t, ok, len(DefaultDeclarationCategories()))

	for _, acc := range walletBackedAccounts {
		_, err := validateCategories([]DeclarationCategory{{Code: "x_y", Label: "X", Direction: DeclarationOut, Account: acc}})
		assert.Error(t, err, "le compte %s ne doit pas être déclarable", acc)
	}
	// Et la liste des comptes proposés n'en contient aucun.
	for _, a := range DeclarableAccounts() {
		for _, forbidden := range walletBackedAccounts {
			assert.NotEqual(t, forbidden, a.Code)
		}
	}
	assert.NotEmpty(t, DeclarableAccounts())

	bad := [][]DeclarationCategory{
		{},
		{{Code: "a", Label: "A", Direction: DeclarationOut, Account: AccExpenseRent}},                                                                                    // code trop court
		{{Code: "Rent Paid", Label: "A", Direction: DeclarationOut, Account: AccExpenseRent}},                                                                            // code non normalisé
		{{Code: "rent", Label: "", Direction: DeclarationOut, Account: AccExpenseRent}},                                                                                  // sans libellé
		{{Code: "rent", Label: "A", Direction: "sideways", Account: AccExpenseRent}},                                                                                     // sens inconnu
		{{Code: "rent", Label: "A", Direction: DeclarationOut, Account: AccExpenseRent}, {Code: "rent", Label: "B", Direction: DeclarationIn, Account: AccRevenueOther}}, // code en double
	}
	for i, cats := range bad {
		_, err := validateCategories(cats)
		assert.Error(t, err, "cas %d", i)
	}
}

// --- LE JSON SERVI ---

// Ce que la console REÇOIT quand on déclare, liste et contre-passe. Lu dans le
// corps servi, clé par clé.
func TestTheServedDeclarationCarriesWhatTheConsoleReads(t *testing.T) {
	h := newHarness(t)
	code, got := h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(nil))
	require.Equal(t, http.StatusCreated, code, "%v", got)

	assert.Equal(t, "out", got["direction"])
	assert.Equal(t, "rent", got["category"])
	assert.Equal(t, "Loyer", got["category_label"])
	assert.Equal(t, AccExpenseRent, got["account"])
	assert.Equal(t, "bank", got["means"])
	assert.EqualValues(t, 450000, got["amount"])
	// ⚠️ La monnaie du PAYS (Gabon → XAF), jamais celle que l'appelant croit.
	assert.Equal(t, "XAF", got["currency"])
	assert.Equal(t, "Immobilière du Golfe", got["counterparty"])
	assert.Equal(t, "FAC-2026-10-014", got["reference"])
	assert.Equal(t, "Loyer du bureau de Libreville, octobre 2026", got["justification"])
	assert.Equal(t, DeclarationPosted, got["status"])
	assert.Equal(t, SourceConsole, got["source"])
	assert.Equal(t, "staff-1", got["declared_by"])
	assert.Equal(t, testCountry, got["country"])
	assert.NotEmpty(t, got["id"])
	// L'écriture est POSÉE, et la réponse la nomme.
	require.NotEmpty(t, got["entry_id"], "la réponse doit porter l'écriture du journal")
	require.Len(t, h.store.entries, 1)
	assert.Equal(t, h.store.entries[0].ID.Hex(), got["entry_id"])
	assert.Equal(t, "2026-10-01T00:00:00Z", got["value_date"])

	// La LISTE sert la même chose.
	code, list := h.call(t, http.MethodGet, "/admin/finance/declarations", nil)
	require.Equal(t, http.StatusOK, code)
	items, _ := list["items"].([]any)
	require.Len(t, items, 1)
	row, _ := items[0].(map[string]any)
	assert.Equal(t, got["id"], row["id"])
	assert.Equal(t, got["entry_id"], row["entry_id"])

	// LA CONTRE-PASSATION : rien ne disparaît, tout se dit.
	id, _ := got["id"].(string)
	code, rev := h.call(t, http.MethodPost, "/admin/finance/declarations/"+id+"/reverse",
		map[string]any{"reason": "payé deux fois, le premier règlement datait de septembre"})
	require.Equal(t, http.StatusOK, code, "%v", rev)
	assert.Equal(t, DeclarationReversed, rev["status"])
	assert.Equal(t, "payé deux fois, le premier règlement datait de septembre", rev["reversal_reason"])
	assert.Equal(t, "staff-1", rev["reversed_by"])
	assert.Equal(t, "2026-10-24T09:30:00Z", rev["reversed_at"])
	require.NotEmpty(t, rev["reversal_entry_id"])
	assert.NotEqual(t, got["entry_id"], rev["reversal_entry_id"], "deux écritures, pas une réécrite")
	// La déclaration est TOUJOURS là, et toujours servie.
	code, again := h.call(t, http.MethodGet, "/admin/finance/declarations/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, DeclarationReversed, again["status"])
	assert.Equal(t, got["entry_id"], again["entry_id"], "son écriture d'origine n'a pas bougé")

	// Contre-passer deux fois est refusé.
	code, _ = h.call(t, http.MethodPost, "/admin/finance/declarations/"+id+"/reverse", map[string]any{"reason": "encore une fois"})
	assert.Equal(t, http.StatusConflict, code)
}

// La CORRECTION est une NOUVELLE déclaration qui désigne la fautive, et le
// lien se remonte dans les deux sens.
func TestACorrectionLinksBothWays(t *testing.T) {
	h := newHarness(t)
	_, first := h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(nil))
	id, _ := first["id"].(string)

	// ⚠️ On ne corrige pas une déclaration VIVANTE : la dépense serait comptée
	// deux fois.
	code, _ := h.call(t, http.MethodPost, "/admin/finance/declarations",
		declareBody(map[string]any{"reference": "FAC-2026-10-014-BIS", "corrects": id}))
	require.Equal(t, http.StatusConflict, code)

	code, _ = h.call(t, http.MethodPost, "/admin/finance/declarations/"+id+"/reverse", map[string]any{"reason": "mauvais montant saisi"})
	require.Equal(t, http.StatusOK, code)

	code, fixed := h.call(t, http.MethodPost, "/admin/finance/declarations",
		declareBody(map[string]any{"amount": 400000, "reference": "FAC-2026-10-014-BIS", "corrects": id}))
	require.Equal(t, http.StatusCreated, code, "%v", fixed)
	assert.Equal(t, id, fixed["corrects"])

	_, old := h.call(t, http.MethodGet, "/admin/finance/declarations/"+id, nil)
	assert.Equal(t, fixed["id"], old["corrected_by"], "la fautive désigne sa correction")

	// Deux corrections pour la même faute : refusé.
	code, _ = h.call(t, http.MethodPost, "/admin/finance/declarations",
		declareBody(map[string]any{"amount": 400000, "reference": "FAC-TER", "corrects": id}))
	assert.Equal(t, http.StatusConflict, code)
}

// Ce qu'une déclaration ne peut pas être.
func TestWhatADeclarationCannotBe(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"sans justificatif", declareBody(map[string]any{"justification": nil}), http.StatusUnprocessableEntity},
		{"justificatif trop court", declareBody(map[string]any{"justification": "ok"}), http.StatusUnprocessableEntity},
		{"sans contrepartie", declareBody(map[string]any{"counterparty": nil}), http.StatusUnprocessableEntity},
		{"montant nul", declareBody(map[string]any{"amount": 0}), http.StatusUnprocessableEntity},
		{"montant négatif", declareBody(map[string]any{"amount": -1000}), http.StatusUnprocessableEntity},
		{"sens inconnu", declareBody(map[string]any{"direction": "sideways"}), http.StatusUnprocessableEntity},
		{"moyen inconnu", declareBody(map[string]any{"means": "wallet"}), http.StatusUnprocessableEntity},
		{"catégorie inconnue", declareBody(map[string]any{"category": "bribes"}), http.StatusUnprocessableEntity},
		{"une subvention qui SORT", declareBody(map[string]any{"category": "subsidy", "direction": "out"}), http.StatusUnprocessableEntity},
		{"un loyer qui ENTRE", declareBody(map[string]any{"direction": "in"}), http.StatusUnprocessableEntity},
		// ⚠️ Déclarer un loyer qu'on paiera le mois prochain écrirait au
		// journal un argent qui n'a pas bougé.
		{"date de valeur dans le futur", declareBody(map[string]any{"value_date": "2026-11-30"}), http.StatusUnprocessableEntity},
		{"date de valeur illisible", declareBody(map[string]any{"value_date": "le 3 du mois"}), http.StatusUnprocessableEntity},
		// ⚠️ Une console restée sur un autre pays enverrait des francs
		// guinéens dans un pays en francs CFA : quatorze fois trop.
		{"monnaie d'un autre pays", declareBody(map[string]any{"currency": "GNF"}), http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			code, got := h.call(t, http.MethodPost, "/admin/finance/declarations", c.body)
			assert.Equal(t, c.want, code, "%v", got)
			assert.Empty(t, h.store.entries, "rien de refusé ne doit laisser d'écriture au journal")
		})
	}
	// La monnaie du pays, elle, passe.
	h := newHarness(t)
	code, _ := h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(map[string]any{"currency": "XAF"}))
	assert.Equal(t, http.StatusCreated, code)
}

// La même PIÈCE ne se déclare pas deux fois — c'est ce qui empêche de régler
// deux fois la même facture.
func TestTheSamePieceCannotBeDeclaredTwice(t *testing.T) {
	h := newHarness(t)
	code, _ := h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(nil))
	require.Equal(t, http.StatusCreated, code)
	code, got := h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(nil))
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "declaration_duplicate", errorCode(got), "%v", got)
	// Sans pièce, aucune protection — et deux pleins le même jour sont deux
	// dépenses.
	code, _ = h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(map[string]any{"reference": nil}))
	require.Equal(t, http.StatusCreated, code)
	code, _ = h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(map[string]any{"reference": nil}))
	assert.Equal(t, http.StatusCreated, code)
}

// --- LA SYNTHÈSE ---

// L'ÉTAT DE SYNTHÈSE DIT LE MÊME CHIFFRE QUE LE JOURNAL, sur la même période.
//
// ⚠️ C'est l'épreuve qui compte : la synthèse est calculée depuis les
// déclarations, le journal depuis les écritures, et les deux doivent se
// rejoindre — sinon l'un des deux mentira le jour d'un contrôle.
func TestTheSummaryAgreesWithTheEntriesActuallyPosted(t *testing.T) {
	h := newHarness(t)
	bodies := []map[string]any{
		declareBody(map[string]any{"value_date": "2026-09-02", "amount": 450000, "reference": "FAC-9"}),
		declareBody(map[string]any{"value_date": "2026-10-02", "amount": 450000, "reference": "FAC-10"}),
		declareBody(map[string]any{"category": "fuel", "means": "cash", "value_date": "2026-10-03", "amount": 25000, "reference": "RC-1"}),
		declareBody(map[string]any{"category": "fuel", "means": "cash", "value_date": "2026-10-10", "amount": 31000, "reference": "RC-2"}),
		declareBody(map[string]any{"category": "subsidy", "direction": "in", "value_date": "2026-10-05", "amount": 3000000, "reference": "SUB-1"}),
	}
	for _, b := range bodies {
		code, got := h.call(t, http.MethodPost, "/admin/finance/declarations", b)
		require.Equal(t, http.StatusCreated, code, "%v", got)
	}
	// La déclaration de SEPTEMBRE est annulée en OCTOBRE : septembre reste tel
	// qu'il était, octobre porte la contre-passation. C'est ce que fait le
	// journal, donc c'est ce que doit faire la synthèse.
	code, _ := h.call(t, http.MethodPost, "/admin/finance/declarations/"+h.store.decls[0].ID.Hex()+"/reverse",
		map[string]any{"reason": "ce loyer avait été réglé par la maison mère"})
	require.Equal(t, http.StatusOK, code)

	from, to := "2026-10-01", "2026-11-01"
	code, sum := h.call(t, http.MethodGet, "/admin/finance/declarations/summary?from="+from+"&to="+to, nil)
	require.Equal(t, http.StatusOK, code, "%v", sum)

	oct1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov1 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// ⚠️ LE POINT DE COMPARAISON : le mouvement NET de la trésorerie, lu dans
	// les lignes d'écriture elles-mêmes. `net` est la seule grandeur que les
	// deux côtés définissent pareil — la synthèse déduit une contre-passation
	// de la catégorie qu'elle corrige (« Loyer : 0 ce mois »), alors que le
	// journal, lui, voit un encaissement sur la banque. Les deux lectures sont
	// justes ; c'est leur DIFFÉRENCE qui doit être nulle.
	assert.EqualValues(t, treasuryNet(h.store.entries, oct1, nov1), sum["net"],
		"le net de la synthèse et le mouvement net de la trésorerie au journal")
	assert.EqualValues(t, 3000000, sum["in"])
	// 450 000 (loyer d'octobre) + 25 000 + 31 000 − 450 000 (contre-passation
	// du loyer de septembre, datée d'octobre) = 56 000.
	assert.EqualValues(t, 56000, sum["out"])
	assert.EqualValues(t, 1, sum["reversals"])
	assert.Equal(t, "XAF", sum["currency"])
	assert.Equal(t, testCountry, sum["country"])
	assert.Equal(t, false, sum["truncated"])

	// Par CATÉGORIE : le carburant pèse ses deux reçus, le loyer s'annule.
	byCat := map[string]map[string]any{}
	cats, _ := sum["categories"].([]any)
	require.NotEmpty(t, cats)
	for _, raw := range cats {
		row, _ := raw.(map[string]any)
		byCat[row["category"].(string)] = row
	}
	require.Contains(t, byCat, "fuel")
	assert.EqualValues(t, 56000, byCat["fuel"]["out"])
	assert.EqualValues(t, 2, byCat["fuel"]["count"])
	assert.Equal(t, AccExpenseFuel, byCat["fuel"]["account"])
	require.Contains(t, byCat, "rent")
	assert.EqualValues(t, 0, byCat["rent"]["out"], "un loyer posé et contre-passé dans la période pèse zéro")
	assert.EqualValues(t, 0, byCat["rent"]["count"])
	require.Contains(t, byCat, "subsidy")
	assert.EqualValues(t, 3000000, byCat["subsidy"]["in"])
	// ⚠️ ET CHAQUE CATÉGORIE SE RETROUVE DANS SON COMPTE. C'est la vérification
	// qui attrape une règle de synthèse fausse : une catégorie qui compterait
	// une contre-passation du mauvais côté, ou l'oublierait, ne collerait plus
	// au solde que le compte porte réellement au journal.
	assert.EqualValues(t, accountNet(h.store.entries, AccExpenseFuel, oct1, nov1), byCat["fuel"]["out"])
	assert.EqualValues(t, accountNet(h.store.entries, AccExpenseRent, oct1, nov1), byCat["rent"]["out"])
	assert.EqualValues(t, -accountNet(h.store.entries, AccRevenueSubsidy, oct1, nov1), byCat["subsidy"]["in"])

	// SEPTEMBRE n'a pas été réécrit : le loyer y est toujours.
	code, sept := h.call(t, http.MethodGet, "/admin/finance/declarations/summary?from=2026-09-01&to=2026-10-01", nil)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 450000, sept["out"], "une contre-passation d'octobre ne retire rien à septembre")
	sep1 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	assert.EqualValues(t, accountNet(h.store.entries, AccExpenseRent, sep1, oct1), sept["out"], "et le journal dit la même chose")
	assert.EqualValues(t, treasuryNet(h.store.entries, sep1, oct1), sept["net"])

	// Les MOIS sont rendus dans l'ordre.
	months, _ := sum["months"].([]any)
	require.Len(t, months, 1)
	m, _ := months[0].(map[string]any)
	assert.Equal(t, "2026-10", m["month"])
	assert.EqualValues(t, 56000, m["out"])
}

// accountNet rend le mouvement NET d'un compte sur une fenêtre, lu dans les
// LIGNES des écritures réellement posées (débits − crédits).
//
// ⚠️ Volontairement calculé depuis les lignes, sans rien savoir des
// déclarations : c'est ce qui en fait un point de comparaison INDÉPENDANT de
// la règle de synthèse qu'on veut vérifier.
func accountNet(entries []Entry, account string, from, to time.Time) int {
	net := 0
	for _, e := range entries {
		if e.At.Before(from) || !e.At.Before(to) {
			continue
		}
		for _, l := range e.Lines {
			if l.Account == account {
				net += l.Debit - l.Credit
			}
		}
	}
	return net
}

// treasuryNet : ce que la trésorerie a gagné ou perdu sur la fenêtre, toutes
// déclarations confondues.
func treasuryNet(entries []Entry, from, to time.Time) int {
	net := 0
	for _, acc := range []string{AccCashBox, AccBank, AccMobileMoney} {
		net += accountNet(entries, acc, from, to)
	}
	return net
}

// errorCode rend le code de refus de l'enveloppe servie.
func errorCode(body map[string]any) string {
	detail, _ := body["error"].(map[string]any)
	code, _ := detail["code"].(string)
	return code
}

// --- LE CATALOGUE ---

func TestTheCatalogueIsPerCountryAndReplacesWholesale(t *testing.T) {
	h := newHarness(t)
	code, got := h.call(t, http.MethodGet, "/admin/finance/declaration-categories", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, testCountry, got["country"])
	cats, _ := got["categories"].([]any)
	assert.Len(t, cats, len(DefaultDeclarationCategories()), "un pays qui n'a rien réglé a le catalogue par défaut")
	accs, _ := got["accounts"].([]any)
	assert.NotEmpty(t, accs, "la console doit pouvoir proposer un compte")
	means, _ := got["means"].([]any)
	assert.Len(t, means, 3)

	// ⚠️ L'ÉCRITURE REMPLACE : deux catégories envoyées, deux catégories en
	// place — les dix autres ont disparu, et c'est ce que l'appelant demande.
	code, saved := h.call(t, http.MethodPut, "/admin/finance/declaration-categories", map[string]any{
		"categories": []map[string]any{
			{"code": "rent", "label": "Loyer du siège", "direction": "out", "account": AccExpenseRent},
			{"code": "grant", "label": "Subvention publique", "direction": "in", "account": AccRevenueSubsidy},
		},
	})
	require.Equal(t, http.StatusOK, code, "%v", saved)
	cats, _ = saved["categories"].([]any)
	require.Len(t, cats, 2)
	assert.Equal(t, "Loyer du siège", cats[0].(map[string]any)["label"])
	assert.Equal(t, "staff-1", saved["updated_by"])

	// Et c'est bien CE catalogue que la déclaration lit : `fuel` n'existe plus.
	code, _ = h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(map[string]any{"category": "fuel"}))
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	code, used := h.call(t, http.MethodPost, "/admin/finance/declarations", declareBody(nil))
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "Loyer du siège", used["category_label"], "le libellé est FIGÉ au moment de la déclaration")

	// Un compte non déclarable est refusé à l'écriture du catalogue.
	code, refused := h.call(t, http.MethodPut, "/admin/finance/declaration-categories", map[string]any{
		"categories": []map[string]any{{"code": "sneaky", "label": "Détournement", "direction": "out", "account": AccClientWallets}},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.NotEmpty(t, errorCode(refused), "%v", refused)
}

// --- LA COUTURE ---

// LA COUTURE LAISSÉE À LA PAIE : un module déclare, c'est une déclaration
// ordinaire, et un réessai ne double pas le versement.
func TestTheModuleSeamIsIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := country.WithCountry(auth.WithUser(context.Background(), "payroll-job", auth.RoleAdmin), testCountry, country.SourceClaims)
	in := DeclareInput{
		Direction: DeclarationOut, Category: "salaries", Means: MeansBank, Amount: 1800000,
		ValueDate: "2026-10-05", Counterparty: "Awa Mbadinga", Justification: "Bulletin de paie d'octobre 2026",
	}
	d, err := h.svc.DeclareFromModule(ctx, SourcePayroll, "payslip-42", "payroll-job", in)
	require.NoError(t, err)
	assert.Equal(t, SourcePayroll, d.Source)
	assert.Equal(t, "payslip-42", d.SourceRef)
	assert.Equal(t, AccExpenseSalaries, d.Account)
	require.NotNil(t, d.EntryID)
	require.Len(t, h.store.entries, 1)
	assert.Equal(t, AccExpenseSalaries, h.store.entries[0].Lines[0].Account)
	assert.Equal(t, AccBank, h.store.entries[0].Lines[1].Account)

	// Le même bulletin, une seconde fois : refusé, et AUCUNE écriture de plus.
	_, err = h.svc.DeclareFromModule(ctx, SourcePayroll, "payslip-42", "payroll-job", in)
	require.Error(t, err)
	assert.Len(t, h.store.entries, 1, "un réessai ne double pas un salaire")

	// Un module sans référence perd l'idempotence : on ne le laisse pas faire.
	_, err = h.svc.DeclareFromModule(ctx, SourcePayroll, "", "payroll-job", in)
	assert.Error(t, err)
	// Et un module ne peut pas se faire passer pour la console.
	_, err = h.svc.DeclareFromModule(ctx, SourceConsole, "x", "payroll-job", in)
	assert.Error(t, err)
}

// --- LES ROUTES ---

// Les routes sont montées DANS le groupe authentifié de la finance.
//
// ⚠️ On interroge le routeur plutôt que les URL : une route montée hors du
// groupe répondrait 200 à n'importe qui, et un appel depuis l'épreuve — qui
// pose toujours un utilisateur — ne verrait pas la différence.
func TestDeclarationRoutesAreMountedInsideTheAdminGroup(t *testing.T) {
	// Le groupe de `Mount` porte les intergiciels ; une route posée dehors
	// n'en a aucun. On compare donc le NOMBRE d'intergiciels de chaque route à
	// celui d'une route de référence du même groupe.
	type route struct {
		pattern string
		mws     int
	}
	found := map[string]int{}
	r := chi.NewRouter()
	NewHandler(NewService(nil, nil, time.Time{})).Mount(r, func(next http.Handler) http.Handler { return next })
	require.NoError(t, chi.Walk(r, func(method, pattern string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		found[method+" "+strings.TrimSuffix(pattern, "/")] = len(mws)
		return nil
	}))
	require.NotEmpty(t, found, "un balayage vide ne prouve rien")

	reference, ok := found["GET /admin/finance/overview"]
	require.True(t, ok, "la route de référence doit exister")

	want := []route{
		{"GET /admin/finance/declarations", reference},
		{"POST /admin/finance/declarations", reference},
		{"GET /admin/finance/declarations/summary", reference},
		{"GET /admin/finance/declarations/{id}", reference},
		{"POST /admin/finance/declarations/{id}/reverse", reference},
		{"GET /admin/finance/declaration-categories", reference},
		{"PUT /admin/finance/declaration-categories", reference},
	}
	names := make([]string, 0, len(found))
	for k := range found {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, w := range want {
		mws, ok := found[w.pattern]
		require.True(t, ok, "%s n'est pas montée — routes : %s", w.pattern, strings.Join(names, ", "))
		assert.Equal(t, w.mws, mws, "%s n'a pas les intergiciels du groupe : elle est montée DEHORS", w.pattern)
	}
	// Rien n'est monté sur la porte de SERVICE : une verticale n'a pas à
	// déclarer les dépenses du pays.
	internal := chi.NewRouter()
	NewHandler(NewService(nil, nil, time.Time{})).MountInternal(internal, func(next http.Handler) http.Handler { return next })
	require.NoError(t, chi.Walk(internal, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		assert.NotContains(t, pattern, "declaration", "%s %s : les déclarations ne s'ouvrent pas aux verticales", method, pattern)
		return nil
	}))
}
