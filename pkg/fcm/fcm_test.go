package fcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Vérifie que le VRAI compte de service du projet est accepté : une clé
// Firebase est au format PKCS#8 (« BEGIN PRIVATE KEY »), pas PKCS#1, et une
// bibliothèque qui n'accepterait que le second échouerait seulement en
// production, au premier envoi.
//
// Ignoré quand le fichier n'est pas là : le test doit passer sur une machine
// qui n'a pas le secret.
func TestRealServiceAccountParses(t *testing.T) {
	path := os.Getenv("FCM_SERVICE_ACCOUNT_FILE")
	if path == "" {
		t.Skip("FCM_SERVICE_ACCOUNT_FILE non défini")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("compte de service illisible: %v", err)
	}
	c, err := New(string(data))
	if err != nil {
		t.Fatalf("le compte de service du projet est refusé: %v", err)
	}
	if c.ProjectID() == "" {
		t.Fatal("project_id vide")
	}
	t.Logf("compte de service accepté, projet %s", c.ProjectID())
}

// --- client complet, contre un faux Google ---

// fakeGoogle joue le jeton OAuth2 et l'envoi FCM.
type fakeGoogle struct {
	srv       *httptest.Server
	tokenHits int
	sendHits  int
	sendCode  int
	sendBody  string
	// bodies garde le JSON de CHAQUE envoi : c'est ce que les épreuves de
	// l'enveloppe APNs lisent.
	bodies [][]byte
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	g := &fakeGoogle{sendCode: 200, sendBody: `{"name":"projects/p/messages/1"}`}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		g.tokenHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","expires_in":3600}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		g.sendHits++
		// ⚠️ ON GARDE LE CORPS ENVOYÉ, et les épreuves le lisent TEL QUEL.
		// Comparer deux structures Go serait passé au vert sur la faute que
		// l'équipe iOS a signalée : la structure était juste, c'est
		// l'enveloppe APNs produite qui ne l'était pas.
		body, _ := io.ReadAll(r.Body)
		g.bodies = append(g.bodies, body)
		w.WriteHeader(g.sendCode)
		_, _ = w.Write([]byte(g.sendBody))
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

// testClient construit un client sur une clé RSA jetable, pointé vers le faux.
func testClient(t *testing.T, g *fakeGoogle) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("clé: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	sa, _ := json.Marshal(ServiceAccount{
		Type: "service_account", ProjectID: "p",
		ClientEmail: "svc@p.iam.gserviceaccount.com",
		PrivateKey:  pemKey, TokenURI: g.srv.URL + "/token",
	})
	c, err := New(string(sa))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.http = g.srv.Client()
	c.sendBase = g.srv.URL
	return c
}

// Un compte de service absent ne doit pas être une erreur fatale : une
// plateforme sans notifications doit démarrer, en le disant.
func TestEmptyServiceAccountIsNotConfigured(t *testing.T) {
	if _, err := New("   "); err != ErrNotConfigured {
		t.Fatalf("err = %v, attendu ErrNotConfigured", err)
	}
}

func TestIncompleteServiceAccountIsRefused(t *testing.T) {
	if _, err := New(`{"type":"service_account","project_id":"p"}`); err == nil {
		t.Fatal("un compte sans clé ni email doit être refusé")
	}
	if _, err := New("pas du json"); err == nil {
		t.Fatal("un JSON invalide doit être refusé")
	}
}

// Le jeton d'accès est MIS EN CACHE : le redemander à chaque notification
// ferait un aller-retour OAuth2 de plus par message, et Google plafonne.
func TestAccessTokenIsReused(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	msgs := []Message{{Token: "t1"}, {Token: "t2"}}
	if _, err := c.Send(context.Background(), msgs); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := c.Send(context.Background(), msgs); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if g.tokenHits != 1 {
		t.Fatalf("jetons demandés = %d, attendu 1", g.tokenHits)
	}
	if g.sendHits != 4 {
		t.Fatalf("envois = %d, attendu 4 (un par jeton, l'API v1 n'a pas d'envoi groupé)", g.sendHits)
	}
}

// Un jeton mort se distingue d'une panne : il ne guérira pas, et le réessayer
// indéfiniment finit par faire refuser le projet.
func TestUnregisteredTokenIsFlagged(t *testing.T) {
	g := newFakeGoogle(t)
	g.sendCode, g.sendBody = 404, `{"error":{"status":"UNREGISTERED","message":"gone"}}`
	c := testClient(t, g)
	res, err := c.Send(context.Background(), []Message{{Token: "dead"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !res[0].Unregistered || res[0].Err == nil {
		t.Fatalf("résultat inattendu: %+v", res[0])
	}
}

// Une panne serveur n'est PAS un jeton mort. Les confondre désactiverait
// toute la flotte après un incident chez Google.
func TestServerErrorIsNotAnUnregisteredToken(t *testing.T) {
	g := newFakeGoogle(t)
	g.sendCode, g.sendBody = 503, `{"error":{"status":"UNAVAILABLE","message":"try later"}}`
	c := testClient(t, g)
	res, err := c.Send(context.Background(), []Message{{Token: "alive"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res[0].Err == nil {
		t.Fatal("l'échec doit remonter")
	}
	if res[0].Unregistered {
		t.Fatal("503 n'est pas une mort d'appareil : le jeton doit être conservé")
	}
}

// L'échec d'un appareil n'emporte pas les autres : chaque résultat est rendu
// séparément.
func TestOneResultPerToken(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	res, err := c.Send(context.Background(), []Message{{Token: "a"}, {Token: "b"}, {Token: "c"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("résultats = %d, attendu 3", len(res))
	}
	for i, want := range []string{"a", "b", "c"} {
		if res[i].Token != want {
			t.Fatalf("res[%d].Token = %q, attendu %q", i, res[i].Token, want)
		}
	}
}

// --- L'ENVELOPPE APNs (socle #193, 18 octobre 2026) ---
//
// ⚠️ CES ÉPREUVES LISENT LE JSON PRODUIT, pas la structure Go. C'est la leçon
// de `pool_plan` et du barème d'annulation : une épreuve qui comparerait deux
// structures serait passée au vert sur la faute signalée, parce que la faute
// n'était pas dans la structure mais dans ce qu'elle produisait sur le fil.

// sent rend le bloc `apns` du n-ième envoi, décodé.
func sent(t *testing.T, g *fakeGoogle, i int) (headers map[string]any, aps map[string]any) {
	t.Helper()
	if len(g.bodies) <= i {
		t.Fatalf("envoi %d absent (%d envois)", i, len(g.bodies))
	}
	var got struct {
		Message struct {
			Apns struct {
				Headers map[string]any `json:"headers"`
				Payload struct {
					Aps map[string]any `json:"aps"`
				} `json:"payload"`
			} `json:"apns"`
		} `json:"message"`
	}
	if err := json.Unmarshal(g.bodies[i], &got); err != nil {
		t.Fatalf("json: %v — %s", err, g.bodies[i])
	}
	return got.Message.Apns.Headers, got.Message.Apns.Payload.Aps
}

// ⚠️ UN APPEL DOIT SONNER APPLICATION FERMÉE. Un message silencieux ne le
// peut pas sur iOS : il faut une notification VISIBLE, prioritaire, sonore et
// « sensible au temps » — c'est ce niveau d'interruption qui passe le mode
// concentration, ce qu'un appel de trente secondes exige.
func TestACallRingsOnIOS(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	_, err := c.Send(context.Background(), []Message{{
		Token: "t", Title: "Nouvelle course", Body: "Vous avez 30 s pour répondre.",
		Data:     map[string]string{"type": "call", "call_id": "abc"},
		DataOnly: true, Ring: true, Sound: "ring_classic.caf", Category: "RIDE_CALL",
		CollapseID: "call-abc", TTL: 30 * time.Second,
	}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	h, aps := sent(t, g, 0)
	if h["apns-push-type"] != "alert" {
		t.Fatalf("push-type = %v, attendu alert : un appel doit s'AFFICHER", h["apns-push-type"])
	}
	if h["apns-priority"] != "10" {
		t.Fatalf("priorité = %v, attendu 10", h["apns-priority"])
	}
	if h["apns-collapse-id"] != "call-abc" {
		t.Fatalf("collapse-id = %v : une relance doit REMPLACER l'appel précédent", h["apns-collapse-id"])
	}
	if h["apns-expiration"] == nil {
		t.Fatal("sans échéance, APNs peut livrer un appel DÉJÀ FERMÉ")
	}
	if aps["sound"] != "ring_classic.caf" {
		t.Fatalf("son = %v : le son par défaut d'iOS ne tient pas trente secondes", aps["sound"])
	}
	if aps["interruption-level"] != "time-sensitive" {
		t.Fatalf("niveau = %v : sans lui, l'appel se tait en mode concentration", aps["interruption-level"])
	}
	if aps["category"] != "RIDE_CALL" {
		t.Fatalf("catégorie = %v : elle dit quel écran ouvrir", aps["category"])
	}
	alert, ok := aps["alert"].(map[string]any)
	if !ok || alert["title"] != "Nouvelle course" {
		t.Fatalf("alerte = %v : une notification visible a besoin de son texte", aps["alert"])
	}
	// ⚠️ LES DONNÉES VOYAGENT QUAND MÊME : c'est par `call_id` que
	// l'application dédoublonne avec la trame du socket.
	if !strings.Contains(string(g.bodies[0]), `"call_id":"abc"`) {
		t.Fatalf("les données de l'appel ont disparu : %s", g.bodies[0])
	}
	// Et AUCUNE notification FCM : elle doublerait l'alerte sur iOS et ferait
	// apparaître sur Android une notification système par-dessus l'écran plein
	// que l'application construit elle-même.
	if strings.Contains(string(g.bodies[0]), `"notification"`) {
		t.Fatalf("notification FCM présente : deux alertes pour un appel — %s", g.bodies[0])
	}
}

// ⚠️ APNs REFUSE LA PRIORITÉ 10 SUR UN MESSAGE SILENCIEUX. C'est la faute
// exacte signalée : `content-available` partait en 10, et l'envoi était
// rejeté. Un signal qui n'est pas un appel (fermeture d'un écran) reste
// silencieux, mais en priorité 5 et déclaré « background ».
func TestASilentSignalIsThrottledAndDeclared(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	if _, err := c.Send(context.Background(), []Message{{
		Token: "t", Data: map[string]string{"type": "call_closed"}, DataOnly: true,
		TTL: time.Minute,
	}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	h, aps := sent(t, g, 0)
	if h["apns-push-type"] != "background" {
		t.Fatalf("push-type = %v, attendu background", h["apns-push-type"])
	}
	if h["apns-priority"] != "5" {
		t.Fatalf("priorité = %v, attendu 5 : APNs REFUSE 10 sur un silencieux", h["apns-priority"])
	}
	if aps["content-available"] != float64(1) {
		t.Fatalf("content-available = %v : sans lui iOS ne réveille rien", aps["content-available"])
	}
}

// Une notification ordinaire déclare son type et se regroupe par objet.
func TestAVisibleNotificationDeclaresItsTypeAndCollapses(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	if _, err := c.Send(context.Background(), []Message{{
		Token: "t", Title: "Course acceptée", Body: "Kossi arrive.",
		Data: map[string]string{"ride_id": "r1"}, CollapseID: "ride-r1",
	}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	h, _ := sent(t, g, 0)
	if h["apns-push-type"] != "alert" {
		t.Fatalf("push-type = %v, attendu alert", h["apns-push-type"])
	}
	if h["apns-collapse-id"] != "ride-r1" {
		t.Fatalf("collapse-id = %v : cinq changements d'état laisseraient cinq lignes", h["apns-collapse-id"])
	}
	// Le texte reste dans le bloc `notification` de FCM, qui sert les deux
	// systèmes : le dupliquer dans `aps` le ferait traduire deux fois.
	if !strings.Contains(string(g.bodies[0]), `"notification"`) {
		t.Fatalf("notification FCM absente : %s", g.bodies[0])
	}
}

// ⚠️ SANS TTL, PAS D'ÉCHÉANCE. Un message sans durée de vie ne doit pas se
// voir imposer une expiration arbitraire — il part avec le défaut d'APNs.
func TestNoTTLMeansNoExpiration(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	if _, err := c.Send(context.Background(), []Message{{Token: "t", Title: "a", Body: "b"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	h, _ := sent(t, g, 0)
	if _, ok := h["apns-expiration"]; ok {
		t.Fatalf("échéance posée sans TTL : %v", h["apns-expiration"])
	}
}

// ⚠️ L'ÉCHÉANCE EST UN INSTANT, pas une durée : APNs attend un horodatage
// Unix absolu. Y poser « 30 » enverrait une échéance du 1er janvier 1970, et
// APNs jetterait TOUS les messages.
func TestTheExpirationIsAnAbsoluteInstant(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	before := time.Now().Unix()
	if _, err := c.Send(context.Background(), []Message{{Token: "t", DataOnly: true, TTL: 30 * time.Second}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	h, _ := sent(t, g, 0)
	got, err := strconv.ParseInt(h["apns-expiration"].(string), 10, 64)
	if err != nil {
		t.Fatalf("échéance illisible %v: %v", h["apns-expiration"], err)
	}
	if got < before+30 || got > before+32 {
		t.Fatalf("échéance = %d, attendu ≈ %d (maintenant + 30 s)", got, before+30)
	}
}

// ⚠️ UN IDENTIFIANT DE REGROUPEMENT TROP LONG FAIT REFUSER LE MESSAGE ENTIER
// par APNs (64 octets). On tronque : perdre le regroupement vaut mieux que
// perdre la notification.
func TestALongCollapseIDIsTruncatedRatherThanLosingTheMessage(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	long := strings.Repeat("x", 100)
	if _, err := c.Send(context.Background(), []Message{{Token: "t", Title: "a", CollapseID: long}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	h, _ := sent(t, g, 0)
	if got := h["apns-collapse-id"].(string); len(got) != 64 {
		t.Fatalf("longueur = %d, attendu 64", len(got))
	}
}

// ⚠️ ANDROID NE CHANGE PAS, et c'est la condition de tout ce qui précède : son
// bloc garde la priorité haute et son TTL. L'enveloppe APNs ne le concerne
// pas — FCM ne la lit que pour un jeton Apple.
func TestAndroidIsUntouched(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	if _, err := c.Send(context.Background(), []Message{{
		Token: "t", DataOnly: true, Ring: true, TTL: 30 * time.Second,
	}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var got struct {
		Message struct {
			Android map[string]any `json:"android"`
		} `json:"message"`
	}
	if err := json.Unmarshal(g.bodies[0], &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got.Message.Android["priority"] != "high" {
		t.Fatalf("priorité Android = %v, attendu high", got.Message.Android["priority"])
	}
	if got.Message.Android["ttl"] != "30s" {
		t.Fatalf("ttl Android = %v, attendu 30s", got.Message.Android["ttl"])
	}
}

// ⚠️ UN JETON REFUSÉ EST UN APPAREIL MORT. Les premières versions iOS
// envoyaient le jeton APNs brut là où FCM attend un jeton d'enregistrement :
// chaque envoi échouait en INVALID_ARGUMENT, et comme ce n'est ni un 404 ni un
// UNREGISTERED, l'appareil restait actif et réessayé indéfiniment.
func TestAnInvalidTokenIsADeadDevice(t *testing.T) {
	g := newFakeGoogle(t)
	g.sendCode = 400
	g.sendBody = `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"The registration token is not a valid FCM registration token","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"message.token"}]}]}}`
	c := testClient(t, g)
	// ⚠️ LA VALEUR NE JOUE AUCUN RÔLE : c'est la réponse de FCM qui décide, et
	// un littéral hexadécimal ici — même inventé — se fait prendre pour une
	// clé par le contrôle des secrets. On la NOMME pour ce qu'elle représente.
	res, err := c.Send(context.Background(), []Message{{Token: "jeton-apns-brut"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !res[0].Unregistered {
		t.Fatal("un jeton refusé par FCM ne guérira pas : l'appareil doit être marqué mort")
	}
}

// ⚠️⚠️ ET UN MESSAGE MAL FORMÉ N'EST PAS UN APPAREIL MORT. `INVALID_ARGUMENT`
// couvre aussi nos propres fautes de sérialisation : le confondre avec un
// jeton mort désactiverait TOUTE LA FLOTTE au premier bogue, en une rafale,
// sans moyen de le défaire. On exige que FCM désigne nommément le jeton.
func TestAMalformedMessageDoesNotKillTheDevice(t *testing.T) {
	g := newFakeGoogle(t)
	g.sendCode = 400
	g.sendBody = `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"Invalid value at 'message.android.ttl'","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"message.android.ttl"}]}]}}`
	c := testClient(t, g)
	res, err := c.Send(context.Background(), []Message{{Token: "alive"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res[0].Err == nil {
		t.Fatal("l'échec doit remonter")
	}
	if res[0].Unregistered {
		t.Fatal("notre faute de sérialisation désactiverait toute la flotte")
	}
}
