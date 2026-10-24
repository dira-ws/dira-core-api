package ownfleet

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the module routes on the /api/v1 router.
//
// Deux publics : l'AGENT (chauffeur ou livreur — rôle `driver`) lit le véhicule
// qui lui est confié et sa redevance ; l'EXPLOITATION (admin, portée `core`)
// tient le parc et les attributions.
//
// ⚠️ IL N'Y A AUCUNE ROUTE DE CYCLE DE VIE ICI — ni accepter, ni remettre, ni
// rendre, ni payer. Elles existent déjà, et une seule fois, dans le module du
// matériel : `POST /me/equipment/{id}/accept`,
// `POST /admin/equipment/contracts/{id}/handover-code`,
// `POST /equipment/handover` (le QR scanné par l'agent), `.../return-code`,
// `POST /equipment/return`, `.../payments`, `.../default`, `.../cancel`. Les
// redoubler ici aurait fait deux chemins vers la même transition — et c'est la
// leçon la plus chère de cette plateforme : trois chemins menaient à
// « annulée » et un seul rendait l'argent.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleDriver))
		// CE QUE L'AGENT VOIT : sa voiture, sa redevance, ce qu'il doit, ce qui
		// a été prélevé.
		g.Get("/me/fleet-vehicles", h.mine)
	})
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/internal-fleet/vocabulary", h.vocabulary)
		g.Get("/admin/internal-fleet/vehicles", h.list)
		g.Post("/admin/internal-fleet/vehicles", h.create)
		g.Get("/admin/internal-fleet/vehicles/{id}", h.get)
		g.Patch("/admin/internal-fleet/vehicles/{id}", h.update)
		// CONFIER — le geste qui ouvre la redevance.
		g.Post("/admin/internal-fleet/vehicles/{id}/assign", h.assign)
		// RENDRE AU PARC À LA MAIN — une CORRECTION, pas une reprise.
		//
		// ⚠️ UNE ROUTE À ELLE, ET UN MOTIF EXIGÉ. La reprise normale passe par
		// le RETOUR du contrat, qui solde la caution et constate l'état ; ce
		// geste-ci ne sert qu'à recaler un registre resté en arrière, et il
		// refuse tant que le contrat vit. Le confondre avec une reprise aurait
		// fait disparaître des cautions.
		g.Post("/admin/internal-fleet/vehicles/{id}/release", h.release)
	})
}

// GET /me/fleet-vehicles — la voiture de Dira que je conduis, et sa redevance.
func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	items, err := h.svc.Mine(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /admin/internal-fleet/vocabulary — statuts, postes inclus, périodes.
func (h *Handler) vocabulary(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, Words())
}

// GET /admin/internal-fleet/vehicles?status=&class_key=&vertical=&holder=
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	f := Filter{
		Status:   r.URL.Query().Get("status"),
		ClassKey: r.URL.Query().Get("class_key"),
		Vertical: r.URL.Query().Get("vertical"),
	}
	// ⚠️ UN IDENTIFIANT ILLISIBLE EST REFUSÉ, PAS IGNORÉ. Laisser tomber la
	// clause aurait rendu LE PARC ENTIER sur un écran qui demandait les
	// véhicules d'une personne — la même faute que la liste des véhicules des
	// courses existe pour ne plus commettre.
	if raw := r.URL.Query().Get("holder"); raw != "" {
		uid, err := primitive.ObjectIDFromHex(raw)
		if err != nil {
			httpx.Error(w, r, apperr.Validation("invalid holder id").WithCause(err))
			return
		}
		f.Holder = uid
	}
	items, next, err := h.svc.List(r.Context(), f, httpx.PageFromRequest(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

// POST /admin/internal-fleet/vehicles
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req AssetInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Create(r.Context(), actorID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// GET /admin/internal-fleet/vehicles/{id}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// PATCH /admin/internal-fleet/vehicles/{id}
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req AssetPatch
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Update(r.Context(), actorID, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/internal-fleet/vehicles/{id}/assign
func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req AssignInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Assign(r.Context(), actorID, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// POST /admin/internal-fleet/vehicles/{id}/release {reason}
//
// ⚠️ LE MOTIF EST EXIGÉ, comme pour le détachement d'une voiture de flotte dans
// les courses : ce geste change ce que le parc montre, et « pourquoi cette
// voiture est-elle revenue ? » doit avoir une réponse six mois plus tard.
func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req struct {
		Reason string `json:"reason" validate:"required,min=3,max=300"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.ForceRelease(r.Context(), actorID, chi.URLParam(r, "id"), req.Reason)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
