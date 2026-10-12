package chatlexicon

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount pose les routes de la console.
//
// ⚠️ LA PORTÉE EST `core` : ce lexique vaut pour les deux métiers, et un
// opérateur habilité aux seules commandes n'a pas à décider ce qu'un chauffeur
// a le droit d'écrire.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin),
			middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/chat/lexicon", h.read)
		g.Put("/admin/chat/lexicon", h.save)
	})
}

// MountService expose ce qu'une VERTICALE vient chercher.
//
// ⚠️ UNE ROUTE INTERNE, jamais publique : la liste des mots surveillés dit
// comment contourner le filtre. Elle est servie au jeton de SERVICE, comme
// `/internal/countries`.
func (h *Handler) MountService(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.With(serviceMW).Get("/internal/chat/lexicon", h.internal)
}

// GET /admin/chat/lexicon[?country=TG]
//
// ⚠️ `country` VIDE DÉSIGNE LE FOND COMMUN, pas « le pays de la requête ». La
// distinction compte : la console doit pouvoir éditer le fond, et le déduire
// du pays du jeton aurait rendu le fond inaccessible à qui n'en a pas.
func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Read(r.Context(), r.URL.Query().Get("country"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	var req UpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Save(r.Context(), r.URL.Query().Get("country"), actor, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /internal/chat/lexicon?country=TG — ce qu'une verticale applique.
//
// ⚠️ ELLE REND LE LEXIQUE EFFECTIF, embarqué compris, pas les seuls termes
// saisis. La verticale n'a donc aucune fusion à refaire — et deux verticales
// ne peuvent pas la refaire différemment.
func (h *Handler) internal(w http.ResponseWriter, r *http.Request) {
	eff, err := h.svc.Effective(r.Context(), r.URL.Query().Get("country"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out := make(map[string]string, len(eff))
	for term, sev := range eff {
		out[term] = string(sev)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"terms": out})
}
