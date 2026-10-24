package payroll

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// La période mensuelle est le MOIS CALENDAIRE, et sa borne de fin est le
// premier instant du mois suivant.
func TestAMonthlyPeriodIsTheCalendarMonth(t *testing.T) {
	start, end, err := PeriodFor(FrequencyMonthly, time.Date(2026, 3, 17, 22, 30, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, day(2026, 3, 1), start)
	assert.Equal(t, day(2026, 4, 1), end, "la borne de fin est EXCLUSIVE : le 1er avril, pas le 31 mars")
}

// ⚠️ LA SEMAINE COMMENCE LE LUNDI. `time.Weekday` met le dimanche à ZÉRO : un
// calcul naïf rangerait le dimanche dans la semaine suivante, et la paie de
// dimanche partirait avec celle de la semaine d'après.
func TestAWeeklyPeriodStartsOnMondayEvenOnASunday(t *testing.T) {
	// Dimanche 15 mars 2026.
	sunday := day(2026, 3, 15)
	require.Equal(t, time.Sunday, sunday.Weekday(), "la date d'épreuve doit bien être un dimanche")

	start, end, err := PeriodFor(FrequencyWeekly, sunday)
	require.NoError(t, err)
	assert.Equal(t, day(2026, 3, 9), start, "le lundi qui précède")
	assert.Equal(t, day(2026, 3, 16), end)
}

// ⚠️ LES QUINZAINES SONT LES MÊMES POUR TOUT LE MONDE. C'est ce qui permet une
// paie COLLECTIVE : ancrées sur la date d'embauche, vingt salariés auraient eu
// vingt calendriers, et « la paie de la quinzaine » n'aurait désigné personne.
func TestBiweeklyPeriodsAreTheSameForEveryone(t *testing.T) {
	a1, a2, err := PeriodFor(FrequencyBiweekly, day(2026, 3, 2))
	require.NoError(t, err)
	// Un autre jour DANS la même quinzaine rend exactement la même période.
	b1, b2, err := PeriodFor(FrequencyBiweekly, day(2026, 3, 12))
	require.NoError(t, err)
	assert.Equal(t, a1, b1)
	assert.Equal(t, a2, b2)

	// Quatorze jours de longueur, et un lundi au départ.
	assert.Equal(t, 14*24*time.Hour, a2.Sub(a1))
	assert.Equal(t, time.Monday, a1.Weekday())

	// La quinzaine suivante commence là où la précédente s'arrête : ni trou ni
	// chevauchement, ce qui est toute la raison d'une borne exclusive.
	c1, _, err := PeriodFor(FrequencyBiweekly, a2)
	require.NoError(t, err)
	assert.Equal(t, a2, c1)
}

// La période préparée par défaut est la DERNIÈRE CLOSE, jamais celle en cours.
func TestThePreparedPeriodIsTheLastClosedOne(t *testing.T) {
	start, end, err := PreviousPeriod(FrequencyMonthly, time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, day(2026, 2, 1), start, "le 20 mars, c'est février qu'on prépare")
	assert.Equal(t, day(2026, 3, 1), end)

	ws, we, err := PreviousPeriod(FrequencyWeekly, day(2026, 3, 11))
	require.NoError(t, err)
	assert.Equal(t, day(2026, 3, 2), ws)
	assert.Equal(t, day(2026, 3, 9), we)
}

// ⚠️ LE VERSEMENT TOMBE APRÈS LA PÉRIODE, JAMAIS DEDANS. « Le 5 » veut dire le
// 5 du mois suivant : sans cette règle, une prime accordée le 29 n'aurait
// aucune paie où aller, et il y aurait deux lectures possibles de la même
// consigne.
func TestThePayDateFallsAfterThePeriod(t *testing.T) {
	set := DefaultSettings("GA")
	_, end, err := PeriodFor(FrequencyMonthly, day(2026, 3, 10))
	require.NoError(t, err)

	e := &Employee{Frequency: FrequencyMonthly, PayDay: 5}
	assert.Equal(t, day(2026, 4, 5), PayDateFor(e, end, &set))
	assert.False(t, PayDateFor(e, end, &set).Before(end), "une date de versement dans la période payerait un mois qu'on n'a pas fini de compter")

	// Sans jour sur la fiche, celui du PAYS.
	assert.Equal(t, day(2026, 4, 28), PayDateFor(&Employee{Frequency: FrequencyMonthly}, end, &set))
}

// ⚠️ LE JOUR DU MOIS EST BORNÉ À 28 — une borne de calendrier, pas une
// préférence. Un « versement le 31 » glisse au 1er du mois suivant en février,
// et une paie qui change de mois toute seule n'appartient plus à sa période.
func TestAPayDayBeyondTheShortestMonthIsBroughtBack(t *testing.T) {
	set := DefaultSettings("GA")
	// Février 2026 : la période va du 1er au 28 inclus, la fin exclusive est le
	// 1er mars.
	_, end, err := PeriodFor(FrequencyMonthly, day(2026, 2, 10))
	require.NoError(t, err)

	got := PayDateFor(&Employee{Frequency: FrequencyMonthly, PayDay: 31}, end, &set)
	assert.Equal(t, day(2026, 3, 28), got, "le 31 mars n'existe pas comme jour de paie : on retombe sur le 28")
	assert.Equal(t, time.March, got.Month(), "le versement ne doit pas glisser sur un autre mois")
}

// Pour une paie hebdomadaire, le versement est le PREMIER jour dit à partir de
// la fin de période.
func TestTheWeeklyPayDateIsTheFirstGivenWeekday(t *testing.T) {
	set := DefaultSettings("GA")
	_, end, err := PeriodFor(FrequencyWeekly, day(2026, 3, 4))
	require.NoError(t, err)
	require.Equal(t, day(2026, 3, 9), end, "la semaine du 2 mars se ferme le lundi 9")

	// Vendredi (5) : le vendredi qui suit la fermeture.
	friday := PayDateFor(&Employee{Frequency: FrequencyWeekly, PayWeekday: 5}, end, &set)
	assert.Equal(t, day(2026, 3, 13), friday)
	assert.Equal(t, time.Friday, friday.Weekday())

	// Lundi (1) : le jour même de la fermeture, et pas une semaine plus tard —
	// « à partir de » inclut le jour de la borne.
	monday := PayDateFor(&Employee{Frequency: FrequencyWeekly, PayWeekday: 1}, end, &set)
	assert.Equal(t, day(2026, 3, 9), monday)
}

// ⚠️ UNE FRÉQUENCE ET UN JOUR QUI NE VONT PAS ENSEMBLE SONT REFUSÉS. Le
// silence est le vrai danger : « hebdomadaire, versé le 15 » ne provoque
// aucune erreur — la paie part le vendredi, le 15 reste écrit sur la fiche, et
// quelqu'un le relit six mois plus tard en croyant que c'est la règle
// appliquée.
func TestAnInconsistentFrequencyIsRefused(t *testing.T) {
	// Un jour du mois sur une paie hebdomadaire : refusé.
	require.Error(t, checkFrequency(FrequencyWeekly, 15, 0))
	require.Error(t, checkFrequency(FrequencyBiweekly, 15, 0))
	// Un jour de semaine sur une paie mensuelle : refusé.
	require.Error(t, checkFrequency(FrequencyMonthly, 0, 5))
	// Un jour du mois qui n'existe pas partout : refusé.
	require.Error(t, checkFrequency(FrequencyMonthly, 31, 0))
	// Une fréquence inconnue : refusée.
	require.Error(t, checkFrequency("quarterly", 0, 0))

	// Et ce qui va ensemble passe.
	require.NoError(t, checkFrequency(FrequencyMonthly, 28, 0))
	require.NoError(t, checkFrequency(FrequencyWeekly, 0, 5))
	require.NoError(t, checkFrequency(FrequencyBiweekly, 0, 1))
	// Rien de posé : le défaut du pays s'appliquera.
	require.NoError(t, checkFrequency(FrequencyMonthly, 0, 0))
}
