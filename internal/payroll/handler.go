package payroll

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the payroll routes on the /api/v1 router.
//
// ⚠️ EXPLOITATION SEULEMENT, ET AUCUNE SURFACE D'APPLICATION. Un salaire, un
// motif de retenue et le nom de celui qui l'a posée sont parmi les données les
// plus sensibles de la plateforme : il n'existe donc PAS de route « ma paie ».
// La personne qui veut sa fiche la demande à l'entreprise, qui la lui donne —
// ouvrir une route mobile aurait demandé de décider, sans y avoir réfléchi, ce
// qu'un salarié a le droit de lire de sa propre retenue, et le défaut aurait
// été « tout ».
//
// ⚠️ PORTÉE `core` : la paie est au SOCLE parce qu'elle ne parle ni de courses
// ni de livraisons. Elle n'est en revanche pas gardée par la FONCTION du
// membre du staff (`finance`, `admin`) : le middleware ne sait vérifier que
// les portées, et une garde par fonction n'existe nulle part ailleurs sur la
// plateforme. Dit ici plutôt que supposé — c'est une décision à prendre, pas
// un oubli.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))

		g.Get("/admin/payroll/settings", h.settings)
		g.Put("/admin/payroll/settings", h.updateSettings)

		g.Get("/admin/payroll/employees", h.employees)
		g.Post("/admin/payroll/employees", h.createEmployee)
		g.Get("/admin/payroll/employees/{id}", h.employee)
		g.Patch("/admin/payroll/employees/{id}", h.updateEmployee)
		g.Get("/admin/payroll/employees/{id}/adjustments", h.adjustments)
		// ⚠️ DEUX ROUTES PLUTÔT QU'UN CHAMP `kind` DANS LE CORPS. Une prime et
		// une retenue ne se relisent pas de la même façon dans un journal
		// d'audit, et un `kind` mal rempli aurait retenu de l'argent là où l'on
		// voulait en donner. Le geste est dans l'URL : il se voit dans les logs
		// de la passerelle comme dans la revue du code.
		g.Post("/admin/payroll/employees/{id}/bonuses", h.grantBonus)
		g.Post("/admin/payroll/employees/{id}/deductions", h.placeDeduction)
		g.Post("/admin/payroll/adjustments/{id}/cancel", h.cancelAdjustment)

		g.Get("/admin/payroll/runs", h.runs)
		g.Post("/admin/payroll/runs", h.prepareRun)
		g.Get("/admin/payroll/runs/{id}", h.run)
		g.Post("/admin/payroll/runs/{id}/approve", h.approveRun)
		g.Post("/admin/payroll/runs/{id}/pay", h.payRun)
		g.Post("/admin/payroll/runs/{id}/cancel", h.cancelRun)
		// CORRIGER UNE PAIE VERSÉE — une pièce nouvelle qui nomme celle qu'elle
		// rattrape. ⚠️ Il n'existe AUCUN `PATCH` sur une paie versée, et c'est
		// cette route qui rend l'absence tenable.
		g.Post("/admin/payroll/runs/{id}/corrections", h.correctRun)
	})
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetSettings(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in SettingsInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.UpdateSettings(r.Context(), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) employees(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := httpx.PageFromRequest(r)
	items, next, err := h.svc.ListEmployees(r.Context(), EmployeeFilter{
		Status: q.Get("status"), Frequency: q.Get("frequency"), Q: q.Get("q"),
	}, page.Limit, page.Cursor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

func (h *Handler) createEmployee(w http.ResponseWriter, r *http.Request) {
	var in EmployeeInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.CreateEmployee(r.Context(), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) employee(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetEmployee(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) updateEmployee(w http.ResponseWriter, r *http.Request) {
	var in EmployeeUpdate
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.UpdateEmployee(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) adjustments(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListAdjustments(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("status"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) grantBonus(w http.ResponseWriter, r *http.Request) { h.grant(w, r, KindBonus) }
func (h *Handler) placeDeduction(w http.ResponseWriter, r *http.Request) {
	h.grant(w, r, KindDeduction)
}

func (h *Handler) grant(w http.ResponseWriter, r *http.Request, kind string) {
	var in AdjustmentInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Grant(r.Context(), chi.URLParam(r, "id"), kind, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) cancelAdjustment(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.CancelAdjustment(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := httpx.PageFromRequest(r)
	f := RunFilter{Status: q.Get("status"), Frequency: q.Get("frequency")}
	if raw := q.Get("employee_id"); raw != "" {
		if eid, err := primitive.ObjectIDFromHex(raw); err == nil {
			f.EmployeeID = &eid
		}
	}
	items, next, err := h.svc.ListRuns(r.Context(), f, page.Limit, page.Cursor)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, items, next)
}

func (h *Handler) prepareRun(w http.ResponseWriter, r *http.Request) {
	var in PrepareInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.PrepareRun(r.Context(), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) approveRun(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ApproveRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) payRun(w http.ResponseWriter, r *http.Request) {
	var in PayInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.PayRun(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) cancelRun(w http.ResponseWriter, r *http.Request) {
	var in CancelInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.CancelRun(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) correctRun(w http.ResponseWriter, r *http.Request) {
	var in CorrectionInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Correct(r.Context(), chi.URLParam(r, "id"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}
