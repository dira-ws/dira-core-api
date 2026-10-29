package appconfig

// LE PONT D'UNE VERTICALE VERS LE REGISTRE — une seule fois, pour les deux.
//
// ⚠️⚠️ ICI PLUTÔT QUE DANS CHAQUE VERTICALE, ET C'EST LA LEÇON DE LA JOURNÉE.
// Les courses ont eu ce pont dans leur `internal/corebridge` ; la livraison
// s'apprêtait à le RECOPIER pour deux routes. Une règle écrite deux fois est
// perdue une fois — et ce pont ne porte pas une règle, il en porte cinq, toutes
// subtiles :
//
//   · un registre nil laisse TOUT passer (le défaut est « disponible ») ;
//   · mais le DERNIER REGISTRE CONNU passe avant nil, sinon une panne
//     passagère rouvre en silence ce qu'on vient de couper ;
//   · une erreur RÉSEAU et un refus HTTP sont deux chemins distincts ;
//   · un corps lisible derrière un `500` n'est PAS un registre ;
//   · un socle non configuré ne tente même pas l'appel.
//
// Quatre de ces cinq ont été trouvées par des mutations qui passaient au vert.
// Les laisser se recopier à la main dans une seconde verticale revenait à
// parier qu'on les retrouverait toutes, dans l'ordre, de mémoire.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/edition"
	"github.com/kgtech-org/dira-core-api/pkg/obs"
)

// clientTTL : on relit le registre à ce rythme.
//
// ⚠️ UNE MINUTE, COMME LE LEXIQUE, et pour la même raison : c'est le temps
// qu'il faut à un exploitant pour aller voir si sa coupure a pris. Plus court
// ferait un appel au socle par course ; plus long le ferait douter de son
// propre écran.
const clientTTL = time.Minute

// Client lit le registre BRUT au socle et le résout localement.
//
// ⚠️⚠️ LE REGISTRE BRUT, PAS LA VUE RÉSOLUE, et c'est la décision qui évite un
// appel HTTP par requête. `GET /app/config` rend « ce que voit Lite au Gabon »
// — une réponse juste pour UN pays et UNE édition. Or une verticale sert deux
// éditions et quatorze pays : il faudrait interroger le socle à chaque appel,
// sur le chemin d'un passager qui attend devant son écran. Le registre entier
// tient en quelques centaines d'octets et se résout ici pour le pays ET
// l'édition de CHAQUE appel.
type Client struct {
	baseURL string
	token   string
	http    *http.Client

	mu     sync.Mutex
	value  *Registry
	readAt time.Time
	// known : le dernier registre LU, gardé même expiré — voir `Registry`.
	known *Registry
}

// NewClient monte le pont. baseURL vide = pont INERTE : rien n'est coupé, et
// aucun appel n'est tenté.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		// COURT : un passager attend devant son écran. Mieux vaut servir la
		// plateforme entière qu'une roue qui tourne.
		http: obs.HTTPClient("core", 5*time.Second),
	}
}

// Enabled dit si le socle est configuré.
func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" }

// Registry rend le registre des éditions, ou nil.
//
// ⚠️⚠️ NIL VEUT DIRE « RIEN N'EST COUPÉ », et c'est le bon sens du repli :
// `Registry` est écrit pour qu'un registre nil laisse TOUT passer. Un socle
// muet rend donc la plateforme entière, pas une plateforme amputée — une
// coupure de réseau ne doit pas vider le choix des véhicules d'un pays.
//
// ⚠️ MAIS LE DERNIER REGISTRE CONNU PASSE AVANT NIL. Si on a déjà lu les
// règles une fois, une panne passagère ne doit pas ROUVRIR ce que
// l'exploitation vient de couper : elle le rouvrirait silencieusement, pour la
// durée de la panne, et plus rien ne le signalerait.
func (c *Client) Registry(ctx context.Context) *Registry {
	// ⚠️ ET UN SOCLE NON CONFIGURÉ NE TENTE MÊME PAS L'APPEL. Sans ce garde,
	// chaque course d'un service de développement partirait sur une adresse
	// vide et attendrait l'échec — cinq secondes par appel, pour une réponse
	// qu'on sait d'avance.
	if c == nil || !c.Enabled() {
		return nil
	}
	c.mu.Lock()
	if c.value != nil && time.Since(c.readAt) < clientTTL {
		reg := c.value
		c.mu.Unlock()
		return reg
	}
	known := c.known
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/internal/app-config", nil)
	if err != nil {
		return known
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// ⚠️ CHEMIN DE L'ERREUR RÉSEAU, distinct du refus HTTP juste en
		// dessous : une mutation passait au vert parce qu'une seule épreuve
		// exerçait le second.
		slog.DebugContext(ctx, "appconfig: registre illisible", "error", err)
		return known
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// ⚠️ ET UN CORPS LISIBLE DERRIÈRE UN `500` N'EST PAS UN REGISTRE. Une
		// passerelle, une page d'erreur en JSON, un service qui démarre
		// peuvent répondre `{}` : décodé et mis en cache, il rouvrirait tout
		// ce qu'on venait de couper pendant une minute.
		slog.DebugContext(ctx, "appconfig: registre refusé", "status", resp.StatusCode)
		return known
	}
	var reg Registry
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		slog.DebugContext(ctx, "appconfig: registre indécodable", "error", err)
		return known
	}
	c.mu.Lock()
	c.value, c.known, c.readAt = &reg, &reg, time.Now()
	c.mu.Unlock()
	return &reg
}

// Allows dit si cette fonction est servie pour le pays ET l'édition de l'appel.
//
// ⚠️ LE DÉFAUT EST OUI. Registre nil, clé inconnue, socle injoignable : la
// fonction est servie. C'est ce qui rend cette couche sûre à poser sur une
// plateforme qui tourne.
func (c *Client) Allows(ctx context.Context, feature string) bool {
	return c.Registry(ctx).Allows(feature,
		country.FromContext(ctx), edition.FromContext(ctx))
}

// AllowedIn croise une liste que LA VERTICALE sert avec la liste blanche du
// socle.
//
// ⚠️ UNE INTERSECTION, L'ORDRE DU MÉTIER CONSERVÉ, et une liste blanche qui ne
// croise RIEN rend la liste entière — jamais une liste vide. Les trois
// garde-fous vivent dans `AllowedIn`, un seul endroit pour les deux verticales.
func (c *Client) AllowedIn(ctx context.Context, list string, served []string) []string {
	return c.Registry(ctx).AllowedIn(list,
		country.FromContext(ctx), edition.FromContext(ctx), served)
}

// Require refuse une action vers une fonction retirée de cette édition.
//
// ⚠️ C'EST LE REFUS DU PAQUET, pas une copie : son code est ce que les
// applications lisent pour cacher un bouton. Il satisfait aussi
// `middleware.FeatureGate`, pour qu'une verticale monte le même intergiciel
// que le socle.
func (c *Client) Require(ctx context.Context, feature string) error {
	if c.Allows(ctx, feature) {
		return nil
	}
	return Refuse(feature)
}
