package payment

// RETROUVER UN PAIEMENT DONT LA RÉPONSE S'EST PERDUE (#248, #250).
//
// ⚠️ CES ÉPREUVES PASSENT PAR LA ROUTE, pas par le service. Le défaut qu'on
// corrige ici est un défaut d'ORDRE (écrire avant d'appeler l'opérateur) et de
// CÂBLAGE (lire un en-tête, monter une route) : une épreuve qui appelle
// `svc.Initiate` directement reste verte si la route n'existe pas, si
// `Idempotency-Key` n'est jamais lu, ou si `/payments` est mangée par
// `/payments/{id}`. C'est la leçon de #257, et celle du transfert.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// --- le routeur réel, avec un porteur ---

const payeur = "6ab253499cb508f24710c036"

func routeur(t *testing.T, svc *Service, userID, role string) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithClaims(req.Context(), auth.Claims{
				UserID: userID, Role: role, Type: "access",
				Scopes: []string{auth.ScopeCore}, Country: "GA",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
	NewHandler(svc).Mount(r, authMW, false)
	return r
}

func appel(t *testing.T, r chi.Router, method, path, body string, entetes map[string]string) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range entetes {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

// --- le fond du sujet ---

// ⚠️⚠️ LA LIGNE EXISTE MÊME QUAND L'OPÉRATEUR N'A PAS RÉPONDU.
//
// C'est LE défaut de #248 : l'opérateur était contacté AVANT l'écriture, et un
// échec laissait une demande de paiement possible sur le téléphone du client
// sans aucune trace chez nous. La route qui sert à retrouver un paiement perdu
// n'aurait rien trouvé dans le seul cas pour lequel elle existe.
func TestAFailedInitiationStillLeavesAPaymentToFind(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	svc.providers["mock"] = muet{}
	commande := primitive.NewObjectID()
	r := routeur(t, svc, payeur, "client")

	code, _ := appel(t, r, http.MethodPost, "/payments/initiate",
		`{"purpose":"order","amount":2500,"ref_id":"`+commande.Hex()+`"}`, nil)
	require.Equal(t, http.StatusInternalServerError, code,
		"l'initiation a échoué : le client doit le savoir")

	// Et pourtant le paiement se retrouve.
	code, body := appel(t, r, http.MethodGet,
		"/payments?purpose=order&ref_id="+commande.Hex(), "", nil)
	require.Equal(t, http.StatusOK, code)
	items := body["items"].([]any)
	require.Len(t, items, 1, "AVANT : aucune ligne n'existait, et « Réessayer » déclenchait une 2e demande")

	p := items[0].(map[string]any)
	assert.Equal(t, StatusPending, p["status"])
	assert.Equal(t, false, p["operator_reached"],
		"`reached:false` dit « on a demandé et on ne sait pas » — pas « rien n'est parti »")
	assert.Empty(t, p["provider_ref"], "la référence d'ATTENTE ne sort pas sur le fil")
	assert.NotEmpty(t, p["expires_at"], "l'application doit cesser de coder 10 minutes en dur")

	// La référence d'attente est bien en base, et elle est unique.
	require.Len(t, repo.byID, 1)
	for id, stocké := range repo.byID {
		assert.Equal(t, reservedRef(id), stocké.ProviderRef)
	}
}

// ⚠️ DEUX RÉSERVATIONS DE SUITE DOIVENT TENIR DANS L'INDEX UNIQUE.
//
// C'est la raison d'être du préfixe `pending:`. Laisser `provider_ref` vide
// était le geste naturel — et la deuxième réservation serait entrée en
// collision sur la valeur vide, en préprod seulement : l'index y existe déjà
// avec ses anciennes options, et `db.EnsureIndexes` ne fait que JOURNALISER le
// conflit d'un index qu'on tente de redéfinir.
func TestTwoReservationsInARowBothSurviveTheUniqueIndex(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	svc.providers["mock"] = muet{}
	r := routeur(t, svc, payeur, "client")

	for range 2 {
		code, _ := appel(t, r, http.MethodPost, "/payments/initiate",
			`{"purpose":"order","amount":1000,"ref_id":"`+primitive.NewObjectID().Hex()+`"}`, nil)
		require.Equal(t, http.StatusInternalServerError, code)
	}
	assert.Len(t, repo.byID, 2,
		"la 2e réservation a été refusée : les références d'attente ne sont pas uniques")
}

// ⚠️ MÊME CLÉ = MÊME PAIEMENT, ET UNE SEULE DEMANDE À L'OPÉRATEUR.
func TestTheSameIdempotencyKeyReturnsTheSamePaymentAndAsksOnce(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	compteur := &compte{PaymentProvider: svc.providers["mock"]}
	svc.providers["mock"] = compteur
	r := routeur(t, svc, payeur, "client")
	corps := `{"purpose":"wallet_topup","amount":5000,"ref_id":"` + payeur + `"}`

	code, un := appel(t, r, http.MethodPost, "/payments/initiate", corps,
		map[string]string{"Idempotency-Key": "recharge-42"})
	require.Equal(t, http.StatusCreated, code)
	code, deux := appel(t, r, http.MethodPost, "/payments/initiate", corps,
		map[string]string{"Idempotency-Key": "recharge-42"})
	require.Equal(t, http.StatusCreated, code)

	premier := un["payment"].(map[string]any)["id"]
	second := deux["payment"].(map[string]any)["id"]
	assert.Equal(t, premier, second, "la clé doit rendre LE MÊME paiement")
	assert.Equal(t, 1, compteur.appels,
		"⚠️ une 2e demande chez l'opérateur, c'est une 2e notification sur le téléphone du client")
	assert.Len(t, repo.byID, 1)
	assert.Equal(t, un["payment_url"], deux["payment_url"],
		"la reprise doit rendre l'adresse de paiement : sans elle, retrouver le paiement ne sert à rien")
}

// ⚠️ ET LA ROUTE LIT BIEN L'EN-TÊTE.
//
// Mes épreuves d'idempotence passaient le champ dans le CORPS : retirer la
// lecture de `Idempotency-Key` du handler les laissait toutes vertes, et
// l'en-tête — la seule chose que les applications avaient demandée — ne
// servait à rien.
func TestTheRouteReadsTheIdempotencyHeader(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	r := routeur(t, svc, payeur, "client")
	corps := `{"purpose":"wallet_topup","amount":5000,"ref_id":"` + payeur + `"}`

	for range 2 {
		code, _ := appel(t, r, http.MethodPost, "/payments/initiate", corps,
			map[string]string{"Idempotency-Key": "par-en-tete"})
		require.Equal(t, http.StatusCreated, code)
	}
	require.Len(t, repo.byID, 1, "l'en-tête n'a pas été lu : deux paiements ont été créés")
	for _, p := range repo.byID {
		assert.Equal(t, "par-en-tete", p.IdempotencyKey)
	}
}

// Le corps reste accepté : `httpx.Decode` refuse les champs inconnus, et une
// application qui l'aurait deviné aurait récolté un `422` au lieu d'un paiement.
func TestTheBodyFieldWorksToo(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	r := routeur(t, svc, payeur, "client")
	corps := `{"purpose":"wallet_topup","amount":5000,"ref_id":"` + payeur +
		`","idempotency_key":"par-le-corps"}`

	for range 2 {
		code, _ := appel(t, r, http.MethodPost, "/payments/initiate", corps, nil)
		require.Equal(t, http.StatusCreated, code)
	}
	assert.Len(t, repo.byID, 1)
}

// ⚠️ UNE CLÉ NE TRAVERSE PAS LES COMPTES. Une clé est choisie par
// l'application : deux clients peuvent tirer la même, et une clé mondiale
// rendrait alors le paiement d'autrui — avec son montant et son adresse de
// paiement.
func TestAnIdempotencyKeyIsScopedToItsAccount(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	autre := primitive.NewObjectID().Hex()
	corps := func(uid string) string {
		return `{"purpose":"wallet_topup","amount":5000,"ref_id":"` + uid + `"}`
	}
	entete := map[string]string{"Idempotency-Key": "la-meme"}

	code, un := appel(t, routeur(t, svc, payeur, "client"), http.MethodPost,
		"/payments/initiate", corps(payeur), entete)
	require.Equal(t, http.StatusCreated, code)
	code, deux := appel(t, routeur(t, svc, autre, "client"), http.MethodPost,
		"/payments/initiate", corps(autre), entete)
	require.Equal(t, http.StatusCreated, code)

	assert.NotEqual(t, un["payment"].(map[string]any)["id"], deux["payment"].(map[string]any)["id"])
	assert.Len(t, repo.byID, 2)
}

// ⚠️ UNE INITIATION TOMBÉE EN PANNE SE REPREND. Sans cela, la clé
// d'idempotence serait un piège : elle rendrait éternellement un paiement
// mort, que l'opérateur n'a jamais vu, et le client ne pourrait plus payer.
func TestAnUnreachedPaymentIsRetriedUnderTheSameKey(t *testing.T) {
	svc, repo, _, mock := newTestService(t)
	svc.providers["mock"] = muet{}
	r := routeur(t, svc, payeur, "client")
	corps := `{"purpose":"order","amount":3000,"ref_id":"` + primitive.NewObjectID().Hex() + `"}`
	entete := map[string]string{"Idempotency-Key": "reprise"}

	code, _ := appel(t, r, http.MethodPost, "/payments/initiate", corps, entete)
	require.Equal(t, http.StatusInternalServerError, code)

	// L'opérateur revient. La même clé reprend le MÊME paiement, et aboutit.
	svc.providers["mock"] = mock
	code, body := appel(t, r, http.MethodPost, "/payments/initiate", corps, entete)
	require.Equal(t, http.StatusCreated, code)
	assert.Len(t, repo.byID, 1, "la reprise ne doit pas créer un second paiement")
	p := body["payment"].(map[string]any)
	assert.Equal(t, true, p["operator_reached"])
	assert.NotEmpty(t, body["payment_url"])
	assert.NotEmpty(t, p["provider_ref"], "la vraie référence sort, elle")
}

// ⚠️ UN PAIEMENT DÉJÀ PARTI N'EST JAMAIS REDEMANDÉ, même si la clé revient.
func TestAReachedPaymentIsNeverAskedTwice(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	compteur := &compte{PaymentProvider: svc.providers["mock"]}
	svc.providers["mock"] = compteur
	r := routeur(t, svc, payeur, "client")
	corps := `{"purpose":"order","amount":3000,"ref_id":"` + primitive.NewObjectID().Hex() + `"}`
	entete := map[string]string{"Idempotency-Key": "une-fois"}

	for range 3 {
		code, _ := appel(t, r, http.MethodPost, "/payments/initiate", corps, entete)
		require.Equal(t, http.StatusCreated, code)
	}
	assert.Equal(t, 1, compteur.appels)
}

// --- la recherche ---

// ⚠️ ON NE VOIT QUE LES SIENS. Un paramètre `user_id` aurait fait de cette
// route un annuaire des paiements de la plateforme ; il n'y en a pas, et le
// `user_id` ne vient que du jeton.
func TestTheSearchOnlyEverReturnsYourOwnPayments(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	autre := primitive.NewObjectID().Hex()
	commande := primitive.NewObjectID()
	corps := `{"purpose":"order","amount":3000,"ref_id":"` + commande.Hex() + `"}`

	code, _ := appel(t, routeur(t, svc, autre, "client"), http.MethodPost,
		"/payments/initiate", corps, nil)
	require.Equal(t, http.StatusCreated, code)

	// Le payeur cherche le paiement de quelqu'un d'autre, sur une commande
	// dont il connaît l'identifiant.
	code, body := appel(t, routeur(t, svc, payeur, "client"), http.MethodGet,
		"/payments?purpose=order&ref_id="+commande.Hex()+"&user_id="+autre, "", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, body["items"], "les paiements d'autrui ne sortent jamais d'ici")
}

// ⚠️ LA ROUTE EXISTE, et elle n'est pas mangée par `/payments/{id}`.
func TestTheSearchRouteIsNotShadowedByTheSingleRead(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	code, body := appel(t, routeur(t, svc, payeur, "client"), http.MethodGet, "/payments", "", nil)
	require.Equal(t, http.StatusOK, code, "`/payments` doit rendre la LISTE")
	assert.Contains(t, body, "items")
	assert.NotContains(t, body, "error")
}

// ⚠️ UN FILTRE ILLISIBLE EST REFUSÉ, NOMMÉMENT — jamais ignoré. L'ignorer
// rendrait TOUS les paiements du compte à une application qui demandait ceux
// d'une commande : elle en conclurait qu'un paiement est en cours là où il n'y
// en a aucun, et retiendrait « Payer ».
func TestAnUnreadableFilterIsRefusedByName(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	r := routeur(t, svc, payeur, "client")
	for requête, champ := range map[string]string{
		"/payments?purpose=commande": "purpose",
		"/payments?status=en-cours":  "status",
		"/payments?ref_id=pas-un-id": "ref_id",
		"/payments?limit=0":          "limit",
		"/payments?limit=beaucoup":   "limit",
	} {
		code, body := appel(t, r, http.MethodGet, requête, "", nil)
		require.Equal(t, http.StatusUnprocessableEntity, code, requête)
		erreur := body["error"].(map[string]any)
		champs := erreur["fields"].([]any)
		assert.Equal(t, champ, champs[0], "%s doit nommer le champ fautif", requête)
	}
}

// Le plus récent d'abord, et le curseur rend la page suivante.
func TestTheSearchDescendsAndPages(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	uid, err := primitive.ObjectIDFromHex(payeur)
	require.NoError(t, err)
	base := time.Now().UTC()
	for i := range 3 {
		id := primitive.NewObjectID()
		require.NoError(t, repo.Create(context.Background(), &Payment{
			ID: id, UserID: uid, Purpose: PurposeWalletTopup, RefID: uid,
			Status: StatusPending, ProviderRef: reservedRef(id),
			Amount: 1000 * (i + 1), CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}))
	}
	r := routeur(t, svc, payeur, "client")

	code, body := appel(t, r, http.MethodGet, "/payments?limit=2", "", nil)
	require.Equal(t, http.StatusOK, code)
	items := body["items"].([]any)
	require.Len(t, items, 2)
	assert.Equal(t, float64(3000), items[0].(map[string]any)["amount"],
		"le plus récent d'abord")
	assert.Equal(t, true, body["has_more"])
	assert.NotEmpty(t, body["next_cursor"])
}

// --- les portes qui ne doivent jamais s'ouvrir ---

// ⚠️⚠️ UN RAPPEL NE PEUT PAS NOMMER UNE RÉFÉRENCE D'ATTENTE.
//
// Depuis que la ligne est écrite avant l'appel, il existe en base des
// paiements portant `pending:<id>` que l'opérateur n'a jamais vus. Un rappel
// qui en nommerait un le ferait ABOUTIR — un paiement crédité que personne n'a
// réglé.
func TestAWebhookCanNeverNameAReservedReference(t *testing.T) {
	svc, repo, _, mock := newTestService(t)
	svc.providers["mock"] = muet{}
	r := routeur(t, svc, payeur, "client")
	code, _ := appel(t, r, http.MethodPost, "/payments/initiate",
		`{"purpose":"wallet_topup","amount":9000,"ref_id":"`+payeur+`"}`, nil)
	require.Equal(t, http.StatusInternalServerError, code)
	require.Len(t, repo.byID, 1)

	var réservé primitive.ObjectID
	for id := range repo.byID {
		réservé = id
	}
	crédité := false
	svc.OnWalletToppedUp = func(context.Context, string, int, string) error { crédité = true; return nil }

	for _, ref := range []string{reservedRef(réservé), ""} {
		payload := []byte(`{"provider_ref":"` + ref + `","status":"succeeded"}`)
		err := svc.HandleWebhook(context.Background(), "mock", payload, mock.Sign(payload))
		require.Error(t, err, "référence %q : le rappel doit être refusé", ref)
	}
	assert.False(t, crédité, "un paiement que l'opérateur n'a jamais vu a été crédité")
	assert.Equal(t, StatusPending, repo.byID[réservé].Status)
}

// ⚠️ L'OPÉRATEUR A RÉPONDU, ET L'ÉCRITURE DE SA RÉFÉRENCE A ÉCHOUÉ : le trou
// résiduel, nommé dans `ask`. Il ne doit pas se solder par un `201` qui ferait
// croire au client que tout va bien.
func TestAnUnstorableProviderReferenceIsNotReportedAsASuccess(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	repo.attachFails = true
	code, _ := appel(t, routeur(t, svc, payeur, "client"), http.MethodPost, "/payments/initiate",
		`{"purpose":"order","amount":4000,"ref_id":"`+primitive.NewObjectID().Hex()+`"}`, nil)
	assert.Equal(t, http.StatusInternalServerError, code)
	require.Len(t, repo.byID, 1, "la ligne reste, pour que l'exploitation puisse rapprocher")
}

// ⚠️ LES LISTES DE VALEURS SONT ÉCRITES DEUX FOIS : une fois en `oneof` sur le
// DTO, une fois dans `knownPurpose`. Un `purpose` ajouté d'un seul côté
// produirait soit un `422` à l'initiation d'un paiement légitime, soit un
// refus FAUX à la recherche — c'est arrivé ailleurs sur cette plateforme avec
// une liste d'états. Faute de pouvoir n'en écrire qu'une, on les CONFRONTE.
func TestTheValidationTagAndTheSearchAgreeOnThePurposes(t *testing.T) {
	champ, ok := reflect.TypeOf(InitiatePaymentRequest{}).FieldByName("Purpose")
	require.True(t, ok)
	tag := champ.Tag.Get("validate")
	_, après, trouvé := strings.Cut(tag, "oneof=")
	require.True(t, trouvé, "le `oneof` du DTO a disparu : relisez cette épreuve")

	déclarés := strings.Fields(après)
	require.NotEmpty(t, déclarés)
	for _, p := range déclarés {
		assert.True(t, knownPurpose(p),
			"`%s` est accepté à l'initiation et REFUSÉ à la recherche", p)
	}
	for _, p := range []string{
		PurposeOrder, PurposeRide, PurposeRideSubscription,
		PurposeTokenPurchase, PurposeWalletTopup,
	} {
		assert.Contains(t, déclarés, p,
			"`%s` passe la recherche et serait refusé à l'initiation", p)
	}
}

// --- outils ---

// muet est l'opérateur qui ne répond pas : un délai, une coupure, un `5xx`.
type muet struct{}

func (muet) Initiate(context.Context, InitiateRequest) (InitiateResult, error) {
	return InitiateResult{}, errors.New("l'opérateur n'a pas répondu")
}
func (muet) VerifyWebhook([]byte, string) (Event, error) {
	return Event{}, errors.New("muet")
}

// compte compte les demandes réellement parties chez l'opérateur.
type compte struct {
	PaymentProvider
	appels int
}

func (c *compte) Initiate(ctx context.Context, req InitiateRequest) (InitiateResult, error) {
	c.appels++
	return c.PaymentProvider.Initiate(ctx, req)
}

// --- CE QUE LA DOUBLURE NE PROUVE PAS ---
//
// ⚠️⚠️ TROIS MUTATIONS DU DÉPÔT SURVIVAIENT à toutes les épreuves ci-dessus :
// retirer le verrou des références d'attente, retirer le payeur du filtre de
// recherche, laisser une référence déjà obtenue se faire écraser. Les épreuves
// de route passent par `fakeRepo`, qui a ses propres filtres : elles
// certifiaient la DOUBLURE. Les trois suivantes prennent les vraies fonctions.

// Le filtre de recherche, tel qu'il part chez Mongo.
func TestTheOwnerFilterAlwaysCarriesThePayer(t *testing.T) {
	uid := primitive.NewObjectID()
	ref := primitive.NewObjectID()

	// Nu : le payeur, et rien d'autre.
	nu := ownerFilter(PaymentQuery{UserID: uid})
	assert.Equal(t, uid, nu["user_id"], "sans le payeur, la route rend les paiements de la plateforme")
	assert.Len(t, nu, 1, "une clause de confort s'est glissée dans le filtre nu")

	// Garni : le payeur reste.
	garni := ownerFilter(PaymentQuery{
		UserID: uid, Purpose: PurposeOrder, RefID: ref, Status: StatusPending,
	})
	assert.Equal(t, uid, garni["user_id"])
	assert.Equal(t, PurposeOrder, garni["purpose"])
	assert.Equal(t, ref, garni["ref_id"])
	assert.Equal(t, StatusPending, garni["status"])

	// ⚠️ Un identifiant nul échoue FERMÉ : `ObjectID("0…0")` qu'aucun compte ne
	// porte — là où un filtre conditionnel aurait tout ouvert.
	assert.Contains(t, ownerFilter(PaymentQuery{}), "user_id",
		"le payeur doit être au filtre même nul : sinon la recherche s'ouvre")
}

// Le filtre qui empêche d'écraser une référence déjà obtenue.
func TestTheAttachFilterRefusesAPaymentThatAlreadyHasItsReference(t *testing.T) {
	id := primitive.NewObjectID()
	f := attachFilter(id)
	assert.Equal(t, id, f["_id"])
	assert.Equal(t, reservedRef(id), f["provider_ref"],
		"sans cette clause, une reprise écrase la référence de l'opérateur — "+
			"un paiement encaissé que son rappel ne retrouve plus")
}

// ⚠️ LE VERROU DU DÉPÔT RÉPOND AVANT DE TOUCHER LA BASE, et c'est la
// collection ABSENTE qui le prouve : si le verrou sautait, la lecture
// paniquerait ici au lieu de rendre un refus.
func TestTheRepositoryRefusesAReservedReferenceWithoutReadingAnything(t *testing.T) {
	sansBase := &Repository{} // col nil : toute lecture paniquerait
	for _, ref := range []string{"", reservedRef(primitive.NewObjectID()), "pending:nimporte"} {
		p, err := sansBase.FindByProviderRef(context.Background(), ref)
		require.Error(t, err, "référence %q", ref)
		assert.Nil(t, p)
	}
	assert.False(t, usableProviderRef(""))
	assert.False(t, usableProviderRef(reservedRef(primitive.NewObjectID())))
	assert.True(t, usableProviderRef("mock-abcdef"), "une vraie référence passe")
}

// ⚠️⚠️ ET LE DÉPÔT PASSE BIEN PAR `attachFilter`.
//
// La dernière mutation qui survivait : remplacer `attachFilter(id)` par un
// `bson.M{"_id": id}` écrit sur place. La doublure, elle, APPELLE le monteur —
// elle reste donc protégée, et aucune épreuve ne rougissait. Sans base dans
// les épreuves, le filtre réel n'est observable que dans la SOURCE.
//
// C'est la même mécanique que le garde des frontières pays : quand le défaut
// ne vit ni dans une valeur ni dans une réponse mais dans la FORME du code,
// l'épreuve lit le code. Et elle DÉCOUVRE sa cible (elle localise la fonction
// par son nom) plutôt que de recopier son corps, pour ne pas rougir au premier
// retour à la ligne.
func TestAttachProviderRefBuildsItsFilterWithTheTestedHelper(t *testing.T) {
	src, err := os.ReadFile("repository.go")
	require.NoError(t, err, "le dépôt est introuvable : ajustez le chemin")

	_, après, trouvé := strings.Cut(string(src), "func (r *Repository) AttachProviderRef(")
	require.True(t, trouvé, "AttachProviderRef a été renommée : relisez cette épreuve")
	corps, _, trouvé := strings.Cut(après, "\nfunc ")
	require.True(t, trouvé)

	assert.Contains(t, corps, "attachFilter(id)",
		"le filtre doit venir du monteur éprouvé : écrit sur place, il n'est plus vérifiable")
	assert.NotContains(t, corps, `bson.M{"_id": id}`,
		"un filtre qui ne nomme que `_id` laisse écraser une référence déjà obtenue")
}

// --- LES BORNES DE RECHARGE SONT APPLIQUÉES, PAS SEULEMENT ANNONCÉES (#250) ---
//
// ⚠️⚠️ C'EST EXACTEMENT LE DÉFAUT DE #251 QU'ON ÉVITE ICI. `GET /wallet`
// annonce désormais un minimum et un maximum de recharge par pays. Une borne
// qu'aucune route ne refuse n'est pas une borne, c'est une suggestion : l'app
// cacherait le bouton, et un appel direct passerait outre.

// bornes est la politique d'un pays, telle que le câblage la fournit.
type bornes struct{ min, max int }

func (b bornes) TopupLimitsOf(context.Context, string) (int, int, string) {
	return b.min, b.max, "XOF"
}

func TestATopUpOutsideTheCountrysBoundsIsRefusedByTheRoute(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	svc.SetTopupLimits(bornes{min: 500, max: 2_000_000})
	r := routeur(t, svc, payeur, "client")

	for _, c := range []struct {
		montant int
		code    string
	}{
		{100, "topup_below_minimum"},
		{499, "topup_below_minimum"},
		{2_000_001, "topup_above_maximum"},
	} {
		code, body := appel(t, r, http.MethodPost, "/payments/initiate",
			`{"purpose":"wallet_topup","amount":`+strconv.Itoa(c.montant)+`,"ref_id":"`+payeur+`"}`, nil)
		require.Equal(t, http.StatusUnprocessableEntity, code, "montant %d", c.montant)
		erreur := body["error"].(map[string]any)
		assert.Equal(t, c.code, erreur["code"],
			"⚠️ un refus que l'application doit LIRE a besoin de SON code, "+
				"pas d'un `validation_failed` générique")
		champs := erreur["fields"].([]any)
		assert.Equal(t, "amount", champs[0])
	}
	assert.Empty(t, repo.byID,
		"⚠️ un montant hors bornes ne doit laisser AUCUNE ligne derrière lui — "+
			"et surtout pas une ligne que la clé d'idempotence rendrait pour toujours")

	// Dans les bornes, ça passe.
	code, _ := appel(t, r, http.MethodPost, "/payments/initiate",
		`{"purpose":"wallet_topup","amount":5000,"ref_id":"`+payeur+`"}`, nil)
	assert.Equal(t, http.StatusCreated, code)
}

// ⚠️ LES BORNES DE RECHARGE NE BORNENT QUE LES RECHARGES. Le prix d'une course,
// d'un panier ou d'un abonnement est décidé par la verticale qui l'a calculé :
// les borner ici aurait fait refuser une commande de restaurant au motif
// qu'elle dépasse le plafond de recharge du pays.
func TestTheTopUpBoundsNeverTouchTheOtherPurposes(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	svc.SetTopupLimits(bornes{min: 500, max: 10_000})
	r := routeur(t, svc, payeur, "client")

	for _, purpose := range []string{PurposeOrder, PurposeRide, PurposeRideSubscription} {
		code, body := appel(t, r, http.MethodPost, "/payments/initiate",
			`{"purpose":"`+purpose+`","amount":250000,"ref_id":"`+primitive.NewObjectID().Hex()+`"}`, nil)
		assert.Equal(t, http.StatusCreated, code,
			"`%s` à 250 000 doit passer : %v", purpose, body["error"])
	}
}

// Sans bornes câblées, rien ne change : seul `amount > 0` s'applique, comme
// avant. Un service de développement sans pays réglés garde ses paiements.
func TestWithoutWiredBoundsNothingIsRefused(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	code, _ := appel(t, routeur(t, svc, payeur, "client"), http.MethodPost, "/payments/initiate",
		`{"purpose":"wallet_topup","amount":1,"ref_id":"`+payeur+`"}`, nil)
	assert.Equal(t, http.StatusCreated, code)
}
