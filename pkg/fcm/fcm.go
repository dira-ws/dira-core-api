// Package fcm envoie des notifications par Firebase Cloud Messaging (API v1).
//
// L'API v1 s'authentifie par un jeton OAuth2 obtenu à partir d'un compte de
// service. Le SDK officiel de Google le ferait, au prix d'un arbre de
// dépendances considérable pour deux appels HTTP : on signe l'assertion
// nous-mêmes avec la bibliothèque JWT déjà présente.
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	tokenURL    = "https://oauth2.googleapis.com/token"
	scope       = "https://www.googleapis.com/auth/firebase.messaging"
	sendBase    = "https://fcm.googleapis.com"
	sendPathFmt = "/v1/projects/%s/messages:send"
	// assertionTTL : Google plafonne à une heure. On reste dessous.
	assertionTTL = 45 * time.Minute
	// refreshMargin : on renouvelle AVANT l'expiration. Sans marge, un envoi
	// parti à la seconde près échouerait en 401 pour rien.
	refreshMargin = 2 * time.Minute
)

// ServiceAccount est le strict nécessaire du JSON de compte de service.
type ServiceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

// Client envoie à FCM.
type Client struct {
	sa   ServiceAccount
	key  any
	http *http.Client
	// sendBase est l'hôte de l'API d'envoi. Un champ, et non une constante,
	// pour que le client puisse être exercé contre un serveur de test — le
	// chemin traversé reste ENTIER : même requête, mêmes en-têtes, même
	// lecture de la réponse d'erreur.
	sendBase string

	mu      sync.Mutex
	token   string
	expires time.Time
}

// ErrNotConfigured dit qu'aucun compte de service n'est fourni.
var ErrNotConfigured = errors.New("fcm: no service account configured")

// New construit un client à partir du JSON de compte de service.
//
// Un JSON vide rend (nil, ErrNotConfigured) plutôt qu'une erreur fatale : une
// plateforme sans notifications doit démarrer, en le disant.
func New(serviceAccountJSON string) (*Client, error) {
	if strings.TrimSpace(serviceAccountJSON) == "" {
		return nil, ErrNotConfigured
	}
	var sa ServiceAccount
	if err := json.Unmarshal([]byte(serviceAccountJSON), &sa); err != nil {
		return nil, fmt.Errorf("fcm: service account json: %w", err)
	}
	if sa.ProjectID == "" || sa.PrivateKey == "" || sa.ClientEmail == "" {
		return nil, errors.New("fcm: service account missing project_id, private_key or client_email")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(sa.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("fcm: private key: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = tokenURL
	}
	return &Client{
		sa:       sa,
		key:      key,
		sendBase: sendBase,
		// Une notification est utile MAINTENANT. Attendre trente secondes un
		// service injoignable retiendrait la requête métier qui l'a déclenchée.
		http: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// ProjectID rend le projet Firebase servi (journalisation, diagnostic).
func (c *Client) ProjectID() string { return c.sa.ProjectID }

// Message est une notification à envoyer à UN appareil.
type Message struct {
	Token string
	Title string
	Body  string
	// Data voyage avec la notification et sert à l'application : ouvrir la
	// bonne commande plutôt que l'écran d'accueil. Toutes les valeurs sont
	// des CHAÎNES — FCM refuse le reste.
	Data map[string]string
	// DataOnly : AUCUNE notification n'est jointe, seulement `Data`. Le
	// système n'affiche rien ; c'est l'application, réveillée, qui décide —
	// un appel de course construit son propre écran plein, avec sa sonnerie.
	// Une notification système par-dessus ferait deux alertes pour un appel.
	DataOnly bool
	// TTL borne la durée pendant laquelle FCM garde le message si l'appareil
	// est injoignable. Un appel de 30 s reçu deux minutes plus tard est un
	// appel fermé : mieux vaut qu'il n'arrive jamais. Zéro = défaut FCM.
	//
	// ⚠️ IL S'APPLIQUE AUX DEUX SYSTÈMES DEPUIS LA 4.70.0. Il ne descendait
	// que dans `android.ttl` : APNs gardait donc le message et pouvait livrer
	// un appel DÉJÀ FERMÉ plusieurs minutes plus tard — exactement ce que ce
	// champ existe pour empêcher. Signalé par l'équipe iOS (socle #193).
	TTL time.Duration

	// Ring : ce message doit SONNER sur un iPhone.
	//
	// ⚠️ UN MESSAGE SILENCIEUX NE PEUT PAS PORTER UN APPEL SUR iOS, et c'est
	// la raison d'être de ce champ. Android construit lui-même son écran
	// plein à partir de `Data`, réveillé par un message silencieux de priorité
	// haute ; iOS, non :
	//
	//   - APNs REFUSE `apns-priority: 10` sur un message silencieux — un
	//     `content-available` seul doit partir en priorité 5 ;
	//   - et même en priorité 5, iOS ne réveille PAS une application que
	//     l'utilisateur a fermée, et plafonne les messages silencieux à
	//     quelques-uns par heure. Un appel de trente secondes ne peut pas
	//     compter là-dessus.
	//
	// Avec `Ring`, l'appel part en notification VISIBLE : le système l'affiche
	// et la fait sonner, application fermée comprise. `Data` voyage quand
	// même — c'est par `call_id` que l'application dédoublonne avec la trame
	// du socket.
	Ring bool
	// Sound est le fichier de sonnerie EMBARQUÉ DANS L'APPLICATION
	// (`ring_classic.caf`…). Vide = le son par défaut du système, qui ne
	// sonne pas assez longtemps pour un appel de course.
	Sound string
	// Category est la catégorie iOS du message (`RIDE_CALL`) : c'est elle qui
	// dit à l'application quel écran ouvrir et quelles actions proposer sur la
	// notification.
	Category string
	// CollapseID : les messages portant le même identifiant SE REMPLACENT sur
	// l'écran verrouillé au lieu de s'empiler.
	//
	// ⚠️ SANS LUI, DEUX CHANGEMENTS DE STATUT D'UNE MÊME COURSE FONT DEUX
	// LIGNES. Une course qui passe par cinq états en laisse cinq, et la
	// dernière — la seule vraie — se lit au milieu de quatre périmées.
	// Borné à 64 octets par APNs ; au-delà, le message entier serait refusé.
	CollapseID string
	// Badge est le nombre à afficher sur l'icône. Nil = ne pas y toucher (un
	// zéro explicite l'effacerait).
	Badge *int
}

// Les valeurs d'en-tête APNs, nommées là où elles sont décidées.
const (
	pushTypeAlert      = "alert"
	pushTypeBackground = "background"
	// priorityNow : livrer tout de suite. Sur un réseau ouest-africain, une
	// notification différée par l'économie d'énergie arrive après la course
	// qu'elle annonçait.
	priorityNow = "10"
	// priorityThrottled : le SEUL niveau qu'APNs accepte sur un message
	// silencieux. En 10, il refuse le message.
	priorityThrottled = "5"
	// collapseIDMax : la borne d'APNs. Un identifiant plus long fait refuser
	// le message ENTIER — on tronque plutôt que de perdre la notification.
	collapseIDMax = 64
)

// Result dit ce qu'il est advenu d'un envoi.
type Result struct {
	Token string
	Err   error
	// Unregistered : FCM déclare ce jeton MORT (application désinstallée,
	// jeton remplacé). À distinguer d'une panne : un jeton mort ne guérira
	// pas, et le réessayer indéfiniment finit par faire refuser le projet.
	Unregistered bool
}

// Send envoie un message par appareil.
//
// UN APPEL PAR JETON, parce que l'API v1 n'a pas d'envoi groupé (`send/batch`
// est retiré depuis 2024). L'important est que l'échec d'un appareil
// n'emporte pas les autres : chaque résultat est rendu séparément.
func (c *Client) Send(ctx context.Context, msgs []Message) ([]Result, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		// Panne d'authentification : AUCUN appareil n'est en cause, et il ne
		// faut surtout pas les marquer morts.
		return nil, err
	}
	out := make([]Result, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, c.sendOne(ctx, token, m))
	}
	return out, nil
}

// apns construit le bloc APNs du message.
//
// ⚠️ IL EST CONSTRUIT POUR TOUS LES APPAREILS, SANS REGARDER LEUR PLATEFORME,
// et ce n'est pas un raccourci : FCM ne lit ce bloc QUE lorsqu'il livre à un
// jeton APNs — un appareil Android l'ignore entièrement, et c'est le bloc
// `android` qui le sert. Conditionner sur la plateforme enregistrée aurait
// ajouté une branche pour le même résultat, et aurait LAISSÉ DE CÔTÉ les
// appareils dont la plateforme n'a jamais été notée (les premières versions
// de `POST /me/devices` ne l'exigeaient pas) : un iPhone inscrit sans
// `platform` serait resté sur l'enveloppe qui ne sonne pas.
//
// Android ne change donc pas « parce qu'on l'a épargné », mais parce que rien
// de ce qui suit ne le concerne.
func (m Message) apns(now time.Time) map[string]any {
	headers := map[string]string{}
	aps := map[string]any{}
	switch {
	case m.Ring:
		// L'APPEL : visible, prioritaire, sonore, et marqué « sensible au
		// temps » — c'est ce niveau d'interruption qui le fait passer même en
		// mode concentration, ce qu'un appel de trente secondes exige.
		headers["apns-push-type"] = pushTypeAlert
		headers["apns-priority"] = priorityNow
		aps["alert"] = map[string]any{"title": m.Title, "body": m.Body}
		aps["interruption-level"] = "time-sensitive"
		if m.Sound != "" {
			aps["sound"] = m.Sound
		}
		if m.Category != "" {
			aps["category"] = m.Category
		}
	case m.DataOnly:
		// ⚠️ PRIORITÉ 5, ET CE N'EST PAS NÉGOCIABLE : APNs REFUSE un message
		// silencieux en priorité 10. C'est la faute que cette fonction
		// corrige — l'ancien code envoyait `content-available` en 10, et APNs
		// rejetait l'envoi.
		headers["apns-push-type"] = pushTypeBackground
		headers["apns-priority"] = priorityThrottled
		aps["content-available"] = 1
	default:
		// Une notification ordinaire : le texte vient du bloc `notification`
		// de FCM, mais le TYPE doit être déclaré — sans lui, APNs devine, et
		// devine mal sur les versions récentes d'iOS.
		headers["apns-push-type"] = pushTypeAlert
		headers["apns-priority"] = priorityNow
	}
	if m.Badge != nil {
		aps["badge"] = *m.Badge
	}
	if m.CollapseID != "" {
		id := m.CollapseID
		if len(id) > collapseIDMax {
			id = id[:collapseIDMax]
		}
		headers["apns-collapse-id"] = id
	}
	// ⚠️ L'ÉCHÉANCE EST UN INSTANT, pas une durée : APNs attend un
	// horodatage Unix absolu. Passé lui, il JETTE le message au lieu de le
	// livrer — c'est ce qui garantit qu'un appel fermé n'arrive jamais.
	if m.TTL > 0 {
		headers["apns-expiration"] = strconv.FormatInt(now.Add(m.TTL).Unix(), 10)
	}
	out := map[string]any{"headers": headers}
	if len(aps) > 0 {
		out["payload"] = map[string]any{"aps": aps}
	}
	return out
}

func (c *Client) sendOne(ctx context.Context, accessToken string, m Message) Result {
	// Priorité haute sur Android : sur un réseau ouest-africain, une
	// notification différée par le mode économie d'énergie arrive après la
	// livraison qu'elle annonçait.
	android := map[string]any{"priority": "high"}
	if m.TTL > 0 {
		android["ttl"] = fmt.Sprintf("%ds", int(m.TTL.Seconds()))
	}
	message := map[string]any{
		"token":   m.Token,
		"data":    m.Data,
		"android": android,
		"apns":    m.apns(time.Now()),
	}
	if !m.DataOnly {
		message["notification"] = map[string]any{"title": m.Title, "body": m.Body}
	}
	payload := map[string]any{"message": message}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{Token: m.Token, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.sendBase+fmt.Sprintf(sendPathFmt, c.sa.ProjectID), bytes.NewReader(body))
	if err != nil {
		return Result{Token: m.Token, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{Token: m.Token, Err: fmt.Errorf("fcm: send: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 {
		return Result{Token: m.Token}
	}
	var e sendError
	_ = json.NewDecoder(resp.Body).Decode(&e)
	return Result{
		Token: m.Token,
		Err:   fmt.Errorf("fcm: send: %d %s: %s", resp.StatusCode, e.Error.Status, e.Error.Message),
		// UNREGISTERED (404) et INVALID_ARGUMENT SUR LE JETON (400) désignent
		// un appareil qui ne reviendra pas. 401/403/5xx sont des pannes : le
		// jeton n'y est pour rien et doit être conservé.
		Unregistered: resp.StatusCode == http.StatusNotFound ||
			e.Error.Status == "UNREGISTERED" || e.Error.Status == "NOT_FOUND" ||
			invalidToken(e),
	}
}

// invalidToken dit si un `INVALID_ARGUMENT` porte SUR LE JETON.
//
// ⚠️ LE COMMENTAIRE LE PROMETTAIT DEPUIS LE DÉBUT, LE CODE NE LE FAISAIT PAS.
// Les premières versions des applications iOS envoyaient le jeton APNs brut
// (64 caractères hexadécimaux) là où FCM attend un jeton d'enregistrement :
// chaque envoi échouait en `INVALID_ARGUMENT`, et comme ce n'est ni un 404 ni
// un `UNREGISTERED`, l'appareil restait actif — réessayé à chaque
// notification, indéfiniment. Signalé par l'équipe iOS (socle #193).
//
// ⚠️ ET ON NE PEUT PAS SE CONTENTER DU STATUT : `INVALID_ARGUMENT` couvre
// aussi un message MAL FORMÉ de notre côté. Le traiter comme une mort
// d'appareil désactiverait TOUTE LA FLOTTE au premier bogue de sérialisation,
// en une rafale, sans que personne puisse le défaire. On exige donc que FCM
// désigne nommément le champ `message.token`.
func invalidToken(e sendError) bool {
	if e.Error.Status != "INVALID_ARGUMENT" {
		return false
	}
	for _, d := range e.Error.Details {
		for _, v := range d.FieldViolations {
			if v.Field == tokenField {
				return true
			}
		}
	}
	return false
}

// tokenField : le chemin sous lequel FCM désigne le jeton dans ses violations.
const tokenField = "message.token"

// sendError est la réponse d'erreur de FCM v1, dans ce qu'on en lit.
type sendError struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		// Details porte les violations de champ : c'est le seul endroit où
		// FCM dit QUEL champ il refuse.
		Details []struct {
			FieldViolations []struct {
				Field string `json:"field"`
			} `json:"fieldViolations"`
		} `json:"details"`
	} `json:"error"`
}

// accessToken rend un jeton d'accès valide, renouvelé au besoin.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires.Add(-refreshMargin)) {
		return c.token, nil
	}
	now := time.Now()
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   c.sa.ClientEmail,
		"scope": scope,
		"aud":   c.sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(assertionTTL).Unix(),
	}).SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("fcm: sign assertion: %w", err)
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sa.TokenURI,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm: token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("fcm: token: status %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("fcm: token: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("fcm: token: empty access_token")
	}
	c.token = out.AccessToken
	c.expires = now.Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}
