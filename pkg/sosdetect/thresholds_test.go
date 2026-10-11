package sosdetect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ LES DÉFAUTS SONT CE QUE LES APPLICATIONS APPLIQUENT AUJOURD'HUI, et ce
// test les fige. C'est la condition pour que ce contrat sorte sans rien
// changer pour personne : si quelqu'un « harmonise » le choc à 7 g en passant,
// le premier déploiement modifierait le comportement de deux applications dans
// quatorze pays, sans qu'aucune décision n'ait été prise.
func TestDefaultsAreWhatTheAppsRunToday(t *testing.T) {
	for _, app := range Apps {
		d := Defaults(app)
		assert.Equal(t, 2.7, d.Shake.MinG, app)
		assert.Equal(t, 3, d.Shake.Count, app)
		assert.Equal(t, 1500, d.Shake.WindowMS, app)
		assert.Equal(t, -12.0, d.Noise.LoudDBFS, app)
		assert.Equal(t, 800, d.Noise.MinLoudMS, app)
	}
	// ⚠️ LA DIVERGENCE DU CHOC EST GARDÉE EXPRÈS. Elle se corrigera sur des
	// mesures, depuis la console — pas dans un commit.
	require.NotNil(t, Defaults(AppDriver).Crash)
	require.NotNil(t, Defaults(AppCourier).Crash)
	assert.Equal(t, 8.0, Defaults(AppDriver).Crash.MinG)
	assert.Equal(t, 6.0, Defaults(AppCourier).Crash.MinG)
	// ⚠️ LE PASSAGER N'A PAS DE CHOC : il n'est pas au volant, et un téléphone
	// qui tombe du canapé ressemble à un choc.
	assert.Nil(t, Defaults(AppClient).Crash)
}

// ⚠️ TOUS LES DÉFAUTS TIENNENT DANS LEURS PROPRES BORNES. Sans ce test, une
// borne resserrée ferait raboter le défaut vers… lui-même, et `Resolve`
// signalerait un remplacement à chaque appel.
func TestEveryDefaultSitsInsideItsOwnBounds(t *testing.T) {
	for _, app := range Apps {
		got, fixed := Resolve(app, Patch{}, Patch{})
		assert.Empty(t, fixed, "aucun défaut ne doit être raboté (%s)", app)
		assert.Equal(t, Defaults(app), got, app)
	}
}

// La cascade : défauts → pays → pays × application, la dernière gagne.
func TestThePerAppOverrideWins(t *testing.T) {
	five, nine := 5.0, 9.0
	got, _ := Resolve(AppDriver,
		Patch{ShakeMinG: &five, CrashMinG: &nine},
		Patch{ShakeMinG: ptrF(3.2)},
	)
	assert.Equal(t, 3.2, got.Shake.MinG, "la surcharge d'application gagne")
	require.NotNil(t, got.Crash)
	assert.Equal(t, 9.0, got.Crash.MinG, "ce que l'application ne règle pas vient du pays")
	assert.Equal(t, 3, got.Shake.Count, "et le reste vient des défauts")
}

// ⚠️⚠️ UNE SURCHARGE DE CHOC NE CRÉE PAS UN CHOC LÀ OÙ IL N'Y EN A PAS.
// Régler `crash.min_g` pour le passager ne doit pas lui pousser une détection
// que sa spec exclut.
func TestOverridingCrashDoesNotGiveTheClientOne(t *testing.T) {
	got, _ := Resolve(AppClient, Patch{CrashMinG: ptrF(9)}, Patch{})
	assert.Nil(t, got.Crash)
}

// ⚠️⚠️ LE SERVEUR NE SERT JAMAIS UNE VALEUR HORS BORNES — même écrite en base.
func TestAValueStoredOutOfBoundsIsNeverServed(t *testing.T) {
	got, fixed := Resolve(AppDriver, Patch{ShakeMinG: ptrF(42), CrashMinG: ptrF(0)}, Patch{})
	assert.Equal(t, 2.7, got.Shake.MinG, "le défaut remplace la valeur folle")
	require.NotNil(t, got.Crash)
	assert.Equal(t, 8.0, got.Crash.MinG)
	// ⚠️ ET LE REMPLACEMENT EST NOMMÉ. Un seuil silencieusement remplacé est ce
	// qu'on cherche pendant trois jours le soir où une alerte n'est pas partie.
	assert.ElementsMatch(t, []string{KeyShakeMinG, KeyCrashMinG}, fixed)
}

// ⚠️⚠️ LA PANNE SILENCIEUSE : une fenêtre plus courte que la durée forte
// qu'elle doit contenir ne peut JAMAIS se remplir. La détection est éteinte, et
// rien ne le dit.
func TestAWindowShorterThanItsDurationIsWidened(t *testing.T) {
	got, fixed := Resolve(AppClient, Patch{NoiseMinLoud: ptrI(2500), NoiseWindow: ptrI(900)}, Patch{})
	assert.GreaterOrEqual(t, got.Noise.WindowMS, got.Noise.MinLoudMS)
	assert.Contains(t, fixed, KeyNoiseWindow)
}

// ⚠️ ET LA CONSOLE, ELLE, REFUSE — au lieu de raboter en silence. Quelqu'un qui
// tape 20 g doit l'apprendre, pas voir « enregistré » et croire que la
// détection est devenue très tolérante alors qu'elle serait à 16.
func TestTheConsoleRefusesWhatTheServerWouldOnlyClamp(t *testing.T) {
	key, ok := Check(Patch{CrashMinG: ptrF(20)})
	assert.False(t, ok)
	assert.Equal(t, KeyCrashMinG, key)

	// ⚠️ 0 dBFS EST LE MAXIMUM ABSOLU : un seuil à 0 ne se déclenche jamais.
	key, ok = Check(Patch{NoiseLoud: ptrF(0)})
	assert.False(t, ok)
	assert.Equal(t, KeyNoiseLoud, key)

	key, ok = Check(Patch{ShakeCount: ptrI(1)})
	assert.False(t, ok)
	assert.Equal(t, KeyShakeCount, key)

	_, ok = Check(Patch{ShakeMinG: ptrF(2.7), NoiseLoud: ptrF(-12)})
	assert.True(t, ok)
}

// ⚠️⚠️ LA RÈGLE QUI LIE DEUX CHAMPS NE SE VÉRIFIE PAS SUR LA SAISIE SEULE.
// Ne régler QUE `min_loud_ms` à 2 000 ms, en laissant la fenêtre à son défaut
// de 1 500, éteint la détection — et la saisie, prise isolément, est dans les
// bornes.
func TestTheWindowRuleIsCheckedOnEffectiveValues(t *testing.T) {
	only := Patch{NoiseMinLoud: ptrI(2000)}
	_, ok := Check(only)
	assert.True(t, ok, "prise isolément, la saisie est irréprochable")

	effective := Defaults(AppClient).Apply(only)
	key, ok := CheckPair(effective)
	assert.False(t, ok, "mais résolue, elle éteint la détection")
	assert.Equal(t, KeyNoiseWindow, key)
}

// Le journal des modifications doit dire QUOI a changé, pas « les seuils ».
func TestAPatchNamesWhatItSets(t *testing.T) {
	assert.True(t, Patch{}.Empty())
	p := Patch{ShakeMinG: ptrF(3), NoiseCooldown: ptrI(90)}
	assert.False(t, p.Empty())
	assert.Equal(t, []string{KeyShakeMinG, KeyNoiseCooldown}, p.Fields())
}

// ⚠️ CHAQUE CLÉ DE LA TABLE EST UNE BORNE RÉELLE, et chaque borne a un sens :
// un minimum sous le maximum, et aucune clé orpheline.
func TestTheBoundsTableIsCoherent(t *testing.T) {
	assert.Len(t, Bounds, 11)
	for _, k := range Keys() {
		b := Bounds[k]
		assert.Less(t, b.Min, b.Max, k)
	}
	assert.Equal(t, "4 – 16", Describe(KeyCrashMinG))
	assert.Equal(t, "-30 – -3", Describe(KeyNoiseLoud))
}

func ptrF(v float64) *float64 { return &v }
func ptrI(v int) *int         { return &v }
