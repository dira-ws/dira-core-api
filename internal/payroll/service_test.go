package payroll

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
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// --- LA DOUBLURE ------------------------------------------------------------
//
// ⚠️ ELLE PORTE LES MÊMES VERROUS QUE LA BASE, et c'est non négociable. Une
// doublure plus permissive que Mongo dit vrai pendant que la base dit le
// contraire : c'est ce qui a laissé partir soixante-deux fois le même rapport
// journalier, avec une épreuve verte. Les trois garanties recopiées ici sont
// celles dont dépend l'argent :
//
//  1. INSERTION D'UNE PAIE : l'index unique partiel
//     (country, frequency, period_start) sur les états `draft`/`approved`/`paid`.
//     ⚠️ Il couvre l'état de NAISSANCE — sans quoi il ne verrouillerait rien.
//  2. CHANGEMENT DE STATUT : une écriture CONDITIONNELLE (`FindOneAndUpdate`
//     filtré sur le statut attendu) — c'est elle qui empêche un double
//     versement.
//  3. RÉSERVATION D'UN AJUSTEMENT : seuls ceux qui n'ont pas de `run_id` sont
//     pris, en une écriture.

type memStore struct {
	employees   map[primitive.ObjectID]*Employee
	adjustments map[primitive.ObjectID]*Adjustment
	runs        map[primitive.ObjectID]*Run
	settings    map[string]*Settings
	// onMove est appelé AVANT le changement de statut : c'est la fenêtre de
	// course que l'on veut pouvoir ouvrir dans une épreuve.
	onMove func(run *Run)
	// onCount est appelé APRÈS avoir compté les paies d'une période et avant
	// que l'appelant n'écrive : c'est l'autre fenêtre de course, celle entre
	// « est-ce déjà préparé ? » et l'insertion.
	onCount func()
}

func newMemStore() *memStore {
	return &memStore{
		employees:   map[primitive.ObjectID]*Employee{},
		adjustments: map[primitive.ObjectID]*Adjustment{},
		runs:        map[primitive.ObjectID]*Run{},
		settings:    map[string]*Settings{},
	}
}

func (m *memStore) InsertEmployee(_ context.Context, e *Employee) error {
	e.ID = primitive.NewObjectID()
	e.CreatedAt, e.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	// L'unicité du compte rattaché — l'index partiel sur `user_id`.
	if e.UserID != nil {
		for _, other := range m.employees {
			if other.UserID != nil && *other.UserID == *e.UserID {
				return errRunExists
			}
		}
	}
	cp := *e
	m.employees[e.ID] = &cp
	return nil
}

func (m *memStore) EmployeeByID(_ context.Context, id primitive.ObjectID) (*Employee, error) {
	e, ok := m.employees[id]
	if !ok {
		return nil, errEmployeeNotFound
	}
	cp := *e
	return &cp, nil
}

func (m *memStore) SaveEmployee(_ context.Context, e *Employee) error {
	if _, ok := m.employees[e.ID]; !ok {
		return errEmployeeNotFound
	}
	cp := *e
	m.employees[e.ID] = &cp
	return nil
}

func (m *memStore) ListEmployees(_ context.Context, f EmployeeFilter, limit int, _ string) ([]Employee, string, error) {
	var out []Employee
	for _, e := range m.employees {
		if f.Status != "" && e.Status != f.Status {
			continue
		}
		if f.Frequency != "" && e.Frequency != f.Frequency {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FullName < out[j].FullName })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func (m *memStore) PayableEmployees(_ context.Context, frequency string) ([]Employee, error) {
	var out []Employee
	for _, e := range m.employees {
		if e.Frequency == frequency && e.Status == StatusActive {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FullName < out[j].FullName })
	return out, nil
}

func (m *memStore) InsertAdjustment(_ context.Context, a *Adjustment) error {
	a.ID = primitive.NewObjectID()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	a.UpdatedAt = a.CreatedAt
	cp := *a
	m.adjustments[a.ID] = &cp
	return nil
}

func (m *memStore) AdjustmentByID(_ context.Context, id primitive.ObjectID) (*Adjustment, error) {
	a, ok := m.adjustments[id]
	if !ok {
		return nil, errAdjustmentNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *memStore) SaveAdjustment(_ context.Context, a *Adjustment) error {
	if _, ok := m.adjustments[a.ID]; !ok {
		return errAdjustmentNotFound
	}
	cp := *a
	m.adjustments[a.ID] = &cp
	return nil
}

func (m *memStore) sorted(list []Adjustment) []Adjustment {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID.Hex() < list[j].ID.Hex()
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return list
}

func (m *memStore) AdjustmentsOfEmployee(_ context.Context, employeeID primitive.ObjectID, status string) ([]Adjustment, error) {
	var out []Adjustment
	for _, a := range m.adjustments {
		if a.EmployeeID != employeeID {
			continue
		}
		if status != "" && a.Status != status {
			continue
		}
		out = append(out, *a)
	}
	return m.sorted(out), nil
}

func (m *memStore) AdjustmentsOfRun(_ context.Context, runID primitive.ObjectID) ([]Adjustment, error) {
	var out []Adjustment
	for _, a := range m.adjustments {
		if a.RunID != nil && *a.RunID == runID {
			out = append(out, *a)
		}
	}
	return m.sorted(out), nil
}

// ClaimAdjustments ne prend QUE ce qui n'est pas déjà réservé — comme le
// filtre `run_id: {$exists: false}` de la base.
func (m *memStore) ClaimAdjustments(_ context.Context, runID primitive.ObjectID, employees []primitive.ObjectID, periodEnd time.Time) ([]Adjustment, error) {
	wanted := map[primitive.ObjectID]bool{}
	for _, id := range employees {
		wanted[id] = true
	}
	for _, a := range m.adjustments {
		switch {
		case !wanted[a.EmployeeID], a.Status != AdjustmentOpen, a.RunID != nil:
			continue
		case !a.EffectiveFrom.IsZero() && a.EffectiveFrom.After(periodEnd):
			continue
		}
		id := runID
		a.RunID = &id
	}
	return m.AdjustmentsOfRun(context.Background(), runID)
}

func (m *memStore) ReleaseAdjustments(_ context.Context, runID primitive.ObjectID) error {
	for _, a := range m.adjustments {
		if a.RunID != nil && *a.RunID == runID && a.Status == AdjustmentOpen {
			a.RunID = nil
		}
	}
	return nil
}

func (m *memStore) ConsumeAdjustments(_ context.Context, applied []Applied, at time.Time) error {
	for _, ap := range applied {
		a, ok := m.adjustments[ap.AdjustmentID]
		if !ok {
			continue
		}
		a.Taken += ap.Amount
		a.Runs++
		a.RunID = nil
		a.UpdatedAt = at
		if ap.Settled {
			a.Status = AdjustmentSettled
			settled := at
			a.SettledAt = &settled
		}
	}
	return nil
}

// InsertRun porte le VERROU DE PÉRIODE — l'index unique partiel, états
// `draft`/`approved`/`paid` compris.
func (m *memStore) InsertRun(_ context.Context, run *Run) error {
	for _, other := range m.runs {
		same := other.Country == run.Country &&
			other.Frequency == run.Frequency &&
			other.PeriodStart.Equal(run.PeriodStart)
		locked := other.Status == RunDraft || other.Status == RunApproved || other.Status == RunPaid
		if same && locked {
			return errRunExists
		}
	}
	run.ID = primitive.NewObjectID()
	run.CreatedAt, run.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	if run.Slips == nil {
		run.Slips = []Payslip{}
	}
	cp := *run
	m.runs[run.ID] = &cp
	return nil
}

func (m *memStore) RunByID(_ context.Context, id primitive.ObjectID) (*Run, error) {
	run, ok := m.runs[id]
	if !ok {
		return nil, errRunNotFound
	}
	cp := *run
	return &cp, nil
}

func (m *memStore) CountRunsForPeriod(_ context.Context, frequency string, periodStart time.Time) (int64, error) {
	var n int64
	for _, run := range m.runs {
		locked := run.Status == RunDraft || run.Status == RunApproved || run.Status == RunPaid
		if run.Frequency == frequency && run.PeriodStart.Equal(periodStart) && locked {
			n++
		}
	}
	// ⚠️ LA FENÊTRE DE COURSE, OUVERTE EXPRÈS. Entre « est-ce déjà préparé ? »
	// et l'écriture, une autre console a le temps de préparer la même période :
	// c'est précisément ce que le comptage ne peut pas empêcher.
	if m.onCount != nil {
		m.onCount()
	}
	return n, nil
}

func (m *memStore) SaveRunSlips(_ context.Context, run *Run) error {
	stored, ok := m.runs[run.ID]
	if !ok || stored.Status != RunDraft {
		return errRunMoved
	}
	stored.Slips = run.Slips
	stored.Gross, stored.Bonuses = run.Gross, run.Bonuses
	stored.Deductions, stored.Net = run.Deductions, run.Net
	stored.Carried, stored.Headcount = run.Carried, run.Headcount
	return nil
}

// MoveRun est CONDITIONNELLE : le statut attendu fait partie du filtre, comme
// dans la base. C'est cette ligne, et elle seule, qui empêche deux versements.
func (m *memStore) MoveRun(_ context.Context, id primitive.ObjectID, from, to string, set bson.M) (*Run, error) {
	stored, ok := m.runs[id]
	if !ok {
		return nil, errRunNotFound
	}
	if m.onMove != nil {
		m.onMove(stored)
	}
	if stored.Status != from {
		return nil, errRunMoved
	}
	stored.Status = to
	for key, value := range set {
		switch key {
		case "approved_by":
			stored.ApprovedBy = value.(string)
		case "approved_at":
			at := value.(time.Time)
			stored.ApprovedAt = &at
		case "paid_by":
			stored.PaidBy = value.(string)
		case "paid_at":
			at := value.(time.Time)
			stored.PaidAt = &at
		case "method":
			stored.Method = value.(string)
		case "reference":
			stored.Reference = value.(string)
		case "cancelled_at":
			at := value.(time.Time)
			stored.CancelledAt = &at
		case "cancel_reason":
			stored.CancelReason = value.(string)
		}
	}
	cp := *stored
	return &cp, nil
}

func (m *memStore) ListRuns(_ context.Context, f RunFilter, limit int, _ string) ([]Run, string, error) {
	var out []Run
	for _, run := range m.runs {
		if f.Status != "" && run.Status != f.Status {
			continue
		}
		if f.Frequency != "" && run.Frequency != f.Frequency {
			continue
		}
		if f.EmployeeID != nil {
			found := false
			for i := range run.Slips {
				if run.Slips[i].EmployeeID == *f.EmployeeID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, *run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeriodStart.After(out[j].PeriodStart) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func (m *memStore) Settings(_ context.Context, code string) (*Settings, error) {
	if s, ok := m.settings[code]; ok {
		cp := *s
		return &cp, nil
	}
	d := DefaultSettings(code)
	return &d, nil
}

func (m *memStore) SaveSettings(_ context.Context, s *Settings) error {
	cp := *s
	m.settings[s.Country] = &cp
	return nil
}

// --- LE BANC D'ESSAI -------------------------------------------------------

// ⚠️ LE GABON (`GA`) ET NON LE TOGO NI LE SÉNÉGAL : ce sont des pays RÉELS, et
// une alerte y prévient six personnes. Le Gabon est le pays d'essai, et sa
// monnaie est le franc CFA d'Afrique centrale (XAF).
const testCountry = "GA"

const operator = "6a0000000000000000000001"

type desk struct {
	svc   *Service
	store *memStore
	// router monte les VRAIES routes avec les vrais gardes : ce que l'épreuve
	// lit est ce qu'une console recevrait.
	router chi.Router
	// now est l'horloge du service — figée pour que les périodes ne dépendent
	// pas du jour où l'épreuve tourne.
	now time.Time
}

// bench monte le service et ses routes. `now` est figé au 10 avril 2026 : mars
// est donc la dernière période mensuelle CLOSE.
func bench(t *testing.T) *desk {
	t.Helper()
	store := newMemStore()
	svc := NewService(store, nil)
	frozen := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return frozen }

	r := chi.NewRouter()
	NewHandler(svc).Mount(r, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), auth.Claims{
				UserID: operator, Role: auth.RoleAdmin, Scopes: []string{auth.ScopeCore},
				Country: testCountry,
			})
			ctx = country.WithCountry(ctx, testCountry, country.SourceClaims)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	return &desk{svc: svc, store: store, router: r, now: frozen}
}

// call joue une requête RÉELLE sur le routeur et rend le code et le corps
// décodé.
//
// ⚠️ L'ÉPREUVE LIT LE JSON SERVI, pas la valeur de retour du service. C'est la
// seule façon de voir qu'un champ ne sort pas, qu'un montant sort sous un autre
// nom, ou qu'un secret voyage — trois défauts qui ne se voient jamais en
// comparant deux structures Go.
func (d *desk) call(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader([]byte("{}"))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	d.router.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out),
			"réponse illisible : %s", rec.Body.String())
	}
	return rec.Code, out
}

func (d *desk) errorCode(body map[string]any) string {
	envelope, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := envelope["code"].(string)
	return code
}

func (d *desk) reason(body map[string]any) string {
	envelope, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}
	reason, _ := envelope["reason"].(string)
	return reason
}

// newEmployee déclare un salarié PAR LA ROUTE et rend son identifiant.
func (d *desk) newEmployee(t *testing.T, name string, salary int) string {
	t.Helper()
	code, body := d.call(t, http.MethodPost, "/admin/payroll/employees", map[string]any{
		"full_name": name, "position": "Agent de comptoir", "base_salary": salary,
		"frequency": FrequencyMonthly, "pay_day": 28, "hired_at": "2025-01-01T00:00:00Z",
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", body)
	id, _ := body["id"].(string)
	require.NotEmpty(t, id)
	return id
}

// prepareMarch prépare la paie de mars 2026 — la dernière période close.
func (d *desk) prepareMarch(t *testing.T) (string, map[string]any) {
	t.Helper()
	code, body := d.call(t, http.MethodPost, "/admin/payroll/runs", map[string]any{
		"frequency": FrequencyMonthly,
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", body)
	id, _ := body["id"].(string)
	require.NotEmpty(t, id)
	return id, body
}

func number(t *testing.T, body map[string]any, key string) int {
	t.Helper()
	raw, ok := body[key]
	require.True(t, ok, "la réponse ne porte pas `%s` : %v", key, body)
	value, ok := raw.(float64)
	require.True(t, ok, "`%s` n'est pas un nombre : %T", key, raw)
	return int(value)
}

// --- CE QUE LES ROUTES EXPOSENT --------------------------------------------

// mounted énumère les routes RÉELLEMENT enregistrées.
//
// ⚠️ On interroge le routeur plutôt que d'appeler les URL : un 404 de routage
// et un 404 métier sont le même code, et une épreuve bâtie sur les réponses
// passe au vert en vérifiant la mauvaise chose.
func mounted(t *testing.T, r chi.Router) []string {
	t.Helper()
	var out []string
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, method+" "+strings.TrimSuffix(route, "/"))
		return nil
	}))
	require.NotEmpty(t, out, "un balayage vide ne prouve rien : le handler n'a rien monté")
	sort.Strings(out)
	return out
}

// ⚠️ IL N'EXISTE AUCUNE ROUTE QUI MODIFIE UNE PAIE, et c'est ce qui rend la
// promesse « une paie versée ne se modifie pas » tenable. La vérifier par un
// refus ne suffirait pas : un `PATCH` ajouté plus tard répondrait 200 sans que
// rien ne le signale.
//
// ⚠️ ET AUCUNE SURFACE D'APPLICATION. Un salaire et un motif de retenue sont
// parmi les données les plus sensibles de la plateforme : la route « ma paie »
// n'existe pas, et son absence est une décision qu'on relit ici.
func TestNoRouteEverModifiesAPayrollAndNoneIsOpenToApps(t *testing.T) {
	routes := mounted(t, bench(t).router)

	for _, forbidden := range []string{
		"PATCH /admin/payroll/runs/{id}",
		"PUT /admin/payroll/runs/{id}",
		"DELETE /admin/payroll/runs/{id}",
	} {
		assert.NotContains(t, routes, forbidden, "une paie ne se modifie pas")
	}
	for _, route := range routes {
		assert.NotContains(t, route, "/me/payroll", "la paie n'a pas de surface d'application")
		assert.Contains(t, route, "/admin/payroll", "toute route de la paie est une route d'exploitation")
	}
	// Le cycle complet est bien là, lui.
	assert.Contains(t, routes, "POST /admin/payroll/runs")
	assert.Contains(t, routes, "POST /admin/payroll/runs/{id}/approve")
	assert.Contains(t, routes, "POST /admin/payroll/runs/{id}/pay")
	assert.Contains(t, routes, "POST /admin/payroll/runs/{id}/corrections")
	// ⚠️ Deux routes pour les deux gestes d'argent : un `kind` mal rempli dans
	// un corps aurait retenu de l'argent là où l'on voulait en donner.
	assert.Contains(t, routes, "POST /admin/payroll/employees/{id}/bonuses")
	assert.Contains(t, routes, "POST /admin/payroll/employees/{id}/deductions")
}

// --- DÉCLARER UN SALARIÉ ---------------------------------------------------

// Un salarié SANS COMPTE est le cas normal, pas une exception : la plupart des
// gens payés par l'entreprise n'ouvrent aucune application.
func TestAnEmployeeWithoutAnAccountIsTheNormalCase(t *testing.T) {
	d := bench(t)
	code, body := d.call(t, http.MethodPost, "/admin/payroll/employees", map[string]any{
		"full_name": "Gardienne de nuit", "position": "Gardiennage",
		"base_salary": 90_000, "frequency": FrequencyMonthly, "pay_day": 28,
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", body)
	assert.NotContains(t, body, "user_id", "aucun compte : le champ ne doit pas sortir")
	assert.Equal(t, "XAF", body["currency"], "le Gabon paie en francs CFA d'Afrique centrale")
	assert.Equal(t, StatusActive, body["status"])
	// La borne de retenue est SERVIE : la console n'a pas à la recalculer.
	assert.Equal(t, 27_000, number(t, body, "max_deduction_per_run"), "30 % de 90 000")
	// Et la prochaine date de versement est calculée.
	assert.NotEmpty(t, body["next_pay_date"])
}

// ⚠️ UNE FRÉQUENCE ET UN JOUR QUI NE VONT PAS ENSEMBLE SONT REFUSÉS PAR LA
// ROUTE. Accepté en silence, le jour du mois resterait affiché sur une fiche
// hebdomadaire et se lirait, six mois plus tard, comme la règle appliquée.
func TestTheRouteRefusesAFrequencyThatDoesNotMatchItsPayDay(t *testing.T) {
	d := bench(t)
	code, body := d.call(t, http.MethodPost, "/admin/payroll/employees", map[string]any{
		"full_name": "Agent hebdomadaire", "position": "Comptoir",
		"base_salary": 40_000, "frequency": FrequencyWeekly, "pay_day": 15,
	})
	assert.Equal(t, http.StatusUnprocessableEntity, code, "corps : %v", body)
	assert.Equal(t, "validation_failed", d.errorCode(body))

	// Et le même geste, cohérent, passe.
	code, _ = d.call(t, http.MethodPost, "/admin/payroll/employees", map[string]any{
		"full_name": "Agent hebdomadaire", "position": "Comptoir",
		"base_salary": 40_000, "frequency": FrequencyWeekly, "pay_weekday": 5,
	})
	assert.Equal(t, http.StatusCreated, code)
}

// --- LA BORNE DES RETENUES -------------------------------------------------

// ⚠️ UNE RETENUE QUE LA PAIE NE POURRA JAMAIS HONORER EST REFUSÉE À
// L'ÉCRITURE. Acceptée, elle se reporterait de mois en mois pendant que la
// console affiche « retenue en cours » sans que rien ne bouge.
//
// ⚠️ ET LE REFUS DIT COMBIEN D'ÉCHÉANCES IL FAUDRAIT, dans `reason` : les
// chiffres du message disparaîtraient à la traduction, et l'opérateur devrait
// calculer de tête.
func TestADeductionBiggerThanOnePaycheckIsRefusedAtWrite(t *testing.T) {
	d := bench(t)
	id := d.newEmployee(t, "Agent endetté", 200_000)

	code, body := d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/deductions", map[string]any{
		"amount": 150_000, "reason": "avance sur salaire",
	})
	require.Equal(t, http.StatusConflict, code, "corps : %v", body)
	assert.Equal(t, "payroll_deduction_too_large", d.errorCode(body))
	assert.Contains(t, d.reason(body), "max_per_run=60000", "60 000 = 30 % de 200 000")
	assert.Contains(t, d.reason(body), "min_installments=3",
		"le refus doit dire combien d'échéances il faudrait")

	// La même retenue, ÉTALÉE sur trois paies, est acceptée.
	code, body = d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/deductions", map[string]any{
		"amount": 150_000, "reason": "avance sur salaire", "installments": 3,
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", body)
	assert.Equal(t, 150_000, number(t, body, "amount"))
	assert.Equal(t, 150_000, number(t, body, "remaining"))
	assert.Equal(t, KindDeduction, body["kind"])
}

// ⚠️ ET LA BORNE TIENT AUSSI AU CALCUL, pas seulement à l'écriture. Les deux
// endroits ne protègent pas de la même chose : l'écriture empêche
// d'enregistrer l'impossible, le calcul empêche une paie vide quand plusieurs
// retenues légitimes se cumulent — ce qu'aucune d'elles ne pouvait prévoir
// seule.
func TestSeveralLegitimateDeductionsStillCannotEmptyAPay(t *testing.T) {
	d := bench(t)
	id := d.newEmployee(t, "Agent endetté", 100_000)
	for _, reason := range []string{"avance de janvier", "casse de matériel", "cotisation"} {
		code, body := d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/deductions", map[string]any{
			"amount": 25_000, "reason": reason,
		})
		require.Equal(t, http.StatusCreated, code, "corps : %v", body)
	}

	_, run := d.prepareMarch(t)
	slips := run["slips"].([]any)
	require.Len(t, slips, 1)
	slip := slips[0].(map[string]any)

	assert.Equal(t, 30_000, number(t, slip, "deductions"), "30 % de 100 000, trois retenues confondues")
	assert.Equal(t, 70_000, number(t, slip, "net"))
	assert.Equal(t, 70_000, number(t, slip, "floor"))
	assert.Equal(t, 45_000, number(t, slip, "carried"), "le reste est reporté, jamais effacé")

	// ⚠️ LA FICHE DIT QUELLE LIGNE A ÉTÉ BORNÉE. Sans `capped`, la borne serait
	// invisible pour qui lit la fiche — c'est-à-dire inexistante.
	lines := slip["lines"].([]any)
	require.Len(t, lines, 3)
	first := lines[0].(map[string]any)
	assert.Equal(t, "avance de janvier", first["reason"])
	assert.Equal(t, false, first["capped"], "la plus ancienne passe en entier")
	third := lines[2].(map[string]any)
	assert.Equal(t, true, third["capped"])
	assert.Equal(t, 0, number(t, third, "amount"))
	assert.Equal(t, 25_000, number(t, third, "planned"), "ce qui était visé reste écrit")
}

// --- LE CYCLE D'UNE PAIE ---------------------------------------------------

// La paie préparée porte une fiche par salarié, ses totaux, et sa monnaie.
func TestThePreparedPayrollIsServedWithOneSlipPerEmployee(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)
	d.newEmployee(t, "Boris", 100_000)

	_, run := d.prepareMarch(t)
	assert.Equal(t, RunDraft, run["status"])
	assert.Equal(t, true, run["editable"], "un brouillon se modifie encore")
	assert.Equal(t, "XAF", run["currency"])
	assert.Equal(t, 2, number(t, run, "headcount"))
	assert.Equal(t, 250_000, number(t, run, "gross"))
	assert.Equal(t, 250_000, number(t, run, "net"))
	assert.Equal(t, "2026-03-01T00:00:00Z", run["period_start"])
	assert.Equal(t, "2026-04-01T00:00:00Z", run["period_end"], "la borne de fin est exclusive")
	// ⚠️ La dernière journée est SERVIE : un écran qui affiche « du 1er au
	// 1er avril » se fait corriger tous les mois.
	assert.Equal(t, "2026-03-31T00:00:00Z", run["period_last_day"])
	assert.Equal(t, "2026-04-28T00:00:00Z", run["pay_date"], "le 28 du pays, après la période")

	slips := run["slips"].([]any)
	require.Len(t, slips, 2)
	assert.Equal(t, "Aline", slips[0].(map[string]any)["name"])
	assert.Equal(t, "2026-04-28T00:00:00Z", slips[0].(map[string]any)["pay_date"],
		"chaque fiche porte la date de versement de SA personne")
}

// ⚠️ ON NE PRÉPARE PAS UNE PÉRIODE OUVERTE. Une paie préparée le 10 pour le
// mois en cours ne porte ni les primes ni les retenues des vingt jours qui
// restent — et elle serait versée comme si elle était complète.
func TestAnOpenPeriodIsNotPrepared(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)

	code, body := d.call(t, http.MethodPost, "/admin/payroll/runs", map[string]any{
		"frequency": FrequencyMonthly, "on": "2026-04-05T00:00:00Z",
	})
	assert.Equal(t, http.StatusConflict, code, "corps : %v", body)
	assert.Equal(t, "payroll_period_open", d.errorCode(body))
}

// ⚠️ DEUX PAIES NE COUVRENT JAMAIS LA MÊME PÉRIODE. C'est le verrou qui
// empêche de payer deux fois tout le monde : sans lui, deux onglets préparent
// deux paies identiques et les deux se versent.
func TestTheSamePeriodIsNeverPreparedTwice(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)
	d.prepareMarch(t)

	code, body := d.call(t, http.MethodPost, "/admin/payroll/runs", map[string]any{
		"frequency": FrequencyMonthly,
	})
	assert.Equal(t, http.StatusConflict, code, "corps : %v", body)
	assert.Equal(t, "payroll_run_exists", d.errorCode(body))
}

// ⚠️ ET C'EST LE VERROU QUI REFUSE LA COURSE, PAS LE CONTRÔLE QUI PRÉCÈDE.
// La distinction n'est pas théorique : la première version de l'épreuve
// ci-dessus restait VERTE quand on réduisait le verrou aux paies versées,
// parce que le comptage, lui, voyait toujours le brouillon. Elle prouvait le
// contrôle et pas le verrou — exactement la faute qui a fait partir soixante-
// deux fois le même rapport journalier : réserver hors de son propre index,
// c'est verrouiller la porte de sortie et laisser celle d'entrée ouverte.
//
// Ici, l'autre console prépare la même période APRÈS le comptage. Le contrôle
// a donc répondu « rien pour cette période », et seule l'unicité à
// l'insertion peut encore refuser.
func TestThePeriodLockRefusesTheRaceThatThePreCheckCannotSee(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)

	once := false
	d.store.onCount = func() {
		if once {
			return
		}
		once = true
		// Une autre console vient de préparer mars, juste après notre comptage.
		require.NoError(t, d.store.InsertRun(context.Background(), &Run{
			Country: testCountry, Frequency: FrequencyMonthly,
			PeriodStart: day(2026, 3, 1), PeriodEnd: day(2026, 4, 1),
			Status: RunDraft,
		}))
	}

	code, body := d.call(t, http.MethodPost, "/admin/payroll/runs", map[string]any{
		"frequency": FrequencyMonthly,
	})
	assert.Equal(t, http.StatusConflict, code, "corps : %v", body)
	assert.Equal(t, "payroll_run_exists", d.errorCode(body))
	assert.Len(t, d.store.runs, 1, "aucune seconde paie ne doit avoir été écrite")
}

// ⚠️ UNE PAIE NE SE VERSE QU'UNE FOIS, et le second geste reçoit un REFUS —
// jamais une seconde confirmation, qui ferait croire à un second virement.
func TestAPayrollIsNeverPaidTwice(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)
	id, _ := d.prepareMarch(t)

	code, _ := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/approve", nil)
	require.Equal(t, http.StatusOK, code)

	code, paid := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/pay", map[string]any{
		"method": MethodBank, "reference": "AVIS-2026-03",
	})
	require.Equal(t, http.StatusOK, code, "corps : %v", paid)
	assert.Equal(t, RunPaid, paid["status"])
	assert.Equal(t, false, paid["editable"], "le serveur DIT que le document est figé")
	assert.Equal(t, MethodBank, paid["method"])
	assert.Equal(t, "AVIS-2026-03", paid["reference"])
	assert.NotEmpty(t, paid["paid_at"])
	assert.Equal(t, operator, paid["paid_by"], "on sait sur l'ordre de qui l'argent est parti")

	code, again := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/pay", map[string]any{
		"method": MethodBank,
	})
	assert.Equal(t, http.StatusConflict, code, "corps : %v", again)
	assert.Equal(t, "payroll_run_paid", d.errorCode(again))
}

// ⚠️ ET DEUX VERSEMENTS SIMULTANÉS NE PASSENT PAS NON PLUS. Les deux appels
// lisent une paie « vérifiée » : c'est l'écriture CONDITIONNELLE qui tranche,
// pas la lecture. `onMove` ouvre exactement cette fenêtre — la paie change
// d'état entre la lecture et l'écriture —, et le second geste doit être refusé.
//
// Sans le filtre de statut dans `MoveRun`, cette épreuve passerait au vert avec
// deux versements écrits, et rien ne le dirait.
func TestTwoSimultaneousPaymentsCannotBothGoThrough(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)
	id, _ := d.prepareMarch(t)
	code, _ := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/approve", nil)
	require.Equal(t, http.StatusOK, code)

	// Quelqu'un d'autre verse pendant que nous décidons de verser.
	d.store.onMove = func(run *Run) {
		if run.Status == RunApproved {
			run.Status = RunPaid
			at := d.now
			run.PaidAt, run.PaidBy, run.Method = &at, "autre-operateur", MethodCash
		}
	}

	code, body := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/pay", map[string]any{
		"method": MethodBank,
	})
	assert.Equal(t, http.StatusConflict, code, "corps : %v", body)
	assert.Equal(t, "payroll_run_moved", d.errorCode(body),
		"le refus doit dire que la paie a bougé, et inviter à la recharger")

	// Et le versement de l'autre est intact : un seul `paid_by`, un seul moyen.
	d.store.onMove = nil
	code, served := d.call(t, http.MethodGet, "/admin/payroll/runs/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "autre-operateur", served["paid_by"])
	assert.Equal(t, MethodCash, served["method"])
}

// ⚠️ UNE PAIE VERSÉE NE BOUGE PLUS — ni par « vérifier », ni par « annuler ».
// Et le refus NOMME LA SORTIE : écrire une correction. Sans cette phrase,
// l'opérateur cherche un bouton « Modifier » qui n'existera jamais.
func TestAPaidPayrollRefusesEveryFurtherGesture(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)
	id, _ := d.prepareMarch(t)
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/approve", nil)
	code, _ := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/pay", map[string]any{"method": MethodCash})
	require.Equal(t, http.StatusOK, code)

	for _, path := range []string{"/approve", "/cancel"} {
		code, body := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+path, nil)
		assert.Equal(t, http.StatusConflict, code, "%s : %v", path, body)
		assert.Equal(t, "payroll_run_paid", d.errorCode(body), path)
	}
}

// ⚠️ CORRIGER UNE PAIE VERSÉE ÉCRIT UNE PIÈCE NOUVELLE, et ne touche pas la
// paie. Rouvrir un versement aurait fait disparaître ce qui a réellement été
// payé : le document de mars dirait ce qu'on en pense en mai, et plus personne
// ne pourrait répondre à « qu'avez-vous versé le 28 ? ».
func TestCorrectingAPaidPayrollWritesANewPieceAndLeavesItUntouched(t *testing.T) {
	d := bench(t)
	employeeID := d.newEmployee(t, "Aline", 150_000)
	id, _ := d.prepareMarch(t)
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/approve", nil)
	_, paid := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/pay", map[string]any{"method": MethodCash})
	frozen, err := json.Marshal(paid)
	require.NoError(t, err)

	code, correction := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/corrections", map[string]any{
		"employee_id": employeeID, "kind": KindBonus, "amount": 20_000,
		"reason": "heures de nuit oubliées en mars",
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", correction)
	assert.Equal(t, id, correction["corrects_run"], "la correction NOMME la paie qu'elle rattrape")
	assert.Equal(t, KindBonus, correction["kind"])
	assert.Equal(t, operator, correction["granted_by"], "on sait qui a corrigé")

	// La paie versée est servie à l'identique, au caractère près.
	code, after := d.call(t, http.MethodGet, "/admin/payroll/runs/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	stillFrozen, err := json.Marshal(after)
	require.NoError(t, err)
	assert.JSONEq(t, string(frozen), string(stillFrozen),
		"la paie versée doit être servie exactement comme elle a été versée")

	// Et la correction entre dans la paie SUIVANTE.
	code, next := d.call(t, http.MethodPost, "/admin/payroll/runs", map[string]any{
		"frequency": FrequencyMonthly, "on": "2026-02-10T00:00:00Z",
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", next)
	slip := next["slips"].([]any)[0].(map[string]any)
	assert.Equal(t, 20_000, number(t, slip, "bonuses"), "le rattrapage passe sur la paie suivante")
}

// ⚠️ CORRIGER AU PROFIT DE QUELQU'UN QUI N'ÉTAIT PAS SUR LA PAIE N'EST PAS UNE
// CORRECTION : c'est une prime, et elle doit s'écrire comme telle, avec son
// motif. Sans ce refus, « correction de la paie de mars » aurait servi à
// accorder n'importe quoi à n'importe qui sous un libellé comptable.
func TestACorrectionOnlyCoversPeopleWhoWereOnThatPayroll(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)
	id, _ := d.prepareMarch(t)
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/approve", nil)
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/pay", map[string]any{"method": MethodCash})

	// Embauché APRÈS : il n'a jamais figuré sur la paie de mars.
	code, newcomer := d.call(t, http.MethodPost, "/admin/payroll/employees", map[string]any{
		"full_name": "Nouvelle recrue", "position": "Comptoir", "base_salary": 80_000,
		"frequency": FrequencyMonthly, "pay_day": 28, "hired_at": "2026-04-01T00:00:00Z",
	})
	require.Equal(t, http.StatusCreated, code)

	code, body := d.call(t, http.MethodPost, "/admin/payroll/runs/"+id+"/corrections", map[string]any{
		"employee_id": newcomer["id"], "kind": KindBonus, "amount": 50_000,
		"reason": "régularisation",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, code, "corps : %v", body)
}

// ⚠️ UNE PRIME N'EST PAS VERSÉE DEUX FOIS. La réservation la retire de la file
// dès la préparation, le versement la solde : la paie suivante ne la retrouve
// pas.
func TestABonusIsPaidOnceAndOnlyOnce(t *testing.T) {
	d := bench(t)
	id := d.newEmployee(t, "Aline", 150_000)
	code, body := d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/bonuses", map[string]any{
		"amount": 30_000, "reason": "prime de fin de chantier",
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", body)

	runID, march := d.prepareMarch(t)
	assert.Equal(t, 30_000, number(t, march, "bonuses"))
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+runID+"/approve", nil)
	code, _ = d.call(t, http.MethodPost, "/admin/payroll/runs/"+runID+"/pay", map[string]any{"method": MethodCash})
	require.Equal(t, http.StatusOK, code)

	// La prime est soldée : la liste des ajustements le dit.
	code, list := d.call(t, http.MethodGet, "/admin/payroll/employees/"+id+"/adjustments", nil)
	require.Equal(t, http.StatusOK, code)
	first := list["items"].([]any)[0].(map[string]any)
	assert.Equal(t, AdjustmentSettled, first["status"])
	assert.Equal(t, 30_000, number(t, first, "taken"))
	assert.Equal(t, 0, number(t, first, "remaining"))

	// Et la période suivante ne la reprend pas.
	code, next := d.call(t, http.MethodPost, "/admin/payroll/runs", map[string]any{
		"frequency": FrequencyMonthly, "on": "2026-02-10T00:00:00Z",
	})
	require.Equal(t, http.StatusCreated, code, "corps : %v", next)
	assert.Equal(t, 0, number(t, next, "bonuses"), "une prime versée ne revient pas")
}

// ⚠️ ANNULER UNE PAIE REND SES PRIMES À LA FILE. Une prime restée réservée par
// un brouillon abandonné n'entrerait plus dans aucune paie : elle
// disparaîtrait sans trace d'annulation — la pire façon de perdre de l'argent
// dû à quelqu'un.
func TestCancellingAPayrollGivesItsBonusesBack(t *testing.T) {
	d := bench(t)
	id := d.newEmployee(t, "Aline", 150_000)
	d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/bonuses", map[string]any{
		"amount": 30_000, "reason": "prime de fin de chantier",
	})
	runID, first := d.prepareMarch(t)
	require.Equal(t, 30_000, number(t, first, "bonuses"))

	code, cancelled := d.call(t, http.MethodPost, "/admin/payroll/runs/"+runID+"/cancel", map[string]any{
		"reason": "erreur de période",
	})
	require.Equal(t, http.StatusOK, code, "corps : %v", cancelled)
	assert.Equal(t, RunCancelled, cancelled["status"])

	// ⚠️ Et la période est de nouveau préparable : le verrou ne couvre pas les
	// paies annulées, sans quoi une erreur de manipulation aurait fermé un mois
	// pour toujours.
	_, again := d.prepareMarch(t)
	assert.Equal(t, 30_000, number(t, again, "bonuses"), "la prime est revenue dans la file")
}

// Un salarié suspendu n'entre pas dans une paie, et un salarié embauché après
// la période non plus. ⚠️ IL N'Y A PAS DE PRORATA : découper une période au
// nombre de jours travaillés demande une règle de droit du travail, et
// l'inventer ici aurait produit des bulletins faux avec l'air d'être justes.
func TestOnlyPeopleTheCompanyOwedThatPeriodAreOnThePayroll(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)

	suspended := d.newEmployee(t, "Boris", 100_000)
	code, _ := d.call(t, http.MethodPatch, "/admin/payroll/employees/"+suspended, map[string]any{
		"status": StatusSuspended,
	})
	require.Equal(t, http.StatusOK, code)

	code, _ = d.call(t, http.MethodPost, "/admin/payroll/employees", map[string]any{
		"full_name": "Chantal", "position": "Comptoir", "base_salary": 80_000,
		"frequency": FrequencyMonthly, "pay_day": 28, "hired_at": "2026-04-01T00:00:00Z",
	})
	require.Equal(t, http.StatusCreated, code)

	code, _ = d.call(t, http.MethodPost, "/admin/payroll/employees", map[string]any{
		"full_name": "Didier", "position": "Comptoir", "base_salary": 80_000,
		"frequency": FrequencyWeekly, "pay_weekday": 5,
	})
	require.Equal(t, http.StatusCreated, code)

	_, run := d.prepareMarch(t)
	assert.Equal(t, 1, number(t, run, "headcount"),
		"seule Aline était due en mars sur la fréquence mensuelle")
	assert.Equal(t, 150_000, number(t, run, "net"))
}

// Sans personne à payer, il n'y a pas de paie vide : on le DIT.
func TestAPayrollWithNobodyOnItIsRefusedRatherThanWrittenEmpty(t *testing.T) {
	d := bench(t)
	code, body := d.call(t, http.MethodPost, "/admin/payroll/runs", map[string]any{
		"frequency": FrequencyMonthly,
	})
	assert.Equal(t, http.StatusConflict, code, "corps : %v", body)
	assert.Equal(t, "payroll_nobody_to_pay", d.errorCode(body))
}

// --- LES RÉGLAGES ----------------------------------------------------------

// ⚠️ UN RÉGLAGE PARTIEL NE REMPLACE PAS TOUT. Une écriture « qui remplace »
// aurait remis le plafond de retenue au défaut en changeant seulement le jour
// de paie — et personne ne l'aurait vu avant la paie suivante.
func TestWritingOneSettingLeavesTheOthersAlone(t *testing.T) {
	d := bench(t)
	code, body := d.call(t, http.MethodPut, "/admin/payroll/settings", map[string]any{
		"max_deduction_percent": 10, "min_net": 50_000,
	})
	require.Equal(t, http.StatusOK, code, "corps : %v", body)
	assert.Equal(t, 10, number(t, body, "max_deduction_percent"))

	code, body = d.call(t, http.MethodPut, "/admin/payroll/settings", map[string]any{
		"default_pay_day": 5,
	})
	require.Equal(t, http.StatusOK, code, "corps : %v", body)
	assert.Equal(t, 5, number(t, body, "default_pay_day"))
	assert.Equal(t, 10, number(t, body, "max_deduction_percent"), "le plafond n'avait pas à bouger")
	assert.Equal(t, 50_000, number(t, body, "min_net"))
	// Le VOCABULAIRE est servi : la console ne recopie pas les fréquences.
	assert.Len(t, body["frequencies"], 3)
}

// Le plafond du pays MORD sur les retenues qu'on peut poser : c'est le réglage
// qui décide, pas une constante du code.
func TestTheCountrySettingDecidesWhatCanBeDeducted(t *testing.T) {
	d := bench(t)
	id := d.newEmployee(t, "Aline", 100_000)

	// À 30 % (le défaut), 25 000 passent.
	code, _ := d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/deductions", map[string]any{
		"amount": 25_000, "reason": "avance",
	})
	require.Equal(t, http.StatusCreated, code)

	// À 10 %, la même retenue est refusée.
	code, _ = d.call(t, http.MethodPut, "/admin/payroll/settings", map[string]any{"max_deduction_percent": 10})
	require.Equal(t, http.StatusOK, code)
	code, body := d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/deductions", map[string]any{
		"amount": 25_000, "reason": "avance",
	})
	assert.Equal(t, http.StatusConflict, code, "corps : %v", body)
	assert.Contains(t, d.reason(body), "max_per_run=10000")

	// Et la fiche du salarié SERT la nouvelle borne.
	code, employee := d.call(t, http.MethodGet, "/admin/payroll/employees/"+id, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 10_000, number(t, employee, "max_deduction_per_run"))
	assert.Equal(t, 25_000, number(t, employee, "open_deductions"), "ce qui reste à retenir")
}

// --- LA COUTURE COMPTABLE --------------------------------------------------

// ⚠️ LA COMPTABILITÉ EST PRÉVENUE PAR UNE SEULE COUTURE, NOMMÉE, et son
// absence ne bloque rien. Un journal indisponible ne doit pas empêcher de payer
// des gens : l'argent part, l'écriture se rattrape.
func TestThePaidPayrollIsHandedToTheLedgerSeamAndItsFailureNeverBlocksPayment(t *testing.T) {
	d := bench(t)
	d.newEmployee(t, "Aline", 150_000)
	id := d.newEmployee(t, "Boris", 100_000)
	d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/deductions", map[string]any{
		"amount": 20_000, "reason": "casse de matériel",
	})

	seam := &fakeLedger{err: assertAnError}
	d.svc.SetLedger(seam)

	runID, _ := d.prepareMarch(t)
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+runID+"/approve", nil)
	code, paid := d.call(t, http.MethodPost, "/admin/payroll/runs/"+runID+"/pay", map[string]any{
		"method": MethodBank, "reference": "AVIS-7",
	})
	require.Equal(t, http.StatusOK, code, "un journal en panne n'empêche pas de payer : %v", paid)
	assert.Equal(t, RunPaid, paid["status"])

	require.Len(t, seam.got, 1, "la couture doit être appelée une fois, et une seule")
	got := seam.got[0]
	assert.Equal(t, runID, got.RunID)
	assert.Equal(t, "XAF", got.Currency)
	assert.Equal(t, 2, got.Headcount)
	assert.Equal(t, MethodBank, got.Method)
	assert.Equal(t, "AVIS-7", got.Reference)
	assert.Equal(t, operator, got.ActorID)
	// ⚠️ LE DÉTAIL FAIT LE TOTAL, sans quoi la pièce comptable serait fausse.
	assert.Equal(t, got.Gross-got.Deductions, got.Net)
	assert.Equal(t, 250_000, got.Gross)
	assert.Equal(t, 20_000, got.Deductions)
	assert.Equal(t, 230_000, got.Net)
}

type fakeLedger struct {
	got []PaidPayroll
	err error
}

func (f *fakeLedger) RecordPayrollPaid(_ context.Context, p PaidPayroll) error {
	f.got = append(f.got, p)
	return f.err
}

// assertAnError est une panne de journal, pas un refus métier.
var assertAnError = errLedgerDown{}

type errLedgerDown struct{}

func (errLedgerDown) Error() string { return "journal comptable indisponible" }

// --- LE JOURNAL D'AUDIT ----------------------------------------------------

// ⚠️ VERSER UNE PAIE EST UN GESTE D'EXPLOITATION, ET IL PART AU JOURNAL avec
// son auteur. « Qui a décidé de verser ça, et quand ? » est la première
// question posée quand un montant surprend.
func TestEveryMoneyGestureReachesTheAuditTrail(t *testing.T) {
	d := bench(t)
	book := &fakeAuditor{}
	d.svc.SetAuditor(book)

	id := d.newEmployee(t, "Aline", 150_000)
	d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/bonuses", map[string]any{
		"amount": 10_000, "reason": "prime",
	})
	d.call(t, http.MethodPost, "/admin/payroll/employees/"+id+"/deductions", map[string]any{
		"amount": 10_000, "reason": "casse",
	})
	runID, _ := d.prepareMarch(t)
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+runID+"/approve", nil)
	d.call(t, http.MethodPost, "/admin/payroll/runs/"+runID+"/pay", map[string]any{"method": MethodCash})

	assert.Equal(t, []string{
		"payroll.employee_create",
		"payroll.bonus_grant",
		"payroll.deduction_place",
		"payroll.run_prepare",
		"payroll.run_approve",
		"payroll.run_pay",
	}, book.actions)

	// Le versement porte le montant NET et le nombre de personnes : un journal
	// qui ne dirait que « payroll.run_pay » n'aiderait personne.
	last := book.after[len(book.after)-1]
	assert.Equal(t, RunPaid, last["status"])
	assert.Equal(t, 150_000, last["net"])
	assert.Equal(t, 1, last["headcount"])
}

type fakeAuditor struct {
	actions []string
	after   []map[string]any
}

func (f *fakeAuditor) Record(_ context.Context, action, _, _ string, _, after any) {
	f.actions = append(f.actions, action)
	row, _ := after.(map[string]any)
	f.after = append(f.after, row)
}
