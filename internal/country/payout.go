package country

// LES RÈGLES DE RETRAIT D'UN PAYS.
//
// ⚠️⚠️ ÉTEINT PAR DÉFAUT, ET C'EST LA SEULE VALEUR DÉFENDABLE. Verser de
// l'argent demande un contrat signé avec un agrégateur, un flottant
// approvisionné et une personne pour le surveiller. Un pays qui n'a pas les
// trois ne doit pas afficher un bouton « retirer » : il échouerait, et un
// bouton de retrait qui échoue fait appeler le support par tout le monde en
// même temps, le même jour.
//
// ⚠️ ET LE MODE EST UNE DÉCISION D'EXPLOITATION, pas un confort :
//
//   - `request` pour un pays qui démarre — on décide au cas par cas, et on voit
//     passer chaque sortie d'argent ;
//   - `direct` quand le flottant est approvisionné et la confiance établie ;
//   - `periodic` quand c'est la plateforme qui décide du rythme, et que
//     personne n'a rien à demander.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Payout est la politique de retrait d'un pays.
type Payout struct {
	// Mode : `off` (défaut), `direct`, `request`, `periodic`.
	Mode string `bson:"mode,omitempty"`
	// MinXOF : zéro = le défaut (voir `defaultPayoutMin`).
	MinXOF      int `bson:"min_xof,omitempty"`
	MaxXOF      int `bson:"max_xof,omitempty"`
	DailyCapXOF int `bson:"daily_cap_xof,omitempty"`
	// PeriodicWeekday : 0 = tous les jours, 1..7 = lundi..dimanche.
	PeriodicWeekday int `bson:"periodic_weekday,omitempty"`
	PeriodicMinXOF  int `bson:"periodic_min_xof,omitempty"`
}

// defaultPayoutMin : en dessous, un agrégateur facture plus que le montant.
//
// ⚠️ UN MINIMUM EXISTE TOUJOURS, même non réglé. Sans lui, un agent retire
// 50 F dix fois par jour et chaque retrait coûte à la plateforme plus qu'il ne
// transporte — et c'est l'agent qui a raison, c'est son argent.
const defaultPayoutMin = 1000

// PayoutResponse est la politique telle que la console et les applications la
// lisent — tous les champs posés, aucun à deviner.
type PayoutResponse struct {
	Mode            string `json:"mode"`
	MinXOF          int    `json:"min_xof"`
	MaxXOF          int    `json:"max_xof,omitempty"`
	DailyCapXOF     int    `json:"daily_cap_xof,omitempty"`
	PeriodicWeekday int    `json:"periodic_weekday,omitempty"`
	PeriodicMinXOF  int    `json:"periodic_min_xof,omitempty"`
	// Currency : la monnaie du pays, servie pour que l'écran n'ait rien à
	// deviner.
	Currency string `json:"currency,omitempty"`
}

// PayoutUpdateRequest règle la politique depuis la console. Tout est
// facultatif.
type PayoutUpdateRequest struct {
	Mode *string `json:"mode" validate:"omitempty,oneof=off direct request periodic"`
	// ⚠️ LES BORNES SONT DES GARDE-FOUS, PAS DES PRÉFÉRENCES. Un minimum à
	// zéro ferait payer des frais d'agrégateur sur des montants dérisoires ; un
	// plafond journalier absent laisse un compte volé se vider en une fois.
	MinXOF          *int `json:"min_xof" validate:"omitempty,min=100,max=1000000"`
	MaxXOF          *int `json:"max_xof" validate:"omitempty,min=0,max=10000000"`
	DailyCapXOF     *int `json:"daily_cap_xof" validate:"omitempty,min=0,max=10000000"`
	PeriodicWeekday *int `json:"periodic_weekday" validate:"omitempty,min=0,max=7"`
	PeriodicMinXOF  *int `json:"periodic_min_xof" validate:"omitempty,min=0,max=10000000"`
}

var errNoPayoutUpdate = apperr.Validation(
	"nothing to update: send mode, min_xof, max_xof, daily_cap_xof, periodic_weekday or periodic_min_xof")

// LES RÉGLAGES QUI REFUSENT TOUT.
//
// ⚠️⚠️ CHAQUE BORNE EST VALIDE SEULE ET LE TRIO PEUT ÊTRE IMPOSSIBLE. Un
// maximum de 1 000 sous un minimum de 5 000 passe les `validate:` un par un —
// et le pays n'a plus un seul montant retirable : tout ce qui est en dessous
// de 5 000 est « trop petit », tout ce qui est au-dessus est « trop grand ».
// L'agent reçoit les deux refus pour le même argent, ils se contredisent, et
// rien nulle part ne dit que c'est le réglage qui est faux.
//
// ⚠️⚠️ ET EN MODE PÉRIODIQUE C'EST PIRE, parce que personne ne voit de refus :
// le tour plafonne le montant au maximum (`RunPeriodic`) et `autoRequest` ne
// repasse pas par `CheckRequest`. Un maximum sous le minimum fait donc partir,
// en silence, des versements sous notre propre minimum — exactement ce que
// `defaultPayoutMin` existe pour empêcher, et chacun coûte en frais
// d'agrégateur plus qu'il ne transporte.
//
// ⚠️⚠️ LA VÉRIFICATION PORTE SUR LE RÉSULTAT FUSIONNÉ, JAMAIS SUR LA REQUÊTE.
// C'est le cœur de la chose : la console envoie les champs modifiés, et
// `{"min_xof": 5000}` seul suffit à rendre impossible un `max_xof: 1000` écrit
// trois mois plus tôt — sans que la requête le nomme. Une règle qui ne regarde
// que ce qu'on lui envoie laisse creuser le trou par le côté.
//
// ⚠️ `periodic_min_xof` SOUS LE MINIMUM N'EST PAS REFUSÉ, et c'est délibéré :
// `RunPeriodic` le relève lui-même au minimum. Ce n'est pas un réglage
// impossible, juste un réglage sans effet — et refuser tout ce qui est sans
// effet apprend à l'exploitation à se battre avec la console.
var (
	errPayoutMaxUnderMin = apperr.New("payout_max_under_min",
		"max_xof is below min_xof: no amount could ever be withdrawn",
		http.StatusUnprocessableEntity)
	errPayoutCapUnderMin = apperr.New("payout_cap_under_min",
		"daily_cap_xof is below min_xof: no amount could ever be withdrawn",
		http.StatusUnprocessableEntity)
)

// checkPayoutCoherence relit les bornes LES UNES CONTRE LES AUTRES.
//
// Elle reçoit la politique déjà complétée par ses défauts — c'est bien le
// minimum EFFECTIF qu'il faut comparer, pas le zéro qui le laisse deviner.
// Zéro sur un maximum ou un plafond veut dire « pas de borne » : ce n'est pas
// une incohérence, c'est l'absence de règle.
func checkPayoutCoherence(p PayoutResponse) error {
	// ⚠️⚠️ ÉTEINDRE NE SE REFUSE JAMAIS. Un pays éteint ne verse rien : ses
	// bornes ne peuvent casser personne, et il n'y a donc rien à protéger.
	//
	// Ce n'est pas une facilité, c'est la correction d'un piège que cette
	// vérification avait elle-même créé. Un pays enregistré AVANT elle peut
	// déjà porter un réglage impossible ; comme le contrôle porte sur le
	// résultat fusionné, le moindre `PUT` partiel — `{"mode":"off"}` compris —
	// repassait par le trio existant et se faisait refuser. L'exploitation se
	// retrouvait devant un pays qu'elle ne pouvait plus mettre en sécurité, et
	// la seule issue était de devenir cohérent d'abord, c'est-à-dire de
	// RÉPARER un pays avant de pouvoir l'ÉTEINDRE. Exactement l'inverse de
	// l'ordre dans lequel on veut agir quand quelque chose va mal.
	//
	// Le refus arrive donc au moment où il compte : quand on OUVRE les
	// retraits. C'est là qu'il y a quelqu'un pour le lire, et là que des
	// bornes impossibles commenceraient à coûter.
	if p.Mode == "" || p.Mode == "off" {
		return nil
	}
	if p.MaxXOF > 0 && p.MaxXOF < p.MinXOF {
		return errPayoutMaxUnderMin.WithMeta(map[string]any{
			"reason": payoutBoundsReason(p.MinXOF, p.MaxXOF),
		})
	}
	if p.DailyCapXOF > 0 && p.DailyCapXOF < p.MinXOF {
		return errPayoutCapUnderMin.WithMeta(map[string]any{
			"reason": payoutBoundsReason(p.MinXOF, p.DailyCapXOF),
		})
	}
	return nil
}

// payoutBoundsReason écrit les DEUX nombres.
//
// ⚠️ LES DEUX, parce que celui qui lit le refus vient d'en changer un seul :
// « le maximum est sous le minimum » sans les chiffres le renvoie chercher
// l'autre valeur ailleurs dans la console — et c'est souvent l'autre valeur
// qu'il faut corriger.
func payoutBoundsReason(min, other int) string {
	return fmt.Sprintf("min=%d, %d", min, other)
}

// payoutResponse complète les trous avec les défauts.
func payoutResponse(code string, p Payout) PayoutResponse {
	out := PayoutResponse{
		Mode: p.Mode, MinXOF: p.MinXOF, MaxXOF: p.MaxXOF,
		DailyCapXOF: p.DailyCapXOF, PeriodicWeekday: p.PeriodicWeekday,
		PeriodicMinXOF: p.PeriodicMinXOF,
	}
	if out.Mode == "" {
		out.Mode = "off"
	}
	if out.MinXOF <= 0 {
		out.MinXOF = defaultPayoutMin
	}
	if info, ok := country.Lookup(code); ok {
		out.Currency = info.Currency
	}
	return out
}

// PayoutPolicy rend la politique d'un pays — la console.
func (s *Service) PayoutPolicy(ctx context.Context, code string) (*PayoutResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := payoutResponse(info.Code, inst.Security.Payout)
	return &out, nil
}

// PayoutOf rend la politique EFFECTIVE — l'adaptateur que le module de retrait
// appelle.
//
// ⚠️ AU MIEUX : un pays inconnu ou une base muette rendent `off`. Le sens de
// l'échec compte ici plus qu'ailleurs — en cas de doute, on ne verse pas.
func (s *Service) PayoutOf(ctx context.Context, code string) PayoutResponse {
	info, ok := country.Lookup(code)
	if !ok {
		return PayoutResponse{Mode: "off", MinXOF: defaultPayoutMin}
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return PayoutResponse{Mode: "off", MinXOF: defaultPayoutMin, Currency: info.Currency}
	}
	return payoutResponse(info.Code, inst.Security.Payout)
}

// UpdatePayout règle la politique de retrait d'un pays.
func (s *Service) UpdatePayout(ctx context.Context, code string, req PayoutUpdateRequest) (*PayoutResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Mode == nil && req.MinXOF == nil && req.MaxXOF == nil &&
		req.DailyCapXOF == nil && req.PeriodicWeekday == nil && req.PeriodicMinXOF == nil {
		return nil, errNoPayoutUpdate
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	cur := inst.Security.Payout
	before := payoutResponse(info.Code, cur)
	if req.Mode != nil {
		cur.Mode = *req.Mode
	}
	if req.MinXOF != nil {
		cur.MinXOF = *req.MinXOF
	}
	if req.MaxXOF != nil {
		cur.MaxXOF = *req.MaxXOF
	}
	if req.DailyCapXOF != nil {
		cur.DailyCapXOF = *req.DailyCapXOF
	}
	if req.PeriodicWeekday != nil {
		cur.PeriodicWeekday = *req.PeriodicWeekday
	}
	if req.PeriodicMinXOF != nil {
		cur.PeriodicMinXOF = *req.PeriodicMinXOF
	}
	// ⚠️ RELU FUSIONNÉ ET AVANT L'ÉCRITURE : voir `checkPayoutCoherence`. Un
	// réglage impossible écrit puis corrigé laisse une fenêtre où le pays ne
	// verse plus rien, et personne ne la voit passer.
	merged := payoutResponse(info.Code, cur)
	if err := checkPayoutCoherence(merged); err != nil {
		return nil, err
	}
	sec := inst.Security
	sec.Payout = cur
	if err := s.repo.SetSecurity(ctx, info.Code, sec); err != nil {
		return nil, apperr.Internal(err)
	}
	out := merged
	// ⚠️ AU JOURNAL D'AUDIT. Ouvrir les retraits d'un pays, ou relever un
	// plafond, est exactement le genre de geste dont on veut savoir qui l'a
	// fait et quand — surtout le jour où de l'argent est sorti en trop.
	if s.audit != nil {
		s.audit.Record(ctx, "country.payout", "country", info.Code, before, out)
	}
	return &out, nil
}
