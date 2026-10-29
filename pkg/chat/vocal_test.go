package chat

// LES MESSAGES VOCAUX D'UNE CONVERSATION (core#256).
//
// ⚠️ POUR LES GENS QUI NE LISENT PAS. Dira Lite sert des zones de faible
// alphabétisation : une conversation qui n'accepte que du texte n'est pas une
// conversation pour eux. Tout ce qui suit découle de là — y compris les deux
// compromis assumés plus bas.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

func oid(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	v, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return v
}

func unVocal(transcription string) Voice {
	return Voice{AudioURL: "https://files.dira.llc/chat/abc.m4a", Seconds: 7, Transcript: transcription}
}

// ⚠️⚠️ LA TRANSCRIPTION VA DANS `body`, ET C'EST CE QUI REND LES VOCAUX
// COMPATIBLES. Une app chauffeur qui ne connaît pas `kind` affiche du texte au
// lieu de rien : le vocal arrive, lisible, sans qu'elle soit republiée.
func TestTheTranscriptLandsInTheBodySoOldAppsStillRead(t *testing.T) {
	m, err := voiceMessage(oid(t, orderID), RefRide, oid(t, driverID), SenderDriver,
		unVocal("je suis devant le portail bleu"), baseLexicon["fr"])
	require.NoError(t, err)

	assert.Equal(t, "je suis devant le portail bleu", m.Body,
		"une app qui ne lit que `body` doit voir le message")
	assert.Equal(t, KindVoice, m.Kind)
	assert.Equal(t, "https://files.dira.llc/chat/abc.m4a", m.AudioURL)
	assert.Equal(t, 7, m.Seconds)

	// Et servi, le message porte les trois.
	resp := toResponse(*m)
	assert.Equal(t, KindVoice, resp.Kind)
	assert.Equal(t, 7, resp.Seconds)
	assert.NotEmpty(t, resp.AudioURL)
	assert.Equal(t, "je suis devant le portail bleu", resp.Body)
}

// ⚠️ `kind` EST TOUJOURS SERVI, même sur du texte et même sur les messages
// d'avant les vocaux. Une application qui devrait deviner le genre à la
// présence d'`audio_url` traiterait un vocal dont l'audio a disparu comme un
// message texte vide.
func TestTheKindIsAlwaysServed(t *testing.T) {
	ancien := Message{SenderRole: SenderClient, Body: "bonjour"} // pas de `kind` en base
	assert.Equal(t, KindText, toResponse(ancien).Kind)

	texte := Message{SenderRole: SenderClient, Body: "bonjour", Kind: KindText}
	assert.Equal(t, KindText, toResponse(texte).Kind)

	vocal := Message{SenderRole: SenderClient, Kind: KindVoice, AudioURL: "u", Seconds: 3}
	assert.Equal(t, KindVoice, toResponse(vocal).Kind)
}

// ⚠️⚠️ LA TRANSCRIPTION PASSE PAR LA MODÉRATION, exactement comme du texte. Une
// menace dite à voix haute est une menace : y échapper aurait fait du micro la
// porte de service de tout ce que le lexique refuse.
func TestAThreatSpokenAloudIsStillBlocked(t *testing.T) {
	// ⚠️ LE LEXIQUE DE BASE, et pas `nil` : `Check` laisse TOUT passer sur un
	// lexique vide, et ma première version de cette épreuve passait donc sans
	// rien vérifier. C'est le piège d'un filtre dont l'absence est silencieuse.
	lex := baseLexicon["fr"]
	require.NotEmpty(t, lex)
	require.True(t, Check("je vais te tuer", lex).Blocked(),
		"le lexique de base doit bloquer une menace")

	_, err := voiceMessage(oid(t, orderID), RefRide, oid(t, driverID), SenderDriver,
		unVocal("je vais te tuer"), lex)
	require.Error(t, err, "un vocal dont la transcription menace ne doit pas partir")
	assert.Equal(t, "message_blocked", apperr.From(err).Code)
}

// ⚠️ ET UNE TRANSCRIPTION SIGNALÉE PORTE SA MARQUE, pour que la file de
// modération la voie comme elle verrait un texte.
func TestAFlaggedTranscriptCarriesItsMark(t *testing.T) {
	lex := baseLexicon["fr"]
	require.True(t, Check("imbécile", lex).Flagged(), "le lexique doit signaler une insulte")

	m, err := voiceMessage(oid(t, orderID), RefRide, oid(t, driverID), SenderDriver,
		unVocal("imbécile"), lex)
	require.NoError(t, err)
	assert.True(t, m.Flagged, "la file de modération doit voir ce vocal")
	assert.NotEmpty(t, m.FlagSeverity)
}

// ⚠️⚠️ TROU NOMMÉ : UN VOCAL NON TRANSCRIT PART QUAND MÊME, ET N'EST PAS MODÉRÉ.
//
// Refuser priverait de parole précisément les gens qui ne savent pas écrire —
// et c'est pour eux que cette fonction existe. Le lexique ne voit alors rien ;
// le signalement humain reste la seule prise. Cette épreuve existe pour que le
// compromis soit VISIBLE, pas pour le célébrer : si quelqu'un décide un jour de
// refuser ces messages, elle rougira et il saura ce qu'il change.
func TestAnUntranscribedVoiceStillGoesThroughAndIsNotModerated(t *testing.T) {
	// ⚠️ AVEC le lexique : ce n'est pas l'absence de filtre qui laisse passer,
	// c'est l'absence de TEXTE à filtrer.
	m, err := voiceMessage(oid(t, orderID), RefRide, oid(t, driverID), SenderDriver,
		unVocal(""), baseLexicon["fr"])
	require.NoError(t, err, "la transcription a échoué : le message part quand même")
	assert.Empty(t, m.Body, "il n'y a rien à écrire")
	assert.Equal(t, KindVoice, m.Kind)
	assert.NotEmpty(t, m.AudioURL, "mais l'audio est là, et c'est lui le message")
	assert.False(t, m.Flagged, "le lexique n'a rien pu voir — c'est le trou, et il est nommé")
}

// ⚠️ UNE TRANSCRIPTION TROP LONGUE EST TRONQUÉE, PAS REFUSÉE : la limite est le
// problème du serveur, pas celui de la personne qui a parlé.
func TestATooLongTranscriptIsTruncatedNotRefused(t *testing.T) {
	m, err := voiceMessage(oid(t, orderID), RefRide, oid(t, driverID), SenderDriver,
		unVocal(strings.Repeat("a", MaxBody+500)), baseLexicon["fr"])
	require.NoError(t, err)
	assert.Len(t, []rune(m.Body), MaxBody)
}

// ⚠️⚠️ UN MESSAGE RETIRÉ PERD SON AUDIO AVEC SON CORPS. Retirer la
// transcription en laissant l'enregistrement réécoutable n'aurait retiré que la
// trace écrite : le destinataire n'avait qu'à appuyer sur « écouter ».
func TestHidingAVoiceMessageAlsoTakesTheAudio(t *testing.T) {
	resp := toResponse(Message{
		SenderRole: SenderDriver, Kind: KindVoice, Body: "une insulte",
		AudioURL: "https://files.dira.llc/chat/abc.m4a", Seconds: 4, Hidden: true,
	})
	assert.True(t, resp.Hidden)
	assert.Empty(t, resp.Body)
	assert.Empty(t, resp.AudioURL, "sinon la modération ne retire que la trace écrite")
	assert.Equal(t, 4, resp.Seconds, "la durée reste : la ligne garde sa place")
}

// ⚠️ LES BORNES DE DURÉE, REFUSÉES NOMMÉMENT.
func TestTheVoiceDurationIsBounded(t *testing.T) {
	svc := NewService(nil, RefRide, fakeParties{c: conv(driverID, "onboard", nil)})
	svc.SetVoiceIntake(intakeDeTest{})

	for _, s := range []int{0, -1, MaxVoiceSeconds + 1} {
		_, err := svc.SendVoice(context.Background(), driverID, "driver", orderID,
			Voice{AudioURL: "u", Seconds: s})
		require.Error(t, err, "durée %d", s)
		champs := apperr.From(err).Meta["fields"].([]string)
		assert.Equal(t, "seconds", champs[0])
	}

	// Et sans audio stocké, il n'y a rien à poser.
	_, err := svc.SendVoice(context.Background(), driverID, "driver", orderID,
		Voice{Seconds: 5})
	require.Error(t, err)
}

// ⚠️⚠️ SANS RÉCEPTION BRANCHÉE, LA ROUTE REFUSE EN `503` — et le texte continue
// de marcher. Un `422` ferait chercher à l'application ce qu'elle a mal envoyé,
// alors que sa requête est parfaitement valide : c'est le déploiement qui ne
// sait pas la servir.
func TestWithoutAnIntakeVoiceIsRefusedWith503(t *testing.T) {
	svc := NewService(nil, RefRide, fakeParties{c: conv(driverID, "onboard", nil)})
	assert.Nil(t, svc.VoicePolicy(), "et l'app ne montre pas le micro")

	// ⚠️⚠️ PAR `send`, PAS PAR `sendVoice`. Ma première version appelait
	// `sendVoice` directement : l'AIGUILLAGE — « un corps multipart est un
	// vocal » — n'était donc vérifié par rien, et le casser laissait la suite
	// verte. Un multipart serait alors tombé dans `httpx.Decode` et aurait
	// rendu un `422 validation_failed` : l'application aurait cherché ce
	// qu'elle avait mal envoyé, alors que son envoi était correct.
	h := NewHandler(svc, "rides")
	rec := httptest.NewRecorder()
	h.send(rec, multipartVocal(t))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"un corps multipart doit être AIGUILLÉ vers le vocal, et refusé en 503")
	assert.Contains(t, rec.Body.String(), "voice_messages_unavailable")
}

// ⚠️ ET AVEC LA RÉCEPTION BRANCHÉE, LE MÊME CORPS MULTIPART DEVIENT UN VOCAL.
// L'aiguillage se vérifie dans les deux sens : refuser en 503 pourrait aussi
// venir d'un `Content-Type` jamais regardé.
func TestAMultipartBodyIsRoutedToTheVoicePath(t *testing.T) {
	svc := NewService(nil, RefRide, fakeParties{c: conv(driverID, "onboard", nil)})
	svc.SetVoiceIntake(intakeDeTest{})
	h := NewHandler(svc, "rides")
	rec := httptest.NewRecorder()
	h.send(rec, multipartVocal(t))

	// ⚠️ On s'arrête au DÉPÔT : `SendVoice` écrit, et le dépôt est nul dans
	// ces épreuves. Ce qui compte ici est que le refus ne soit NI un `503`
	// (réception absente) NI un `422` (corps mal décodé) : l'aiguillage a bien
	// mené au chemin vocal.
	assert.NotEqual(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "validation_failed")
}

// multipartVocal monte une requête multipart minimale.
func multipartVocal(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rides/"+orderID+"/messages",
		strings.NewReader("--x\r\n\r\n--x--\r\n"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	ctx := auth.WithClaims(req.Context(), auth.Claims{
		UserID: driverID, Role: auth.RoleDriver, Type: "access",
	})
	return req.WithContext(ctx)
}

// ⚠️ ET AVEC, LA POLITIQUE EST SERVIE — c'est elle qui fait apparaître le micro.
func TestTheVoicePolicyTellsTheAppWhetherToShowTheMic(t *testing.T) {
	svc := NewService(nil, RefRide, fakeParties{c: conv(driverID, "onboard", nil)})
	svc.SetVoiceIntake(intakeDeTest{})

	p := svc.VoicePolicy()
	require.NotNil(t, p)
	assert.True(t, p.Enabled)
	assert.Equal(t, MaxVoiceSeconds, p.MaxSeconds,
		"la borne servie doit être celle que le serveur applique")
}

type intakeDeTest struct{}

func (intakeDeTest) Receive(*http.Request) (Voice, error) {
	return unVocal("bonjour"), nil
}

// ⚠️⚠️ CE QUE LA POUSSÉE ANNONCE, pour les deux genres.
//
// Une mutation qui annonçait un vocal comme du TEXTE survivait à tout : rien
// n'observait la poussée, et les deux envois construisaient leur charge sur
// place avec la même connaissance recopiée. L'application se fie à `data.kind`
// pour dessiner — elle aurait dessiné un message texte vide.
func TestThePushAnnouncesTheRightKind(t *testing.T) {
	texte := pushFor(Message{SenderRole: SenderClient, Body: "je suis en bas"})
	assert.Equal(t, KindText, texte.Kind)
	assert.Equal(t, "je suis en bas", texte.Body)
	assert.Zero(t, texte.Seconds, "un texte n'a pas de durée")

	const adresse = "https://files.dira.llc/chat/secret-9f2c.m4a"
	vocal := pushFor(Message{
		SenderRole: SenderDriver, Kind: KindVoice, Seconds: 9,
		Body: "je suis devant le portail bleu", AudioURL: adresse,
	})
	assert.Equal(t, KindVoice, vocal.Kind)
	assert.Equal(t, 9, vocal.Seconds, "l'app doit pouvoir dessiner la barre")
	// ⚠️ L'APERÇU EST LA TRANSCRIPTION, pas le mot « vocal » : le destinataire
	// doit pouvoir décider s'il écoute MAINTENANT sans ouvrir l'application.
	assert.Equal(t, "je suis devant le portail bleu", vocal.Body)

	// ⚠️ ET L'ADRESSE DE L'AUDIO NE PART JAMAIS DANS UNE POUSSÉE : une
	// notification n'est pas un endroit où poser un lien vers un
	// enregistrement — elle s'affiche sur un écran verrouillé, et elle
	// transite par un service tiers.
	//
	// ⚠️ Ma première version comparait à `"u"`, la chaîne que j'avais donnée
	// comme adresse : « u » est une LETTRE du texte français, et l'assertion
	// échouait sur le corps du message. Une adresse distinctive, et on vérifie
	// la poussée ENTIÈRE.
	assert.NotContains(t, fmt.Sprintf("%+v", vocal), adresse)

	// Un vocal sans transcription : l'aperçu est vide, et c'est à la verticale
	// de nommer la chose dans la langue de l'appareil.
	muet := pushFor(Message{SenderRole: SenderClient, Kind: KindVoice, Seconds: 3})
	assert.Empty(t, muet.Body)
	assert.Equal(t, KindVoice, muet.Kind)
	assert.Equal(t, 3, muet.Seconds)
}
