package payout

// ⚠️⚠️ CE FICHIER TESTE DE L'ARGENT QUI SORT ET NE REVIENT PAS.
//
// Tout le reste de la plateforme déplace des chiffres entre deux comptes que
// nous tenons. Ici, un versement parti est parti. Les tests ci-dessous ne
// vérifient donc pas « que ça marche » : ils vérifient que l'argent ne peut
// pas sortir deux fois, ni sortir sans trace, ni rester retenu quand il aurait
// dû revenir.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- des doublures qui COMPTENT l'argent ----------------------------------

type fakeWallet struct {
	withdrawable int
	held         int
	released     int
	holdFails    bool
}

func (w *fakeWallet) HoldForWithdrawal(_ context.Context, _ string, amount int) (bool, error) {
	if w.holdFails {
		return false, errors.New("boom")
	}
	if amount > w.withdrawable {
		return false, nil
	}
	w.withdrawable -= amount
	w.held += amount
	return true, nil
}

func (w *fakeWallet) ReleaseWithdrawal(_ context.Context, _ string, amount int, _, _ string) error {
	w.withdrawable += amount
	w.held -= amount
	w.released += amount
	return nil
}

func (w *fakeWallet) Withdrawable(_ context.Context, _ string) (int, error) {
	return w.withdrawable, nil
}

type fakeAccounts struct{ phone string }

func (a fakeAccounts) PhoneOf(_ context.Context, _ string) (string, error) {
	return a.phone, nil
}

type fixedPolicy struct{ r Rules }

func (p fixedPolicy) PayoutRules(_ context.Context, _ string) Rules { return p.r }

// --- les règles, sans base ------------------------------------------------

func rules(mode string) Rules {
	return Rules{Mode: mode, MinXOF: 1000, Currency: "XOF"}
}

// ⚠️ UN PAYS QUI N'A PAS OUVERT LES RETRAITS N'AFFICHE PAS DE BOUTON. Le
// défaut est `off`, parce que verser demande un contrat signé, un flottant
// approvisionné et quelqu'un pour le surveiller.
func TestWithdrawalsAreClosedUntilACountryOpensThem(t *testing.T) {
	for _, mode := range []string{"", ModeOff} {
		err := CheckRequest(Rules{Mode: mode}, 5000)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "payout_unavailable")
	}
}

// ⚠️⚠️ EN MODE PÉRIODIQUE, L'AGENT NE DEMANDE PAS — et le refus le DIT, au
// lieu d'un bouton grisé qui fait chercher ce qui ne va pas.
func TestInPeriodicModeThereIsNothingToAskFor(t *testing.T) {
	err := CheckRequest(rules(ModePeriodic), 5000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payout_is_periodic")
}

// ⚠️ LE MINIMUM EXISTE PARCE QU'UN AGRÉGATEUR FACTURE AU VERSEMENT : verser
// 100 F coûte parfois plus que les 100 F. Et le refus PORTE LE SEUIL.
func TestABelowMinimumRequestIsRefusedWithItsThreshold(t *testing.T) {
	err := CheckRequest(rules(ModeDirect), 500)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payout_below_minimum")
	assert.Contains(t, apperr.From(err).Meta["reason"].(string), "1", "le seuil est dans la phrase")

	// Et un montant au-dessus du plafond est refusé avec SON seuil.
	err = CheckRequest(Rules{Mode: ModeDirect, MinXOF: 1000, MaxXOF: 50000}, 60000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payout_above_maximum")
}

// ⚠️ LE MONTANT EST FORMATÉ LISIBLEMENT dans les refus : « le minimum est
// 1000 » se relit deux fois, « 1 000 F » se lit.
func TestAmountsInRefusalsAreReadable(t *testing.T) {
	assert.Equal(t, "1 000 F", formatXOF(1000))
	assert.Equal(t, "42 500 F", formatXOF(42500))
	assert.Equal(t, "0 F", formatXOF(0))
}

// --- ce qui tient l'argent ------------------------------------------------

// ⚠️⚠️ LE TEST LE PLUS IMPORTANT DU FICHIER. La retenue est CONDITIONNELLE :
// demander plus que le retirable ne doit RIEN sortir du portefeuille.
func TestAskingForMoreThanWithdrawableTakesNothing(t *testing.T) {
	w := &fakeWallet{withdrawable: 3000}
	ok, err := w.HoldForWithdrawal(context.Background(), "u", 5000)
	require.NoError(t, err)
	assert.False(t, ok, "la retenue refuse")
	assert.Equal(t, 3000, w.withdrawable, "le solde n'a pas bougé")
	assert.Zero(t, w.held, "rien n'est retenu")

	// Et ce qui tient passe, en sortant exactement le montant demandé.
	ok, err = w.HoldForWithdrawal(context.Background(), "u", 3000)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Zero(t, w.withdrawable)
	assert.Equal(t, 3000, w.held)

	// ⚠️ ET UNE REMISE REND EXACTEMENT CE QUI A ÉTÉ RETENU : un refus ou un
	// échec ne doit ni rendre moins, ni rendre plus.
	require.NoError(t, w.ReleaseWithdrawal(context.Background(), "u", 3000, "payout_failed", "p1"))
	assert.Equal(t, 3000, w.withdrawable)
	assert.Zero(t, w.held)
}

// ⚠️ LA PHRASE NE DIT PAS « SOLDE INSUFFISANT ». Le solde peut être large et le
// RETIRABLE nul — la dette passe avant, et le crédit offert ne se retire pas.
// Un agent qui lit « solde insuffisant » devant 40 000 F appelle le support.
func TestTheRefusalExplainsWhatCannotBeCashedOut(t *testing.T) {
	msg := errNotWithdrawable.Error()
	assert.NotContains(t, msg, "insufficient balance")
	assert.Contains(t, msg, "promotional")
	assert.Contains(t, msg, "owe")
}

// ⚠️ UN NUMÉRO MANQUANT ARRÊTE TOUT AVANT LA RETENUE. Retenir d'abord et
// découvrir ensuite qu'on ne sait pas où envoyer laisserait l'argent bloqué.
func TestNoPhoneMeansNoHold(t *testing.T) {
	// ⚠️ ON LIT LA SOURCE : l'ordre des gestes est ce qui compte, et il ne se
	// voit qu'en lisant. Le numéro est cherché AVANT la retenue — retenir
	// d'abord et découvrir ensuite qu'on ne sait pas où envoyer laisserait
	// l'argent bloqué sur un compte, sans demande pour le rendre.
	src := mustRead(t, "service.go")
	phoneAt := indexOf(src, "phone, err := s.accounts.PhoneOf(ctx, userID)")
	holdAt := indexOf(src, "ok, err := s.wallet.HoldForWithdrawal(ctx, userID, amount)")
	require.Positive(t, phoneAt)
	require.Positive(t, holdAt)
	assert.Less(t, phoneAt, holdAt, "le numéro se lit AVANT de retenir l'argent")
}

// --- le bouchon d'agrégateur ----------------------------------------------

// ⚠️ LE BOUCHON ÉCHOUE PARFOIS, ET C'EST VOLONTAIRE : un bouchon qui réussit
// toujours laisse écrire un chemin d'échec que personne n'exécute jamais, et
// c'est en production qu'on découvre que la remise au solde ne marchait pas.
func TestTheMockFailsOnPurposeForSomeNumbers(t *testing.T) {
	p := NewMockProvider("")
	res, err := p.Send(context.Background(), SendRequest{Phone: "+22890000100", Amount: 5000})
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, res.Status)
	assert.NotEmpty(t, res.FailureReason, "un échec dit POURQUOI")

	res, err = p.Send(context.Background(), SendRequest{Phone: "+22890000101", Amount: 5000})
	require.NoError(t, err)
	assert.Equal(t, StatusSent, res.Status)
	assert.NotEmpty(t, res.ProviderRef, "ce qu'on cite au support")
}

// ⚠️ UNE SIGNATURE QUI NE COLLE PAS EST REFUSÉE. Cette route est PUBLIQUE :
// la signature est la seule chose qui distingue l'agrégateur de n'importe qui.
func TestAWebhookWithABadSignatureIsRefused(t *testing.T) {
	p := NewMockProvider("secret")
	_, err := p.VerifyWebhook([]byte(`{"status":"paid"}`), "pas-la-bonne")
	require.Error(t, err)
}

// --- les états ------------------------------------------------------------

// ⚠️ `failed` ET `rejected` SONT DEUX CHOSES. Les confondre ferait croire à un
// agent qu'on lui a refusé son argent alors que c'est son numéro qui n'est pas
// enregistré chez son opérateur.
func TestFailedAndRejectedAreNotTheSameThing(t *testing.T) {
	assert.NotEqual(t, StateFailed, StateRejected)
	assert.False(t, Payout{State: StateFailed}.Open())
	assert.False(t, Payout{State: StateRejected}.Open())
	assert.True(t, Payout{State: StatePending}.Open())
	assert.True(t, Payout{State: StateSending}.Open())
	assert.False(t, Payout{State: StatePaid}.Open())
}

// ⚠️ UN REFUS SANS RAISON EST REFUSÉ : l'agent la lit, et sans elle il ne sait
// pas ce qu'il doit corriger — le support non plus.
func TestRejectingNeedsAReason(t *testing.T) {
	s := &Service{now: time.Now}
	_, err := s.Reject(context.Background(), "6a0000000000000000000001", "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reject_reason_required")
}

// ⚠️ LE NUMÉRO EST MASQUÉ DANS LES RÉPONSES. L'agent sait quel est son numéro ;
// ce qu'il veut vérifier, c'est que c'est bien le sien. Un numéro complet finit
// dans une capture d'écran envoyée au support.
func TestThePhoneIsMaskedEverywhereItIsServed(t *testing.T) {
	assert.Equal(t, "••••0001", maskPhone("+22890000001"))
	assert.Equal(t, "123", maskPhone("123"), "trop court pour masquer quoi que ce soit")
	r := toResponse(Payout{Phone: "+22890000001", Amount: 5000})
	assert.Equal(t, "••••0001", r.PhoneMasked)
	assert.NotContains(t, r.PhoneMasked, "2289000000")
}

// ⚠️ LA DEMANDE NE PORTE PAS DE NUMÉRO, et ce test lit la source : un champ
// libre serait la porte ouverte au détournement, et le défaut ne se verrait
// qu'une fois l'argent parti ailleurs.
func TestTheRequestCarriesNoPhoneNumber(t *testing.T) {
	src := mustRead(t, "handler.go")
	start := indexOf(src, "type RequestInput struct {")
	require.Positive(t, start)
	body := src[start : start+indexOf(src[start:], "\n}")]
	for _, forbidden := range []string{"Phone", "phone", "msisdn", "Number"} {
		assert.NotContains(t, body, forbidden,
			"le numéro vient du COMPTE, jamais de l'appelant")
	}
}

// ⚠️ LES DEUX CONVENTIONS DE JOUR DE SEMAINE — « 1 » est lundi pour un humain
// et dimanche pour Go. Traduit en un seul endroit, et ce test le fige.
func TestTheWeekdayConventionIsTranslatedOnce(t *testing.T) {
	assert.Equal(t, 1, weekdayOf(1), "lundi")
	assert.Equal(t, 6, weekdayOf(6), "samedi")
	assert.Equal(t, 0, weekdayOf(7), "dimanche — 7 chez l'humain, 0 chez Go")
}

func TestEveryModeIsRecognised(t *testing.T) {
	for _, m := range Modes {
		assert.True(t, ValidMode(m), m)
	}
	assert.False(t, ValidMode("instant"))
	assert.False(t, ValidMode(""))
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	require.NoError(t, err)
	return string(b)
}

func indexOf(s, sub string) int { return strings.Index(s, sub) }
