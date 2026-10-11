package user

import (
	"os"
	"strings"
	"testing"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ L'ANCIEN MOT DE PASSE EST EXIGÉ, et c'est la seule chose qui sépare
// « changer son mot de passe » de « prendre un compte ».
//
// Un jeton volé, un écran laissé ouvert dans un bureau, un téléphone posé sur
// une table : sans cette vérification, n'importe lequel de ces trois suffirait à
// verrouiller quelqu'un HORS de son propre compte — et à garder l'accès.
func TestChangingAPasswordRequiresTheOldOne(t *testing.T) {
	hash, err := HashPassword("le-mot-de-passe-du-gerant")
	require.NoError(t, err)

	ok, err := VerifyPassword("le-mot-de-passe-du-gerant", hash)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = VerifyPassword("autre-chose", hash)
	require.NoError(t, err)
	assert.False(t, ok, "un mot de passe qui n'est pas le bon ne doit jamais passer")
}

// ⚠️ LES TROIS REFUS PORTENT LEUR PROPRE CODE. La traduction se fait PAR LE
// CODE : en `validation_failed`, les trois sortiraient en « Requête invalide »
// — et la personne ne saurait pas lequel des deux champs corriger, ni qu'il
// faut demander à celui qui a ouvert son accès.
func TestThePasswordRefusalsKeepTheirOwnPhrase(t *testing.T) {
	for _, e := range []error{errWrongCurrent, errSamePassword, errNoPassword} {
		code := apperr.From(e).Code
		assert.NotEqual(t, "validation_failed", code,
			"un refus en `validation_failed` sort en « Requête invalide »")
		assert.NotEmpty(t, code)
	}
	assert.Equal(t, "current_password_wrong", apperr.From(errWrongCurrent).Code)
	assert.Equal(t, "password_unchanged", apperr.From(errSamePassword).Code)
	// ⚠️ Un client entre par CODE : lui parler de mot de passe l'enverrait en
	// chercher un qu'il n'a jamais eu.
	assert.Equal(t, "account_has_no_password", apperr.From(errNoPassword).Code)
}

// ⚠️ UNE EMPREINTE N'EST JAMAIS LA MÊME DEUX FOIS, même pour le même mot de
// passe : le sel est tiré à chaque fois. C'est ce qui empêche de reconnaître
// deux comptes qui partagent un mot de passe en comparant leurs empreintes —
// et c'est pour ça que « le même mot de passe » se vérifie en VÉRIFIANT, jamais
// en comparant deux chaînes.
func TestTwoHashesOfTheSamePasswordDiffer(t *testing.T) {
	a, err := HashPassword("meme-mot-de-passe")
	require.NoError(t, err)
	b, err := HashPassword("meme-mot-de-passe")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)

	same, err := VerifyPassword("meme-mot-de-passe", b)
	require.NoError(t, err)
	assert.True(t, same, "c'est la VÉRIFICATION qui dit l'égalité, pas la comparaison")
}

// ⚠️⚠️ LA RÈGLE DE LONGUEUR NE DOIT PAS RETOURNER DANS LE TAG `validate:`.
//
// Elle y était. `min=8` lève `validation_failed`, et `httpx.Message` traduit PAR
// LE CODE : la personne recevait « Requête invalide » en choisissant son mot de
// passe, sans jamais apprendre qu'il en faut huit. Ce test lit la SOURCE, parce
// que le défaut ne se voit pas à l'exécution — la requête est bien refusée, elle
// est juste refusée sans rien dire.
func TestTheLengthRuleIsNotHiddenInADTOTag(t *testing.T) {
	src, err := os.ReadFile("password_change.go")
	require.NoError(t, err)
	// ⚠️ ON NE LIT QUE LES LIGNES DE TAG : un commentaire a le droit de citer
	// `min=8` pour expliquer pourquoi il n'y est plus.
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(line, "validate:") {
			continue
		}
		for _, tag := range []string{"min=", "max="} {
			assert.NotContains(t, line, tag,
				"une règle de longueur dans un tag `validate:` sort en « Requête invalide » : "+
					"elle appartient au service, avec un code et une phrase qui portent le nombre")
		}
	}
}

// Et la règle est bien appliquée, des deux côtés.
func TestThePasswordLengthRefusalsCarryTheNumber(t *testing.T) {
	assert.Equal(t, "password_too_short", apperr.From(errPasswordTooShort).Code)
	assert.Contains(t, apperr.From(errPasswordTooShort).Message, "8",
		"la phrase doit porter le nombre, sinon on essaie au hasard")
	assert.Equal(t, "password_too_long", apperr.From(errPasswordTooLong).Code)
	assert.Equal(t, 8, passwordMinLen)
	assert.Equal(t, 120, passwordMaxLen)
}
