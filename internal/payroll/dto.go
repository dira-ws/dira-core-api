package payroll

import "time"

// --- réglages ---

type SettingsInput struct {
	DefaultFrequency    *string `json:"default_frequency" validate:"omitempty,oneof=monthly biweekly weekly"`
	DefaultPayDay       *int    `json:"default_pay_day" validate:"omitempty,min=1,max=28"`
	DefaultPayWeekday   *int    `json:"default_pay_weekday" validate:"omitempty,min=1,max=7"`
	MaxDeductionPercent *int    `json:"max_deduction_percent" validate:"omitempty,min=0,max=100"`
	MinNet              *int    `json:"min_net" validate:"omitempty,min=0"`
}

type SettingsResponse struct {
	Country             string     `json:"country"`
	Currency            string     `json:"currency"`
	DefaultFrequency    string     `json:"default_frequency"`
	DefaultPayDay       int        `json:"default_pay_day"`
	DefaultPayWeekday   int        `json:"default_pay_weekday"`
	MaxDeductionPercent int        `json:"max_deduction_percent"`
	MinNet              int        `json:"min_net"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
	// Le VOCABULAIRE, pour que la console propose exactement ce que le serveur
	// accepte. Deux listes recopiées divergeraient au premier ajout.
	Frequencies []string `json:"frequencies"`
	Statuses    []string `json:"statuses"`
	Methods     []string `json:"methods"`
}

// whenSet rend la date, ou NIL si elle n'a jamais été posée.
//
// ⚠️⚠️ LA DATE ZÉRO D'UN `time.Time` GO PART SUR LE FIL COMME UNE VRAIE DATE.
// `0001-01-01T00:00:00Z` ne se distingue pas d'une décision d'exploitation
// prise un 1er janvier : une application qui compare `updated_at` pour savoir
// si son cache a vieilli se compare à l'an 1, donc ne rafraîchit jamais, et une
// console l'affiche « modifié le 01/01/0001 ». La console de Dira porte
// d'ailleurs encore le contournement écrit à la main le jour où quelqu'un l'a
// découvert (`!updated_at.startsWith("0001")`).
//
// « Jamais réglé » et « réglé à telle date » sont deux faits différents, et
// seul un champ ABSENT dit le premier.
func whenSet(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func toSettings(s *Settings, currency string) SettingsResponse {
	return SettingsResponse{
		Country: s.Country, Currency: currency,
		DefaultFrequency: s.DefaultFrequency, DefaultPayDay: s.DefaultPayDay,
		DefaultPayWeekday: s.DefaultPayWeekday, MaxDeductionPercent: s.MaxDeductionPercent,
		MinNet: s.MinNet, UpdatedAt: whenSet(s.UpdatedAt),
		Frequencies: Frequencies, Statuses: Statuses, Methods: Methods,
	}
}

// --- salariés ---

// EmployeeInput est ce que l'exploitation déclare d'un salarié.
type EmployeeInput struct {
	FullName string `json:"full_name" validate:"required,min=2,max=160"`
	Phone    string `json:"phone" validate:"omitempty,e164"`
	Email    string `json:"email" validate:"omitempty,email"`
	Position string `json:"position" validate:"required,min=2,max=160"`
	// UserID : le compte d'exploitation, S'IL EXISTE. ⚠️ Facultatif, et il
	// n'accorde aucun droit — voir l'en-tête du paquet.
	UserID string `json:"user_id" validate:"omitempty,len=24,hexadecimal"`
	// HiredAt : la date d'entrée. Absente = aujourd'hui.
	HiredAt    *time.Time `json:"hired_at"`
	LeftAt     *time.Time `json:"left_at"`
	BaseSalary int        `json:"base_salary" validate:"required,min=1"`
	Frequency  string     `json:"frequency" validate:"omitempty,oneof=monthly biweekly weekly"`
	PayDay     int        `json:"pay_day" validate:"omitempty,min=1,max=28"`
	PayWeekday int        `json:"pay_weekday" validate:"omitempty,min=1,max=7"`
	Notes      string     `json:"notes" validate:"omitempty,max=2000"`
}

// EmployeeUpdate ne porte QUE ce qui change — chaque champ est un pointeur,
// sinon « ne pas toucher au salaire » et « mettre le salaire à zéro »
// s'écriraient pareil.
type EmployeeUpdate struct {
	FullName   *string    `json:"full_name" validate:"omitempty,min=2,max=160"`
	Phone      *string    `json:"phone" validate:"omitempty,e164"`
	Email      *string    `json:"email" validate:"omitempty,email"`
	Position   *string    `json:"position" validate:"omitempty,min=2,max=160"`
	UserID     *string    `json:"user_id" validate:"omitempty"`
	HiredAt    *time.Time `json:"hired_at"`
	LeftAt     *time.Time `json:"left_at"`
	BaseSalary *int       `json:"base_salary" validate:"omitempty,min=1"`
	Frequency  *string    `json:"frequency" validate:"omitempty,oneof=monthly biweekly weekly"`
	PayDay     *int       `json:"pay_day" validate:"omitempty,min=0,max=28"`
	PayWeekday *int       `json:"pay_weekday" validate:"omitempty,min=0,max=7"`
	Status     *string    `json:"status" validate:"omitempty,oneof=active suspended left"`
	Notes      *string    `json:"notes" validate:"omitempty,max=2000"`
}

type EmployeeResponse struct {
	ID       string `json:"id"`
	Country  string `json:"country"`
	Currency string `json:"currency"`
	UserID   string `json:"user_id,omitempty"`
	// AccountName est le nom porté par le COMPTE rattaché, quand il y en a un.
	//
	// ⚠️ SERVI À CÔTÉ DU NOM DE LA FICHE, jamais à sa place. Les deux doivent
	// pouvoir différer sous les yeux de quelqu'un : c'est ainsi qu'on voit
	// qu'une fiche de paie a été rattachée au mauvais compte.
	AccountName string     `json:"account_name,omitempty"`
	FullName    string     `json:"full_name"`
	Phone       string     `json:"phone,omitempty"`
	Email       string     `json:"email,omitempty"`
	Position    string     `json:"position"`
	HiredAt     time.Time  `json:"hired_at"`
	LeftAt      *time.Time `json:"left_at,omitempty"`
	Status      string     `json:"status"`
	BaseSalary  int        `json:"base_salary"`
	Frequency   string     `json:"frequency"`
	PayDay      int        `json:"pay_day,omitempty"`
	PayWeekday  int        `json:"pay_weekday,omitempty"`
	// NextPayDate : la date du prochain versement, calculée — ce que la
	// console affiche sans refaire l'arithmétique des périodes.
	NextPayDate time.Time `json:"next_pay_date"`
	// MaxDeductionPerRun : ce qu'une paie peut au plus retenir à cette
	// personne, aux réglages du pays du moment.
	//
	// ⚠️ SERVI, ET NON RECALCULÉ PAR LA CONSOLE. Deux arithmétiques de la même
	// borne divergent au premier changement de réglage, et c'est l'écran qui
	// aurait tort — en promettant une retenue que le serveur refuse.
	MaxDeductionPerRun int        `json:"max_deduction_per_run"`
	OpenDeductions     int        `json:"open_deductions"`
	Notes              string     `json:"notes,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	LastPaidAt         *time.Time `json:"last_paid_at,omitempty"`
}

// --- primes et retenues ---

// AdjustmentInput est une prime ou une retenue. `kind` vient de la ROUTE :
// `/bonuses` ou `/deductions`.
type AdjustmentInput struct {
	Amount  int    `json:"amount" validate:"omitempty,min=1"`
	Percent int    `json:"percent" validate:"omitempty,min=1,max=100"`
	Reason  string `json:"reason" validate:"required,min=2,max=300"`
	// Recurring : à repasser sur chaque paie. Exclusif de `installments`.
	Recurring bool `json:"recurring"`
	// Installments : sur combien de paies étaler — l'échéancier.
	Installments  int        `json:"installments" validate:"omitempty,min=1,max=60"`
	EffectiveFrom *time.Time `json:"effective_from"`
}

type AdjustmentResponse struct {
	ID            string     `json:"id"`
	EmployeeID    string     `json:"employee_id"`
	Kind          string     `json:"kind"`
	Amount        int        `json:"amount,omitempty"`
	Percent       int        `json:"percent,omitempty"`
	Reason        string     `json:"reason"`
	Recurring     bool       `json:"recurring"`
	Installments  int        `json:"installments,omitempty"`
	Runs          int        `json:"runs"`
	Taken         int        `json:"taken"`
	Remaining     int        `json:"remaining"`
	Status        string     `json:"status"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	// RunID : la paie qui l'a réservé. Sa présence dit « déjà sur une paie en
	// préparation » — ce qui explique pourquoi on ne peut plus l'annuler
	// librement.
	RunID string `json:"run_id,omitempty"`
	// CorrectsRun : la paie VERSÉE que cet ajustement régularise.
	CorrectsRun string    `json:"corrects_run,omitempty"`
	GrantedBy   string    `json:"granted_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toAdjustment(a *Adjustment) AdjustmentResponse {
	out := AdjustmentResponse{
		ID: a.ID.Hex(), EmployeeID: a.EmployeeID.Hex(), Kind: a.Kind,
		Amount: a.Amount, Percent: a.Percent, Reason: a.Reason,
		Recurring: a.Recurring, Installments: a.Installments, Runs: a.Runs,
		Taken: a.Taken, Remaining: a.Remaining(), Status: a.Status,
		GrantedBy: a.GrantedBy, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if !a.EffectiveFrom.IsZero() {
		from := a.EffectiveFrom
		out.EffectiveFrom = &from
	}
	if a.RunID != nil {
		out.RunID = a.RunID.Hex()
	}
	if a.CorrectsRun != nil {
		out.CorrectsRun = a.CorrectsRun.Hex()
	}
	return out
}

// --- paies ---

// PrepareInput : ce qu'on prépare. Tout est facultatif — sans rien, la
// DERNIÈRE PÉRIODE CLOSE de la fréquence demandée.
type PrepareInput struct {
	Frequency string `json:"frequency" validate:"required,oneof=monthly biweekly weekly"`
	// On : une date DANS la période à préparer. Absente = la dernière période
	// close. ⚠️ Pas « period_start » : une date libre dans la période évite
	// d'avoir à connaître l'origine des quinzaines pour préparer une paie.
	On    *time.Time `json:"on"`
	Notes string     `json:"notes" validate:"omitempty,max=2000"`
}

type PayInput struct {
	Method    string `json:"method" validate:"required,oneof=cash bank_transfer mobile_money"`
	Reference string `json:"reference" validate:"omitempty,max=160"`
}

type CancelInput struct {
	Reason string `json:"reason" validate:"omitempty,max=300"`
}

// CorrectionInput régularise une paie VERSÉE — voir
// `Adjustment.CorrectsRun`.
type CorrectionInput struct {
	EmployeeID string `json:"employee_id" validate:"required,len=24,hexadecimal"`
	// Kind : `bonus` pour un rattrapage en faveur de la personne, `deduction`
	// pour un trop-versé à reprendre.
	Kind   string `json:"kind" validate:"required,oneof=bonus deduction"`
	Amount int    `json:"amount" validate:"required,min=1"`
	Reason string `json:"reason" validate:"required,min=2,max=300"`
	// Installments : étaler la reprise d'un trop-versé. ⚠️ Pour une retenue de
	// correction, c'est souvent indispensable : reprendre un trop-versé en une
	// fois vide la paie suivante, ce que le plancher refusera de toute façon.
	Installments int `json:"installments" validate:"omitempty,min=1,max=60"`
}

type SlipLineResponse struct {
	AdjustmentID string `json:"adjustment_id"`
	Kind         string `json:"kind"`
	Reason       string `json:"reason"`
	Planned      int    `json:"planned"`
	Amount       int    `json:"amount"`
	// Capped dit que la ligne a été BORNÉE : ce qui était visé n'est pas passé.
	//
	// ⚠️ SERVI EXPLICITEMENT plutôt que laissé à déduire de `planned != amount`.
	// Une console qui doit comparer deux nombres pour savoir s'il s'est passé
	// quelque chose finit par ne pas le faire, et la borne devient invisible —
	// c'est-à-dire inexistante pour celui qui lit la fiche.
	Capped    bool   `json:"capped"`
	GrantedBy string `json:"granted_by,omitempty"`
}

type PayslipResponse struct {
	EmployeeID string             `json:"employee_id"`
	Name       string             `json:"name"`
	Position   string             `json:"position,omitempty"`
	PayDate    time.Time          `json:"pay_date"`
	Base       int                `json:"base"`
	Lines      []SlipLineResponse `json:"lines"`
	Bonuses    int                `json:"bonuses"`
	Gross      int                `json:"gross"`
	Deductions int                `json:"deductions"`
	Net        int                `json:"net"`
	Carried    int                `json:"carried"`
	Floor      int                `json:"floor"`
}

type RunResponse struct {
	ID          string    `json:"id"`
	Country     string    `json:"country"`
	Currency    string    `json:"currency"`
	Frequency   string    `json:"frequency"`
	PeriodStart time.Time `json:"period_start"`
	// PeriodEnd est EXCLUSIVE — le premier instant de la période suivante.
	// `period_last_day` est la même borne écrite comme un humain la lit, parce
	// qu'un écran qui affiche « du 1er au 1er février » se fait corriger tous
	// les mois.
	PeriodEnd     time.Time         `json:"period_end"`
	PeriodLastDay time.Time         `json:"period_last_day"`
	PayDate       time.Time         `json:"pay_date"`
	Status        string            `json:"status"`
	Slips         []PayslipResponse `json:"slips"`
	Gross         int               `json:"gross"`
	Bonuses       int               `json:"bonuses"`
	Deductions    int               `json:"deductions"`
	Net           int               `json:"net"`
	Carried       int               `json:"carried"`
	Headcount     int               `json:"headcount"`
	PreparedBy    string            `json:"prepared_by,omitempty"`
	ApprovedBy    string            `json:"approved_by,omitempty"`
	ApprovedAt    *time.Time        `json:"approved_at,omitempty"`
	PaidBy        string            `json:"paid_by,omitempty"`
	PaidAt        *time.Time        `json:"paid_at,omitempty"`
	Method        string            `json:"method,omitempty"`
	Reference     string            `json:"reference,omitempty"`
	CancelReason  string            `json:"cancel_reason,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	// Editable dit si ce document peut encore changer.
	//
	// ⚠️ SERVI PAR LE SERVEUR, et non deviné de `status` par chaque écran. Une
	// console qui décide elle-même de montrer un bouton « Modifier » sur une
	// paie versée a déjà menti à celui qui le voit : la règle (« versée = figée »)
	// est au serveur, et elle voyage avec le document.
	Editable  bool      `json:"editable"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toRun(run *Run) RunResponse {
	slips := make([]PayslipResponse, 0, len(run.Slips))
	for i := range run.Slips {
		s := &run.Slips[i]
		lines := make([]SlipLineResponse, 0, len(s.Lines))
		for _, l := range s.Lines {
			lines = append(lines, SlipLineResponse{
				AdjustmentID: l.AdjustmentID.Hex(), Kind: l.Kind, Reason: l.Reason,
				Planned: l.Planned, Amount: l.Amount, Capped: l.Amount < l.Planned,
				GrantedBy: l.GrantedBy,
			})
		}
		slips = append(slips, PayslipResponse{
			EmployeeID: s.EmployeeID.Hex(), Name: s.Name, Position: s.Position,
			PayDate: s.PayDate, Base: s.Base, Lines: lines, Bonuses: s.Bonuses, Gross: s.Gross,
			Deductions: s.Deductions, Net: s.Net, Carried: s.Carried, Floor: s.Floor,
		})
	}
	return RunResponse{
		ID: run.ID.Hex(), Country: run.Country, Currency: run.Currency,
		Frequency: run.Frequency, PeriodStart: run.PeriodStart, PeriodEnd: run.PeriodEnd,
		PeriodLastDay: run.PeriodEnd.AddDate(0, 0, -1), PayDate: run.PayDate,
		Status: run.Status, Slips: slips, Gross: run.Gross, Bonuses: run.Bonuses,
		Deductions: run.Deductions, Net: run.Net, Carried: run.Carried,
		Headcount: run.Headcount, PreparedBy: run.PreparedBy,
		ApprovedBy: run.ApprovedBy, ApprovedAt: run.ApprovedAt,
		PaidBy: run.PaidBy, PaidAt: run.PaidAt, Method: run.Method,
		Reference: run.Reference, CancelReason: run.CancelReason, Notes: run.Notes,
		Editable:  run.Status == RunDraft,
		CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
}
