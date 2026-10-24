package finance

// LES ROUTES DES DÉCLARATIONS — montées dans le MÊME groupe que le reste de
// la finance (rôle `admin`, portée `core`), parce qu'elles donnent à lire et à
// écrire la comptabilité du pays.

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// mountDeclarations ajoute les routes au groupe DÉJÀ authentifié de `Mount`.
//
// ⚠️ Il prend le routeur du groupe, et pas le routeur racine : monter ces
// routes à côté du groupe les aurait ouvertes à tout le monde, et rien dans
// une réponse ne l'aurait dit. L'épreuve des routes montées le vérifie.
func (h *Handler) mountDeclarations(g chi.Router) {
	g.Get("/admin/finance/declarations", h.listDeclarations)
	g.Post("/admin/finance/declarations", h.declare)
	// ⚠️ `summary` AVANT `{id}` : chi préfère le segment littéral, mais
	// l'ordre de déclaration rend la lecture sans ambiguïté pour qui relit.
	g.Get("/admin/finance/declarations/summary", h.declarationSummary)
	g.Get("/admin/finance/declarations/{id}", h.declaration)
	g.Post("/admin/finance/declarations/{id}/reverse", h.reverseDeclaration)
	g.Get("/admin/finance/declaration-categories", h.declarationCategories)
	g.Put("/admin/finance/declaration-categories", h.saveDeclarationCategories)
}

func (h *Handler) declare(w http.ResponseWriter, r *http.Request) {
	var in DeclareInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := auth.UserFromContext(r.Context())
	d, err := h.svc.Declare(r.Context(), actor, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (h *Handler) listDeclarations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := DeclarationFilter{
		Direction: q.Get("direction"), Category: q.Get("category"), Means: q.Get("means"),
		Status: q.Get("status"), Source: q.Get("source"), Search: q.Get("q"),
		From: parseTime(q.Get("from")), To: parseTime(q.Get("to")),
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, next, err := h.svc.Declarations(r.Context(), f, limit, q.Get("cursor"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

func (h *Handler) declaration(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Declaration(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (h *Handler) reverseDeclaration(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason" validate:"required,min=5,max=500"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := auth.UserFromContext(r.Context())
	d, err := h.svc.ReverseDeclaration(r.Context(), actor, chi.URLParam(r, "id"), in.Reason)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (h *Handler) declarationSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sum, err := h.svc.DeclarationSummary(r.Context(), parseTime(q.Get("from")), parseTime(q.Get("to")))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sum)
}

// declarationCategories rend le catalogue du pays ET les comptes déclarables.
//
// Les deux ensemble, parce que l'un ne se règle pas sans l'autre : la console
// doit proposer un compte à chaque catégorie, et seuls ceux-là sont permis
// (voir la règle 1 dans `declaration.go`).
func (h *Handler) declarationCategories(w http.ResponseWriter, r *http.Request) {
	cat, err := h.svc.DeclarationCategories(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"country":    cat.Country,
		"categories": cat.Categories,
		"updated_by": cat.UpdatedBy,
		"updated_at": cat.UpdatedAt,
		"accounts":   DeclarableAccounts(),
		"means":      []string{MeansCash, MeansBank, MeansMobileMoney},
	})
}

func (h *Handler) saveDeclarationCategories(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Categories []DeclarationCategory `json:"categories" validate:"required,min=1,max=60,dive"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	actor, _ := auth.UserFromContext(r.Context())
	cat, err := h.svc.SaveDeclarationCategories(r.Context(), actor, in.Categories)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"country":    cat.Country,
		"categories": cat.Categories,
		"updated_by": cat.UpdatedBy,
		"updated_at": cat.UpdatedAt,
		"accounts":   DeclarableAccounts(),
		"means":      []string{MeansCash, MeansBank, MeansMobileMoney},
	})
}
