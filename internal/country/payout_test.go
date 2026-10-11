package country

import (
	"errors"
	"testing"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codeOf et reasonOf lisent le refus comme l'écran le lira : par son CODE, et
// par le `reason` que la réponse porte à côté de la phrase — jamais par le
// texte, qui est traduit.
func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *apperr.Error
	require.True(t, errors.As(err, &ae), "le refus doit être codé")
	return ae.Code
}

func reasonOf(t *testing.T, err error) any {
	t.Helper()
	var ae *apperr.Error
	require.True(t, errors.As(err, &ae))
	return ae.Meta["reason"]
}

// LES BORNES DE RETRAIT SE JUGENT ENSEMBLE, JAMAIS UNE PAR UNE.
//
// ⚠️⚠️ CHAQUE BORNE EST VALIDE SEULE ET LE TRIO PEUT ÊTRE IMPOSSIBLE. C'est
// tout l'objet de ces tests : les `validate:` du DTO voient un minimum
// plausible et un maximum plausible, et laissent passer un pays où plus rien
// ne peut sortir.

// Un pays non réglé est éteint, et son minimum est déjà là — une politique
// servie à moitié ferait deviner à l'application ce qu'un zéro veut dire.
func TestAnUnsetCountryIsOffAndAlreadyHasAMinimum(t *testing.T) {
	p := payoutResponse("TG", Payout{})
	assert.Equal(t, "off", p.Mode)
	assert.Equal(t, defaultPayoutMin, p.MinXOF)
	assert.Equal(t, "XOF", p.Currency, "la monnaie du pays, pour que l'écran n'ait rien à deviner")
}

// ⚠️⚠️ LE CAS QUI MOTIVE TOUT CE FICHIER : un maximum sous le minimum refuse
// TOUT. En dessous du minimum c'est « trop petit », au-dessus du maximum c'est
// « trop grand », et l'agent reçoit les deux refus pour le même argent.
func TestAMaximumUnderTheMinimumWouldRefuseEveryAmount(t *testing.T) {
	err := checkPayoutCoherence(PayoutResponse{Mode: "direct", MinXOF: 5000, MaxXOF: 1000})
	require.Error(t, err)
	assert.Equal(t, errPayoutMaxUnderMin.Code, codeOf(t, err))
}

// Et le plafond du jour a exactement le même défaut : s'il est sous le
// minimum, la PREMIÈRE demande de la journée est déjà au-delà.
func TestADailyCapUnderTheMinimumWouldRefuseEveryAmount(t *testing.T) {
	err := checkPayoutCoherence(PayoutResponse{Mode: "direct", MinXOF: 5000, DailyCapXOF: 2000})
	require.Error(t, err)
	assert.Equal(t, errPayoutCapUnderMin.Code, codeOf(t, err))
}

// ⚠️ ZÉRO N'EST PAS UNE BORNE BASSE, C'EST L'ABSENCE DE BORNE. Confondre les
// deux rendrait impossible le réglage le plus courant : un minimum, et rien
// d'autre.
func TestZeroMeansNoBoundRatherThanAnImpossibleOne(t *testing.T) {
	assert.NoError(t, checkPayoutCoherence(PayoutResponse{Mode: "direct", MinXOF: 5000}))
	assert.NoError(t, checkPayoutCoherence(PayoutResponse{
		Mode: "direct", MinXOF: 5000, MaxXOF: 0, DailyCapXOF: 0,
	}))
}

// Égal passe : un pays qui ne veut qu'un seul montant retirable a le droit de
// le dire.
func TestAMaximumEqualToTheMinimumIsAllowed(t *testing.T) {
	assert.NoError(t, checkPayoutCoherence(PayoutResponse{
		Mode: "direct", MinXOF: 5000, MaxXOF: 5000, DailyCapXOF: 5000,
	}))
}

// ⚠️ LE REFUS DIT LES DEUX NOMBRES. Celui qui le lit vient d'en changer un
// seul, et c'est souvent l'AUTRE qu'il faut corriger.
func TestTheRefusalNamesBothNumbers(t *testing.T) {
	err := checkPayoutCoherence(PayoutResponse{Mode: "direct", MinXOF: 5000, MaxXOF: 1000})
	require.Error(t, err)
	reason, _ := reasonOf(t, err).(string)
	assert.Contains(t, reason, "5000")
	assert.Contains(t, reason, "1000")
}

// ⚠️⚠️ LE PIÈGE QUI A MOTIVÉ LE « FUSIONNÉ » : la console n'envoie que les
// champs modifiés. `{"min_xof": 5000}` seul rend impossible un `max_xof: 1000`
// écrit trois mois plus tôt — SANS que la requête le nomme. Une règle qui ne
// regarderait que la requête laisserait creuser le trou par le côté.
func TestAOneFieldEditCanMakeTheRestImpossible(t *testing.T) {
	stored := Payout{Mode: "direct", MaxXOF: 1000}
	assert.NoError(t, checkPayoutCoherence(payoutResponse("TG", stored)),
		"tel quel le réglage tient : le minimum par défaut est 1 000")

	// Un seul champ change, et le réglage devient impossible.
	raised := stored
	raised.MinXOF = 5000
	assert.Error(t, checkPayoutCoherence(payoutResponse("TG", raised)),
		"la requête ne nomme pas max_xof, et c'est pourtant lui qui devient faux")
}

// ⚠️ LE MINIMUM COMPARÉ EST L'EFFECTIF, pas le zéro qui le laisse deviner. Un
// pays sans minimum réglé en a un quand même (`defaultPayoutMin`), et c'est
// contre celui-là qu'un maximum doit tenir.
func TestTheComparedMinimumIsTheEffectiveOne(t *testing.T) {
	err := checkPayoutCoherence(payoutResponse("TG", Payout{Mode: "direct", MaxXOF: 500}))
	require.Error(t, err, "500 est sous le minimum par défaut de 1 000, même si personne ne l'a réglé")
}

// ⚠️ `periodic_min_xof` SOUS LE MINIMUM N'EST PAS REFUSÉ : `RunPeriodic` le
// relève lui-même. Ce n'est pas un réglage impossible, juste un réglage sans
// effet — et refuser tout ce qui est sans effet apprend à l'exploitation à se
// battre avec la console.
func TestAPeriodicFloorUnderTheMinimumIsNotRefused(t *testing.T) {
	assert.NoError(t, checkPayoutCoherence(PayoutResponse{
		Mode: "periodic", MinXOF: 5000, PeriodicMinXOF: 500,
	}))
}

// Un réglage vide est refusé plutôt qu'accepté sans rien faire.
func TestAnEmptyPayoutUpdateIsRefused(t *testing.T) {
	assert.Equal(t, "validation_failed", errNoPayoutUpdate.Code)
	assert.Contains(t, errNoPayoutUpdate.Message, "mode")
}

// ⚠️⚠️ ÉTEINDRE NE SE REFUSE JAMAIS — et ce test existe parce que la
// vérification ci-dessus avait d'abord créé le piège inverse.
//
// Un pays enregistré avant elle peut déjà porter un réglage impossible. Comme
// le contrôle porte sur le résultat FUSIONNÉ, un `PUT {"mode":"off"}` repassait
// par le trio existant et se faisait refuser : l'exploitation avait devant elle
// un pays qu'elle ne pouvait plus mettre en sécurité, et devait le RÉPARER
// avant de pouvoir l'ÉTEINDRE. C'est l'inverse de l'ordre dans lequel on agit
// quand quelque chose va mal.
func TestTurningPayoutsOffIsNeverRefused(t *testing.T) {
	impossible := PayoutResponse{Mode: "off", MinXOF: 5000, MaxXOF: 1000, DailyCapXOF: 200}
	assert.NoError(t, checkPayoutCoherence(impossible),
		"un pays éteint ne verse rien : ses bornes ne peuvent casser personne")
}

// Et le refus revient à l'instant où il compte : quand on OUVRE les retraits.
func TestOpeningPayoutsWithImpossibleBoundsIsRefused(t *testing.T) {
	for _, mode := range []string{"direct", "request", "periodic"} {
		err := checkPayoutCoherence(PayoutResponse{Mode: mode, MinXOF: 5000, MaxXOF: 1000})
		assert.Error(t, err, "ouvrir en %s avec un maximum sous le minimum", mode)
	}
}
