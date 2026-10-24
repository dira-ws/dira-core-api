package payroll

import (
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// L'ARITHMÉTIQUE DES PÉRIODES DE PAIE. Pas de stockage, pas d'horloge propre :
// `at` est toujours passé, donc chaque règle est éprouvable au jour près.
//
// ⚠️ UNE PÉRIODE NE DÉPEND PAS DU SALARIÉ, et c'est ce qui permet une paie
// COLLECTIVE. Ancrer les périodes sur la date d'embauche aurait donné à chacun
// son propre calendrier : vingt salariés, vingt périodes différentes, et aucune
// « paie du mois » à préparer. Les périodes sont donc celles du CALENDRIER
// (mois, semaine ISO) ou comptées depuis une origine fixe (quinzomadaire).

// biweeklyEpoch est l'origine des périodes quinzomadaires : LUNDI 5 janvier
// 1970, à minuit UTC.
//
// ⚠️ UNE ORIGINE FIXE, ET UN LUNDI. Fixe, parce qu'une période quinzomadaire
// n'existe pas dans le calendrier : il faut bien compter depuis quelque part,
// et compter depuis « aujourd'hui » aurait fait glisser les périodes à chaque
// déploiement. Un lundi, pour que la quinzaine contienne exactement deux
// semaines entières et que le début d'une période ne tombe jamais au milieu
// d'un week-end.
var biweeklyEpoch = time.Date(1970, 1, 5, 0, 0, 0, 0, time.UTC)

var (
	errUnknownFrequency = apperr.Validation("unknown pay frequency: expected monthly, biweekly or weekly")
	errPeriodOpen       = apperr.Conflict("payroll_period_open",
		"this pay period is not over yet: a payroll is prepared once the period is closed")
)

// startOfDay / startOfWeek en UTC — l'heure de référence de la plateforme.
// Le lundi ouvre la semaine (ISO) : `time.Weekday` met le dimanche à zéro, ce
// qui rangerait un dimanche dans la semaine suivante.
func startOfDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func startOfWeek(t time.Time) time.Time {
	d := startOfDay(t)
	wd := int(d.Weekday())
	if wd == 0 {
		wd = 7
	}
	return d.AddDate(0, 0, -(wd - 1))
}

// PeriodFor rend la période de paie qui CONTIENT `at`, pour une fréquence.
// `start` est inclusive, `end` exclusive.
func PeriodFor(frequency string, at time.Time) (start, end time.Time, err error) {
	switch frequency {
	case FrequencyMonthly:
		d := startOfDay(at)
		start = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	case FrequencyWeekly:
		start = startOfWeek(at)
		return start, start.AddDate(0, 0, 7), nil
	case FrequencyBiweekly:
		// Le nombre de quinzaines écoulées depuis l'origine. La division
		// entière d'un nombre de jours POSITIF suffit ; une date antérieure à
		// 1970 n'existe pas sur cette plateforme, et la borner ici ferait
		// croire à un cas traité.
		days := int(startOfDay(at).Sub(biweeklyEpoch).Hours() / 24)
		if days < 0 {
			days = 0
		}
		start = biweeklyEpoch.AddDate(0, 0, (days/14)*14)
		return start, start.AddDate(0, 0, 14), nil
	default:
		return time.Time{}, time.Time{}, errUnknownFrequency
	}
}

// PreviousPeriod rend la DERNIÈRE période CLOSE à l'instant `at` — celle
// qu'on prépare.
//
// ⚠️ C'EST LE DÉFAUT DE LA PRÉPARATION, et il évite la faute la plus facile :
// cliquer sur « Préparer » le 20 du mois et obtenir la paie du mois EN COURS,
// qui n'est pas finie — donc une paie à laquelle il manque dix jours de primes
// et de retenues, versée comme si elle était complète.
func PreviousPeriod(frequency string, at time.Time) (start, end time.Time, err error) {
	cur, _, err := PeriodFor(frequency, at)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	// La veille du début de la période courante tombe dans la précédente, quelle
	// que soit la fréquence — y compris en quinzomadaire, dont les périodes se
	// touchent sans trou.
	return PeriodFor(frequency, cur.AddDate(0, 0, -1))
}

// PayDateFor rend la date de versement d'une période : le premier jour dit À
// PARTIR DE la fin de la période.
//
// ⚠️ APRÈS LA PÉRIODE, JAMAIS DEDANS. On ne paie pas une période qu'on n'a pas
// finie de compter : une prime accordée le 29 ou une retenue posée le 30
// n'aurait aucune paie où aller. C'est aussi ce qui rend la date de versement
// calculable sans réglage supplémentaire — « le 5 » veut dire le 5 du mois
// suivant, et il n'y a pas deux lectures possibles.
func PayDateFor(e *Employee, periodEnd time.Time, s *Settings) time.Time {
	end := startOfDay(periodEnd)
	switch e.Frequency {
	case FrequencyMonthly:
		day := e.PayDay
		if day <= 0 {
			day = s.DefaultPayDay
		}
		day = min(max(day, 1), 28)
		// `end` est le 1er du mois suivant la période : le jour dit de CE
		// mois-là est donc toujours à partir de la fin de période.
		return time.Date(end.Year(), end.Month(), day, 0, 0, 0, 0, time.UTC)
	default:
		wd := e.PayWeekday
		if wd <= 0 {
			wd = s.DefaultPayWeekday
		}
		wd = min(max(wd, 1), 7)
		cur := int(end.Weekday())
		if cur == 0 {
			cur = 7
		}
		delta := wd - cur
		if delta < 0 {
			delta += 7
		}
		return end.AddDate(0, 0, delta)
	}
}

// checkFrequency refuse une fréquence inconnue et un jour de versement qui ne
// veut rien dire pour elle.
//
// ⚠️ LE SILENCE EST LE VRAI DANGER ICI. Un « hebdomadaire, versé le 15 »
// accepté sans broncher ne provoque aucune erreur : la paie part le vendredi,
// le 15 reste écrit sur la fiche, et quelqu'un le relit six mois plus tard en
// croyant que c'est la règle appliquée. Un champ qui ne commande rien mais qui
// s'affiche est pire qu'un champ absent.
func checkFrequency(frequency string, payDay, payWeekday int) error {
	switch frequency {
	case FrequencyMonthly:
		if payWeekday != 0 {
			return apperr.Validation("a monthly pay frequency has a day of month (pay_day), not a weekday")
		}
		if payDay < 0 || payDay > 28 {
			return apperr.Validation("pay_day must be between 1 and 28: a later day does not exist in every month")
		}
	case FrequencyWeekly, FrequencyBiweekly:
		if payDay != 0 {
			return apperr.Validation("a weekly or biweekly pay frequency has a weekday (pay_weekday), not a day of month")
		}
		if payWeekday < 0 || payWeekday > 7 {
			return apperr.Validation("pay_weekday must be between 1 (monday) and 7 (sunday)")
		}
	default:
		return errUnknownFrequency
	}
	return nil
}
