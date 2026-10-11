package sos

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Handler sert les routes d'alerte.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount pose les routes.
//
// ⚠️ DÉCLENCHER N'EXIGE AUCUN RÔLE — seulement un compte. C'est la décision
// d'accès la plus importante du module : un passager, un chauffeur, un livreur,
// un marchand, un membre du staff en déplacement. Restreindre aux rôles
// « agents » aurait laissé le passager, qui est précisément la partie la plus
// exposée d'une course, sans bouton.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/sos", h.raise)
		g.Get("/sos/settings", h.settings)
		g.Get("/sos/me", h.mine)
		g.Post("/sos/{id}/position", h.position)
		g.Post("/sos/{id}/cancel", h.cancel)
		// ⚠️ LA MESURE DES FAUSSES ALERTES, et elle ne passe JAMAIS devant une
		// alerte : son plafond rend `429` sans rien bloquer d'autre. Un compte
		// bavard en comptes rendus doit rester capable d'appeler au secours.
		g.Post("/sos/detections", h.detections)
	})
	r.Group(func(g chi.Router) {
		// ⚠️ LA PORTÉE EST `core`, PAS `vtc` NI `food`. Une personne en danger
		// n'est pas un sujet de verticale : un opérateur habilité aux seules
		// commandes doit pouvoir traiter l'alerte d'un chauffeur qui passe
		// devant lui. Les notifications, elles, sont bien dirigées par
		// verticale — mais l'écran reste ouvert à toute l'exploitation.
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Get("/admin/sos", h.live)
		g.Get("/admin/sos/history", h.history)
		g.Get("/admin/sos/{id}", h.one)
		g.Post("/admin/sos/{id}/ack", h.ack)
		// LE PROTOCOLE DU SERVICE CLIENT : appeler la personne, puis les
		// secours. ⚠️ Deux routes et non une case à cocher sur la fermeture :
		// ce sont des gestes HORODATÉS, et ce qu'on mesure est le DÉLAI entre
		// le déclenchement et chacun d'eux.
		g.Post("/admin/sos/{id}/contacted", h.contacted)
		g.Post("/admin/sos/{id}/emergency-called", h.emergencyCalled)
		g.Post("/admin/sos/{id}/close", h.close)
		// LA VUE QUI SERT À RÉGLER LES SEUILS : pays × application × source ×
		// révision.
		g.Get("/admin/sos/detections", h.detectionStats)
	})
}

// POST /sos — DÉCLENCHER.
//
// ⚠️ UN CORPS ILLISIBLE NE FAIT PAS ÉCHOUER L'APPEL. `httpx.Decode` rendrait un
// `422` sur un JSON tronqué par une coupure réseau — exactement la situation où
// l'on déclenche une alerte. On garde ce qu'on a pu lire et on écrit l'alerte :
// « quelqu'un a appuyé, on ne sait rien d'autre » est l'information la plus
// importante de ce module, et elle ne doit dépendre d'aucun champ.
func (h *Handler) raise(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var in RaiseInput
	_ = httpx.Decode(r, &in)
	out, err := h.svc.Raise(r.Context(), userID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// GET /sos/settings — ce que ce pays décide des détections et des numéros.
// GET /sos/settings[?app=client|driver|courier]
//
// ⚠️ `?app=` EST UNE PRÉCISION, PAS UNE OBLIGATION. Sans lui, l'application se
// déduit du compte — exactement pour un chauffeur comme pour un livreur, que
// le rôle ne distingue pas. Avec lui, elle est certaine : c'est ce que demande
// l'écran « tester la détection », et c'est ce qu'une application prudente
// envoie.
//
// ⚠️ ET LA ROUTE EXIGE UN JETON, aujourd'hui comme avant. C'était une question
// ouverte côté applications : la réponse est oui, elle est montée dans le
// groupe authentifié. Le pays vient donc du compte, et `?app=` ne sert qu'à
// lever l'ambiguïté chauffeur/livreur.
func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.svc.SettingsFor(r.Context(), r.URL.Query().Get("app")))
}

// GET /sos/me — mon alerte vivante, pour retrouver l'écran.
//
// ⚠️ `200` AVEC `null`, ET NON `404`. Un `404` sur « je n'ai pas d'alerte en
// cours » aurait fait traiter le cas normal comme une erreur dans chaque
// application — et une application qui se relance après un choc appelle
// précisément cette route.
func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Mine(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"alert": out})
}

// PositionInput : où la personne est MAINTENANT.
type PositionInput struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	AccuracyM int     `json:"accuracy_m"`
}

// POST /sos/{id}/position
func (h *Handler) position(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var in PositionInput
	_ = httpx.Decode(r, &in)
	if err := h.svc.Position(r.Context(), userID, chi.URLParam(r, "id"), in.Lat, in.Lng, in.AccuracyM); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /sos/{id}/cancel — « fausse alerte », par la personne elle-même.
func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Cancel(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/sos — la file.
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, n, err := h.svc.Live(r.Context(), limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// `live` est le compte de ce qui est VRAIMENT ouvert — la pastille. Il
	// diffère de `len(items)`, qui comprend les annulées récentes.
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "live": n})
}

// GET /admin/sos/history?status=&since=&limit=
func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	var since *time.Time
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = &t
		}
	}
	items, err := h.svc.History(r.Context(), q.Get("status"), since, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /admin/sos/{id}
func (h *Handler) one(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/sos/{id}/ack — « je m'en occupe ».
func (h *Handler) ack(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.Acknowledge(r.Context(), actorID, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/sos/{id}/contacted {reached, note?} — l'opérateur a appelé la
// personne.
//
// ⚠️ `reached` EST UNE RÉPONSE, PAS UNE CASE COCHÉE PAR LE CLIC. « J'ai
// appelé » et « j'ai eu la personne » sont deux faits : dix sonneries sans
// réponse après un choc violent est l'information la plus inquiétante de
// l'écran, et la déduire du clic l'aurait effacée.
func (h *Handler) contacted(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var in ContactInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Contacted(r.Context(), actorID, chi.URLParam(r, "id"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/sos/{id}/emergency-called {service, note?} — l'opérateur a appelé
// les secours.
//
// ⚠️ ELLE N'EXIGE PAS QU'ON AIT D'ABORD JOINT LA PERSONNE. L'ordre normal est
// « joindre, puis appeler » ; après un choc violent sur quelqu'un
// d'injoignable, exiger le premier avant le second bloquerait le seul cas où
// chaque seconde compte.
func (h *Handler) emergencyCalled(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var in EmergencyInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.EmergencyCalled(r.Context(), actorID, chi.URLParam(r, "id"), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// CloseInput : le dénouement, exigé.
type CloseInput struct {
	Outcome    string `json:"outcome"`
	Resolution string `json:"resolution" validate:"omitempty,max=2000"`
}

// POST /admin/sos/{id}/close
func (h *Handler) close(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var in CloseInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Close(r.Context(), actorID, chi.URLParam(r, "id"), in.Outcome, in.Resolution)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /sos/detections — les comptes rendus de proposition.
//
// ⚠️⚠️ `204`, ET PRESQUE JAMAIS AUTRE CHOSE. Cette route est appelée en
// arrière-plan, sans insistance, par une application dont personne ne lit la
// réponse : un corps illisible, une date folle, un `outcome` inventé ne
// produisent rien d'autre qu'une mesure perdue. Les deux seuls refus sont un
// lot trop grand (erreur de câblage) et le plafond du jour.
func (h *Handler) detections(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var batch DetectionBatch
	// ⚠️⚠️ PAS `httpx.Decode` ICI, ET C'EST LA SEULE EXCEPTION DU DÉPÔT.
	//
	// `httpx.Decode` REFUSE une clé inconnue, et cette règle est bonne presque
	// partout : une application qui envoie `first_name` à une route qui ne le
	// lit pas croirait l'avoir enregistré.
	//
	// Ici elle se retourne contre nous. Ces comptes rendus sont envoyés en
	// arrière-plan, SANS RÉESSAI, et personne ne lit la réponse. Le jour où une
	// application ajoute un champ, tout serveur plus ancien jetterait LE LOT
	// ENTIER — en répondant `204`, donc sans que rien ne le signale. On perdrait
	// précisément la mesure qui dit qu'un seuil ne déclenche plus, c'est-à-dire
	// la panne que cette route existe pour trouver.
	//
	// Le champ en trop est donc IGNORÉ, jamais stocké : `Detection` ne le porte
	// pas, et c'est le type qui garantit qu'il n'entre pas en base.
	//
	// ⚠️ ET UN CORPS ILLISIBLE NE FAIT PAS ÉCHOUER L'ENVOI non plus : ce qui
	// arrive cassé par une coupure réseau est perdu, pas transformé en erreur
	// qu'une application devra gérer sur une route dont elle ignore la réponse.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&batch); err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.svc.RecordDetections(r.Context(), userID, batch); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /admin/sos/detections[?app=&source=&days=] — la vue qui règle les seuils.
func (h *Handler) detectionStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := DetectionFilter{App: q.Get("app"), Source: q.Get("source")}
	// ⚠️ TRENTE JOURS PAR DÉFAUT, et bornés à la conservation : une fenêtre
	// plus large que ce qu'on garde donnerait l'illusion d'une série longue.
	days := 30
	if n, err := strconv.Atoi(q.Get("days")); err == nil && n > 0 && n <= 90 {
		days = n
	}
	f.Since = time.Now().UTC().AddDate(0, 0, -days)
	out, err := h.svc.DetectionStats(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out, "days": days})
}
