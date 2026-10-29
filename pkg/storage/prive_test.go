package storage

// ⚠️⚠️ LA VOIX DE QUELQU'UN N'EST PAS UNE PHOTO DE PLAT (analytics#33 §1).
//
// Le seau recevait une politique de LECTURE PUBLIQUE sur tout son contenu, et
// la clé d'un vocal a la forme `voice/<userId>/<uuid>.m4a` : la voix d'un
// client était servie à qui connaissait l'URL, sans jeton, indéfiniment — le
// TTL de 90 jours effaçait la LIGNE Mongo, jamais l'OBJET, et l'effacement d'un
// compte ne retirait que les objets encore référencés par une ligne.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type policy struct {
	Statement []struct {
		Effect   string   `json:"Effect"`
		Action   []string `json:"Action"`
		Resource []string `json:"Resource"`
	} `json:"Statement"`
}

// ⚠️⚠️ UN `Deny` NOMMÉ SUR LA VOIX, ET UN `Allow` LARGE SUR LE RESTE.
//
// L'inverse — lister les préfixes publics — aurait rendu public tout genre
// ajouté ensuite, et c'est exactement le défaut qu'on corrige : la voix est
// devenue publique parce que la politique disait « tout ».
func TestTheVoiceIsDeniedByName(t *testing.T) {
	var p policy
	require.NoError(t, json.Unmarshal([]byte(bucketPolicy("dira")), &p))
	require.Len(t, p.Statement, 2, "il faut les DEUX clauses")

	allow, deny := p.Statement[0], p.Statement[1]
	assert.Equal(t, "Allow", allow.Effect)
	assert.Equal(t, []string{"arn:aws:s3:::dira/*"}, allow.Resource)

	assert.Equal(t, "Deny", deny.Effect)
	assert.Contains(t, deny.Resource, "arn:aws:s3:::dira/voice/*")
	// ⚠️ ET LE REFUS PORTE SUR `s3:*`, pas seulement sur la lecture : un seau
	// dont les objets privés se LISTENT laisse deviner les clés des autres, et
	// un `voice/<userId>/` listable dirait au passage combien de vocaux a
	// envoyés un compte donné.
	assert.Equal(t, []string{"s3:*"}, deny.Action)
}

// ⚠️ LA POLITIQUE DÉCOUVRE SES SUJETS : elle lit `privateKinds`. Un genre privé
// ajouté demain est refusé sans qu'on y pense — et c'est l'inverse du défaut
// d'origine, où ne rien faire rendait public.
func TestEveryPrivateKindIsDenied(t *testing.T) {
	require.NotEmpty(t, privateKinds, "la table est vide : plus rien n'est privé")
	raw := bucketPolicy("dira")
	for kind := range privateKinds {
		assert.Contains(t, raw, "arn:aws:s3:::dira/"+kind+"/*", "genre %q", kind)
		assert.True(t, Private(kind))
	}
	// Et ce qui est public le reste : une photo de plat est faite pour être
	// servie à tout le monde, par un navigateur, sans jeton.
	for _, kind := range []string{"dish", "avatar", "feed", "marker", "banner"} {
		assert.False(t, Private(kind), "genre %q", kind)
		assert.NotContains(t, raw, "arn:aws:s3:::dira/"+kind+"/*")
	}
}

// ⚠️⚠️ UN PRÉFIXE TROP LARGE EST REFUSÉ, et ce n'est pas une précaution de
// style : `RemovePrefix` sert l'effacement d'un compte, et un `userID` non
// renseigné produirait `voice/` — c'est-à-dire la voix de TOUT LE MONDE.
func TestATooWidePrefixIsRefused(t *testing.T) {
	s := &Store{bucket: "dira"}

	for _, prefix := range []string{
		"", "   ", "/", "//",
		// ⚠️ LES QUATRE ÉCRITURES DU MÊME DANGER : « tous les vocaux ».
		"voice", "voice/", "/voice", "/voice/",
	} {
		err := s.RemovePrefix(context.Background(), prefix)
		require.Error(t, err, "préfixe %q", prefix)
		assert.Contains(t, err.Error(), "trop large")
	}
}

// ⚠️ ET SIGNER UNE URL QUI N'EST PAS DE CE SEAU EST REFUSÉ : signer une adresse
// fournie par un appelant ferait de ce dépôt un signeur d'URL arbitraires.
func TestSigningAForeignURLIsRefused(t *testing.T) {
	s := &Store{bucket: "dira", public: "https://files.dira.llc"}

	_, err := s.SignedURL(nil, "https://ailleurs.example/voice/x.m4a", 0) //nolint:staticcheck // refus avant tout appel réseau
	require.Error(t, err)
	assert.Contains(t, err.Error(), "n'appartient pas")
}

// ⚠️ ET UN PRÉFIXE QUI NOMME UNE ENTITÉ EST ACCEPTÉ : c'est le cas normal de
// l'effacement d'un compte, `voice/<userId>/`.
func TestAnAccountPrefixIsAccepted(t *testing.T) {
	assert.False(t, tooWide("voice/6ae384"))
	assert.False(t, tooWide("voice/6ae384/"))
	for _, p := range []string{"", "/", "voice", "voice/", "//"} {
		assert.True(t, tooWide(p), "préfixe %q", p)
	}
}

// tooWide appelle LA MÊME borne que `RemovePrefix`, sans dépôt.
//
// ⚠️ ELLE NE RECOPIE PAS LA RÈGLE : `namedPrefix` est la fonction que le dépôt
// appelle, et c'est pour cela qu'un préfixe dangereux ne part jamais sur le
// réseau — le refus tombe avant.
func tooWide(prefix string) bool {
	_, ok := namedPrefix(prefix)
	return !ok
}

// ⚠️ LE GENRE PRÉFIXE LA CLÉ, et c'est ce qui rend « privé » applicable par la
// politique du seau et par le cycle de vie des objets. Si la forme de la clé
// changeait, les deux cesseraient de mordre en silence.
func TestThePrefixIsTheKind(t *testing.T) {
	s := &Store{bucket: "dira", public: "https://files.dira.llc"}

	key, ok := s.Key("https://files.dira.llc/dira/voice/6ae384/a1b2.m4a")

	require.True(t, ok)
	assert.True(t, strings.HasPrefix(key, "voice/"),
		"la clé ne commence plus par le genre : la politique et le cycle de vie ne mordent plus")
}

// ⚠️ LA RÉTENTION DES OBJETS PRIVÉS EST PARTAGÉE, et c'est la raison de sa
// place dans `pkg/config` : le cycle de vie se pose sur le SEAU, et le dernier
// service qui démarre écrase la règle du précédent. Deux valeurs différentes
// feraient une rétention qui dépend de l'ordre des redémarrages — c'est-à-dire
// du hasard, sur l'effacement de la voix de quelqu'un.
func TestThePrivateRetentionIsAShotSetting(t *testing.T) {
	// Le défaut doit valoir la rétention de la LIGNE d'un vocal
	// (`voicedesk.VoiceRetention`, 90 jours) : les deux doivent dire la même
	// chose, sinon l'un efface une voix que l'autre croit encore là.
	b, err := config.LoadBase("dira", "dira")
	require.NoError(t, err)
	assert.Equal(t, 90*24*time.Hour, b.PrivateMediaRetention)
}

// ⚠️⚠️ LA SIGNATURE COUVRE L'HÔTE, ET CETTE ÉPREUVE EXISTE PARCE QUE JE L'AI
// CRUE LE CONTRAIRE.
//
// Premier jet : on signait avec le client du réseau INTERNE (`minio:9000`),
// puis on remplaçait l'hôte par le public (`files.dira.llc`) — en écrivant en
// commentaire que « la signature ne couvre pas l'hôte ». C'est FAUX pour
// SigV4 : `Host` est un en-tête signé. Mesuré en préproduction — l'adresse
// servie répondait `403`, comme l'adresse nue, et la doublure de l'analytique
// ne pouvait pas le voir puisqu'elle rend une signature inventée.
//
// La vérification en direct a trouvé ce que trente mutations n'auraient pas
// trouvé.
func TestTheSignerTargetsThePublicHost(t *testing.T) {
	host, secure, ok := hostOf("https://files.dira.llc")
	require.True(t, ok)
	assert.Equal(t, "files.dira.llc", host)
	assert.True(t, secure, "https doit donner TLS, sinon la signature vise le mauvais schéma")

	host, secure, ok = hostOf("http://localhost:9000")
	require.True(t, ok)
	assert.Equal(t, "localhost:9000", host, "le PORT fait partie de l'hôte signé")
	assert.False(t, secure)

	// ⚠️ UNE BASE ILLISIBLE NE DONNE PAS D'HÔTE : on signe alors pour le
	// client interne, et le journal de démarrage le dit. Deviner un hôte
	// produirait des adresses mortes en silence.
	for _, base := range []string{"", "pas-une-url", "/chemin/seul"} {
		_, _, ok := hostOf(base)
		assert.False(t, ok, "base %q", base)
	}
}

// ⚠️⚠️ CE QUE LE SERVEUR DIT EST AUSSI PRIVÉ QUE CE QUE LA PERSONNE DIT, et
// c'est moins évident.
//
// « De Moursal à Chagoua, quatre mille cinq cent cinquante francs » est son
// adresse et le prix de sa course, dans un fichier audio. Public, il serait
// lisible par qui connaît l'URL — exactement le défaut qu'on vient de corriger
// sur les vocaux.
func TestWhatTheServerSaysIsPrivateToo(t *testing.T) {
	assert.True(t, Private(KindSpeech))
	assert.True(t, Private(KindVoice))
	assert.Contains(t, bucketPolicy("dira"), "arn:aws:s3:::dira/speech/*")
}

// ⚠️⚠️ ET LE MOTEUR ENCODE : pas de WAV, même pour un son que NOUS produisons.
//
// J'ai essayé de l'y mettre, avec ce raisonnement : « notre moteur en sort, et
// refuser obligerait à réencoder ce que nous venons de produire ». Il oubliait
// QUI TÉLÉCHARGE. Ce son part vers un téléphone sur un réseau d'ici : vingt
// secondes de WAV pèsent un demi-mégaoctet, la même phrase en opus trente
// kilooctets. Un garde de ce paquet l'a dit avant moi.
func TestTheEngineEncodesBeforeSending(t *testing.T) {
	for _, mime := range []string{"audio/wav", "audio/x-wav", "audio/wave"} {
		_, ok := Accepts(KindSpeech, mime)
		assert.False(t, ok, "le WAV passe : un demi-mégaoctet pour vingt secondes, sur un réseau d'ici")
	}
	for _, mime := range []string{"audio/opus", "audio/ogg", "audio/mpeg", "audio/mp4"} {
		_, ok := Accepts(KindSpeech, mime)
		assert.True(t, ok, "le moteur ne peut plus rendre %s", mime)
	}
}
