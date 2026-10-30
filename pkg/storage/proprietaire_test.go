package storage

// UN SEUL PROPRIÉTAIRE DU SEAU, ET PRIVÉ N'EST PAS ÉPHÉMÈRE.

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ LES PREUVES NE S'EFFACENT JAMAIS TOUTES SEULES.
//
// Le cycle de vie était posé sur TOUS les genres privés : rendre privées les
// pièces de conformité les aurait fait effacer au bout de quatre-vingt-dix
// jours — un casier judiciaire disparu du seau pendant que sa ligne disait
// « validé ». ⚠️ La liste des preuves est NOMMÉE ici : un garde qui la tirerait
// du code ne verrait pas une preuve ajoutée par erreur aux genres qui expirent.
func TestProofsNeverExpireByThemselves(t *testing.T) {
	for _, proof := range []string{"document", "receipt"} {
		assert.False(t, expiringKinds[proof], "%q expirerait : une pièce disparaîtrait du seau", proof)
		for _, r := range expiryRules(90).Rules {
			assert.NotEqual(t, proof+"/", r.RuleFilter.Prefix, "une règle d'expiration vise %q", proof)
		}
	}
	// La voix, elle, expire — c'est ce qui l'efface du seau à la fin de sa
	// rétention, objet compris.
	var prefixes []string
	for _, r := range expiryRules(90).Rules {
		prefixes = append(prefixes, r.RuleFilter.Prefix)
		assert.Equal(t, 90, int(r.Expiration.Days))
	}
	assert.ElementsMatch(t, []string{"voice/", "speech/"}, prefixes)
}

// ⚠️ UNE ADRESSE SIGNÉE EST UNE ADRESSE DE CE SEAU, et on range la NUE.
func TestASignedAddressIsStillOurs(t *testing.T) {
	const pub, bucket = "https://files.dira.llc", "dira-media"
	signed := pub + "/" + bucket + "/document/u1/a.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc"
	key, ok := keyOf(pub, bucket, signed)
	require.True(t, ok, "une adresse signée de ce seau a été jugée étrangère")
	assert.Equal(t, "document/u1/a.pdf", key, "la signature est entrée dans la clé")
	assert.Equal(t, pub+"/"+bucket+"/document/u1/a.pdf", Bare(signed))
	assert.Equal(t, pub+"/"+bucket+"/dish/b.jpg", Bare(pub+"/"+bucket+"/dish/b.jpg#x"))

	for _, foreign := range []string{
		"https://evil.example/" + bucket + "/document/a.pdf",
		pub + "/autre-seau/document/a.pdf",
		pub + "/" + bucket + "/../" + bucket + "/a.pdf",
		pub + "/" + bucket + "/",
	} {
		_, ok := keyOf(pub, bucket, foreign)
		assert.False(t, ok, "%q a été accepté", foreign)
	}
}

// ⚠️⚠️ RANGER UNE PIÈCE DÉPOSÉE SOUS UN AUTRE GENRE.
func TestAdoptingMovesUnderTheKindWithANewName(t *testing.T) {
	dst, moved := adoptKey("vehicle/3f2a.jpg", "document", "6aa3550382777a391c0d917d")
	require.True(t, moved)
	assert.True(t, strings.HasPrefix(dst, "document/6aa3550382777a391c0d917d/"), dst)
	assert.True(t, strings.HasSuffix(dst, ".jpg"), "l'extension suit l'objet : %s", dst)
	assert.NotContains(t, dst, "3f2a", "la nouvelle adresse dit encore l'ancienne")

	same, moved := adoptKey("document/u1/a.pdf", "document", "u2")
	assert.False(t, moved, "une pièce déjà rangée a été déplacée")
	assert.Equal(t, "document/u1/a.pdf", same)

	// ⚠️ UNE ENTITÉ QUI SORTIRAIT DE SON SEGMENT EST IGNORÉE, jamais suivie.
	for _, evil := range []string{"a/b", "..", "../voice"} {
		dst, _ := adoptKey("vehicle/x.png", "document", evil)
		assert.True(t, strings.HasPrefix(dst, "document/"), "%q : %s", evil, dst)
		assert.Equal(t, 1, strings.Count(dst, "/"), "%q a ouvert un segment : %s", evil, dst)
		assert.NotContains(t, dst, "..")
		assert.NotContains(t, dst, "voice")
	}
	noExt, _ := adoptKey("vehicle/x", "document", "u")
	assert.False(t, strings.Contains(noExt[strings.LastIndex(noExt, "/"):], "."), noExt)
}

// ⚠️⚠️ LA POLITIQUE ET LE CYCLE DE VIE NE SE POSENT QUE PAR LE PROPRIÉTAIRE.
//
// Le dernier service démarré gagnait : une verticale embarquant une version du
// socle d'avant la voix privée rouvrait les vocaux à chaque redémarrage. Le
// garde lit `New` — une épreuve sans dépôt ne voit pas un appel au dépôt.
func TestOnlyTheOwnerSetsThePolicy(t *testing.T) {
	src, err := os.ReadFile("storage.go")
	require.NoError(t, err)
	code := string(src)
	at := strings.Index(code, "func New(")
	require.Positive(t, at)
	body := code[at:]
	body = body[:strings.Index(body, "\n}\n")]
	for _, call := range []string{"SetBucketPolicy(", "setPrivateLifecycle("} {
		i := strings.Index(body, call)
		require.Positive(t, i, "%s a disparu de New", call)
		guard := strings.LastIndex(body[:i], "if cfg.OwnsPolicy")
		require.Positive(t, guard, "%s n'est plus gardé par OwnsPolicy", call)
		assert.NotContains(t, body[guard:i], "\n\t}\n", "%s est sorti du bloc gardé", call)
	}
}
