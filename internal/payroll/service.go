package payroll

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Clé de notification — gabarit dans `internal/notify`.
//
// ⚠️ ELLE NE PART QU'AUX SALARIÉS QUI ONT UN COMPTE, et la plupart n'en ont
// pas. Ce n'est donc pas le canal par lequel on annonce une paie : c'est un
// confort pour ceux qui sont déjà sur la plateforme. Compter là-dessus pour
// « prévenir le personnel » aurait laissé la majorité sans nouvelle.
const KeyPayrollPaid = "payroll_paid"

var (
	errNoBonusAmount = apperr.Validation(
		"a bonus or deduction needs an amount or a percentage")
	errBothAmountAndPercent = apperr.Validation(
		"a bonus or deduction carries an amount OR a percentage, never both")
	errRecurringInstallments = apperr.Validation(
		"a recurring adjustment has no instalments: it has no end")
	errRunNotDraft = apperr.Conflict("payroll_run_not_draft",
		"only a prepared payroll can be changed")
	errRunNotApproved = apperr.Conflict("payroll_run_not_approved",
		"a payroll is paid once it has been verified")
	errRunPaid = apperr.Conflict("payroll_run_paid",
		"a paid payroll is never modified: write a correction, which lands on the next payroll")
	errRunNotPaid = apperr.Conflict("payroll_run_not_paid",
		"only a paid payroll is corrected")
	errNobodyToPay = apperr.Conflict("payroll_nobody_to_pay",
		"no active employee on this pay frequency for this period")
	errNotInRun = apperr.Validation("this employee is not on that payroll")
	// errDeductionTooLarge est LA borne des retenues. Son `reason` porte les
	// montants exacts — voir `checkAdjustment`.
	errDeductionTooLarge = apperr.Conflict("payroll_deduction_too_large",
		"this deduction exceeds what a single payroll may deduct: spread it over several instalments")
	errOtherCountry = apperr.Validation(
		"this account belongs to another country: a payroll record is read in the country of the request")
)

// Store est ce que le service attend de la persistance — déclaré ICI, côté
// consommateur, pour que les parcours d'argent s'éprouvent sans Mongo.
type Store interface {
	InsertEmployee(ctx context.Context, e *Employee) error
	EmployeeByID(ctx context.Context, id primitive.ObjectID) (*Employee, error)
	SaveEmployee(ctx context.Context, e *Employee) error
	ListEmployees(ctx context.Context, f EmployeeFilter, limit int, cursor string) ([]Employee, string, error)
	PayableEmployees(ctx context.Context, frequency string) ([]Employee, error)

	InsertAdjustment(ctx context.Context, a *Adjustment) error
	AdjustmentByID(ctx context.Context, id primitive.ObjectID) (*Adjustment, error)
	SaveAdjustment(ctx context.Context, a *Adjustment) error
	AdjustmentsOfEmployee(ctx context.Context, employeeID primitive.ObjectID, status string) ([]Adjustment, error)
	ClaimAdjustments(ctx context.Context, runID primitive.ObjectID, employees []primitive.ObjectID, periodEnd time.Time) ([]Adjustment, error)
	AdjustmentsOfRun(ctx context.Context, runID primitive.ObjectID) ([]Adjustment, error)
	ReleaseAdjustments(ctx context.Context, runID primitive.ObjectID) error
	ConsumeAdjustments(ctx context.Context, applied []Applied, at time.Time) error

	InsertRun(ctx context.Context, run *Run) error
	RunByID(ctx context.Context, id primitive.ObjectID) (*Run, error)
	CountRunsForPeriod(ctx context.Context, frequency string, periodStart time.Time) (int64, error)
	SaveRunSlips(ctx context.Context, run *Run) error
	MoveRun(ctx context.Context, id primitive.ObjectID, from, to string, set bson.M) (*Run, error)
	ListRuns(ctx context.Context, f RunFilter, limit int, cursor string) ([]Run, string, error)

	Settings(ctx context.Context, code string) (*Settings, error)
	SaveSettings(ctx context.Context, s *Settings) error
}

// Accounts est ce que ce module demande à l'annuaire des comptes : de quoi
// VÉRIFIER un rattachement, et de quoi afficher un nom. Facultatif.
type Accounts interface {
	CountryOf(ctx context.Context, userID string) (string, error)
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
}

// Auditor enregistre les gestes sensibles. Verser une paie en est un.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Notifier atteint un salarié QUI A UN COMPTE.
type Notifier interface {
	Notify(ctx context.Context, userID, key string, vars, data map[string]string)
}

// Ledger est LA COUTURE COMPTABLE de ce module — le seul point par lequel une
// paie versée peut devenir une écriture.
//
// ⚠️ ELLE EST FACULTATIVE, ET DÉLIBÉRÉMENT NON BRANCHÉE. La paie enregistre ce
// qui a été versé ; la comptabilité est un autre sujet, tenu par un autre
// module. Les coudre ici aurait créune dépendance croisée entre deux chantiers
// menés en parallèle, et la jonction se fait après — en implémentant cette
// interface au CÂBLAGE (`cmd/api/main.go`), là où les deux modules se
// connaissent déjà.
//
// ⚠️ ET UNE ERREUR DE JOURNAL N'EMPÊCHE PAS DE PAYER. Le versement est déjà
// écrit quand cette méthode est appelée : l'argent est parti, et refuser la
// paie parce qu'un journal est indisponible aurait inversé l'ordre des
// importances. L'échec est journalisé À VOIX HAUTE pour être rattrapé.
type Ledger interface {
	RecordPayrollPaid(ctx context.Context, p PaidPayroll) error
}

// PaidPayroll est ce que la paie DIT d'elle-même à la comptabilité : des
// totaux, une période, une monnaie, un auteur. Aucun type de ce paquet n'en
// sort — c'est ce qui permet au module comptable de ne pas dépendre de celui-ci.
type PaidPayroll struct {
	RunID       string
	Country     string
	Currency    string
	Frequency   string
	PeriodStart time.Time
	PeriodEnd   time.Time
	PaidAt      time.Time
	// Gross = base + primes ; Deductions = ce qui a été retenu ; Net = ce qui
	// est sorti. ⚠️ Gross - Deductions = Net, toujours : une pièce comptable
	// dont le détail ne fait pas le total est refusée (voir `pkg/receipt`).
	Gross      int
	Bonuses    int
	Deductions int
	Net        int
	Headcount  int
	Method     string
	Reference  string
	ActorID    string
}

type Service struct {
	repo     Store
	accounts Accounts
	audit    Auditor
	notifier Notifier
	ledger   Ledger
	log      *slog.Logger
	now      func() time.Time
}

func NewService(repo Store, accounts Accounts) *Service {
	return &Service{
		repo: repo, accounts: accounts,
		log: slog.Default(),
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetAuditor(a Auditor)   { s.audit = a }
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// SetLedger branche la couture comptable — voir `Ledger`.
func (s *Service) SetLedger(l Ledger) { s.ledger = l }

func (s *Service) record(ctx context.Context, action, resourceType, id string, before, after any) {
	if s.audit != nil {
		s.audit.Record(ctx, action, resourceType, id, before, after)
	}
}

func actor(ctx context.Context) string {
	id, _ := auth.UserFromContext(ctx)
	return id
}

// currencyOf rend la monnaie du pays de la requête.
//
// ⚠️ ELLE N'EST JAMAIS DEVINÉE. Un pays hors catalogue rend une monnaie VIDE,
// que la console affiche comme telle ; écrire « XOF » par défaut aurait fait
// lire des francs CFA sur une paie guinéenne, où le même nombre vaut quatorze
// fois moins.
func currencyOf(ctx context.Context) string {
	if info, ok := country.Lookup(country.FromContext(ctx)); ok {
		return info.Currency
	}
	return ""
}

// --- réglages ---

func (s *Service) GetSettings(ctx context.Context) (SettingsResponse, error) {
	set, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return SettingsResponse{}, err
	}
	return toSettings(set, currencyOf(ctx)), nil
}

// UpdateSettings applique ce qui est FOURNI et laisse le reste.
//
// ⚠️ UN PUT PARTIEL NE REMPLACE PAS TOUT, et c'est pour cela que les champs
// sont des pointeurs. Un écriture « qui remplace » a déjà effacé trois prix de
// l'immobilier le mois dernier : ici, elle aurait remis le plafond de retenue
// au défaut en changeant seulement le jour de paie.
func (s *Service) UpdateSettings(ctx context.Context, in SettingsInput) (SettingsResponse, error) {
	code := country.FromContext(ctx)
	set, err := s.repo.Settings(ctx, code)
	if err != nil {
		return SettingsResponse{}, err
	}
	before := *set
	if in.DefaultFrequency != nil {
		set.DefaultFrequency = *in.DefaultFrequency
	}
	if in.DefaultPayDay != nil {
		set.DefaultPayDay = *in.DefaultPayDay
	}
	if in.DefaultPayWeekday != nil {
		set.DefaultPayWeekday = *in.DefaultPayWeekday
	}
	if in.MaxDeductionPercent != nil {
		set.MaxDeductionPercent = *in.MaxDeductionPercent
	}
	if in.MinNet != nil {
		set.MinNet = *in.MinNet
	}
	set.Country = code
	if err := s.repo.SaveSettings(ctx, set); err != nil {
		return SettingsResponse{}, err
	}
	s.record(ctx, "payroll.settings", "payroll_settings", code, before, *set)
	return toSettings(set, currencyOf(ctx)), nil
}

// --- salariés ---

// CreateEmployee déclare un salarié.
func (s *Service) CreateEmployee(ctx context.Context, in EmployeeInput) (EmployeeResponse, error) {
	set, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return EmployeeResponse{}, err
	}
	freq := in.Frequency
	if freq == "" {
		freq = set.DefaultFrequency
	}
	if err := checkFrequency(freq, in.PayDay, in.PayWeekday); err != nil {
		return EmployeeResponse{}, err
	}
	e := &Employee{
		Country: country.FromContext(ctx), FullName: in.FullName, Phone: in.Phone,
		Email: in.Email, Position: in.Position, Status: StatusActive,
		BaseSalary: in.BaseSalary, Frequency: freq, PayDay: in.PayDay,
		PayWeekday: in.PayWeekday, Notes: in.Notes, CreatedBy: actor(ctx),
	}
	if in.HiredAt != nil {
		e.HiredAt = startOfDay(*in.HiredAt)
	} else {
		e.HiredAt = startOfDay(s.now())
	}
	if in.LeftAt != nil {
		left := startOfDay(*in.LeftAt)
		e.LeftAt = &left
	}
	if in.UserID != "" {
		uid, err := s.linkAccount(ctx, in.UserID)
		if err != nil {
			return EmployeeResponse{}, err
		}
		e.UserID = uid
	}
	if err := s.repo.InsertEmployee(ctx, e); err != nil {
		return EmployeeResponse{}, err
	}
	s.record(ctx, "payroll.employee_create", "payroll_employee", e.ID.Hex(), nil, employeeAudit(e))
	return s.employeeResponse(ctx, e, set)
}

// linkAccount vérifie un rattachement et rend l'identifiant du compte.
//
// ⚠️ IL VÉRIFIE LE PAYS, et refuse un compte d'ailleurs. Une fiche de paie
// gabonaise rattachée à un compte togolais ne serait lisible en entier par
// aucune des deux consoles : chacune verrait la moitié de la personne, et le
// rattachement passerait pour une donnée corrompue.
//
// ⚠️ IL NE VÉRIFIE AUCUN RÔLE, et c'est voulu. Un chauffeur salarié existe ;
// exiger un compte `admin` aurait forcé à donner la console à quelqu'un pour
// pouvoir le payer.
func (s *Service) linkAccount(ctx context.Context, userID string) (*primitive.ObjectID, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("user_id must be a 24-character hexadecimal identifier")
	}
	if s.accounts == nil {
		return &uid, nil
	}
	code, err := s.accounts.CountryOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if code == "" {
		return nil, apperr.NotFound("user_not_found", "this account does not exist")
	}
	if want := country.FromContext(ctx); want != "" && code != want {
		return nil, errOtherCountry
	}
	return &uid, nil
}

func (s *Service) GetEmployee(ctx context.Context, id string) (EmployeeResponse, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return EmployeeResponse{}, errEmployeeNotFound
	}
	e, err := s.repo.EmployeeByID(ctx, oid)
	if err != nil {
		return EmployeeResponse{}, err
	}
	set, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return EmployeeResponse{}, err
	}
	return s.employeeResponse(ctx, e, set)
}

// UpdateEmployee modifie une fiche. Les champs absents ne bougent pas.
func (s *Service) UpdateEmployee(ctx context.Context, id string, in EmployeeUpdate) (EmployeeResponse, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return EmployeeResponse{}, errEmployeeNotFound
	}
	e, err := s.repo.EmployeeByID(ctx, oid)
	if err != nil {
		return EmployeeResponse{}, err
	}
	before := employeeAudit(e)
	if in.FullName != nil {
		e.FullName = *in.FullName
	}
	if in.Phone != nil {
		e.Phone = *in.Phone
	}
	if in.Email != nil {
		e.Email = *in.Email
	}
	if in.Position != nil {
		e.Position = *in.Position
	}
	if in.HiredAt != nil {
		e.HiredAt = startOfDay(*in.HiredAt)
	}
	if in.LeftAt != nil {
		left := startOfDay(*in.LeftAt)
		e.LeftAt = &left
	}
	if in.BaseSalary != nil {
		e.BaseSalary = *in.BaseSalary
	}
	if in.Notes != nil {
		e.Notes = *in.Notes
	}
	if in.Status != nil {
		e.Status = *in.Status
	}
	if in.UserID != nil {
		switch *in.UserID {
		case "":
			// ⚠️ DÉTACHER EST UN GESTE EXPLICITE : la chaîne vide. Sans ce cas,
			// un rattachement posé par erreur ne pourrait plus être défait
			// qu'en supprimant la fiche — et avec elle l'historique des paies.
			e.UserID = nil
		default:
			uid, err := s.linkAccount(ctx, *in.UserID)
			if err != nil {
				return EmployeeResponse{}, err
			}
			e.UserID = uid
		}
	}
	// ⚠️ LA FRÉQUENCE ET LE JOUR SE VALIDENT ENSEMBLE, sur l'état d'ARRIVÉE.
	// Changer la fréquence pour « hebdomadaire » sans toucher au jour du mois
	// laissait une fiche qui porte les deux — et le jour du mois, affiché mais
	// inopérant, se lisait comme la règle appliquée.
	if in.Frequency != nil && *in.Frequency != e.Frequency {
		e.Frequency = *in.Frequency
		// Le passage d'une famille de fréquence à l'autre efface le jour qui
		// n'a plus de sens, au lieu de le laisser traîner.
		if e.Frequency == FrequencyMonthly {
			e.PayWeekday = 0
		} else {
			e.PayDay = 0
		}
	}
	if in.PayDay != nil {
		e.PayDay = *in.PayDay
	}
	if in.PayWeekday != nil {
		e.PayWeekday = *in.PayWeekday
	}
	if err := checkFrequency(e.Frequency, e.PayDay, e.PayWeekday); err != nil {
		return EmployeeResponse{}, err
	}
	if err := s.repo.SaveEmployee(ctx, e); err != nil {
		return EmployeeResponse{}, err
	}
	s.record(ctx, "payroll.employee_update", "payroll_employee", e.ID.Hex(), before, employeeAudit(e))
	set, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return EmployeeResponse{}, err
	}
	return s.employeeResponse(ctx, e, set)
}

func (s *Service) ListEmployees(ctx context.Context, f EmployeeFilter, limit int, cursor string) ([]EmployeeResponse, string, error) {
	items, next, err := s.repo.ListEmployees(ctx, f, limit, cursor)
	if err != nil {
		return nil, "", err
	}
	set, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return nil, "", err
	}
	names := s.accountNames(ctx, items)
	out := make([]EmployeeResponse, 0, len(items))
	for i := range items {
		out = append(out, s.employeeRow(&items[i], set, names, 0))
	}
	return out, next, nil
}

// accountNames rend le nom porté par les COMPTES rattachés — en UN appel.
// Sans annuaire, la liste s'affiche sans ces noms, ce qui reste actionnable.
func (s *Service) accountNames(ctx context.Context, items []Employee) map[string]string {
	if s.accounts == nil {
		return nil
	}
	ids := make([]string, 0, len(items))
	for i := range items {
		if items[i].UserID != nil {
			ids = append(ids, items[i].UserID.Hex())
		}
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := s.accounts.UserNames(ctx, ids)
	if err != nil {
		s.log.Warn("payroll: noms des comptes rattachés indisponibles", "error", err)
		return nil
	}
	return names
}

func (s *Service) employeeRow(e *Employee, set *Settings, names map[string]string, openDeductions int) EmployeeResponse {
	out := EmployeeResponse{
		ID: e.ID.Hex(), Country: e.Country, FullName: e.FullName, Phone: e.Phone,
		Email: e.Email, Position: e.Position, HiredAt: e.HiredAt, LeftAt: e.LeftAt,
		Status: e.Status, BaseSalary: e.BaseSalary, Frequency: e.Frequency,
		PayDay: e.PayDay, PayWeekday: e.PayWeekday, Notes: e.Notes,
		OpenDeductions: openDeductions,
		CreatedAt:      e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
	if info, ok := country.Lookup(e.Country); ok {
		out.Currency = info.Currency
	}
	if e.UserID != nil {
		out.UserID = e.UserID.Hex()
		out.AccountName = names[out.UserID]
	}
	_, end, err := PeriodFor(e.Frequency, s.nowOr())
	if err == nil {
		out.NextPayDate = PayDateFor(e, end, set)
	}
	out.MaxDeductionPerRun = maxDeductibleShare(e.BaseSalary, set)
	return out
}

// nowOr est l'horloge du service — remplaçable dans les épreuves.
func (s *Service) nowOr() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now()
}

// employeeResponse enrichit une fiche de ce que seule une lecture
// supplémentaire sait : ce qui reste à retenir.
func (s *Service) employeeResponse(ctx context.Context, e *Employee, set *Settings) (EmployeeResponse, error) {
	open, err := s.repo.AdjustmentsOfEmployee(ctx, e.ID, AdjustmentOpen)
	if err != nil {
		return EmployeeResponse{}, err
	}
	remaining := 0
	for i := range open {
		if open[i].Kind == KindDeduction {
			remaining += open[i].Remaining()
		}
	}
	names := s.accountNames(ctx, []Employee{*e})
	return s.employeeRow(e, set, names, remaining), nil
}

func employeeAudit(e *Employee) map[string]any {
	out := map[string]any{
		"full_name": e.FullName, "position": e.Position, "status": e.Status,
		"base_salary": e.BaseSalary, "frequency": e.Frequency,
	}
	if e.UserID != nil {
		out["user_id"] = e.UserID.Hex()
	}
	return out
}

// --- primes et retenues ---

// Grant pose une prime ou une retenue sur un salarié.
//
// ⚠️ LA BORNE DE LA RETENUE EST VÉRIFIÉE ICI, À L'ÉCRITURE, et pas seulement
// au calcul de la paie. Les deux endroits sont nécessaires et ne protègent pas
// de la même chose : le calcul empêche une paie vide, l'écriture empêche
// d'ENREGISTRER une retenue que la paie ne pourra jamais honorer — c'est-à-dire
// une dette qui traînerait de mois en mois en se reportant toute seule, pendant
// que la console affiche « retenue en cours » sans que rien ne bouge.
//
// Le refus DIT COMBIEN D'ÉCHÉANCES il faudrait, parce que c'est la seule
// information qui permette de corriger le geste sans calculer de tête.
func (s *Service) Grant(ctx context.Context, employeeID, kind string, in AdjustmentInput) (AdjustmentResponse, error) {
	oid, err := primitive.ObjectIDFromHex(employeeID)
	if err != nil {
		return AdjustmentResponse{}, errEmployeeNotFound
	}
	e, err := s.repo.EmployeeByID(ctx, oid)
	if err != nil {
		return AdjustmentResponse{}, err
	}
	set, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return AdjustmentResponse{}, err
	}
	a := &Adjustment{
		Country: country.FromContext(ctx), EmployeeID: e.ID, Kind: kind,
		Amount: in.Amount, Percent: in.Percent, Reason: in.Reason,
		Recurring: in.Recurring, Installments: in.Installments,
		Status: AdjustmentOpen, GrantedBy: actor(ctx),
	}
	if in.EffectiveFrom != nil {
		a.EffectiveFrom = startOfDay(*in.EffectiveFrom)
	}
	if err := checkAdjustment(a, e, set); err != nil {
		return AdjustmentResponse{}, err
	}
	if err := s.repo.InsertAdjustment(ctx, a); err != nil {
		return AdjustmentResponse{}, err
	}
	action := "payroll.bonus_grant"
	if kind == KindDeduction {
		action = "payroll.deduction_place"
	}
	s.record(ctx, action, "payroll_adjustment", a.ID.Hex(), nil, map[string]any{
		"employee_id": e.ID.Hex(), "kind": kind, "amount": a.Amount,
		"percent": a.Percent, "reason": a.Reason, "installments": a.Installments,
		"recurring": a.Recurring,
	})
	return toAdjustment(a), nil
}

// checkAdjustment porte les bornes d'une prime et d'une retenue.
func checkAdjustment(a *Adjustment, e *Employee, set *Settings) error {
	switch {
	case a.Amount <= 0 && a.Percent <= 0:
		return errNoBonusAmount
	case a.Amount > 0 && a.Percent > 0:
		return errBothAmountAndPercent
	}
	if a.Recurring && a.Installments > 0 {
		return errRecurringInstallments
	}
	if a.Installments <= 0 {
		a.Installments = 1
	}
	if a.Kind != KindDeduction {
		return nil
	}
	// LA BORNE DE LA RETENUE. On compare l'échéance — ce qui serait pris sur
	// UNE paie — à ce qu'une paie peut au plus retenir.
	//
	// ⚠️ LA COMPARAISON PORTE SUR LE SALAIRE DE BASE SEUL, sans les primes à
	// venir. Compter sur une prime future pour qu'une retenue passe aurait fait
	// dépendre l'acceptation d'un geste d'un autre geste que personne n'a encore
	// posé — et la retenue serait devenue impossible à honorer le jour où la
	// prime n'est pas accordée.
	room := maxDeductibleShare(e.BaseSalary, set)
	if room <= 0 {
		return apperr.Conflict("payroll_deduction_impossible",
			"the country settings leave nothing deductible: raise max_deduction_percent or lower min_net")
	}
	per := a.Amount / max(a.Installments, 1)
	if a.Percent > 0 {
		per = e.BaseSalary * a.Percent / 100
	}
	if per <= room {
		return nil
	}
	// ⚠️ LE REFUS PORTE LES CHIFFRES DANS `reason`, ET NON DANS SON MESSAGE.
	// Le message est TRADUIT : la phrase française remplace l'anglaise, et les
	// montants calculés ici auraient disparu avec elle. Seuls `reason` et
	// `fields` traversent l'enveloppe (`httpx.Error`) — c'est le même piège que
	// le refus de connexion, qui portait son motif dans `meta`, où personne ne
	// le lisait.
	if a.Percent > 0 {
		return errDeductionTooLarge.WithMeta(map[string]any{
			"reason": "max_per_run=" + strconv.Itoa(room) + ";requested=" + strconv.Itoa(per),
		})
	}
	// Le nombre d'échéances qu'il FAUDRAIT — l'arrondi vers le HAUT, parce
	// qu'une échéance de plus vaut mieux qu'une dernière qui dépasse.
	need := (a.Amount + room - 1) / room
	return errDeductionTooLarge.WithMeta(map[string]any{
		"reason": "max_per_run=" + strconv.Itoa(room) +
			";requested=" + strconv.Itoa(per) +
			";min_installments=" + strconv.Itoa(need),
	})
}

func (s *Service) ListAdjustments(ctx context.Context, employeeID, status string) ([]AdjustmentResponse, error) {
	oid, err := primitive.ObjectIDFromHex(employeeID)
	if err != nil {
		return nil, errEmployeeNotFound
	}
	if _, err := s.repo.EmployeeByID(ctx, oid); err != nil {
		return nil, err
	}
	items, err := s.repo.AdjustmentsOfEmployee(ctx, oid, status)
	if err != nil {
		return nil, err
	}
	out := make([]AdjustmentResponse, 0, len(items))
	for i := range items {
		out = append(out, toAdjustment(&items[i]))
	}
	return out, nil
}

// CancelAdjustment annule une prime ou une retenue qui n'a pas fini de passer.
//
// ⚠️ CE QUI A DÉJÀ ÉTÉ VERSÉ OU RETENU RESTE : `Taken` n'est pas remis à zéro.
// Annuler une retenue de 60 000 F dont 15 000 ont déjà été pris ne rend pas ces
// 15 000 — il faut une correction pour cela, et c'est une décision qu'on prend
// les yeux ouverts.
func (s *Service) CancelAdjustment(ctx context.Context, id string) (AdjustmentResponse, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return AdjustmentResponse{}, errAdjustmentNotFound
	}
	a, err := s.repo.AdjustmentByID(ctx, oid)
	if err != nil {
		return AdjustmentResponse{}, err
	}
	if a.Status != AdjustmentOpen {
		return AdjustmentResponse{}, apperr.Conflict("payroll_adjustment_closed",
			"this bonus or deduction is already settled or cancelled")
	}
	// ⚠️ RÉSERVÉ PAR UNE PAIE EN PRÉPARATION : on refuse. Annuler sous une paie
	// en cours aurait laissé une fiche déjà calculée porter une ligne qui
	// n'existe plus — et c'est le montant calculé qui serait versé.
	if a.RunID != nil {
		return AdjustmentResponse{}, apperr.Conflict("payroll_adjustment_on_run",
			"this bonus or deduction is on a payroll being prepared: cancel that payroll first")
	}
	before := toAdjustment(a)
	now := s.nowOr()
	a.Status, a.CancelledAt = AdjustmentCancelled, &now
	if err := s.repo.SaveAdjustment(ctx, a); err != nil {
		return AdjustmentResponse{}, err
	}
	s.record(ctx, "payroll.adjustment_cancel", "payroll_adjustment", a.ID.Hex(), before, toAdjustment(a))
	return toAdjustment(a), nil
}

// --- le cycle d'une paie ---

// PrepareRun calcule la paie d'une période : une fiche par salarié, avec ses
// primes et ses retenues.
func (s *Service) PrepareRun(ctx context.Context, in PrepareInput) (RunResponse, error) {
	set, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return RunResponse{}, err
	}
	now := s.nowOr()
	var start, end time.Time
	if in.On != nil {
		start, end, err = PeriodFor(in.Frequency, *in.On)
	} else {
		start, end, err = PreviousPeriod(in.Frequency, now)
	}
	if err != nil {
		return RunResponse{}, err
	}
	// ⚠️ ON NE PRÉPARE PAS UNE PÉRIODE OUVERTE. Une paie préparée le 20 pour le
	// mois en cours ne porte ni les primes ni les retenues des dix jours qui
	// restent, et elle serait versée comme si elle était complète.
	if end.After(now) {
		return RunResponse{}, errPeriodOpen
	}
	n, err := s.repo.CountRunsForPeriod(ctx, in.Frequency, start)
	if err != nil {
		return RunResponse{}, err
	}
	if n > 0 {
		return RunResponse{}, errRunExists
	}

	employees, err := s.repo.PayableEmployees(ctx, in.Frequency)
	if err != nil {
		return RunResponse{}, err
	}
	payable := make([]Employee, 0, len(employees))
	ids := make([]primitive.ObjectID, 0, len(employees))
	for i := range employees {
		if employees[i].PayableOn(start, end) {
			payable = append(payable, employees[i])
			ids = append(ids, employees[i].ID)
		}
	}
	if len(payable) == 0 {
		return RunResponse{}, errNobodyToPay
	}

	run := &Run{
		Country: country.FromContext(ctx), Currency: currencyOf(ctx),
		Frequency: in.Frequency, PeriodStart: start, PeriodEnd: end,
		// ⚠️ LA PAIE PORTE LA DATE DU PAYS, CHAQUE FICHE CELLE DE LA PERSONNE.
		// Prendre la date du premier salarié de la liste aurait fait dépendre la
		// date d'une paie collective de l'ordre alphabétique — et quelqu'un
		// payé le 5 aurait fait dire à la paie entière qu'elle part le 5.
		PayDate: PayDateFor(&Employee{Frequency: in.Frequency}, end, set), Status: RunDraft,
		Notes: in.Notes, PreparedBy: actor(ctx),
	}
	if err := s.repo.InsertRun(ctx, run); err != nil {
		return RunResponse{}, err
	}

	// LA RÉSERVATION — voir `ClaimAdjustments` : c'est elle qui empêche deux
	// préparations de compter la même prime.
	claimed, err := s.repo.ClaimAdjustments(ctx, run.ID, ids, end)
	if err != nil {
		return RunResponse{}, err
	}
	byEmployee := map[primitive.ObjectID][]Adjustment{}
	for _, a := range claimed {
		byEmployee[a.EmployeeID] = append(byEmployee[a.EmployeeID], a)
	}
	for i := range payable {
		slip, _ := buildPayslip(&payable[i], byEmployee[payable[i].ID], set, end)
		slip.PayDate = PayDateFor(&payable[i], end, set)
		run.Slips = append(run.Slips, slip)
	}
	run.totals()
	if err := s.repo.SaveRunSlips(ctx, run); err != nil {
		return RunResponse{}, err
	}
	s.record(ctx, "payroll.run_prepare", "payroll_run", run.ID.Hex(), nil, runAudit(run))
	return toRun(run), nil
}

func (s *Service) GetRun(ctx context.Context, id string) (RunResponse, error) {
	run, err := s.run(ctx, id)
	if err != nil {
		return RunResponse{}, err
	}
	return toRun(run), nil
}

func (s *Service) run(ctx context.Context, id string) (*Run, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errRunNotFound
	}
	return s.repo.RunByID(ctx, oid)
}

func (s *Service) ListRuns(ctx context.Context, f RunFilter, limit int, cursor string) ([]RunResponse, string, error) {
	items, next, err := s.repo.ListRuns(ctx, f, limit, cursor)
	if err != nil {
		return nil, "", err
	}
	out := make([]RunResponse, 0, len(items))
	for i := range items {
		out = append(out, toRun(&items[i]))
	}
	return out, next, nil
}

// ApproveRun VÉRIFIE une paie : elle a été relue, elle est bonne à verser.
//
// ⚠️ UNE ÉTAPE DE PLUS, ET ELLE N'EST PAS DÉCORATIVE. Sans elle, « préparer »
// et « verser » sont le même clic, et une paie se verse avant d'avoir été
// regardée. Avec elle, le versement ne peut partir que d'un état que quelqu'un
// a validé — et c'est ce que `MoveRun` vérifie en une écriture.
func (s *Service) ApproveRun(ctx context.Context, id string) (RunResponse, error) {
	run, err := s.run(ctx, id)
	if err != nil {
		return RunResponse{}, err
	}
	if run.Status == RunPaid {
		return RunResponse{}, errRunPaid
	}
	if run.Status != RunDraft {
		return RunResponse{}, errRunNotDraft
	}
	now := s.nowOr()
	moved, err := s.repo.MoveRun(ctx, run.ID, RunDraft, RunApproved, bson.M{
		"approved_by": actor(ctx), "approved_at": now,
	})
	if err != nil {
		return RunResponse{}, err
	}
	s.record(ctx, "payroll.run_approve", "payroll_run", moved.ID.Hex(), runAudit(run), runAudit(moved))
	return toRun(moved), nil
}

// PayRun VERSE une paie. C'est le geste irréversible du module.
//
// ⚠️ UN SEUL VERSEMENT, GARANTI PAR UNE ÉCRITURE CONDITIONNELLE. Deux clics
// simultanés passent tous les deux la lecture ; seul le premier trouve la paie
// en « approuvée » au moment d'écrire. Le second reçoit un refus explicite, et
// non une seconde confirmation qui ferait croire à un second virement.
//
// ⚠️ ET APRÈS, PLUS RIEN NE CHANGE. Les primes et retenues sont consommées, la
// comptabilité est prévenue si elle est branchée, les salariés qui ont un
// compte reçoivent un message — mais le document est figé. Un échec sur l'une
// de ces suites n'annule pas le versement : il est journalisé.
func (s *Service) PayRun(ctx context.Context, id string, in PayInput) (RunResponse, error) {
	run, err := s.run(ctx, id)
	if err != nil {
		return RunResponse{}, err
	}
	switch run.Status {
	case RunPaid:
		return RunResponse{}, errRunPaid
	case RunApproved:
	default:
		return RunResponse{}, errRunNotApproved
	}
	now := s.nowOr()
	paid, err := s.repo.MoveRun(ctx, run.ID, RunApproved, RunPaid, bson.M{
		"paid_by": actor(ctx), "paid_at": now,
		"method": in.Method, "reference": in.Reference,
	})
	if err != nil {
		return RunResponse{}, err
	}
	s.record(ctx, "payroll.run_pay", "payroll_run", paid.ID.Hex(), runAudit(run), runAudit(paid))

	// LES SUITES. Chacune peut échouer sans remettre en cause le versement.
	//
	// ⚠️ LES AJUSTEMENTS SONT RELUS AVANT D'ÊTRE CONSOMMÉS : c'est leur état en
	// base — ce qui a déjà été pris sur les paies précédentes — qui dit si ce
	// passage les SOLDE. Le deviner depuis la fiche aurait fermé une retenue
	// bornée qui avait encore à prendre, ou rouvert une prime déjà épuisée.
	held, err := s.repo.AdjustmentsOfRun(ctx, paid.ID)
	if err != nil {
		s.log.Error("payroll: primes et retenues réservées illisibles après versement — à rattraper",
			"run", paid.ID.Hex(), "error", err)
	}
	applied := appliedFrom(paid, held)
	if err := s.repo.ConsumeAdjustments(ctx, applied, now); err != nil {
		s.log.Error("payroll: primes et retenues non consommées après versement — à rattraper",
			"run", paid.ID.Hex(), "error", err)
	}
	s.toLedger(ctx, paid)
	s.announce(ctx, paid)
	return toRun(paid), nil
}

// appliedFrom relit ce que la paie VERSÉE a passé, depuis ses propres fiches.
//
// ⚠️ DEPUIS LE DOCUMENT VERSÉ, et non depuis le calcul d'il y a cinq minutes.
// C'est ce qui est écrit qui a été payé : consommer les ajustements d'après un
// calcul refait aurait pu retenir un montant différent de celui que porte la
// fiche — et la fiche est ce que la personne a sous les yeux.
func appliedFrom(run *Run, held []Adjustment) []Applied {
	byID := make(map[primitive.ObjectID]*Adjustment, len(held))
	for i := range held {
		byID[held[i].ID] = &held[i]
	}
	var out []Applied
	for i := range run.Slips {
		for _, l := range run.Slips[i].Lines {
			ap := Applied{AdjustmentID: l.AdjustmentID, Planned: l.Planned, Amount: l.Amount}
			// Sans l'ajustement sous la main, on inscrit ce qui a été pris sans
			// prétendre le solder : un ajustement qu'on n'a pas relu ne doit pas
			// être fermé sur une supposition.
			if a := byID[l.AdjustmentID]; a != nil {
				ap.Settled = settledAfter(a, l.Amount)
			}
			out = append(out, ap)
		}
	}
	return out
}

func (s *Service) toLedger(ctx context.Context, run *Run) {
	if s.ledger == nil {
		return
	}
	at := s.nowOr()
	if run.PaidAt != nil {
		at = *run.PaidAt
	}
	err := s.ledger.RecordPayrollPaid(ctx, PaidPayroll{
		RunID: run.ID.Hex(), Country: run.Country, Currency: run.Currency,
		Frequency: run.Frequency, PeriodStart: run.PeriodStart, PeriodEnd: run.PeriodEnd,
		PaidAt: at, Gross: run.Gross, Bonuses: run.Bonuses,
		Deductions: run.Deductions, Net: run.Net, Headcount: run.Headcount,
		Method: run.Method, Reference: run.Reference, ActorID: run.PaidBy,
	})
	if err != nil {
		s.log.Error("payroll: écriture comptable refusée après versement — à rattraper",
			"run", run.ID.Hex(), "error", err)
	}
}

// announce prévient les salariés QUI ONT UN COMPTE.
func (s *Service) announce(ctx context.Context, run *Run) {
	if s.notifier == nil {
		return
	}
	// Le rattachement vit sur la FICHE du salarié, pas sur la paie : la paie
	// porte un nom figé, ce qui est exactement ce qu'il faut pour la relire,
	// et pas ce qu'il faut pour joindre quelqu'un.
	for i := range run.Slips {
		slip := &run.Slips[i]
		e, err := s.repo.EmployeeByID(ctx, slip.EmployeeID)
		if err != nil || e.UserID == nil {
			continue
		}
		s.notifier.Notify(ctx, e.UserID.Hex(), KeyPayrollPaid,
			map[string]string{
				"period": run.PeriodStart.Format("01/2006"),
				"amount": strconv.Itoa(slip.Net) + " " + run.Currency,
			},
			map[string]string{"run_id": run.ID.Hex(), "kind": "payroll"})
	}
}

// CancelRun abandonne une paie AVANT versement et libère ses réservations.
func (s *Service) CancelRun(ctx context.Context, id string, in CancelInput) (RunResponse, error) {
	run, err := s.run(ctx, id)
	if err != nil {
		return RunResponse{}, err
	}
	if run.Status == RunPaid {
		return RunResponse{}, errRunPaid
	}
	if run.Status == RunCancelled {
		return toRun(run), nil
	}
	now := s.nowOr()
	cancelled, err := s.repo.MoveRun(ctx, run.ID, run.Status, RunCancelled, bson.M{
		"cancelled_at": now, "cancel_reason": in.Reason,
	})
	if err != nil {
		return RunResponse{}, err
	}
	// ⚠️ LA LIBÉRATION VIENT APRÈS, ET ELLE EST OBLIGATOIRE. Une prime restée
	// réservée par un brouillon abandonné n'entrerait plus dans aucune paie :
	// elle disparaîtrait sans trace d'annulation, ce qui est la pire des
	// façons de perdre de l'argent dû à quelqu'un.
	if err := s.repo.ReleaseAdjustments(ctx, run.ID); err != nil {
		s.log.Error("payroll: réservations non libérées après annulation — à rattraper",
			"run", run.ID.Hex(), "error", err)
	}
	s.record(ctx, "payroll.run_cancel", "payroll_run", run.ID.Hex(), runAudit(run), runAudit(cancelled))
	return toRun(cancelled), nil
}

// Correct régularise une paie VERSÉE.
//
// ⚠️ C'EST LA RÉPONSE À « CORRIGER UNE PAIE », et elle ne touche pas la paie.
// Rouvrir un versement aurait fait disparaître ce qui a réellement été payé :
// le document de janvier dirait ce qu'on en pense en mars, et plus personne ne
// pourrait répondre à « qu'avez-vous versé le 28 ? ». La correction est donc
// une pièce NOUVELLE, qui nomme la paie corrigée et passe sur la suivante.
func (s *Service) Correct(ctx context.Context, runID string, in CorrectionInput) (AdjustmentResponse, error) {
	run, err := s.run(ctx, runID)
	if err != nil {
		return AdjustmentResponse{}, err
	}
	if run.Status != RunPaid {
		return AdjustmentResponse{}, errRunNotPaid
	}
	eid, err := primitive.ObjectIDFromHex(in.EmployeeID)
	if err != nil {
		return AdjustmentResponse{}, errEmployeeNotFound
	}
	// ⚠️ LA PERSONNE DOIT ÊTRE SUR CETTE PAIE. Corriger la paie de janvier au
	// profit de quelqu'un qui n'y figurait pas n'est pas une correction, c'est
	// une prime — et elle doit s'écrire comme telle, avec son motif.
	onRun := false
	for i := range run.Slips {
		if run.Slips[i].EmployeeID == eid {
			onRun = true
			break
		}
	}
	if !onRun {
		return AdjustmentResponse{}, errNotInRun
	}
	out, err := s.Grant(ctx, in.EmployeeID, in.Kind, AdjustmentInput{
		Amount: in.Amount, Reason: in.Reason, Installments: in.Installments,
	})
	if err != nil {
		return AdjustmentResponse{}, err
	}
	// Le lien vers la paie corrigée est posé après coup : `Grant` porte les
	// bornes, celle-ci porte la traçabilité.
	oid, err := primitive.ObjectIDFromHex(out.ID)
	if err != nil {
		return out, nil
	}
	a, err := s.repo.AdjustmentByID(ctx, oid)
	if err != nil {
		return out, nil
	}
	a.CorrectsRun = &run.ID
	if err := s.repo.SaveAdjustment(ctx, a); err != nil {
		return out, nil
	}
	s.record(ctx, "payroll.correction", "payroll_run", run.ID.Hex(), nil, map[string]any{
		"adjustment_id": a.ID.Hex(), "employee_id": in.EmployeeID,
		"kind": in.Kind, "amount": in.Amount, "reason": in.Reason,
	})
	return toAdjustment(a), nil
}

func runAudit(run *Run) map[string]any {
	return map[string]any{
		"status": run.Status, "frequency": run.Frequency,
		"period_start": run.PeriodStart.Format(time.RFC3339),
		"net":          run.Net, "gross": run.Gross, "deductions": run.Deductions,
		"headcount": run.Headcount, "method": run.Method,
	}
}
