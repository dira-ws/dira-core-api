// Package upload exposes the authenticated media upload endpoint. Files land
// in object storage; the caller persists the returned public URL on the
// relevant document (avatar, vehicle photo, compliance document, dish images,
// store image, brand logo, feed video, banner).
//
// AU SOCLE, et pas dans une verticale. Il y a vécu par accident d'histoire :
// écrit dans le monolithe, resté dans la livraison à la scission parce que
// ses appelants l'y trouvaient. Mais un avatar est un objet du profil, une
// photo de véhicule ou un permis servent aux deux métiers — et les courses
// n'avaient AUCUNE porte d'envoi : un chauffeur VTC photographiait sa berline
// via `/food/uploads`. Une seule règle de poids et de type, un seul bucket,
// une seule configuration à tenir.
package upload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/media"
	"github.com/kgtech-org/dira-core-api/pkg/storage"
)

// objectStore is the storage contract consumed by the handler. L'interface
// existe pour que la règle des shorts (durée, type, poids) soit vérifiable
// sans MinIO : c'est elle qui protège le format, pas le stockage.
type objectStore interface {
	Put(ctx context.Context, kind, entity, contentType string, size int64, r io.Reader) (string, error)
	SignedURL(ctx context.Context, rawURL string, ttl time.Duration) (string, error)
}

// privateFor : la durée de l'adresse rendue pour un genre PRIVÉ — le temps de
// montrer un aperçu, puis de la renvoyer à la route qui la range.
const privateFor = 30 * time.Minute

type Handler struct {
	store objectStore
}

// NewHandler builds the upload handler. A nil store keeps the endpoint
// mounted but answering 503.
func NewHandler(store *storage.Store) *Handler {
	if store == nil {
		return &Handler{} // évite une interface non-nil enveloppant un pointeur nil
	}
	return &Handler{store: store}
}

// Mount registers POST /uploads (any authenticated role).
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/uploads", h.upload)
	})
}

// ⚠️⚠️ TROIS REFUS NOMMÉS, ET CE QU'ILS DISENT EST DANS LA PHRASE TRADUITE.
//
// Vérifié sur la préproduction le 24 octobre 2026 : `apperr.Validation("kind
// must be one of …")` répondait **« Requête invalide »**, et rien d'autre — en
// français comme en anglais. L'enveloppe de refus ne sert que `code`,
// `message`, `fields` et `reason`, et le `message` est REMPLACÉ par la phrase
// traduite du code : tout ce qui n'est pas dans cette phrase est perdu en
// route. La liste des genres et les formats acceptés étaient donc du texte
// mort, lisible seulement dans les journaux du serveur.
//
// ⚠️ C'est la QUATRIÈME fois que ce piège se présente sur cette plateforme
// (après `meta.cooldown_s` du klaxon, `pool_too_short` et
// `rental_out_of_range`) — et la première où il emportait une règle écrite le
// jour même. La règle tient maintenant : un code à soi, et les listes passées
// en `WithMeta` pour que le gabarit les insère DANS la phrase.
//
// ⚠️ ET LES LISTES SE LISENT DE LA TABLE, elles ne se recopient pas : une liste
// écrite à la main dans une phrase de refus vieillit en silence à chaque genre
// ajouté, et personne ne relit un message d'erreur.
func errUnknownKind() *apperr.Error {
	kinds := strings.Join(storage.KindNames(), ", ")
	return apperr.New("upload_kind_unknown", "kind must be one of "+kinds,
		http.StatusUnprocessableEntity).
		WithMeta(map[string]any{"kinds": kinds, "fields": []string{"kind"}})
}

func errBadFormat(kind string) *apperr.Error {
	formats := storage.LabelFor(kind)
	return apperr.New("upload_format_rejected",
		"unsupported type for kind "+kind+" (accepted: "+formats+")",
		http.StatusUnprocessableEntity).
		WithMeta(map[string]any{"formats": formats, "fields": []string{"file"}})
}

// errTooLarge : le plafond DU GENRE, dit à qui vient de perdre son envoi.
//
// ⚠️ IL COUVRE AUSSI LE CORPS MULTIPART ILLISIBLE, et le dit comme une taille.
// `MaxBytesReader` coupe la lecture : l'erreur qui remonte est une erreur
// d'analyse, indiscernable d'un vrai corps malformé. Entre « votre fichier est
// trop gros » sur un corps cassé et « requête invalide » sur un fichier trop
// gros, la première se corrige — un corps multipart malformé n'arrive que d'un
// client qu'on écrit, jamais de quelqu'un qui envoie une photo.
func errTooLarge(kind string) *apperr.Error {
	mib := storage.MaxMiBFor(kind)
	return apperr.New("upload_too_large",
		"file too large for kind "+kind+" (max "+mib+" MiB) or invalid multipart body",
		http.StatusUnprocessableEntity).
		WithMeta(map[string]any{"max_mib": mib, "fields": []string{"file"}})
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.Error(w, r, apperr.New("storage_unavailable", "object storage is not configured", http.StatusServiceUnavailable))
		return
	}
	kind := r.URL.Query().Get("kind")
	if !storage.Known(kind) {
		httpx.Error(w, r, errUnknownKind())
		return
	}
	// Le plafond dépend du kind : une vidéo de feed pèse bien plus qu'une
	// photo de plat, mais rien d'autre ne doit profiter de ce plafond.
	maxBytes := storage.MaxBytesFor(kind)
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		httpx.Error(w, r, errTooLarge(kind).WithCause(err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, r, apperr.Validation("multipart field 'file' is required").WithCause(err))
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if _, ok := storage.Accepts(kind, contentType); !ok {
		// ⚠️ LA PHRASE DIT LES FORMATS DE *CE* GENRE — et elle les dit
		// VRAIMENT, depuis le 24 octobre 2026 : voir `errBadFormat`. Un
		// message générique ne disait ni pourquoi un SVG est refusé ici, ni
		// qu'un reçu prend le PDF.
		httpx.Error(w, r, errBadFormat(kind))
		return
	}
	// Une vidéo de feed est un SHORT : la durée est lue dans le conteneur, pas
	// déclarée par l'appelant. Le contrôle du navigateur sert le confort de
	// l'utilisateur ; celui-ci sert la règle.
	var duration time.Duration
	if _, isVideo := storage.VideoTypes[contentType]; isVideo {
		var tooLong media.ErrTooLong
		d, err := media.CheckShort(file, contentType)
		switch {
		case errors.As(err, &tooLong):
			httpx.Error(w, r, apperr.New("video_too_long",
				fmt.Sprintf("la vidéo dure %.0f s, la limite est de %.0f s",
					d.Seconds(), media.MaxShortDuration.Seconds()),
				http.StatusUnprocessableEntity))
			return
		case err != nil:
			// Refuser plutôt qu'accepter à l'aveugle : un conteneur dont on ne
			// sait pas lire la durée n'est pas contrôlable, et le transcodage
			// échouerait de toute façon plus tard, après coup et sans message.
			httpx.Error(w, r, apperr.New("video_unreadable",
				"vidéo illisible : réexportez-la en MP4 (H.264) et réessayez",
				http.StatusUnprocessableEntity).WithCause(err))
			return
		}
		duration = d
	}

	// ⚠️⚠️ UN GENRE PRIVÉ SE RANGE SOUS LE COMPTE QUI L'ENVOIE, jamais sous une
	// entité choisie par l'appelant : c'est le préfixe qui permet d'effacer
	// les pièces d'une personne avec elle, et un appelant ne doit pas pouvoir
	// ranger un fichier sous le préfixe d'un autre.
	entity := r.URL.Query().Get("entity")
	private := storage.Private(kind)
	if private {
		entity, _ = auth.UserFromContext(r.Context())
	}
	url, err := h.store.Put(r.Context(), kind, entity, contentType, header.Size, file)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	// ⚠️ ET L'ADRESSE RENDUE EST SIGNÉE, à durée limitée : un genre privé ne se
	// lit pas sans signature, et l'application doit pouvoir montrer l'aperçu.
	// Elle la renvoie telle quelle à la route qui range la pièce, qui garde
	// l'adresse NUE. Une signature ratée rend l'adresse nue — l'aperçu ne
	// s'affichera pas, mais le dépôt de la pièce, lui, marchera.
	if private {
		if signed, err := h.store.SignedURL(r.Context(), url, privateFor); err == nil {
			url = signed
		} else {
			slog.WarnContext(r.Context(), "upload: private object stored, its preview address could not be signed",
				"kind", kind, "error", err)
		}
	}
	// La durée revient à l'appelant : la console l'affiche et la repasse au
	// feed sans avoir à re-sonder le fichier.
	body := map[string]any{"url": url}
	if duration > 0 {
		body["duration_seconds"] = int(duration.Round(time.Second).Seconds())
	}
	httpx.JSON(w, http.StatusCreated, body)
}
