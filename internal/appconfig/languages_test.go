package appconfig

// CE QUE LE CATALOGUE DES LANGUES REFUSE À L'ÉCRITURE.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconf "github.com/kgtech-org/dira-core-api/pkg/appconfig"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

func catalogue(extra ...pkgconf.Language) []pkgconf.Language {
	return append([]pkgconf.Language{{Code: "fr", Name: "Français", App: true, Spoken: true}}, extra...)
}

// ⚠️ LE REFUS NOMME LA LANGUE, dans `reason` : c'est ce que la console affiche.
func reasonOf(t *testing.T, err error) string {
	t.Helper()
	var ae *apperr.Error
	require.ErrorAs(t, err, &ae)
	reason, _ := ae.Meta["reason"].(string)
	return reason
}

func TestACatalogueIsNormalised(t *testing.T) {
	got, err := cleanCatalogue(catalogue(pkgconf.Language{Code: " WO ", Name: " Wolof ", Spoken: true, Whisper: " XX "}))
	require.NoError(t, err)
	assert.Equal(t, "wo", got[1].Code)
	assert.Equal(t, "Wolof", got[1].Name)
	assert.Equal(t, "Wolof", got[1].Native, "le nom sert de nom natif quand on n'en donne pas")
	assert.Equal(t, "xx", got[1].Whisper)
}

func TestTheOrderOfTheConsoleIsKept(t *testing.T) {
	got, err := cleanCatalogue([]pkgconf.Language{
		{Code: "wo", Name: "Wolof", Spoken: true},
		{Code: "fr", Name: "Français", App: true, Spoken: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "wo", got[0].Code, "l'ordre est un réglage : c'est lui qui donne le défaut")
}

func TestWhatACatalogueRefuses(t *testing.T) {
	long := make([]rune, pkgconf.LanguageHintMax+1)
	for i := range long {
		long[i] = 'é'
	}
	cases := []struct {
		name string
		bad  pkgconf.Language
		code string
	}{
		{"un code trop court", pkgconf.Language{Code: "w", Name: "x", Spoken: true}, "w"},
		{"un code trop long", pkgconf.Language{Code: "abcdefghi", Name: "x", Spoken: true}, "abcdefghi"},
		{"un code qui entrerait dans un chemin", pkgconf.Language{Code: "a/b", Name: "x", Spoken: true}, "a/b"},
		{"un code en deux parties", pkgconf.Language{Code: "fr-tg", Name: "x", Spoken: true}, "fr-tg"},
		{"une langue sans nom", pkgconf.Language{Code: "wo", Spoken: true}, "wo"},
		{"un doublon", pkgconf.Language{Code: "FR", Name: "x", Spoken: true}, "fr"},
		{"une écriture inconnue", pkgconf.Language{Code: "wo", Name: "x", Spoken: true, Script: "latin"}, "wo"},
		{"une indication trop longue", pkgconf.Language{Code: "wo", Name: "x", Spoken: true, Hint: string(long)}, "wo"},
		{"un code Whisper impossible", pkgconf.Language{Code: "wo", Name: "x", Spoken: true, Whisper: "wolof"}, "wo"},
		{"le direct sans la voix", pkgconf.Language{Code: "wo", Name: "x", Live: true}, "wo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := cleanCatalogue(catalogue(c.bad))
			require.Error(t, err)
			assert.Equal(t, c.code, reasonOf(t, err))
		})
	}
}

// L'indication se compte en CARACTÈRES : l'éwé et l'arabe en prennent deux par
// lettre, et une borne en octets aurait refusé une phrase de longueur permise.
func TestTheHintLimitCountsCharactersNotBytes(t *testing.T) {
	hint := make([]rune, pkgconf.LanguageHintMax)
	for i := range hint {
		hint[i] = 'ɖ'
	}
	_, err := cleanCatalogue(catalogue(pkgconf.Language{Code: "ee", Name: "Éwé", Spoken: true, Hint: string(hint)}))
	assert.NoError(t, err)
}

// ⚠️ L'APPLICATION NE DOIT JAMAIS SE RETROUVER SANS CHOIX.
func TestACatalogueLeavesTheAppSomethingToOffer(t *testing.T) {
	_, err := cleanCatalogue(nil)
	require.Error(t, err, "une liste vide n'est pas « revenir au départ »")

	_, err = cleanCatalogue([]pkgconf.Language{{Code: "wo", Name: "Wolof", Spoken: true}})
	require.Error(t, err, "aucune langue d'écran")

	_, err = cleanCatalogue([]pkgconf.Language{{Code: "fr", Name: "Français", App: true}})
	require.Error(t, err, "aucune langue parlée")
}

func TestACatalogueIsBounded(t *testing.T) {
	var many []pkgconf.Language
	for i := range pkgconf.LanguagesMax + 1 {
		many = append(many, pkgconf.Language{Code: "a" + string(rune('a'+i/26)) + string(rune('a'+i%26)), Name: "x", App: true, Spoken: true})
	}
	_, err := cleanCatalogue(many)
	require.Error(t, err)
}

// ⚠️ UN CATALOGUE ENREGISTRÉ SE VOIT TOUT DE SUITE.
func TestSavingTheCatalogueClearsTheCache(t *testing.T) {
	store := &fakeStore{reg: &pkgconf.Registry{}}
	svc := NewService(store)
	ctx := context.Background()
	svc.Registry(ctx)
	require.Equal(t, 1, store.loads)

	require.NoError(t, svc.SaveLanguages(ctx, catalogue(), "admin"))
	assert.Equal(t, "admin", store.actor)
	assert.Len(t, store.languages, 1)

	svc.Registry(ctx)
	assert.Equal(t, 2, store.loads, "le cache devait être vidé")
}

// ⚠️⚠️ LA GRILLE DES ÉDITIONS NE TOUCHE PAS AU CATALOGUE. Elle renvoie tout le
// registre lu à l'ouverture de son écran : le laisser passer aurait défait, en
// silence, une retouche faite dans l'écran des langues entre-temps.
func TestTheEditionGridCannotRewriteTheCatalogue(t *testing.T) {
	store := &fakeStore{reg: &pkgconf.Registry{}}
	svc := NewService(store)
	err := svc.Save(context.Background(), pkgconf.Registry{Languages: catalogue()}, "admin")
	require.NoError(t, err, "la console renvoie ce champ avec le reste : il ne doit rien refuser")
	require.NotNil(t, store.saved)
	assert.Empty(t, store.saved.Languages)
	assert.Nil(t, store.languages)
}

// Un refus n'écrit rien.
func TestARefusedCatalogueWritesNothing(t *testing.T) {
	store := &fakeStore{reg: &pkgconf.Registry{}}
	err := NewService(store).SaveLanguages(context.Background(), nil, "admin")
	require.Error(t, err)
	assert.Nil(t, store.languages)
}
