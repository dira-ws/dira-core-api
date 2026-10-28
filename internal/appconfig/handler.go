package appconfig

// LES ROUTES — une pour les applications, trois pour la console.

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	pkgconf "github.com/kgtech-org/dira-core-api/pkg/appconfig"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/edition"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct {
	svc  *Service
	repo *Repository
}

func NewHandler(svc *Service, repo *Repository) *Handler {
	return &Handler{svc: svc, repo: repo}
}

func (h *Handler) Mount(r chi.Router, authMW, serviceMW func(http.Handler) http.Handler) {
	// LE REGISTRE BRUT, POUR LES VERTICALES.
	//
	// ⚠️⚠️ LE REGISTRE, PAS LA VUE RÉSOLUE, et c'est la décision qui évite un
	// appel HTTP par requête. Une verticale doit filtrer ses listes pour LE
	// PAYS ET L'ÉDITION DE CHAQUE APPEL : lui servir `/app/config` l'aurait
	// obligée à interroger le socle à chaque course. Elle lit donc le registre
	// une fois, le garde quelques secondes, et le résout LOCALEMENT avec le
	// même `pkg/appconfig` que nous — c'est exactement pour cela que la
	// résolution est dans un paquet partagé et pure.
	//
	// ⚠️ ET DERRIÈRE LE JETON DE SERVICE, contrairement à `/app/config` : le
	// registre brut porte les règles de TOUS les pays, et une application n'a
	// pas à savoir ce qui est coupé ailleurs.
	r.With(serviceMW).Get("/internal/app-config", h.rawRegistry)

	// ⚠️⚠️ PUBLIQUE, SANS JETON, ET C'EST DÉLIBÉRÉ. Une application lit sa
	// configuration AU DÉMARRAGE — avant l'écran de connexion, pour savoir
	// quels menus dessiner. L'enfermer derrière un jeton aurait obligé Lite à
	// afficher un écran complet puis à le réduire après connexion : la
	// personne verrait apparaître puis disparaître des entrées de menu.
	//
	// Rien d'ici n'est secret : ce sont les fonctions d'un pays, que n'importe
	// quelle application installée découvre en s'en servant.
	r.Get("/app/config", h.config)

	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin))
		g.Get("/admin/app-config", h.adminConfig)
		g.Put("/admin/app-config", h.saveConfig)
		// La vue « ce que voit Lite au Tchad » : la résolution, pour un pays
		// et une édition choisis dans l'écran.
		g.Get("/admin/app-config/preview", h.preview)
	})
}

// GET /internal/app-config — le registre brut, pour les verticales.
func (h *Handler) rawRegistry(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.svc.Registry(r.Context()))
}

// GET /app/config — ce que CETTE application voit dans CE pays.
func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.svc.Resolve(r.Context()))
}

// AdminView est l'écran de réglage : le catalogue, les règles, et qui a touché.
//
// ⚠️ LE CATALOGUE EST SERVI AVEC LES RÈGLES, et c'est ce qui rend l'écran
// utilisable : la console n'a pas à tenir sa propre liste de fonctions — elle
// en aurait oublié une à chaque ajout — ni à inventer les phrases qui disent
// ce que couper une fonction fait voir à la personne.
type AdminView struct {
	Features  []pkgconf.Feature `json:"features"`
	Lists     []pkgconf.Feature `json:"lists"`
	Editions  []string          `json:"editions"`
	Rules     pkgconf.Registry  `json:"rules"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
}

func (h *Handler) adminConfig(w http.ResponseWriter, r *http.Request) {
	reg := h.svc.Registry(r.Context())
	at, by := h.repo.Meta(r.Context())
	httpx.JSON(w, http.StatusOK, AdminView{
		Features: pkgconf.Features, Lists: pkgconf.Lists,
		Editions: edition.All(), Rules: *reg,
		UpdatedAt: at, UpdatedBy: by,
	})
}

func (h *Handler) saveConfig(w http.ResponseWriter, r *http.Request) {
	var req pkgconf.Registry
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := auth.UserFromContext(r.Context())
	if err := h.svc.Save(r.Context(), req, actor); err != nil {
		httpx.Error(w, r, err)
		return
	}
	h.adminConfig(w, r)
}

// GET /admin/app-config/preview?country=TD&edition=lite
//
// ⚠️⚠️ L'ÉCRAN QUI ÉVITE LA MAUVAISE SURPRISE. Un opérateur coche des cases
// sur une grille de fonctions ; ce qu'il veut vraiment savoir, c'est « que
// voit Lite au Tchad ? ». Sans cette vue, il faudrait installer Lite et
// changer de pays pour le vérifier — et personne ne le fait, donc personne ne
// vérifie.
//
// ⚠️ ET ELLE NE LIT PAS L'ÉDITION DE LA REQUÊTE : c'est une console, elle
// s'ouvre depuis l'application complète. Résoudre pour l'édition de l'appelant
// aurait toujours montré la vue `full`, c'est-à-dire précisément pas celle
// qu'on vient voir.
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	httpx.JSON(w, http.StatusOK, h.svc.Registry(r.Context()).
		Resolve(q.Get("country"), q.Get("edition")))
}
