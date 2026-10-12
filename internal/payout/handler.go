package payout

// LES ROUTES DU RETRAIT.
//
// ⚠️⚠️ RÉSERVÉES AU RÔLE `driver`, et c'est la décision d'accès la plus
// importante du module. Le rôle `driver` couvre les deux métiers d'agent — un
// chauffeur de course et un livreur le partagent au socle. Un CLIENT n'a rien
// à retirer : son solde vient de recharges et de remboursements, et lui ouvrir
// cette porte ferait de la plateforme un service de transfert d'argent, métier
// qui demande une licence que nous n'avons pas.

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// maxWebhookBody borne ce qu'un rappel peut peser.
const maxWebhookBody = 1 << 20

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleDriver))
		g.Get("/payouts/available", h.available)
		g.Get("/payouts", h.mine)
		g.Post("/payouts", h.request)
	})
	r.Group(func(g chi.Router) {
		// ⚠️ LA PORTÉE EST `core` : l'argent d'un agent est un objet du socle,
		// pas d'une verticale. Un opérateur habilité aux seules commandes n'a
		// pas à valider le retrait d'un chauffeur.
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/payouts", h.pending)
		g.Post("/admin/payouts/{id}/approve", h.approve)
		g.Post("/admin/payouts/{id}/reject", h.reject)
	})
	// ⚠️ LE RAPPEL DE L'AGRÉGATEUR EST PUBLIC — il n'a pas de jeton, et il ne
	// peut pas en avoir. C'est la SIGNATURE qui l'authentifie, vérifiée avant
	// de toucher quoi que ce soit.
	r.Post("/webhooks/payouts", h.webhook)
}

// GET /payouts/available — l'écran de retrait.
func (h *Handler) available(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Available(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /payouts[?limit=] — mes retraits.
func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.Mine(r.Context(), userID, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// RequestInput : le montant, et RIEN D'AUTRE.
//
// ⚠️⚠️ PAS DE NUMÉRO DE TÉLÉPHONE, et c'est délibéré. L'argent part sur le
// numéro DU COMPTE, lu côté serveur. Un champ libre serait la porte ouverte au
// détournement : quelqu'un qui obtient une session — un téléphone prêté, un
// mot de passe transmis de vive voix, ce qui arrive souvent ici — n'aurait
// qu'à changer le numéro pour vider le compte.
type RequestInput struct {
	AmountXOF int `json:"amount_xof" validate:"required,min=1"`
}

// POST /payouts — demander un retrait.
func (h *Handler) request(w http.ResponseWriter, r *http.Request) {
	var req RequestInput
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	userID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Request(r.Context(), userID, req.AmountXOF)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// GET /admin/payouts — la file de validation.
func (h *Handler) pending(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.Pending(r.Context(), limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /admin/payouts/{id}/approve
func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Approve(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/payouts/{id}/reject {reason}
//
// ⚠️ LA RAISON EST OBLIGATOIRE, et l'agent la LIT. « Refusé » tout court fait
// rappeler le support, et le support n'a rien à répondre non plus.
func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Reject(r.Context(), chi.URLParam(r, "id"), req.Reason)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /webhooks/payouts — le rappel de l'agrégateur.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	// ⚠️ BORNÉ : un corps sans limite est une invitation à saturer la mémoire
	// du serveur depuis une route PUBLIQUE.
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		httpx.Error(w, r, apperr.Validation("unreadable payload"))
		return
	}
	sig := r.Header.Get("X-Signature")
	if err := h.svc.HandleWebhook(r.Context(), payload, sig); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
