// Package diramaps talks to the dira-maps SIG for road-network routing.
//
// ⚠️ dira-maps ne rend AUCUN fond de carte : c'est du JSON sur HTTP — un tracé
// routier et des adresses. Le fond de carte vient du composant natif du
// téléphone, côté application.
package diramaps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/obs"
)

// HeaderAPIKey est l'en-tête par lequel le SIG reconnaît son appelant. Le nom
// est un contrat EXTERNE (dira-maps l'accepte aussi en `?key=`), donc il est
// écrit tel quel dans les ponts des autres dépôts plutôt que partagé par un
// import : une montée de `go.mod` pour une chaîne de dix caractères coûterait
// plus que la duplication.
const HeaderAPIKey = "X-Api-Key"

// Client calls the SIG. Base d'URL DISTINCTE de l'API et du suivi : les trois
// services se déplacent indépendamment.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds the client. Une base vide rend un client INERTE plutôt que nil :
// l'appelant n'a pas à tester la présence à chaque usage, et l'absence de SIG
// se traduit par une absence de durée, pas par une panne.
//
// ⚠️⚠️ LA CLÉ EST UN ARGUMENT, PAS UNE OPTION, ET C'EST DÉLIBÉRÉ. Le SIG
// identifie ses appelants par clé, avec un quota et un décompte par clé. Le
// détail de l'exploitation est suivi dans le dépôt privé du SIG (dira-maps#67) ;
// ce qui compte ici est la forme du constructeur.
//
// En faire un argument fait nommer chaque appelant PAR LE COMPILATEUR. Une
// option, ou un `SetKey` facultatif, s'oublie — et un client qui ne porte pas
// sa clé ne provoque aucune panne visible, parce que le SIG est AU MIEUX : les
// distances retombent à vol d'oiseau, les durées disparaissent, et rien ne se
// lève. C'est exactement le genre d'oubli qui dure des mois.
//
// Une clé vide reste acceptée, pour que la transition soit possible service par
// service. Mais l'appelant doit le JOURNALISER au démarrage : c'est la seule
// chose qui rend l'oubli visible avant qu'il coûte.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		// Court, et c'est délibéré : la durée est un CONFORT. Retenir la
		// création d'une course pendant trente secondes parce qu'un service
		// de cartographie rame serait un très mauvais échange.
		http: obs.HTTPClient("maps", 4*time.Second),
	}
}

// Enabled dit si un SIG est configuré.
func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" }

// routeURL monte l'adresse du calcul d'itinéraire, QUE LA BASE PORTE OU NON
// SON `/api`.
//
// ⚠️⚠️ ELLE EXISTE PARCE QUE LA DURÉE D'UNE TOURNÉE N'A JAMAIS MARCHÉ EN
// PRÉPROD, et que personne ne pouvait le voir. `MAPS_BASE_URL` y vaut
// `…:8000/api` — comme pour le suivi — et ce client ajoutait `/api/calc/route`
// par-dessus : chaque appel partait sur `/api/api/calc/route`. Comme la durée
// est AU MIEUX, l'échec se journalisait et la course se créait sans elle.
// Relevé en vérifiant le tracé prévu en direct (core#230).
//
// ⚠️ ET LA LEÇON ÉTAIT DÉJÀ APPRISE À CÔTÉ. `dira-vtc-api/internal/mapsbridge`
// porte ce commentaire mot pour mot : « baseURL porte DÉJÀ le `/api` […]
// l'ajouter ici produisait `/api/api/calc/route` ». Deux clients du même
// service, deux conventions, et le PARTAGÉ était celui qui avait tort. On ne
// choisit donc pas une convention : on accepte les deux.
func (c *Client) routeURL() string {
	base := strings.TrimRight(c.baseURL, "/")
	if strings.HasSuffix(base, "/api") {
		return base + "/calc/route"
	}
	return base + "/api/calc/route"
}

// Route est ce que le réseau routier dit d'une tournée : sa durée, sa
// DISTANCE par la route, et son TRACÉ.
//
// ⚠️⚠️ LE TRACÉ NE TARIFIE RIEN, ET C'EST UNE DÉCISION ÉCRITE AILLEURS AUSSI.
// La distance qui fixe le prix d'une course de REPAS reste calculée
// localement, à vol d'oiseau entre les arrêts : la faire dépendre d'un service
// externe ferait varier le prix d'une commande selon la disponibilité d'un
// tiers. Ce tracé sert à DESSINER, et seulement à dessiner.
//
// Il est donc AU MIEUX, comme la durée : un SIG muet rend une tournée sans
// tracé, jamais une erreur qui empêcherait de créer la course.
type Route struct {
	DurationS int
	// DistanceM : la distance PAR LA ROUTE, en mètres, telle que le SIG la
	// rend (`routes[].distance_m`), arrondie au mètre.
	//
	// ⚠️ ZÉRO N'EST PAS UNE DISTANCE : c'est un SIG muet, une réponse sans le
	// champ (les réponses consolidées n'en portent pas toujours), ou un nom de
	// champ qui a changé. Qui tarifie au kilomètre par la route (le colis) doit
	// le lire comme « pas de distance » (`HasDistance`) et refuser de vendre —
	// jamais retomber sur la ligne droite, ni facturer zéro kilomètre.
	DistanceM int
	// Geometry : les points du tracé, `[lng, lat]`, dans l'ordre de GeoJSON et
	// de toute cette plateforme. Vide si le moteur ne l'a pas rendue — on ne
	// l'invente pas, et surtout on ne retombe pas sur la ligne droite : une
	// ligne droite présentée comme un itinéraire est exactement le défaut
	// qu'on corrige (voir core#230).
	Geometry [][2]float64
}

// HasDistance dit si le SIG a rendu une distance par la route.
//
// ⚠️ UNE DISTANCE NULLE OU NÉGATIVE N'EN EST PAS UNE : un trajet de zéro mètre
// ne coûterait que la prise en charge, et c'est exactement ce que produirait
// un champ manquant — une erreur qui facture moins, donc que personne ne
// signale.
func (r Route) HasDistance() bool { return r.DistanceM > 0 }

// Polyline encode le tracé pour le fil, ou rend "" s'il n'y en a pas.
func (r Route) Polyline() string {
	if len(r.Geometry) < 2 {
		return ""
	}
	return EncodePolyline(r.Geometry)
}

// RouteDuration ne rend que la durée — l'appelant qui n'a pas besoin du tracé.
func (c *Client) RouteDuration(ctx context.Context, city string, points [][2]float64) (int, error) {
	r, err := c.Route(ctx, city, points)
	return r.DurationS, err
}

// RouteDurationAs : la durée, pour un mode donné.
func (c *Client) RouteDurationAs(ctx context.Context, city string, points [][2]float64, mode string) (int, error) {
	r, err := c.RouteAs(ctx, city, points, mode)
	return r.DurationS, err
}

// LES MODES DE DÉPLACEMENT que le SIG sait router.
//
// ⚠️⚠️ ILS NE SONT PAS DÉCORATIFS. Le SIG tient un GRAPHE PAR PROFIL : une
// moto emprunte des passages qu'une voiture ne prend pas, un tricycle est trop
// large pour ceux de la moto et interdit de voie rapide. Demander « voiture »
// pour une moto, c'est annoncer un délai qu'elle battra et facturer une
// distance qu'elle n'a pas parcourue — dans les deux sens, c'est de l'argent.
//
// ⚠️ ET CE DÉFAUT A VÉCU : ce client écrivait `"driving"` EN DUR. Le SIG sait
// router une moto et un tricycle depuis le premier jour, il sait choisir
// l'instance correspondante — et le seul appelant de la plateforme disait
// toujours « voiture ». Toute la chaîne était en place sauf sa première ligne.
const (
	ModeDriving    = "driving"
	ModeMotorcycle = "motorcycle"
	ModeTricycle   = "tricycle"
	ModeWalking    = "walking"
	ModeCycling    = "cycling"
)

// Route interroge le SIG, en VOITURE.
//
// Conservée telle quelle pour les appelants qui routent un trajet sans
// véhicule nommé — un devis d'adresse à adresse, une distance de contrôle.
// Celui qui connaît le véhicule appelle `RouteAs`.
func (c *Client) Route(ctx context.Context, city string, points [][2]float64) (Route, error) {
	return c.RouteAs(ctx, city, points, ModeDriving)
}

// RouteAs interroge le SIG POUR UN MODE donné.
//
// ⚠️ UN MODE INCONNU N'EST PAS UNE ERREUR : le SIG retombe sur la voiture et
// répond. Refuser ici aurait fait échouer un devis parce qu'une verticale a
// ajouté une classe de véhicule — une course perdue pour un mot.
func (c *Client) RouteAs(ctx context.Context, city string, points [][2]float64, mode string) (Route, error) {
	if !c.Enabled() {
		return Route{}, nil
	}
	if len(points) < 2 {
		return Route{}, nil
	}
	if mode == "" {
		mode = ModeDriving
	}
	body, err := json.Marshal(map[string]any{
		"ville":       city,
		"coordinates": points, // [[lon, lat], …]
		"mode":        mode,
	})
	if err != nil {
		return Route{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.routeURL(), bytes.NewReader(body))
	if err != nil {
		return Route{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	// ⚠️ PAS D'EN-TÊTE VIDE. Côté SIG, l'ABSENCE d'en-tête et un en-tête VIDE
	// ne suivent pas le même chemin : le second est traité comme une clé
	// inconnue, et refusé en `401` quelle que soit la politique en vigueur.
	// `X-Api-Key: ` nous rangerait donc parmi les clés révoquées, ce qui n'est
	// pas ce qu'un service sans clé configurée veut dire.
	if c.apiKey != "" {
		req.Header.Set(HeaderAPIKey, c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Route{}, fmt.Errorf("diramaps: route: %w", err)
	}
	defer resp.Body.Close()
	// ⚠️ UN REFUS DE CLÉ SE DIT. « status 401 » dans un journal n'apprend rien à
	// qui n'a pas le code sous les yeux, et c'est précisément le jour où le SIG
	// fermera sa porte qu'il faudra comprendre vite. Les deux codes du portail
	// sont donc nommés.
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return Route{}, fmt.Errorf("diramaps: route: clé API refusée par le SIG (absente, inconnue ou révoquée) — voir MAPS_API_KEY")
	case http.StatusForbidden:
		return Route{}, fmt.Errorf("diramaps: route: service « itineraire » désactivé pour cette clé, ou quota du jour atteint")
	}
	if resp.StatusCode >= 300 {
		return Route{}, fmt.Errorf("diramaps: route: status %d", resp.StatusCode)
	}
	var out struct {
		Routes []struct {
			DurationS float64 `json:"duration_s"`
			// DistanceM : la distance par la route. ⚠️ ELLE ÉTAIT RENDUE ET
			// JETÉE : seul le pont des courses la décodait. C'est la troisième
			// fois que le SIG rend plus que ce qu'on lit (la géométrie, puis
			// `maneuver.location` de la navigation).
			DistanceM float64 `json:"distance_m"`
			// Geometry : un LineString GeoJSON (`overview=full` côté SIG).
			//
			// ⚠️ LE TYPE EST VÉRIFIÉ, PAS SUPPOSÉ. Le SIG rend aussi des
			// réponses consolidées par Google, qui n'ont pas de géométrie du
			// tout : décoder sans vérifier donnerait un tracé vide traité
			// comme un tracé.
			Geometry struct {
				Type        string       `json:"type"`
				Coordinates [][2]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Route{}, fmt.Errorf("diramaps: route: %w", err)
	}
	if len(out.Routes) == 0 {
		return Route{}, nil
	}
	// La PREMIÈRE route : le service rend les alternatives par ordre de
	// préférence, et c'est celle-là que la carte du livreur affichera.
	first := out.Routes[0]
	r := Route{DurationS: int(first.DurationS + 0.5)}
	// Arrondie au mètre, comme la durée à la seconde. ⚠️ Une valeur nulle ou
	// négative reste à zéro : « pas de distance », jamais une distance.
	if first.DistanceM > 0 {
		r.DistanceM = int(first.DistanceM + 0.5)
	}
	if first.Geometry.Type == "LineString" && len(first.Geometry.Coordinates) >= 2 {
		r.Geometry = first.Geometry.Coordinates
	}
	return r, nil
}
