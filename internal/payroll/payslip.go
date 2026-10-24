package payroll

import (
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LE CALCUL D'UNE FICHE DE PAIE. Arithmétique pure : des entiers, aucune
// horloge, aucune base — c'est ce qui permet d'éprouver les bornes au franc
// près, et c'est là que vivent les deux règles qui comptent.
//
//	1. ce qu'on ajoute : le salaire de base, puis les primes
//	2. ce qu'on retire : les retenues, JAMAIS en dessous du plancher
//
// ⚠️ L'ORDRE N'EST PAS DÉCORATIF. Les retenues se calculent sur le BRUT (base
// + primes) : une retenue qui ne mordrait que sur la base aurait laissé
// intacte une prime de 200 000 F pendant qu'on dit à la personne qu'elle doit
// de l'argent.

// Applied dit, pour un ajustement, ce que la paie en a réellement fait. Le
// service s'en sert pour écrire la suite (`Taken`, `Runs`, `settled`) : la
// partie pure ne touche pas la base.
type Applied struct {
	AdjustmentID primitive.ObjectID
	// Planned : ce que l'ajustement visait sur cette paie ; Amount : ce qui est
	// passé. Les deux diffèrent quand le plancher a mordu.
	Planned int
	Amount  int
	// Settled : ce passage solde l'ajustement (montant total atteint).
	Settled bool
}

// floorOf rend le plancher d'une paie : ce en dessous de quoi le net ne
// descend pas.
//
// ⚠️ LES DEUX BORNES SE CUMULENT ET LA PLUS PROTECTRICE GAGNE. Le pourcentage
// seul laisse « 70 % de presque rien » à un petit salaire ; le montant seul ne
// dit rien d'un gros. Prendre le maximum des deux est la seule combinaison qui
// protège dans les deux sens — leur somme aurait interdit toute retenue, leur
// minimum aurait rendu la seconde borne inutile.
//
// ⚠️ ET LE PLANCHER NE DÉPASSE JAMAIS LE BRUT. Un plancher de 100 000 F sur
// une paie de 40 000 F ne rend pas la personne créditrice : il rend seulement
// toute retenue impossible.
func floorOf(gross int, s *Settings) int {
	pct := s.MaxDeductionPercent
	pct = min(max(pct, 0), 100)
	floor := gross * (100 - pct) / 100
	if s.MinNet > floor {
		floor = s.MinNet
	}
	if floor > gross {
		floor = gross
	}
	if floor < 0 {
		floor = 0
	}
	return floor
}

// plannedShare rend ce qu'un ajustement VISE sur cette paie.
//
// `base` sert aux primes en pourcentage, `gross` aux retenues en pourcentage —
// voir `Adjustment.Percent` : une prime est une part de RÉMUNÉRATION, une
// retenue une part de ce qui est effectivement dû.
func plannedShare(a *Adjustment, base, gross int) int {
	if a.Percent > 0 {
		on := gross
		if a.Kind == KindBonus {
			on = base
		}
		// L'arrondi entier va vers le bas des deux côtés. Un franc n'a pas de
		// centième dans la zone CFA, et un arrondi dont le sens dépendrait de
		// la nature de la ligne serait une règle de plus à se rappeler le jour
		// d'un litige.
		return on * a.Percent / 100
	}
	if a.Recurring {
		return a.Amount
	}
	// Un montant total étalé : ce qui reste, divisé par les échéances qui
	// restent. La dernière absorbe le reste de la division — comme
	// l'échéancier du matériel —, et la règle se corrige d'elle-même quand une
	// paie a passé moins que prévu.
	left := a.Installments - a.Runs
	if left < 1 {
		left = 1
	}
	remaining := a.Remaining()
	if left == 1 {
		return remaining
	}
	share := remaining / left
	// ⚠️ UNE ÉCHÉANCE QUI TOMBE À ZÉRO NE PROGRESSERAIT JAMAIS. Deux francs
	// étalés sur trois paies font zéro par division entière — et comme rien
	// n'est passé, le compteur d'échéances ne bouge pas : la retenue
	// reviendrait indéfiniment en ne prenant rien, « en cours » pour toujours.
	// On prend donc le reste tout de suite. C'est moins d'une unité de monnaie
	// par période, et le plancher borne cette prise comme les autres.
	if share == 0 {
		return remaining
	}
	return share
}

// settledAfter dit si ce passage SOLDE l'ajustement — il n'a plus rien à
// donner ni à prendre.
//
// ⚠️ UN RÉCURRENT NE SE SOLDE JAMAIS : il n'a pas de total, seulement une
// cadence. Le clore au premier passage aurait fait d'une prime de transport une
// prime unique, et personne ne l'aurait vu avant le mois suivant.
//
// ⚠️ ET UNE RETENUE BORNÉE PAR LE PLANCHER NE SE SOLDE PAS NON PLUS : c'est ce
// qui la fait revenir sur la paie suivante avec ce qui reste (`Carried`).
func settledAfter(a *Adjustment, take int) bool {
	if a.Recurring {
		return false
	}
	// ⚠️⚠️ UNE LIGNE QUI N'A RIEN PRIS NE SOLDE RIEN, ET C'ÉTAIT UN DÉFAUT
	// D'ARGENT. Une retenue en POURCENTAGE se solde au nombre d'échéances
	// déclaré, sans regarder ce qu'elle a réellement pris : bornée à zéro par
	// le plancher, elle se refermait en ayant tout reporté sur une fiche que
	// plus aucune paie n'allait lire. La personne lisait « reporté : 10 000 »
	// et ne devait plus rien — ce qui est la bonne nouvelle du mauvais côté :
	// c'est l'entreprise qui perdait la retenue, en silence, et le mot
	// « reporté » devenait faux.
	if take <= 0 {
		return false
	}
	if a.Percent > 0 {
		// Un pourcentage n'a pas de total : il s'épuise au nombre de paies
		// déclaré.
		return a.Runs+1 >= max(a.Installments, 1)
	}
	return a.Amount > 0 && a.Taken+take >= a.Amount
}

// eligible dit si un ajustement a sa place sur une paie dont la période se
// termine à `periodEnd`.
//
// ⚠️ LA DATE D'EFFET SE COMPARE À LA FIN DE PÉRIODE, pas au jour où l'on
// prépare. Une prime accordée le 28 pour le mois qui vient de s'achever doit
// entrer dans la paie de ce mois-là ; la comparer à « maintenant » l'aurait
// fait entrer ou non selon l'heure à laquelle quelqu'un a cliqué.
func eligible(a *Adjustment, periodEnd time.Time) bool {
	if a.Status != AdjustmentOpen {
		return false
	}
	if !a.EffectiveFrom.IsZero() && a.EffectiveFrom.After(periodEnd) {
		return false
	}
	if a.Recurring {
		return true
	}
	if a.Percent > 0 {
		// Un pourcentage non récurrent passe sur autant de paies que déclaré.
		return a.Runs < max(a.Installments, 1)
	}
	return a.Remaining() > 0
}

// buildPayslip calcule la fiche d'un salarié et ce que chaque ajustement y a
// laissé.
//
// Les ajustements sont passés du PLUS ANCIEN au plus récent : une retenue
// posée en janvier se sert avant celle de mars, comme les échéances du
// matériel. Sans cet ordre, la retenue bornée serait celle qui se trouve en
// dernier dans le JSON — c'est-à-dire n'importe laquelle.
func buildPayslip(e *Employee, adjustments []Adjustment, s *Settings, periodEnd time.Time) (Payslip, []Applied) {
	slip := Payslip{
		EmployeeID: e.ID,
		Name:       e.FullName,
		Position:   e.Position,
		Base:       e.BaseSalary,
		Lines:      []SlipLine{},
	}

	adjs := make([]Adjustment, 0, len(adjustments))
	for _, a := range adjustments {
		if eligible(&a, periodEnd) {
			adjs = append(adjs, a)
		}
	}
	sort.SliceStable(adjs, func(i, j int) bool { return adjs[i].CreatedAt.Before(adjs[j].CreatedAt) })

	applied := make([]Applied, 0, len(adjs))

	// 1. LES PRIMES. Elles ne sont pas bornées : une prime n'appauvrit
	// personne, et un plafond de prime serait une règle de budget, pas une
	// protection.
	for i := range adjs {
		a := &adjs[i]
		if a.Kind != KindBonus {
			continue
		}
		take := plannedShare(a, e.BaseSalary, 0)
		if take <= 0 {
			continue
		}
		slip.Bonuses += take
		slip.Lines = append(slip.Lines, SlipLine{
			AdjustmentID: a.ID, Kind: KindBonus, Reason: a.Reason,
			Planned: take, Amount: take, GrantedBy: a.GrantedBy,
		})
		applied = append(applied, Applied{
			AdjustmentID: a.ID, Planned: take, Amount: take, Settled: settledAfter(a, take),
		})
	}

	slip.Gross = slip.Base + slip.Bonuses
	slip.Floor = floorOf(slip.Gross, s)
	// La place qui reste pour TOUTES les retenues de cette paie.
	room := slip.Gross - slip.Floor

	// 2. LES RETENUES, dans la limite de cette place.
	for i := range adjs {
		a := &adjs[i]
		if a.Kind != KindDeduction {
			continue
		}
		planned := plannedShare(a, e.BaseSalary, slip.Gross)
		if planned <= 0 {
			continue
		}
		take := min(planned, room)
		if take < 0 {
			take = 0
		}
		room -= take
		slip.Deductions += take
		slip.Carried += planned - take
		slip.Lines = append(slip.Lines, SlipLine{
			AdjustmentID: a.ID, Kind: KindDeduction, Reason: a.Reason,
			Planned: planned, Amount: take, GrantedBy: a.GrantedBy,
		})
		applied = append(applied, Applied{
			AdjustmentID: a.ID, Planned: planned, Amount: take, Settled: settledAfter(a, take),
		})
	}

	slip.Net = slip.Gross - slip.Deductions
	return slip, applied
}

// totals recalcule les totaux d'une paie depuis ses fiches. Une somme tenue à
// jour à la main à chaque ajout finit par ne plus faire la somme — c'est
// exactement ce que le module des reçus refuse (`pkg/receipt`).
func (r *Run) totals() {
	r.Gross, r.Bonuses, r.Deductions, r.Net, r.Carried, r.Headcount = 0, 0, 0, 0, 0, len(r.Slips)
	for i := range r.Slips {
		s := &r.Slips[i]
		r.Gross += s.Gross
		r.Bonuses += s.Bonuses
		r.Deductions += s.Deductions
		r.Net += s.Net
		r.Carried += s.Carried
	}
}

// maxDeductibleShare rend ce qu'UNE paie peut au plus retenir sur un brut de
// `gross` — ce que la console affiche, et ce que la validation d'une retenue
// compare à son échéance.
func maxDeductibleShare(gross int, s *Settings) int {
	return gross - floorOf(gross, s)
}
