package payroll

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ⚠️ CES ÉPREUVES LISENT LE DOCUMENT ÉCRIT, pas la structure Go. `written`
// repasse la fiche par le même encodeur BSON que la base : une étiquette
// oubliée, un champ qui ne sort pas, un montant rangé sous un autre nom se
// voient ici. La plateforme s'est fait prendre trois fois par des épreuves qui
// comparaient deux structures pendant que le fil portait autre chose.
func written(t *testing.T, v any) bson.M {
	t.Helper()
	raw, err := bson.Marshal(v)
	require.NoError(t, err)
	var out bson.M
	require.NoError(t, bson.Unmarshal(raw, &out))
	return out
}

// employee rend un salarié de GABON (pays d'essai) au salaire donné.
func employee(base int) *Employee {
	return &Employee{
		ID: primitive.NewObjectID(), Country: "GA", FullName: "Salarié d'essai",
		Position: "Agent de comptoir", Status: StatusActive, BaseSalary: base,
		Frequency: FrequencyMonthly, PayDay: 28, HiredAt: day(2025, 1, 1),
	}
}

func adjustment(kind string, amount, percent int, at time.Time) Adjustment {
	return Adjustment{
		ID: primitive.NewObjectID(), Country: "GA", Kind: kind, Amount: amount,
		Percent: percent, Reason: "motif d'essai", Installments: 1,
		Status: AdjustmentOpen, GrantedBy: "operateur", CreatedAt: at,
	}
}

var march = day(2026, 4, 1) // la fin (exclusive) de la période de mars

// ⚠️ UNE RETENUE NE VIDE PAS UNE PAIE. C'est LA borne du module : une retenue
// légitime, appliquée en entier, laisse quelqu'un sans rien — et quelqu'un
// sans rien ne revient pas travailler le mois suivant.
//
// Le reste n'est pas effacé : il est REPORTÉ, et la fiche le dit.
func TestADeductionNeverEmptiesAPay(t *testing.T) {
	set := DefaultSettings("GA") // 30 % au plus
	e := employee(200_000)
	a := adjustment(KindDeduction, 150_000, 0, day(2026, 3, 2))

	slip, applied := buildPayslip(e, []Adjustment{a}, &set, march)
	doc := written(t, slip)

	assert.EqualValues(t, 200_000, doc["gross"])
	assert.EqualValues(t, 60_000, doc["deductions"], "30 % de 200 000, et pas un franc de plus")
	assert.EqualValues(t, 140_000, doc["net"])
	assert.EqualValues(t, 140_000, doc["floor"], "le net ne descend pas sous le plancher")
	assert.EqualValues(t, 90_000, doc["carried"], "ce qui n'a pas pu être retenu reste dû")

	// ⚠️ LA LIGNE PORTE LES DEUX MONTANTS. Sans `planned`, la borne serait
	// invisible : la personne lirait « retenue : 60 000 » sans savoir qu'on en
	// visait 150 000, et le reste reviendrait le mois suivant sans explication.
	lines, ok := doc["lines"].(bson.A)
	require.True(t, ok, "les lignes doivent sortir sous `lines`")
	require.Len(t, lines, 1)
	line := lines[0].(bson.M)
	assert.EqualValues(t, 150_000, line["planned"])
	assert.EqualValues(t, 60_000, line["amount"])
	assert.Equal(t, "motif d'essai", line["reason"], "une retenue sans motif n'est pas défendable")

	// Et l'ajustement n'est PAS soldé : il reste 90 000 à prendre.
	require.Len(t, applied, 1)
	assert.False(t, applied[0].Settled)
	assert.Equal(t, 60_000, applied[0].Amount)
}

// ⚠️ LA PLUS PROTECTRICE DES DEUX BORNES GAGNE. Le pourcentage seul laisse
// « 70 % de presque rien » à un petit salaire ; le montant seul ne dit rien
// d'un gros. Leur somme aurait interdit toute retenue, leur minimum aurait
// rendu la seconde inutile.
func TestTheFloorIsTheMostProtectiveOfTheTwoBounds(t *testing.T) {
	// Pourcentage plus protecteur que le montant absolu.
	pct := Settings{MaxDeductionPercent: 30, MinNet: 50_000}
	assert.Equal(t, 140_000, floorOf(200_000, &pct))

	// Montant absolu plus protecteur que le pourcentage.
	absolute := Settings{MaxDeductionPercent: 30, MinNet: 80_000}
	assert.Equal(t, 80_000, floorOf(100_000, &absolute), "70 000 laisserait moins que le minimum promis")

	// ⚠️ Un plancher au-dessus du brut ne rend personne créditeur : il rend
	// seulement toute retenue impossible.
	high := Settings{MaxDeductionPercent: 30, MinNet: 500_000}
	assert.Equal(t, 40_000, floorOf(40_000, &high))
	assert.Equal(t, 0, maxDeductibleShare(40_000, &high))
}

// Un échéancier fait EXACTEMENT le total : la dernière échéance absorbe le
// reste de la division. Sans cela, 100 000 sur trois paies auraient retenu
// 99 999 et laissé un franc dû pour toujours.
func TestInstalmentsAddUpToTheTotal(t *testing.T) {
	set := Settings{MaxDeductionPercent: 100}
	e := employee(500_000)
	a := adjustment(KindDeduction, 100_000, 0, day(2026, 1, 2))
	a.Installments = 3

	total, shares := 0, []int{}
	for range 3 {
		slip, applied := buildPayslip(e, []Adjustment{a}, &set, march)
		require.Len(t, applied, 1, "l'échéance %d doit passer", a.Runs+1)
		shares = append(shares, slip.Deductions)
		total += slip.Deductions
		// Ce que la base inscrirait après le versement.
		a.Taken += applied[0].Amount
		a.Runs++
		if applied[0].Settled {
			a.Status = AdjustmentSettled
		}
	}
	assert.Equal(t, 100_000, total, "les échéances doivent faire le total, au franc")
	assert.Equal(t, []int{33_333, 33_333, 33_334}, shares, "le reste tombe sur la DERNIÈRE")
	assert.Equal(t, AdjustmentSettled, a.Status, "la retenue doit se solder d'elle-même")

	// Et une quatrième paie ne reprend rien.
	slip, applied := buildPayslip(e, []Adjustment{a}, &set, march)
	assert.Empty(t, applied)
	assert.Equal(t, 0, slip.Deductions)
}

// ⚠️ UNE ÉCHÉANCE QUI TOMBE À ZÉRO NE BLOQUE PAS LA RETENUE. Deux francs
// étalés sur trois paies font zéro par division entière — et comme rien n'est
// passé, le compteur d'échéances ne bouge pas : la retenue reviendrait
// indéfiniment en ne prenant rien, « en cours » pour toujours, et la dette
// s'effacerait d'elle-même sans que personne ne l'ait décidé.
func TestAnInstalmentThatRoundsToZeroDoesNotStallForever(t *testing.T) {
	set := Settings{MaxDeductionPercent: 100}
	e := employee(150_000)
	a := adjustment(KindDeduction, 2, 0, day(2026, 1, 2))
	a.Installments = 3

	slip, applied := buildPayslip(e, []Adjustment{a}, &set, march)
	require.Len(t, applied, 1, "la retenue doit passer, pas attendre un tour pour rien")
	assert.Equal(t, 2, slip.Deductions, "le reste est pris tout de suite")
	assert.True(t, applied[0].Settled, "et la retenue est soldée : elle ne doit pas revenir")
}

// ⚠️ LES DEUX POURCENTAGES NE PORTENT PAS SUR LA MÊME BASE. Une PRIME est une
// part de RÉMUNÉRATION (le salaire de base) ; une RETENUE porte sur le BRUT
// effectivement dû — sinon elle pourrait dépasser ce qu'il y a à retenir.
func TestAPercentBonusIsOnTheSalaryAndAPercentDeductionOnTheGross(t *testing.T) {
	set := Settings{MaxDeductionPercent: 100}
	e := employee(100_000)
	bonus := adjustment(KindBonus, 0, 10, day(2026, 3, 1))
	deduction := adjustment(KindDeduction, 0, 10, day(2026, 3, 2))

	slip := written(t, mustSlip(buildPayslip(e, []Adjustment{bonus, deduction}, &set, march)))
	assert.EqualValues(t, 10_000, slip["bonuses"], "10 % du SALAIRE")
	assert.EqualValues(t, 110_000, slip["gross"])
	assert.EqualValues(t, 11_000, slip["deductions"], "10 % du BRUT, primes comprises")
	assert.EqualValues(t, 99_000, slip["net"])
}

// ⚠️ UNE RETENUE S'APPLIQUE AUSSI AUX PRIMES, et c'est voulu : une retenue qui
// ne mordrait que sur la base aurait laissé intacte une prime de 200 000 F
// pendant qu'on dit à la personne qu'elle doit de l'argent.
func TestABonusWidensWhatCanBeDeducted(t *testing.T) {
	set := DefaultSettings("GA")
	e := employee(100_000)
	debt := adjustment(KindDeduction, 100_000, 0, day(2026, 3, 1))

	withoutBonus, _ := buildPayslip(e, []Adjustment{debt}, &set, march)
	assert.Equal(t, 30_000, withoutBonus.Deductions)

	bonus := adjustment(KindBonus, 100_000, 0, day(2026, 3, 2))
	withBonus, _ := buildPayslip(e, []Adjustment{debt, bonus}, &set, march)
	assert.Equal(t, 60_000, withBonus.Deductions, "30 % de 200 000 : la prime élargit la place")
	assert.Equal(t, 140_000, withBonus.Net)
}

// Les retenues passent de la PLUS ANCIENNE à la plus récente, comme les
// échéances du matériel. Sans cet ordre, la retenue bornée serait celle qui se
// trouve en dernier dans le JSON — c'est-à-dire n'importe laquelle.
func TestTheOldestDeductionIsServedFirst(t *testing.T) {
	set := DefaultSettings("GA") // 30 % de 100 000 = 30 000
	e := employee(100_000)
	old := adjustment(KindDeduction, 25_000, 0, day(2026, 1, 5))
	old.Reason = "la plus ancienne"
	recent := adjustment(KindDeduction, 25_000, 0, day(2026, 3, 5))
	recent.Reason = "la plus récente"

	// Passées dans le DÉSORDRE : c'est la date qui décide, pas l'argument.
	slip, _ := buildPayslip(e, []Adjustment{recent, old}, &set, march)
	require.Len(t, slip.Lines, 2)
	assert.Equal(t, "la plus ancienne", slip.Lines[0].Reason)
	assert.Equal(t, 25_000, slip.Lines[0].Amount, "l'ancienne passe en entier")
	assert.Equal(t, 5_000, slip.Lines[1].Amount, "la récente prend ce qui reste de la place")
	assert.Equal(t, 20_000, slip.Carried)
}

// Un ajustement RÉCURRENT ne se solde jamais : il n'a pas de total, seulement
// une cadence. Le clore au premier passage aurait fait d'une prime de transport
// une prime unique — et personne ne l'aurait vu avant le mois suivant.
func TestARecurringAdjustmentNeverSettles(t *testing.T) {
	set := DefaultSettings("GA")
	e := employee(100_000)
	a := adjustment(KindBonus, 15_000, 0, day(2026, 1, 1))
	a.Recurring, a.Installments = true, 0

	for range 3 {
		slip, applied := buildPayslip(e, []Adjustment{a}, &set, march)
		require.Len(t, applied, 1)
		assert.Equal(t, 15_000, slip.Bonuses)
		assert.False(t, applied[0].Settled, "un récurrent n'a pas de fin")
		a.Taken += applied[0].Amount
		a.Runs++
	}
}

// Une date d'effet postérieure à la période attend la paie suivante. ⚠️ Elle
// se compare à la FIN DE PÉRIODE, pas au jour où l'on prépare : une prime
// accordée le 28 pour le mois qui s'achève doit entrer dans la paie de ce
// mois-là, et non dépendre de l'heure à laquelle quelqu'un a cliqué.
func TestAnAdjustmentWaitsForItsEffectiveDate(t *testing.T) {
	set := DefaultSettings("GA")
	e := employee(100_000)
	later := adjustment(KindBonus, 10_000, 0, day(2026, 3, 1))
	later.EffectiveFrom = day(2026, 5, 1)

	slip, applied := buildPayslip(e, []Adjustment{later}, &set, march)
	assert.Empty(t, applied)
	assert.Equal(t, 0, slip.Bonuses)

	onTime := adjustment(KindBonus, 10_000, 0, day(2026, 3, 1))
	onTime.EffectiveFrom = day(2026, 3, 28)
	slip, applied = buildPayslip(e, []Adjustment{onTime}, &set, march)
	require.Len(t, applied, 1)
	assert.Equal(t, 10_000, slip.Bonuses)
}

// Les totaux d'une paie sont RECALCULÉS depuis ses fiches : une somme tenue à
// jour à la main finit par ne plus faire la somme.
func TestARunTotalsItsOwnSlips(t *testing.T) {
	run := &Run{Slips: []Payslip{
		{Base: 100_000, Bonuses: 10_000, Gross: 110_000, Deductions: 10_000, Net: 100_000, Carried: 5_000},
		{Base: 50_000, Gross: 50_000, Net: 50_000},
	}}
	run.totals()
	doc := written(t, run)
	assert.EqualValues(t, 160_000, doc["gross"])
	assert.EqualValues(t, 10_000, doc["bonuses"])
	assert.EqualValues(t, 10_000, doc["deductions"])
	assert.EqualValues(t, 150_000, doc["net"])
	assert.EqualValues(t, 5_000, doc["carried"])
	assert.EqualValues(t, 2, doc["headcount"])
	assert.EqualValues(t, doc["gross"].(int32)-doc["deductions"].(int32), doc["net"],
		"le net d'une paie doit faire le brut moins les retenues : une pièce dont le détail ne fait pas le total est fausse")
}

func mustSlip(slip Payslip, _ []Applied) Payslip { return slip }

// ⚠️⚠️ UNE RETENUE BORNÉE À ZÉRO NE SE REFERME PAS, ET C'ÉTAIT UN DÉFAUT.
//
// Une retenue en POURCENTAGE s'épuise au nombre d'échéances déclaré. Quand le
// plancher ne laisse aucune place — parce qu'une retenue plus ancienne a pris
// toute la marge —, elle passait quand même pour consommée : la fiche disait
// « reporté », l'ajustement se soldait, et plus aucune paie ne revenait le
// chercher. L'entreprise perdait la retenue sans qu'une ligne ne le dise.
func TestADeductionThatTookNothingIsNotSettled(t *testing.T) {
	set := DefaultSettings("GA") // 30 % au plus → 30 000 de place sur 100 000
	e := employee(100_000)
	older := adjustment(KindDeduction, 30_000, 0, day(2026, 3, 2)) // prend toute la place
	percent := adjustment(KindDeduction, 0, 10, day(2026, 3, 3))   // 10 000 visés, rien de disponible

	slip, applied := buildPayslip(e, []Adjustment{older, percent}, &set, march)

	require.Len(t, applied, 2)
	assert.EqualValues(t, 30_000, slip.Deductions)
	assert.EqualValues(t, 10_000, slip.Carried, "les 10 000 sont reportés")

	var floored Applied
	for _, ap := range applied {
		if ap.AdjustmentID == percent.ID {
			floored = ap
		}
	}
	assert.Equal(t, 10_000, floored.Planned)
	assert.Zero(t, floored.Amount, "le plancher n'a rien laissé passer")
	assert.False(t, floored.Settled, "une retenue qui n'a rien pris ne se solde pas")

	// Et elle revient sur la paie suivante : ce que `ConsumeAdjustments` écrit
	// d'un passage à zéro ne touche ni le compteur d'échéances ni le statut.
	percent.Taken += floored.Amount
	assert.True(t, eligible(&percent, day(2026, 5, 1)), "la retenue reportée revient")
}

// Et le compteur d'échéances ne bouge pas non plus : c'est lui qui épuise une
// retenue en pourcentage, et le document écrit est ce qui compte.
func TestAZeroPassDoesNotBurnAnInstalment(t *testing.T) {
	inc := incOf(Applied{AdjustmentID: primitive.NewObjectID(), Planned: 10_000, Amount: 0})
	assert.NotContains(t, inc, "runs", "rien n'a été pris : aucune échéance consommée")
	assert.EqualValues(t, 0, inc["taken"])

	inc = incOf(Applied{AdjustmentID: primitive.NewObjectID(), Planned: 10_000, Amount: 4_000})
	assert.EqualValues(t, 1, inc["runs"])
	assert.EqualValues(t, 4_000, inc["taken"])
}
