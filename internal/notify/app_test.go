package notify

// CE QU'UNE APPLICATION RETIRÉE A LAISSÉ — ses jetons, ses messages, ses
// gabarits personnalisés — et ce qu'il en advient.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/db"
	"github.com/kgtech-org/dira-core-api/pkg/edition"
)

func tokens(ds []Device) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Token)
	}
	return out
}

func pushed(msgs []PushMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Token)
	}
	return out
}

// ⚠️⚠️ UN JETON MARQUÉ D'UNE APPLICATION N'EST PLUS JOINT — quelle qu'elle soit :
// aucune application de Dira ne s'enregistre avec `app`. Un jeton sans marque
// est servi, comme avant.
func TestOnlyTheTokensOfDiraAppsAreServed(t *testing.T) {
	devices := []Device{
		{Token: "client"},
		{Token: "lite", Edition: edition.Lite},
		{Token: "retiree", App: "immo"},
		{Token: "autre", App: "kiosque"},
		{Token: "blanc", App: "  "},
	}
	assert.Equal(t, []string{"client", "lite", "blanc"}, tokens(servedDevices(devices)))
	assert.Empty(t, servedDevices(nil))
}

// ⚠️ `?app=` EST REFUSÉ, PAS IGNORÉ : ignoré, il rendrait la boîte ENTIÈRE à qui
// demandait la sienne. Le refus nomme le champ.
func TestTheAppParameterIsRefused(t *testing.T) {
	for _, raw := range []string{"", "  "} {
		assert.NoError(t, refuseAppParam(raw), "%q : la boîte entière, comme toujours", raw)
	}
	for _, raw := range []string{"immo", " immo ", "client"} {
		err := refuseAppParam(raw)
		require.Error(t, err, raw)
		assert.Equal(t, []string{"app"}, apperr.From(err).Meta["fields"], raw)
	}
}

func baseDeNotifs(t *testing.T) *Repository {
	t.Helper()
	uri := os.Getenv("DIRA_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("DIRA_TEST_MONGO_URI absent : pas de base pour cette épreuve")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := db.Connect(ctx, uri, fmt.Sprintf("dira_test_notify_app_%d", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.DB.Drop(context.Background()) })
	return NewRepository(m)
}

// ⚠️⚠️ SUR UNE VRAIE BASE : le jeton qu'une application retirée a laissé n'est
// joint par AUCUN chemin d'envoi — une notification, un appel —, et le repli
// sur l'édition se fait SANS lui : sinon un compte dont la seule « complète »
// est ce jeton-là verrait sa course partir dans le vide, alors que Lite
// l'attend. Réenregistré par une application de Dira (même jeton), il perd sa
// marque et redevient joignable.
func TestARetiredAppsTokenIsReachedByNoPathOnARealBase(t *testing.T) {
	repo := baseDeNotifs(t)
	svc := NewService(repo)
	pusher := &fakePusher{}
	svc.SetPusher(pusher)
	ctx := context.Background()
	uid := primitive.NewObjectID()
	user := uid.Hex()

	// Le jeton tel qu'une version d'avant l'a écrit, avec sa marque.
	_, err := repo.devices.InsertOne(ctx, bson.M{
		"user_id": uid, "token": "retiree", "platform": "android", "app": "immo",
		"created_at": time.Now().UTC(), "last_seen_at": time.Now().UTC(),
	})
	require.NoError(t, err)
	require.NoError(t, svc.RegisterDevice(ctx, user, RegisterDeviceRequest{Token: "lite", Platform: "android", Edition: edition.Lite}))

	got, err := svc.devicesFor(ctx, uid, edition.Full)
	require.NoError(t, err)
	assert.Equal(t, []string{"lite"}, tokens(got), "le repli sur l'édition se fait sans le jeton retiré")
	got, err = svc.devicesFor(ctx, uid, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"lite"}, tokens(got))

	svc.Notify(ctx, user, KeyRideAccepted, map[string]string{"driver": "Kodjo"}, map[string]string{"ride_id": "r1"})
	assert.Equal(t, []string{"lite"}, pushed(pusher.sent), "une notification")
	pusher.sent = nil
	n, err := svc.Signal(ctx, user, map[string]string{"type": "call", "call_id": "c1"}, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"lite"}, pushed(pusher.sent), "un appel")

	// Réinstallée dans une application de Dira, même jeton : la marque part.
	require.NoError(t, svc.RegisterDevice(ctx, user, RegisterDeviceRequest{Token: "retiree", Platform: "android"}))
	got, err = svc.devicesFor(ctx, uid, "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"retiree", "lite"}, tokens(got))
	var raw bson.M
	require.NoError(t, repo.devices.FindOne(ctx, bson.M{"token": "retiree"}).Decode(&raw))
	assert.NotContains(t, raw, "app", "la marque est effacée, pas réécrite vide")
}

// ⚠️⚠️ UNE BOÎTE QUI PORTE LES MESSAGES D'UN MÉTIER RETIRÉ SE LIT ENTIÈRE, sur une
// vraie base : leur titre et leur texte ont été rendus et rangés à l'ENVOI, et
// la lecture ne relit aucun gabarit — un gabarit retiré ne casse pas
// l'affichage. Le badge les compte, « tout marquer lu » les éteint.
func TestAnInboxWithARetiredVerticalsMessagesStillReadsOnARealBase(t *testing.T) {
	repo := baseDeNotifs(t)
	svc := NewService(repo)
	ctx := context.Background()
	uid := primitive.NewObjectID()
	for _, n := range []Inbox{
		{Key: "order_ready", Category: CategoryOrderUpdates, Title: "Commande prête", Body: "Votre commande est prête."},
		{Key: "immo_visit_confirmed", Category: CategoryOrderUpdates, Title: "Visite confirmée",
			Body: "Votre visite de « T3 Bè » est confirmée le 15/11 à 10:00.", Data: map[string]string{"type": "immo_visit"}},
	} {
		n.ID, n.UserID, n.CreatedAt = primitive.NewObjectID(), uid, time.Now().UTC()
		require.NoError(t, repo.InsertInbox(ctx, &n))
	}

	items, unread, _, err := svc.ListInbox(ctx, uid.Hex(), "", 30)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, 2, unread)
	var retired InboxResponse
	for _, it := range items {
		if it.Key == "immo_visit_confirmed" {
			retired = it
		}
	}
	assert.Equal(t, "Visite confirmée", retired.Title, "le titre rangé à l'envoi")
	assert.Equal(t, "Votre visite de « T3 Bè » est confirmée le 15/11 à 10:00.", retired.Body)

	marked, err := svc.MarkRead(ctx, uid.Hex(), "")
	require.NoError(t, err)
	assert.Equal(t, 2, marked)
}

// ⚠️⚠️ UN GABARIT PERSONNALISÉ SOUS UNE CLÉ RETIRÉE, RESTÉ EN BASE, NE SE LISTE
// PLUS ET NE PART PLUS — sur une vraie base. Sans cette garde, un appelant qui
// enverrait encore cette clé ferait partir vers les téléphones de Dira le
// message d'un métier qui n'en fait plus partie.
func TestACustomTemplateOfARetiredKeyIsNeitherListedNorSentOnARealBase(t *testing.T) {
	repo := baseDeNotifs(t)
	svc := NewService(repo)
	pusher := &fakePusher{}
	svc.SetPusher(pusher)
	ctx := context.Background()
	uid := primitive.NewObjectID()

	_, err := repo.templates.InsertOne(ctx, bson.M{
		"key": "immo_visit_confirmed", "enabled": true, "updated_at": time.Now().UTC(),
		"locales": bson.M{"fr": bson.M{"title": "Visite confirmée", "body": "Votre visite est confirmée."}},
	})
	require.NoError(t, err)
	require.NoError(t, svc.RegisterDevice(ctx, uid.Hex(), RegisterDeviceRequest{Token: "client", Platform: "android"}))

	list, err := svc.ListTemplates(ctx)
	require.NoError(t, err)
	for _, tpl := range list {
		assert.NotEqual(t, "immo_visit_confirmed", tpl.Key, "la console ne liste plus un gabarit qui ne part plus")
	}
	assert.NotEmpty(t, list, "les autres gabarits restent")

	svc.Notify(ctx, uid.Hex(), "immo_visit_confirmed", nil, nil)
	assert.Empty(t, pusher.sent, "rien ne part")
	items, _, _, err := svc.ListInbox(ctx, uid.Hex(), "", 30)
	require.NoError(t, err)
	assert.Empty(t, items, "rien ne s'archive")

	_, err = svc.Preview(ctx, "immo_visit_confirmed", "fr", nil)
	require.Error(t, err)
	assert.Equal(t, "template_not_found", apperr.From(err).Code)

	// Et une clé servie part toujours.
	svc.Notify(ctx, uid.Hex(), KeyOrderReady, nil, map[string]string{"order_id": "o1"})
	assert.Equal(t, []string{"client"}, pushed(pusher.sent))
}

// ⚠️ PAR LA VRAIE ROUTE, SUR UNE VRAIE BASE : le décodeur strict et la
// validation vivent au-dessus du service. `app` à l'enregistrement est refusé
// en le nommant — rangé en silence, le jeton d'une autre application recevrait
// des messages qu'il ne sait pas ouvrir —, comme `?app=` sur la boîte ; sans
// lui, la boîte entière.
func TestTheAppIsRefusedThroughTheRoutesOnARealBase(t *testing.T) {
	repo := baseDeNotifs(t)
	uid := primitive.NewObjectID()
	ctx := context.Background()
	for _, key := range []string{"order_ready", "immo_visit_confirmed"} {
		require.NoError(t, repo.InsertInbox(ctx, &Inbox{ID: primitive.NewObjectID(), UserID: uid, Key: key, CreatedAt: time.Now().UTC()}))
	}
	r := chi.NewRouter()
	NewHandler(NewService(repo)).Mount(r, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithClaims(req.Context(), auth.Claims{
				UserID: uid.Hex(), Role: auth.RoleClient, Type: "access",
			})))
		})
	})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := call(http.MethodPost, "/me/devices", `{"token":"t","platform":"android","app":"immo"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"fields":["app"]`)
	n, err := repo.devices.CountDocuments(ctx, bson.M{"user_id": uid})
	require.NoError(t, err)
	assert.Zero(t, n, "rien n'est enregistré sur un refus")

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/me/notifications?app=immo"},
		{http.MethodPost, "/me/notifications/read?app=immo"},
	} {
		w = call(c.method, c.path, "")
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code, c.path)
		assert.Contains(t, w.Body.String(), `"fields":["app"]`, c.path)
	}

	w = call(http.MethodGet, "/me/notifications", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"unread":2`, "la boîte entière")
	w = call(http.MethodPost, "/me/devices", `{"token":"t","platform":"android"}`)
	assert.Less(t, w.Code, 300, w.Body.String())
}
