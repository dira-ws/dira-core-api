package notify

// DEUX APPLICATIONS CLIENTES, UN COMPTE — qui reçoit quoi.

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

// ⚠️⚠️ L'ASYMÉTRIE : l'immobilier va à Dira Immo et REPLIE sur les autres ; un
// autre métier ne va JAMAIS à Dira Immo, même quand il n'y a qu'elle.
func TestEachMessageGoesToTheAppThatCanOpenIt(t *testing.T) {
	client := Device{Token: "client"}
	lite := Device{Token: "lite", Edition: edition.Lite}
	immo := Device{Token: "immo", App: AppImmo}

	cases := []struct {
		name    string
		devices []Device
		key     string
		want    []string
	}{
		{"une visite, les deux applications", []Device{client, immo}, "immo_visit_confirmed", []string{"immo"}},
		{"une visite, sans Dira Immo : le repli", []Device{client, lite}, "immo_visit_confirmed", []string{"client", "lite"}},
		{"une alerte de recherche", []Device{immo, client}, "immo_saved_search_match", []string{"immo"}},
		{"une course, les deux applications", []Device{client, immo}, "ride_accepted", []string{"client"}},
		{"une course, Dira Immo seule : personne", []Device{immo}, "ride_accepted", []string{}},
		{"un ticket du support", []Device{immo, lite}, "ticket_reply", []string{"lite"}},
		{"une clé qui CONTIENT immo sans en être", []Device{immo, client}, "promo_immo_week", []string{"client"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, tokens(routeByApp(c.devices, c.key)))
		})
	}
}

func TestTheAppParameterIsRefusedWhenUnknown(t *testing.T) {
	for raw, want := range map[string]string{"": "", "immo": AppImmo, " immo ": AppImmo} {
		got, err := ParseApp(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got)
	}
	for _, raw := range []string{"immobilier", "IMMO", "client"} {
		_, err := ParseApp(raw)
		assert.Error(t, err, "%q rendrait toute la boîte dans Dira Immo", raw)
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

// ⚠️⚠️ SUR UNE VRAIE BASE : l'application s'écrit avec le jeton, et le tri passe
// AVANT le repli sur l'édition — sinon Dira Immo + Lite ne prévenait personne.
func TestTheAppIsStoredWithTheTokenAndRoutesBeforeTheEditionFallback(t *testing.T) {
	repo := baseDeNotifs(t)
	svc := NewService(repo)
	ctx := context.Background()
	user := primitive.NewObjectID().Hex()
	uid, _ := primitive.ObjectIDFromHex(user)

	require.NoError(t, svc.RegisterDevice(ctx, user, RegisterDeviceRequest{Token: "immo", Platform: "android", App: AppImmo}))
	require.NoError(t, svc.RegisterDevice(ctx, user, RegisterDeviceRequest{Token: "lite", Platform: "android", Edition: edition.Lite}))

	got, err := svc.devicesFor(ctx, uid, edition.Full, "ride_accepted")
	require.NoError(t, err)
	assert.Equal(t, []string{"lite"}, tokens(got),
		"la seule « complète » est Dira Immo : la course doit replier sur Lite, pas se perdre")

	got, err = svc.devicesFor(ctx, uid, "", "immo_visit_confirmed")
	require.NoError(t, err)
	assert.Equal(t, []string{"immo"}, tokens(got))

	// Le même jeton réenregistré SANS application la perd : réinstallé dans
	// l'application cliente, il ne doit pas garder l'étiquette de Dira Immo.
	require.NoError(t, svc.RegisterDevice(ctx, user, RegisterDeviceRequest{Token: "immo", Platform: "android"}))
	got, err = svc.devicesFor(ctx, uid, "", "ride_accepted")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"immo", "lite"}, tokens(got))
}

// ⚠️ LA BOÎTE DE DIRA IMMO NE MONTRE QUE L'IMMOBILIER — et « tout marquer lu »
// n'y éteint pas le badge des commandes. Sans `app`, la boîte reste entière.
func TestTheImmoInboxIsScopedOnARealBase(t *testing.T) {
	repo := baseDeNotifs(t)
	svc := NewService(repo)
	ctx := context.Background()
	uid := primitive.NewObjectID()
	for _, key := range []string{"order_ready", "immo_visit_confirmed", "ride_accepted", "immo_saved_search_match"} {
		require.NoError(t, repo.InsertInbox(ctx, &Inbox{ID: primitive.NewObjectID(), UserID: uid, Key: key, CreatedAt: time.Now().UTC()}))
	}

	items, unread, _, err := svc.ListInbox(ctx, uid.Hex(), "", 30, AppImmo)
	require.NoError(t, err)
	keys := []string{}
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	assert.ElementsMatch(t, []string{"immo_visit_confirmed", "immo_saved_search_match"}, keys)
	assert.Equal(t, 2, unread, "le badge de Dira Immo ne compte que ses messages")

	marked, err := svc.MarkRead(ctx, uid.Hex(), "", AppImmo)
	require.NoError(t, err)
	assert.Equal(t, 2, marked)

	all, unread, _, err := svc.ListInbox(ctx, uid.Hex(), "", 30, "")
	require.NoError(t, err)
	assert.Len(t, all, 4, "sans `app`, la boîte entière — celle de l'application cliente")
	assert.Equal(t, 2, unread, "les commandes et les courses restent NON LUES")

	n, err := repo.inbox.CountDocuments(ctx, bson.M{"user_id": uid, "key": bson.M{"$regex": "^immo_"}, "read_at": bson.M{"$exists": true}})
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
}

// ⚠️ PAR LA VRAIE ROUTE : le décodeur strict et la validation vivent au-dessus du
// service. Une application inconnue est refusée À L'ENREGISTREMENT — rangée en
// silence, elle ferait recevoir à Dira Immo les courses du compte — et à la
// lecture de la boîte. Le refus nomme le champ.
func TestAnUnknownAppIsRefusedThroughTheRoutes(t *testing.T) {
	r := chi.NewRouter()
	session := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithClaims(req.Context(), auth.Claims{
				UserID: primitive.NewObjectID().Hex(), Role: auth.RoleClient, Type: "access",
			})))
		})
	}
	NewHandler(NewService(nil)).Mount(r, session)

	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := call(http.MethodPost, "/me/devices", `{"token":"t","platform":"android","app":"immobilier"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "app")

	for _, path := range []string{"/me/notifications?app=immobilier", "/me/notifications/read?app=immobilier"} {
		method := http.MethodGet
		if strings.HasSuffix(strings.Split(path, "?")[0], "/read") {
			method = http.MethodPost
		}
		w = call(method, path, "")
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code, path)
		assert.Contains(t, w.Body.String(), `"fields":["app"]`, path)
	}
}

// ⚠️ ET À TRAVERS LES ROUTES, SUR UNE VRAIE BASE : le paramètre doit arriver
// jusqu'au dépôt — un handler qui le lirait sans le passer éteindrait le badge
// des commandes depuis Dira Immo, sans que l'épreuve du service le voie.
func TestTheImmoInboxIsScopedThroughTheRoutes(t *testing.T) {
	repo := baseDeNotifs(t)
	uid := primitive.NewObjectID()
	ctx := context.Background()
	for _, key := range []string{"order_ready", "immo_visit_confirmed", "ride_accepted", "immo_visit_reminder"} {
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
	call := func(method, path string) string {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}
	assert.Contains(t, call(http.MethodGet, "/me/notifications?app=immo"), `"unread":2`)
	assert.Contains(t, call(http.MethodPost, "/me/notifications/read?app=immo"), `"marked":2`)
	assert.Contains(t, call(http.MethodGet, "/me/notifications"), `"unread":2`,
		"les commandes et les courses restent non lues dans l'application cliente")
}

// ⚠️ LA MESSAGERIE DE L'IMMOBILIER VA À DIRA IMMO, ET SE COUPE AVEC LES AUTRES
// MESSAGES : un gabarit hors du préfixe `immo_` partirait vers l'application
// des courses ; rangé ailleurs, il échapperait à l'interrupteur des messages.
func TestTheImmoThreadMessageGoesToDiraImmoAndIsAMessage(t *testing.T) {
	assert.Equal(t, AppImmo, AppOfKey(KeyImmoThreadMessage))
	assert.Equal(t, CategoryChatMessages, categoryOf(KeyImmoThreadMessage))
	tmpl, ok := defaults[KeyImmoThreadMessage]
	require.True(t, ok)
	for _, loc := range []string{LocaleFR, LocaleEN} {
		assert.Contains(t, tmpl.Locales[loc].Title, "[listing_title]")
		assert.Equal(t, "[body]", tmpl.Locales[loc].Body)
	}
}

// ⚠️ #72, point 21 : CE QUI PRÉVIENT ENFIN L'ANNONCEUR va à Dira Immo, se range
// avec les mises à jour — sauf un changement d'ACCÈS (rattaché, détaché), qui
// ne se coupe pas — et ne promet aucune variable qu'il n'écrit pas.
func TestTheImmoPublisherNoticesAreRoutedAndRanked(t *testing.T) {
	updates := []string{KeyImmoListingPublished, KeyImmoListingReturned, KeyImmoListingModerated,
		KeyImmoVisitExpired, KeyImmoVisitExpiredHost, KeyImmoVisitUnanswered,
		KeyImmoRentNotice, KeyImmoRentDue, KeyImmoRentLate}
	access := []string{KeyImmoMemberAttached, KeyImmoMemberDetached}
	for _, key := range append(append([]string{}, updates...), access...) {
		assert.Equal(t, AppImmo, AppOfKey(key), key)
		tmpl, ok := defaults[key]
		require.True(t, ok, key)
		for _, loc := range []string{LocaleFR, LocaleEN} {
			assert.NotEmpty(t, tmpl.Locales[loc].Title, "%s/%s", key, loc)
			assert.NotEmpty(t, tmpl.Locales[loc].Body, "%s/%s", key, loc)
		}
	}
	for _, key := range updates {
		assert.Equal(t, CategoryOrderUpdates, categoryOf(key), key)
	}
	for _, key := range access {
		assert.Equal(t, CategorySecurity, categoryOf(key), key)
		assert.False(t, Muteable(categoryOf(key)), "%s : un changement d'accès ne se coupe pas", key)
	}
}

// ⚠️ LES RELANCES DE LOYER N'ACCUSENT PAS ET NE DISENT PAS L'ADRESSE : aucun
// argent ne passe par Dira (c'est le bailleur qui marque payé), et un écran
// verrouillé se lit par-dessus l'épaule. Chaque variable écrite est fournie.
func TestRentRemindersSpeakSoftlyAndOnlyWithWhatIsProvided(t *testing.T) {
	for _, key := range []string{KeyImmoRentNotice, KeyImmoRentDue, KeyImmoRentLate} {
		tmpl := defaults[key]
		for _, loc := range []string{LocaleFR, LocaleEN} {
			text := tmpl.Locales[loc].Title + " " + tmpl.Locales[loc].Body
			for _, v := range []string{"[amount]", "[lease_ref]"} {
				assert.Contains(t, tmpl.Locales[loc].Body, v, "%s/%s", key, loc)
			}
			for _, banned := range []string{"[address]", "[property]", "[publisher_name]"} {
				assert.NotContains(t, text, banned, "%s/%s", key, loc)
			}
			for _, v := range bracketVars(text) {
				assert.Contains(t, provided[key], v, "%s/%s : [%s] n'est fourni par personne", key, loc, v)
			}
		}
	}
	assert.Contains(t, defaults[KeyImmoRentLate].Locales[LocaleFR].Body, "Dites-le à votre bailleur")
}

// bracketVars : les variables `[nom]` d'un texte de gabarit.
func bracketVars(text string) []string {
	var out []string
	for {
		i := strings.IndexByte(text, '[')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(text[i:], ']')
		if j < 0 {
			return out
		}
		out = append(out, text[i+1:i+j])
		text = text[i+j+1:]
	}
}
