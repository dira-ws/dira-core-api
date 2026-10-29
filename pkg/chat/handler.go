package chat

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

type Handler struct {
	svc *Service
	// base est le segment de route de la VERTICALE : « orders » pour la
	// livraison, « rides » pour les courses.
	//
	// ⚠️ Un argument, pas une constante. Écrire « orders » en dur dans une
	// bibliothèque partagée obligerait les courses à servir leurs
	// conversations sous le nom de la livraison — et un passager lirait
	// « /rides/… » dans sa spec pour appeler « /orders/… ».
	base string
}

// NewHandler builds the HTTP surface. `base` est le segment de la verticale :
// « orders » ou « rides ».
func NewHandler(svc *Service, base string) *Handler {
	return &Handler{svc: svc, base: base}
}

// Mount registers the module routes on the /api/v1 router.
//
// Aucune route publique : une conversation n'a que deux participants, plus
// l'administration qui la LIT pour le support.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	messages := "/" + h.base + "/{id}/messages"
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Get(messages, h.list)
		g.Post(messages, h.send)
		g.Post(messages+"/read", h.markRead)
		// Émulateurs de la console : écrire POUR une partie, par la même
		// méthode de service. Un administrateur n'écrit jamais en son nom —
		// un message de la plateforme passerait pour celui d'un livreur.
		g.With(middleware.RequireRole(auth.RoleAdmin)).
			Post("/admin"+messages, h.sendAs)

		// ⚠️⚠️ SIGNALER UN MESSAGE — et c'est le geste le plus important de
		// toute cette modération. Ce qu'un automate rate — l'ironie, la menace
		// polie, le sous-entendu, la langue qu'on n'a pas mise au lexique —
		// c'est exactement ce qu'un humain reconnaît. Sans ce bouton, la file
		// ne contient que ce que la machine a su voir.
		g.Post(messages+"/{messageID}/report", h.report)

		// LA FILE DE MODÉRATION, et le geste qui retire un message.
		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/"+h.base+"/messages/flagged", h.flagged)
		g.With(admin).Post("/admin/"+h.base+"/messages/{messageID}/hide", h.hide)
		g.With(admin).Post("/admin/"+h.base+"/messages/{messageID}/keep", h.keep)
	})
}

// POST /{orders|rides}/{id}/messages/{messageID}/report
//
// ⚠️ ON NE SIGNALE QUE CE QU'ON A REÇU, jamais ce qu'on a écrit. Se signaler
// soi-même n'a aucun sens et remplirait la file de bruit — ou servirait à
// faire croire qu'on a été agressé.
func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	out, err := h.svc.Report(r.Context(), userID, role,
		chi.URLParam(r, "id"), chi.URLParam(r, "messageID"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/{orders|rides}/messages/flagged[?limit=&cursor=]
//
// ⚠️ LES SIGNALEMENTS HUMAINS D'ABORD, puis les drapeaux de l'automate. Une
// file triée par date noie les trois messages qu'une personne a pris la peine
// de signaler sous les quatre cents que la machine a marqués.
func (h *Handler) flagged(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.svc.Flagged(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/{orders|rides}/messages/{messageID}/hide — retirer un message.
func (h *Handler) hide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = httpx.Decode(r, &req)
	if err := h.svc.Hide(r.Context(), chi.URLParam(r, "messageID"), req.Reason); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/{orders|rides}/messages/{messageID}/keep — « rien à redire ».
//
// ⚠️ UN GESTE EXPLICITE, ET PAS SEULEMENT « RETIRER DE LA FILE ». Un lexique
// trop large se corrige en regardant ce qui a été gardé : si la même règle
// produit vingt « gardés » d'affilée, c'est la règle qu'il faut retirer.
func (h *Handler) keep(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Keep(r.Context(), chi.URLParam(r, "messageID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /{orders|rides}/{id}/messages
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	page := httpx.PageFromRequest(r)
	items, unread, err := h.svc.List(r.Context(), userID, role, chi.URLParam(r, "id"), page.Cursor, page.Limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp := ConversationResponse{Items: items, Unread: unread}
	if len(items) > 0 {
		resp.NextCursor = items[len(items)-1].ID
	}
	// ⚠️ SERVIE À CHAQUE LECTURE, pas une fois à l'ouverture : l'application
	// doit pouvoir faire apparaître ou disparaître le micro si le stockage ou
	// la transcription tombent en cours de course.
	resp.Voice = h.svc.VoicePolicy()
	httpx.JSON(w, http.StatusOK, resp)
}

// POST /{orders|rides}/{id}/messages
func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	// ⚠️⚠️ LA MÊME ROUTE POUR LE TEXTE ET LE VOCAL, choisie par le TYPE DE
	// CONTENU. Une seconde route (`…/messages/voice`) aurait obligé chaque
	// application à deux chemins de code pour un seul geste — « envoyer ce que
	// je viens de composer » —, et aurait doublé les refus à gérer.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		h.sendVoice(w, r, userID, role)
		return
	}
	var req SendMessageRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	msg, err := h.svc.Send(r.Context(), userID, role, chi.URLParam(r, "id"), req.Body)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, msg)
}

// sendVoice reçoit le vocal, le fait stocker et transcrire par la VERTICALE,
// puis le pose dans la conversation.
//
// ⚠️ LE TRAVAIL EST DÉLÉGUÉ, et ce n'est pas de la timidité : ce paquet est au
// socle, et TOUTE L'IA de la plateforme vit dans `dira-analytics`. Transcrire
// ici ferait du socle le client d'un service dont il ne dépend pas, et
// stocker ici lui ferait connaître les seaux d'une verticale.
func (h *Handler) sendVoice(w http.ResponseWriter, r *http.Request, userID, role string) {
	intake := h.svc.intake
	if intake == nil {
		httpx.Error(w, r, errVoiceUnavailable)
		return
	}
	v, err := intake.Receive(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	msg, err := h.svc.SendVoice(r.Context(), userID, role, chi.URLParam(r, "id"), v)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, msg)
}

// POST /{orders|rides}/{id}/messages/read — tout ce que l'autre a envoyé.
func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	userID, role, ok := caller(r)
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
		return
	}
	n, err := h.svc.MarkRead(r.Context(), userID, role, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"marked": n})
}

// POST /admin/{orders|rides}/{id}/messages — au nom d'une partie.
func (h *Handler) sendAs(w http.ResponseWriter, r *http.Request) {
	var req SendAsRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	msg, err := h.svc.SendAs(r.Context(), chi.URLParam(r, "id"), req.As, req.Body)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, msg)
}

func caller(r *http.Request) (userID, role string, ok bool) {
	userID, ok = auth.UserFromContext(r.Context())
	if !ok {
		return "", "", false
	}
	role, _ = auth.RoleFromContext(r.Context())
	return userID, role, true
}
