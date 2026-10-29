package appconfig

// LE RÉGLAGE QUI ALLUME L'ASSISTANT EN DIRECT — ce qu'on refuse à l'écriture,
// et le champ qu'on oublierait d'écrire.

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconf "github.com/kgtech-org/dira-core-api/pkg/appconfig"
)

// ⚠️⚠️ LE GARDE LE PLUS UTILE DE CE FICHIER, et il regarde LE CODE.
//
// `Repository.Save` nomme ses champs un par un dans un `$set`. Un champ ajouté
// à la `Registry` et oublié là ne s'écrit JAMAIS, et rien n'échoue : l'écran
// de la console affiche le réglage qu'il vient d'envoyer — il le relit de sa
// propre requête — et c'est au rechargement suivant, parfois des jours plus
// tard, que le réglage a disparu. C'est exactement ce qui serait arrivé au
// bloc `live` : le direct réglé le matin, éteint l'après-midi, sans trace.
//
// L'épreuve DÉCOUVRE ses sujets : elle lit les étiquettes bson de la structure
// plutôt qu'une liste écrite à la main, qu'il aurait fallu penser à allonger —
// c'est-à-dire la même erreur, un étage plus haut.
func TestEveryStoredFieldOfTheRegistryIsActuallyWritten(t *testing.T) {
	src, err := os.ReadFile("appconfig.go")
	require.NoError(t, err)
	set := string(src)
	start := strings.Index(set, "func (r *Repository) Save")
	require.Positive(t, start, "Save a été renommée : ce garde ne regarde plus rien")
	set = set[start:]

	rt := reflect.TypeOf(pkgconf.Registry{})
	for i := range rt.NumField() {
		tag := strings.Split(rt.Field(i).Tag.Get("bson"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		assert.Contains(t, set, `"`+tag+`":`,
			"le champ %s de la Registry n'est pas écrit par Save : il se réglera sans jamais être gardé",
			rt.Field(i).Name)
	}
}

// ⚠️ UN PAYS SANS LANGUE EST UN RÉGLAGE MORT, et il se refuse par son nom.
// Rangé, il aurait laissé la console montrer « le direct est ouvert au Tchad »
// quand `LiveFor` le rend éteint — le défaut de #245, un an plus tard.
func TestACountryWithoutALanguageIsRefused(t *testing.T) {
	_, err := cleanLive(map[string]pkgconf.LiveRule{"TD": {MaxSessionSeconds: 300}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "language")
}

// Les bornes mordent, et zéro veut dire « prends le défaut ».
func TestTheLimitsAreBoundedAndZeroMeansDefault(t *testing.T) {
	ok, err := cleanLive(map[string]pkgconf.LiveRule{"TD": {Languages: []string{"fr"}}})
	require.NoError(t, err, "zéro doit passer : la console envoie un champ vide pour « je ne règle pas »")
	assert.Zero(t, ok["TD"].MaxSessionSeconds, "le défaut s'applique à la LECTURE, pas à l'écriture")

	_, err = cleanLive(map[string]pkgconf.LiveRule{"TD": {Languages: []string{"fr"}, MaxSessionSeconds: 28800}})
	require.Error(t, err, "huit heures de micro ouvert sont une facture, pas un réglage")

	_, err = cleanLive(map[string]pkgconf.LiveRule{"TD": {Languages: []string{"fr"}, IdleTimeoutSeconds: 1}})
	require.Error(t, err, "couper après une seconde de silence rendrait le direct inutilisable")
}

// Le pays est normalisé — `td` et `TD` sont le même pays.
func TestTheCountryKeyIsNormalised(t *testing.T) {
	got, err := cleanLive(map[string]pkgconf.LiveRule{" td ": {Languages: []string{"FR"}}})
	require.NoError(t, err)
	require.Contains(t, got, "TD")
	assert.Equal(t, []string{"fr"}, got["TD"].Languages)
}

// Un code de pays qui n'en est pas un est refusé.
func TestAnImpossibleCountryIsRefused(t *testing.T) {
	_, err := cleanLive(map[string]pkgconf.LiveRule{"TCHAD": {Languages: []string{"fr"}}})
	require.Error(t, err)
}

// Un réglage absent ne range RIEN — et surtout pas une carte vide, qui
// ressemblerait à « quelqu'un a réglé le direct, et ne l'a ouvert nulle part ».
func TestNoLiveSettingStoresNothing(t *testing.T) {
	got, err := cleanLive(nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}
