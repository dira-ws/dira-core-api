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

// Client calls the SIG. Base d'URL DISTINCTE de l'API et du suivi : les trois
// services se déplacent indépendamment.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds the client. Une base vide rend un client INERTE plutôt que nil :
// l'appelant n'a pas à tester la présence à chaque usage, et l'absence de SIG
// se traduit par une absence de durée, pas par une panne.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
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

// Route est ce que le réseau routier dit d'une tournée : sa durée, et son
// TRACÉ.
//
// ⚠️⚠️ LE TRACÉ NE TARIFIE RIEN, ET C'EST UNE DÉCISION ÉCRITE AILLEURS AUSSI.
// La distance qui fixe le prix d'une course reste calculée localement, à vol
// d'oiseau entre les arrêts : la faire dépendre d'un service externe ferait
// varier le prix d'une commande selon la disponibilité d'un tiers. Ce tracé
// sert à DESSINER, et seulement à dessiner.
//
// Il est donc AU MIEUX, comme la durée : un SIG muet rend une tournée sans
// tracé, jamais une erreur qui empêcherait de créer la course.
type Route struct {
	DurationS int
	// Geometry : les points du tracé, `[lng, lat]`, dans l'ordre de GeoJSON et
	// de toute cette plateforme. Vide si le moteur ne l'a pas rendue — on ne
	// l'invente pas, et surtout on ne retombe pas sur la ligne droite : une
	// ligne droite présentée comme un itinéraire est exactement le défaut
	// qu'on corrige (voir core#230).
	Geometry [][2]float64
}

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

// Route interroge le SIG.
func (c *Client) Route(ctx context.Context, city string, points [][2]float64) (Route, error) {
	if !c.Enabled() {
		return Route{}, nil
	}
	if len(points) < 2 {
		return Route{}, nil
	}
	body, err := json.Marshal(map[string]any{
		"ville":       city,
		"coordinates": points, // [[lon, lat], …]
		"mode":        "driving",
	})
	if err != nil {
		return Route{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.routeURL(), bytes.NewReader(body))
	if err != nil {
		return Route{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Route{}, fmt.Errorf("diramaps: route: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Route{}, fmt.Errorf("diramaps: route: status %d", resp.StatusCode)
	}
	var out struct {
		Routes []struct {
			DurationS float64 `json:"duration_s"`
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
	if first.Geometry.Type == "LineString" && len(first.Geometry.Coordinates) >= 2 {
		r.Geometry = first.Geometry.Coordinates
	}
	return r, nil
}
