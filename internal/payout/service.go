package payout

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Wallet est ce que ce module demande au portefeuille — et rien de plus.
//
// ⚠️ TROIS MÉTHODES, PAS UN ACCÈS AU PORTEFEUILLE. Ce module ne doit pas
// pouvoir créditer, débiter librement ni lire un solde pour le recalculer :
// il retient, il remet, il lit le retirable. Une interface plus large finirait
// par servir à « corriger » un solde depuis un écran de retrait.
type Wallet interface {
	// HoldForWithdrawal retient le montant. `false` = le retirable ne suffit
	// pas. ⚠️ Atomique et conditionnelle.
	HoldForWithdrawal(ctx context.Context, ownerID string, amount int) (bool, error)
	// ReleaseWithdrawal remet le montant au solde.
	ReleaseWithdrawal(ctx context.Context, ownerID string, amount int, reason, payoutID string) error
	// Withdrawable rend ce qui peut réellement sortir : l'argent réel, moins
	// la dette, et JAMAIS le crédit offert.
	Withdrawable(ctx context.Context, ownerID string) (int, error)
}

// Accounts donne le numéro et le rôle du compte.
type Accounts interface {
	// PhoneOf rend le numéro du compte, en E.164.
	//
	// ⚠️ C'EST LE SERVEUR QUI LE DONNE, JAMAIS L'APPELANT. Voir `Payout.Phone`.
	PhoneOf(ctx context.Context, userID string) (string, error)
}

// Policy rend les règles de retrait d'un pays.
type Policy interface {
	PayoutRules(ctx context.Context, countryCode string) Rules
}

// Auditor écrit au journal des actions.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Rules sont les règles d'un pays.
type Rules struct {
	Mode string `json:"mode"`
	// MinXOF : en dessous, on refuse. ⚠️ Un agrégateur facture au versement :
	// verser 100 F coûte parfois plus cher que les 100 F.
	MinXOF int `json:"min_xof"`
	// MaxXOF : par demande. Zéro = pas de plafond.
	MaxXOF int `json:"max_xof,omitempty"`
	// DailyCapXOF : par personne et par jour. Zéro = pas de plafond.
	//
	// ⚠️ CE PLAFOND N'EST PAS CONTRE L'AGENT, il est contre le vol de session :
	// un compte pris vide le solde en une fois. Avec un plafond, il en reste
	// pour le lendemain, et le temps de s'en apercevoir.
	DailyCapXOF int `json:"daily_cap_xof,omitempty"`
	// Currency est la monnaie du pays.
	Currency string `json:"currency,omitempty"`
	// PeriodicWeekday : 0 = tous les jours, 1..7 = lundi..dimanche.
	PeriodicWeekday int `json:"periodic_weekday,omitempty"`
	// PeriodicMinXOF : en dessous, le versement périodique attend la période
	// suivante plutôt que de verser des miettes et de payer des frais dessus.
	PeriodicMinXOF int `json:"periodic_min_xof,omitempty"`
}

// Service tient les retraits.
type Service struct {
	repo     *Repository
	wallet   Wallet
	accounts Accounts
	policy   Policy
	provider Provider
	audit    Auditor
	earners  Earners
	now      func() time.Time
}

func NewService(repo *Repository, w Wallet, a Accounts) *Service {
	return &Service{repo: repo, wallet: w, accounts: a, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetPolicy(p Policy)     { s.policy = p }
func (s *Service) SetProvider(p Provider) { s.provider = p }
func (s *Service) SetAuditor(a Auditor)   { s.audit = a }

var (
	errPayoutOff = apperr.New("payout_unavailable",
		"withdrawals are not open in this country yet", http.StatusUnprocessableEntity)
	// ⚠️ EN MODE PÉRIODIQUE, L'AGENT NE DEMANDE PAS — et la phrase le dit au
	// lieu d'un bouton grisé, qui fait chercher ce qui ne va pas.
	errPayoutPeriodic = apperr.New("payout_is_periodic",
		"your earnings are paid out automatically: there is nothing to ask for",
		http.StatusUnprocessableEntity)
	errPayoutTooSmall = apperr.New("payout_below_minimum",
		"this amount is below the minimum we can send", http.StatusUnprocessableEntity)
	errPayoutTooLarge = apperr.New("payout_above_maximum",
		"this amount is above what we send in one go", http.StatusUnprocessableEntity)
	errPayoutDailyCap = apperr.New("payout_daily_cap",
		"you have reached today's withdrawal limit: the rest can go out tomorrow",
		http.StatusUnprocessableEntity)
	// ⚠️ LA PHRASE NE DIT PAS « SOLDE INSUFFISANT » : le solde peut être large
	// et le RETIRABLE nul, parce que la dette passe avant et que le crédit
	// offert ne se retire pas. Un agent qui lit « solde insuffisant » devant un
	// solde de 40 000 appelle le support.
	errNotWithdrawable = apperr.New("not_withdrawable",
		"this is more than you can withdraw: promotional credit cannot be cashed out, and what you owe comes first",
		http.StatusPaymentRequired)
	errNoPhone = apperr.New("payout_no_phone",
		"your account has no phone number to send money to", http.StatusUnprocessableEntity)
	errPayoutPending = apperr.New("payout_already_pending",
		"you already have a withdrawal in progress: wait for it to finish",
		http.StatusConflict)
	errPayoutNotFound = apperr.NotFound("payout_not_found", "this withdrawal does not exist")
	errPayoutDecided  = apperr.New("payout_already_decided",
		"this withdrawal has already been decided", http.StatusConflict)
	// ⚠️ UN REFUS SANS RAISON FAIT RAPPELER LE SUPPORT. L'agent ne sait pas ce
	// qu'il doit corriger, et le support non plus.
	errRejectNeedsReason = apperr.New("reject_reason_required",
		"say why you are refusing: the agent sees this phrase", http.StatusUnprocessableEntity)
)

// RulesFor rend les règles du pays de la requête.
func (s *Service) RulesFor(ctx context.Context) Rules {
	if s.policy == nil {
		return Rules{Mode: ModeOff}
	}
	return s.policy.PayoutRules(ctx, country.FromContext(ctx))
}

// CheckRequest dit si une demande est recevable au vu des règles du pays.
//
// ⚠️⚠️ PURE, ET EXTRAITE EXPRÈS. Ce sont les refus qu'un agent lit, et ce sont
// eux qui méritent d'être prouvés : le reste du chemin est de la plomberie
// (lire le numéro, retenir, écrire). Une fonction qui a besoin d'une base pour
// être testée est une fonction qu'on finit par ne pas tester.
func CheckRequest(rules Rules, amount int) error {
	switch {
	case rules.Mode == ModeOff || rules.Mode == "":
		return errPayoutOff
	case rules.Mode == ModePeriodic:
		return errPayoutPeriodic
	case amount <= 0:
		return apperr.Validation("amount must be positive")
	case rules.MinXOF > 0 && amount < rules.MinXOF:
		return errPayoutTooSmall.WithMeta(map[string]any{"reason": money(rules.MinXOF)})
	case rules.MaxXOF > 0 && amount > rules.MaxXOF:
		return errPayoutTooLarge.WithMeta(map[string]any{"reason": money(rules.MaxXOF)})
	}
	return nil
}

// Available dit à l'agent ce qu'il peut faire — l'écran de retrait.
type Available struct {
	Rules Rules `json:"rules"`
	// WithdrawableXOF : ce qui peut SORTIR, pas le solde.
	WithdrawableXOF int `json:"withdrawable_xof"`
	// Pending : la demande en cours, s'il y en a une.
	Pending *Response `json:"pending,omitempty"`
	// PhoneMasked : là où l'argent ira.
	PhoneMasked string `json:"phone_masked,omitempty"`
}

// Available compose l'écran de retrait.
func (s *Service) Available(ctx context.Context, userID string) (*Available, error) {
	out := &Available{Rules: s.RulesFor(ctx)}
	w, err := s.wallet.Withdrawable(ctx, userID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out.WithdrawableXOF = w
	if phone, err := s.accounts.PhoneOf(ctx, userID); err == nil {
		out.PhoneMasked = maskPhone(phone)
	}
	open, err := s.repo.OpenOf(ctx, userID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if open != nil {
		r := toResponse(*open)
		out.Pending = &r
	}
	return out, nil
}

// Request : l'agent demande un retrait.
func (s *Service) Request(ctx context.Context, userID string, amount int) (*Response, error) {
	rules := s.RulesFor(ctx)
	if err := CheckRequest(rules, amount); err != nil {
		return nil, err
	}

	// ⚠️ UNE SEULE DEMANDE OUVERTE À LA FOIS. Deux demandes en vol rendent le
	// plafond du jour incalculable, et surtout : l'agent ne sait plus laquelle
	// a échoué quand une seule revient.
	if open, err := s.repo.OpenOf(ctx, userID); err != nil {
		return nil, apperr.Internal(err)
	} else if open != nil {
		return nil, errPayoutPending
	}

	if rules.DailyCapXOF > 0 {
		today, err := s.repo.SentTodaySince(ctx, userID, s.now().Add(-24*time.Hour))
		if err != nil {
			return nil, apperr.Internal(err)
		}
		if today+amount > rules.DailyCapXOF {
			return nil, errPayoutDailyCap.WithMeta(map[string]any{
				"reason": money(rules.DailyCapXOF - today),
			})
		}
	}

	// ⚠️ LE NUMÉRO VIENT DU COMPTE, et il est lu ICI — jamais reçu de
	// l'appelant. Voir `Payout.Phone` : un champ libre est la porte ouverte au
	// détournement.
	phone, err := s.accounts.PhoneOf(ctx, userID)
	if err != nil || strings.TrimSpace(phone) == "" {
		return nil, errNoPhone
	}

	// ⚠️⚠️ LA RETENUE EST ATOMIQUE ET CONDITIONNELLE. Deux demandes simultanées
	// qui liraient le solde puis débiteraient feraient sortir le double.
	ok, err := s.wallet.HoldForWithdrawal(ctx, userID, amount)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if !ok {
		return nil, errNotWithdrawable
	}

	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	p := &Payout{
		OwnerID: oid, Country: country.FromContext(ctx), Mode: rules.Mode,
		Amount: amount, Currency: rules.Currency, Phone: phone,
		State: StatePending, CreatedAt: s.now(),
	}
	if err := s.repo.Insert(ctx, p); err != nil {
		// ⚠️ LA RETENUE EST ANNULÉE SI LA DEMANDE NE S'ÉCRIT PAS. Sans cette
		// remise, l'argent sortirait du solde sans qu'aucune demande n'existe
		// pour le rendre — il disparaîtrait, simplement.
		if rerr := s.wallet.ReleaseWithdrawal(ctx, userID, amount, "payout_write_failed", ""); rerr != nil {
			slog.ErrorContext(ctx, "payout: retenue non remise après échec d'écriture",
				"owner_id", userID, "amount", amount, "error", rerr)
		}
		return nil, apperr.Internal(err)
	}
	s.record(ctx, "payout.request", p)

	// En mode direct, l'ordre part tout de suite.
	if rules.Mode == ModeDirect {
		s.send(ctx, p)
	}
	out := toResponse(*p)
	return &out, nil
}

// Approve : l'exploitation valide une demande, et l'ordre part.
func (s *Service) Approve(ctx context.Context, payoutID string) (*Response, error) {
	p, err := s.decide(ctx, payoutID, StateSending, "")
	if err != nil {
		return nil, err
	}
	s.send(ctx, p)
	out := toResponse(*p)
	return &out, nil
}

// Reject : l'exploitation refuse, et l'argent retourne au solde.
func (s *Service) Reject(ctx context.Context, payoutID, reason string) (*Response, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errRejectNeedsReason
	}
	p, err := s.decide(ctx, payoutID, StateRejected, reason)
	if err != nil {
		return nil, err
	}
	s.release(ctx, p, "payout_rejected")
	out := toResponse(*p)
	return &out, nil
}

func (s *Service) decide(ctx context.Context, payoutID, state, reason string) (*Payout, error) {
	oid, err := primitive.ObjectIDFromHex(payoutID)
	if err != nil {
		return nil, errPayoutNotFound
	}
	actor, _ := auth.UserFromContext(ctx)
	by, _ := primitive.ObjectIDFromHex(actor)
	// ⚠️ LA DÉCISION EST CONDITIONNELLE SUR L'ÉTAT `pending`. Deux opérateurs
	// qui cliquent en même temps : le second ne trouve rien à décider, et on
	// ne verse pas deux fois.
	p, err := s.repo.Decide(ctx, oid, state, reason, by, s.now())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if p == nil {
		return nil, errPayoutDecided
	}
	s.record(ctx, "payout."+state, p)
	return p, nil
}

// send pousse l'ordre chez l'agrégateur.
//
// ⚠️ ELLE NE REND PAS D'ERREUR À L'APPELANT, et c'est voulu. Une demande
// ACCEPTÉE l'est : si l'agrégateur ne répond pas, la demande reste `sending` et
// sera reprise. Faire échouer la requête de l'agent le ferait redemander — et
// l'argent est déjà retenu.
func (s *Service) send(ctx context.Context, p *Payout) {
	if s.provider == nil {
		slog.ErrorContext(ctx, "payout: aucun agrégateur câblé — la demande reste en attente",
			"payout_id", p.ID.Hex())
		return
	}
	res, err := s.provider.Send(ctx, SendRequest{
		PayoutID: p.ID.Hex(), Phone: p.Phone,
		Amount: p.Amount, Currency: p.Currency,
		Reference: "Dira " + p.ID.Hex()[:8],
	})
	now := s.now()
	switch {
	case err != nil:
		// ⚠️ ON NE REMET PAS L'ARGENT ICI. Une erreur de transport ne dit PAS
		// que le versement n'a pas eu lieu : l'ordre a pu partir et la réponse
		// se perdre. Remettre au solde ferait exister l'argent deux fois. La
		// demande reste `sending`, et c'est le rappel de l'agrégateur — ou une
		// reprise — qui tranchera.
		slog.ErrorContext(ctx, "payout: l'agrégateur n'a pas répondu — demande laissée en cours",
			"payout_id", p.ID.Hex(), "error", err)
		_ = s.repo.MarkSending(ctx, p.ID, "", now)
		p.State = StateSending
	case res.Status == StatusFailed:
		// Un refus EXPLICITE, lui, est sûr : rien n'est parti.
		_ = s.repo.MarkFailed(ctx, p.ID, res.FailureReason, now)
		p.State, p.FailureReason = StateFailed, res.FailureReason
		s.release(ctx, p, "payout_failed")
	case res.Status == StatusPaid:
		_ = s.repo.MarkPaid(ctx, p.ID, res.ProviderRef, now)
		p.State, p.ProviderRef = StatePaid, res.ProviderRef
	default:
		_ = s.repo.MarkSending(ctx, p.ID, res.ProviderRef, now)
		p.State, p.ProviderRef = StateSending, res.ProviderRef
	}
}

// release remet l'argent au solde.
func (s *Service) release(ctx context.Context, p *Payout, reason string) {
	if err := s.wallet.ReleaseWithdrawal(ctx, p.OwnerID.Hex(), p.Amount, reason, p.ID.Hex()); err != nil {
		// ⚠️ UNE REMISE QUI RATE EST UNE ALERTE, pas une ligne de journal
		// ordinaire : l'argent d'un agent est retenu et ne lui revient pas.
		slog.ErrorContext(ctx, "payout: ARGENT RETENU NON REMIS — intervention requise",
			"payout_id", p.ID.Hex(), "owner_id", p.OwnerID.Hex(),
			"amount_xof", p.Amount, "reason", reason, "error", err)
	}
}

func (s *Service) record(ctx context.Context, action string, p *Payout) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "payout", p.ID.Hex(), nil, map[string]any{
		"owner_id": p.OwnerID.Hex(), "amount_xof": p.Amount,
		"state": p.State, "mode": p.Mode,
	})
}

// Mine rend les retraits d'un agent.
func (s *Service) Mine(ctx context.Context, userID string, limit int) ([]Response, error) {
	rows, err := s.repo.ListOf(ctx, userID, limit)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return responses(rows), nil
}

// Pending rend la file de validation — la console.
func (s *Service) Pending(ctx context.Context, limit int) ([]Response, error) {
	rows, err := s.repo.ListPending(ctx, limit)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return responses(rows), nil
}

func responses(rows []Payout) []Response {
	// ⚠️ UNE TRANCHE VIDE, JAMAIS `nil` : `items: null` fait planter un écran
	// qui boucle dessus sans vérifier.
	out := make([]Response, 0, len(rows))
	for _, p := range rows {
		out = append(out, toResponse(p))
	}
	return out
}

func money(v int) string { return strings.TrimSpace(formatXOF(v)) }

// --- LE RAPPEL DE L'AGRÉGATEUR --------------------------------------------

// HandleWebhook applique une notification vérifiée.
//
// ⚠️⚠️ IDEMPOTENTE PAR CONSTRUCTION. Un agrégateur rejoue ses notifications —
// c'est même ce qu'on lui demande quand on ne répond pas assez vite. Les deux
// écritures sont conditionnées sur un état NON FINAL : une notification rejouée
// ne ressuscite pas une demande close, et surtout ne remet pas l'argent au
// solde une seconde fois.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, signature string) error {
	if s.provider == nil {
		return apperr.Internal(ErrNoProvider)
	}
	ev, err := s.provider.VerifyWebhook(payload, signature)
	if err != nil {
		// ⚠️ UNE SIGNATURE QUI NE COLLE PAS EST UN REFUS, PAS UNE PANNE : c'est
		// quelqu'un qui n'est pas l'agrégateur, et lui rendre `500` l'inviterait
		// à réessayer.
		return apperr.Unauthorized("bad_signature", "this notification is not signed by the aggregator")
	}
	oid, err := primitive.ObjectIDFromHex(ev.PayoutID)
	if err != nil {
		return errPayoutNotFound
	}
	p, err := s.repo.ByID(ctx, oid)
	if err != nil {
		return apperr.Internal(err)
	}
	if p == nil {
		return errPayoutNotFound
	}
	// ⚠️ DÉJÀ CLOSE : on répond OK sans rien faire. Une notification en double
	// n'est pas une erreur de l'agrégateur, c'est son fonctionnement normal.
	if !p.Open() {
		return nil
	}
	now := s.now()
	switch ev.Status {
	case StatusPaid:
		if err := s.repo.MarkPaid(ctx, p.ID, ev.ProviderRef, now); err != nil {
			return apperr.Internal(err)
		}
		p.State = StatePaid
		s.record(ctx, "payout.paid", p)
	case StatusFailed:
		if err := s.repo.MarkFailed(ctx, p.ID, ev.FailureReason, now); err != nil {
			return apperr.Internal(err)
		}
		p.State, p.FailureReason = StateFailed, ev.FailureReason
		// ⚠️ L'ARGENT REVIENT AU SOLDE. C'est le seul endroit où un échec
		// confirmé par l'agrégateur est certain — contrairement à une erreur
		// de transport, qui ne dit rien.
		s.release(ctx, p, "payout_failed")
		s.record(ctx, "payout.failed", p)
	}
	return nil
}

// --- LE TOUR PÉRIODIQUE ----------------------------------------------------

// Earners rend les agents qui ont de quoi être payés — le portefeuille.
type Earners interface {
	// WithdrawableAbove rend les propriétaires d'un pays dont le retirable
	// atteint le seuil. ⚠️ Le portefeuille est le seul à le savoir.
	WithdrawableAbove(ctx context.Context, countryCode string, min int) (map[string]int, error)
}

// SetEarners branche la source du tour périodique (câblage).
func (s *Service) SetEarners(e Earners) { s.earners = e }

// RunPeriodic verse automatiquement, pour un pays en mode `periodic`.
//
// ⚠️⚠️ LE JOUR SE VÉRIFIE ICI, PAS DANS LA CADENCE DU TICKER. Un tour qui
// partirait « toutes les 168 heures » dériverait à chaque redémarrage, et
// finirait par verser un mercredi. Le ticker bat souvent ; c'est cette
// fonction qui décide si c'est le bon jour.
//
// ⚠️ ET ELLE NE PAIE PAS DEUX FOIS LE MÊME JOUR : une demande déjà ouverte
// écarte son propriétaire, et une demande créée aujourd'hui reste ouverte le
// temps du versement.
func (s *Service) RunPeriodic(ctx context.Context) (int, error) {
	rules := s.RulesFor(ctx)
	if rules.Mode != ModePeriodic || s.earners == nil {
		return 0, nil
	}
	now := s.now()
	if rules.PeriodicWeekday != 0 && int(now.Weekday()) != weekdayOf(rules.PeriodicWeekday) {
		return 0, nil
	}
	min := rules.PeriodicMinXOF
	if min < rules.MinXOF {
		min = rules.MinXOF
	}
	if min <= 0 {
		min = 1
	}
	code := country.FromContext(ctx)
	owners, err := s.earners.WithdrawableAbove(ctx, code, min)
	if err != nil {
		return 0, apperr.Internal(err)
	}
	open, err := s.repo.OpenOwners(ctx)
	if err != nil {
		return 0, apperr.Internal(err)
	}

	n := 0
	for ownerID, amount := range owners {
		if open[ownerID] {
			continue
		}
		if rules.MaxXOF > 0 && amount > rules.MaxXOF {
			amount = rules.MaxXOF
		}
		p, err := s.autoRequest(ctx, ownerID, amount, rules)
		if err != nil {
			// ⚠️ UN ÉCHEC N'ARRÊTE PAS LE TOUR. Un numéro manquant chez l'un ne
			// doit pas priver les quarante autres de leur paie.
			slog.WarnContext(ctx, "payout: versement périodique non créé",
				"owner_id", ownerID, "error", err)
			continue
		}
		s.send(ctx, p)
		n++
	}
	return n, nil
}

// autoRequest crée la demande d'un versement périodique — sans passer par les
// règles de la demande manuelle, qui n'ont pas de sens ici (personne ne
// demande).
func (s *Service) autoRequest(ctx context.Context, ownerID string, amount int, rules Rules) (*Payout, error) {
	phone, err := s.accounts.PhoneOf(ctx, ownerID)
	if err != nil || strings.TrimSpace(phone) == "" {
		return nil, errNoPhone
	}
	ok, err := s.wallet.HoldForWithdrawal(ctx, ownerID, amount)
	if err != nil {
		return nil, err
	}
	if !ok {
		// Le retirable a bougé entre la liste et la retenue : on passe.
		return nil, errNotWithdrawable
	}
	oid, err := primitive.ObjectIDFromHex(ownerID)
	if err != nil {
		return nil, err
	}
	p := &Payout{
		OwnerID: oid, Country: country.FromContext(ctx), Mode: ModePeriodic,
		Amount: amount, Currency: rules.Currency, Phone: phone,
		State: StatePending, CreatedAt: s.now(),
	}
	if err := s.repo.Insert(ctx, p); err != nil {
		if rerr := s.wallet.ReleaseWithdrawal(ctx, ownerID, amount, "payout_write_failed", ""); rerr != nil {
			slog.ErrorContext(ctx, "payout: retenue non remise après échec d'écriture",
				"owner_id", ownerID, "amount", amount, "error", rerr)
		}
		return nil, err
	}
	s.record(ctx, "payout.periodic", p)
	return p, nil
}

// weekdayOf traduit 1..7 (lundi..dimanche) vers `time.Weekday` (0 = dimanche).
//
// ⚠️ DEUX CONVENTIONS, ET C'EST LA SOURCE D'ERREUR CLASSIQUE : « 1 » veut dire
// lundi pour un humain et dimanche pour Go. Traduit en un seul endroit.
func weekdayOf(n int) int {
	if n == 7 {
		return 0 // dimanche
	}
	return n
}
