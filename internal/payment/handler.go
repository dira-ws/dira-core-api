package payment

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

const maxWebhookBody = 1 << 20 // 1 MiB

// Handler exposes the payment HTTP endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the module routes on the /api/v1 router.
// authMW is the JWT middleware built at wiring time.
//
// allowSandbox ouvre la confirmation manuelle d'un paiement. C'est un
// paramètre EXPLICITE et non une lecture de configuration cachée ici : le
// choix d'autoriser la simulation d'un encaissement doit se lire à
// l'assemblage, là où l'environnement est connu.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler, allowSandbox bool) {
	// Public, protected by webhook signature verification.
	r.Post("/webhooks/payment/{provider}", h.webhook)

	r.Group(func(g chi.Router) {
		g.Use(authMW)
		// La liste des opérateurs RÉELLEMENT branchés. Sans elle, une
		// application code la liste en dur et propose des paiements qui
		// échoueront.
		g.With(middleware.RequireRole(auth.RoleClient, auth.RoleDriver, auth.RoleMerchant)).
			Get("/payments/providers", h.providers)
		g.With(middleware.RequireRole(auth.RoleClient, auth.RoleDriver, auth.RoleMerchant)).
			Post("/payments/initiate", h.initiate)
		// RETROUVER UN PAIEMENT DONT LA RÉPONSE S'EST PERDUE.
		//
		// ⚠️ DÉCLARÉE AVANT `/payments/{id}`, et ce n'est pas qu'une question
		// de style : chi range ses routes, mais une route littérale et une
		// route à paramètre au même niveau se disputent `/payments` — et c'est
		// la liste qu'on veut là.
		g.With(middleware.RequireRole(auth.RoleClient, auth.RoleDriver, auth.RoleMerchant)).
			Get("/payments", h.list)
		g.Get("/payments/{id}", h.get)
	})

	if allowSandbox {
		r.Group(func(g chi.Router) {
			g.Use(authMW)
			g.With(middleware.RequireRole(auth.RoleAdmin)).
				Post("/admin/payments/{id}/confirm", h.confirmSandbox)
		})
	}
}

// POST /admin/payments/{id}/confirm — hors production uniquement.
func (h *Handler) confirmSandbox(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ConfirmSandboxPayment(r.Context(), chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /payments/providers — ce que le serveur sait réellement encaisser.
func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"items": h.svc.Providers()})
}

func (h *Handler) initiate(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req InitiatePaymentRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	// L'en-tête l'emporte sur le corps : c'est la convention, et c'est elle
	// que les applications ont demandée.
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		req.IdempotencyKey = key
	}
	resp, err := h.svc.Initiate(r.Context(), userID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

// GET /payments[?purpose=&ref_id=&status=&limit=&cursor=] — SES paiements.
//
// ⚠️ `purpose` EST LE `ref_kind` QUE LES APPLICATIONS DEMANDAIENT. Elles
// voulaient « les paiements de cette commande » : `?purpose=order&ref_id=…`
// le dit déjà, et un second nom pour la même chose est la manière habituelle
// dont un contrat se met à se contredire.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	q, err := parseQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.ListOwn(r.Context(), userID, q)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// parseQuery lit les bornes de la recherche.
//
// ⚠️ UN `purpose`, UN `status` OU UN `ref_id` ILLISIBLE EST REFUSÉ, jamais
// ignoré. L'ignorer rendrait TOUS les paiements du compte à une application
// qui demandait ceux d'une commande — et elle conclurait qu'un paiement est
// en cours sur une commande qui n'en a aucun, puis retiendrait « Payer ».
func parseQuery(r *http.Request) (PaymentQuery, error) {
	query := r.URL.Query()
	var q PaymentQuery

	if raw := strings.TrimSpace(query.Get("purpose")); raw != "" {
		if !knownPurpose(raw) {
			return q, apperr.Validation("unknown `purpose`").
				WithMeta(map[string]any{"fields": []string{"purpose"}})
		}
		q.Purpose = raw
	}
	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		if !knownStatus(raw) {
			return q, apperr.Validation("unknown `status`").
				WithMeta(map[string]any{"fields": []string{"status"}})
		}
		q.Status = raw
	}
	if raw := strings.TrimSpace(query.Get("ref_id")); raw != "" {
		id, err := primitive.ObjectIDFromHex(raw)
		if err != nil {
			return q, apperr.Validation("`ref_id` must be an object id").
				WithMeta(map[string]any{"fields": []string{"ref_id"}}).WithCause(err)
		}
		q.RefID = id
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return q, apperr.Validation("`limit` must be a positive number").
				WithMeta(map[string]any{"fields": []string{"limit"}})
		}
		// Plafonné en silence, comme les autres listes : une application qui
		// demande mille veut « beaucoup », pas une erreur.
		q.Limit = n
	}
	// ⚠️ UN CURSEUR ILLISIBLE EST IGNORÉ, lui, et pas refusé : c'est le
	// comportement de toutes les paginations du socle, et une page qui
	// redémarre au début vaut mieux qu'un écran d'historique en erreur.
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		if id, err := primitive.ObjectIDFromHex(raw); err == nil {
			q.Cursor = id
		}
	}
	return q, nil
}

// knownPurpose et knownStatus : les valeurs servies, et rien d'autre.
//
// ⚠️ UNE SEULE LISTE PAR NOTION, celle des constantes. Une liste de statuts
// écrite une deuxième fois a déjà produit un refus FAUX ailleurs sur cette
// plateforme : on ajoute un statut d'un côté, et l'autre côté le refuse.
func knownPurpose(p string) bool {
	switch p {
	case PurposeOrder, PurposeRide, PurposeRideSubscription, PurposeTokenPurchase, PurposeWalletTopup:
		return true
	}
	return false
}

func knownStatus(s string) bool {
	switch s {
	case StatusPending, StatusSucceeded, StatusFailed, StatusRefunded:
		return true
	}
	return false
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	role, _ := auth.RoleFromContext(r.Context())
	resp, err := h.svc.Get(r.Context(), userID, role, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		httpx.Error(w, r, apperr.Validation("unreadable webhook body").WithCause(err))
		return
	}
	signature := r.Header.Get("X-Signature")
	if err := h.svc.HandleWebhook(r.Context(), chi.URLParam(r, "provider"), payload, signature); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, WebhookAck{Received: true})
}
