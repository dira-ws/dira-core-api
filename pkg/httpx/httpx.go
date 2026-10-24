// Package httpx provides shared HTTP helpers: JSON writing, request decoding
// with validation, the standard error envelope (localized via i18n) and
// cursor pagination.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/i18n"
	"github.com/kgtech-org/dira-core-api/pkg/obs"
	"github.com/kgtech-org/dira-core-api/pkg/phone"
)

var validate = newValidator()

// newValidator construit le validateur avec DEUX choix qui regardent le client :
//
//   - les champs sont nommés par leur clé JSON. Le validateur nomme les champs
//     Go (`Phone`, `FirstName`) ; une application qui reçoit « FirstName »
//     ne sait pas à quelle case le rattacher, elle envoie `first_name` ;
//   - `e164` exige le `+`. Celui de la bibliothèque le rend facultatif :
//     « 22899000001 » passait, était stocké tel quel, et devenait un second
//     compte à côté de « +22899000001 » — que la personne ne retrouvait plus.
func newValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})
	_ = v.RegisterValidation("e164", func(fl validator.FieldLevel) bool {
		return phone.Valid(fl.Field().String())
	})
	return v
}

var translator *i18n.Translator

// faults capture les pannes SERVEUR — voir `pkg/obs`.
//
// ⚠️ ICI ET NULLE PART AILLEURS, parce que TOUS les refus passent par
// `Error`. Le poser à chaque appelant aurait été des centaines d'endroits,
// dont ceux qu'on n'a pas encore écrits ; posé ici, un `5xx` ne peut pas
// passer inaperçu.
var faults obs.Faulter

// SetFaultSink installe la capture des pannes serveur (câblage). Sans elle,
// un `5xx` est seulement journalisé — le comportement d'avant.
func SetFaultSink(f obs.Faulter) { faults = f }

// SetTranslator installs the process-wide translator used to localize error
// messages. Called once at startup; without it English fallbacks are used.
func SetTranslator(t *i18n.Translator) { translator = t }

// JSON writes v as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			slog.Error("httpx: encode response", "error", err)
		}
	}
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Fields nomme les clés JSON en cause dans un `validation_failed` — les
	// champs invalides, ou la clé inconnue. C'est ce qui permet à une
	// application de souligner LA case fautive au lieu d'afficher « vérifiez
	// vos informations » sous un formulaire entièrement correct.
	Fields []string `json:"fields,omitempty"`
	// Reason précise un `validation_failed` quand la cause n'est pas la valeur
	// d'un champ : `unknown_field` (une clé que le serveur ne connaît pas),
	// `invalid_json` (corps illisible).
	Reason string `json:"reason,omitempty"`
}

// Error writes err using the standard envelope {"error":{"code","message"}},
// localizing the message from the request language when a translation exists.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	appErr := apperr.From(err)
	if appErr.HTTPStatus >= 500 {
		slog.ErrorContext(r.Context(), "request failed", "code", appErr.Code, "error", appErr.Error())
		// ⚠️ SEULEMENT LES `5xx`. Un `404` n'est pas une panne, un `422` non
		// plus : ce sont des refus attendus, déjà comptés par les mesures.
		// Les capturer noierait la liste des vraies pannes sous le bruit
		// normal d'une journée — et une liste de pannes qu'on n'ose plus
		// ouvrir ne sert à rien.
		if faults != nil {
			faults.Capture(r.Context(), obs.Fault{
				Kind: "http_5xx", Message: appErr.Code + ": " + appErr.Error(),
				Method: r.Method, Status: appErr.HTTPStatus,
			})
		}
	}
	detail := errorDetail{Code: appErr.Code, Message: Message(r.Context(), appErr)}
	if fields, ok := appErr.Meta["fields"].([]string); ok {
		detail.Fields = fields
	}
	if reason, ok := appErr.Meta["reason"].(string); ok {
		detail.Reason = reason
	}
	JSON(w, appErr.HTTPStatus, errorBody{Error: detail})
}

// Message rend la phrase d'un refus DANS LA LANGUE DE LA REQUÊTE — la même
// que l'enveloppe `Error` écrit —, pour les endroits qui portent un refus
// ailleurs que dans l'enveloppe : une ligne de `results[]` d'une
// resynchronisation, par exemple.
//
// ⚠️ Sans cela, l'enveloppe parlait français et la ligne de résultat
// anglais — « ride not found », lu par un chauffeur au moment précis où on
// lui annonce qu'une course roulée n'a pas été enregistrée (relevé de
// l'équipe chauffeur, 29 sept. 2026). Sans traducteur installé, la phrase
// anglaise du code.
func Message(ctx context.Context, err error) string {
	appErr := apperr.From(err)
	if translator != nil {
		if localized, ok := translator.Translate(i18n.LangFromContext(ctx), "errors."+appErr.Code, appErr.Meta); ok {
			return localized
		}
	}
	return appErr.Message
}

// DecodeService parse le corps d'un RAPPEL DE SERVICE et le valide — sans
// refuser les clés inconnues.
//
// ⚠️⚠️ LA RÈGLE DE `Decode` EST INVERSÉE ICI, ET IL FAUT QU'ELLE LE SOIT.
// Refuser une clé inconnue protège un développeur d'application : il croirait
// avoir enregistré un champ que la route ne lit pas. Mais quand l'appelant est
// un SERVICE, cette même règle transforme un ajout ADDITIF en rupture — et une
// rupture muette, puisque personne ne lit les réponses d'un rappel.
//
// C'est arrivé, et pendant un mois : le suivi a ajouté `attempt` au corps de
// `POST /internal/dispatch/wave` (le numéro de la tentative, dont les courses
// se servent pour appeler la classe exacte d'abord). Les courses l'ont lu ; la
// LIVRAISON, qui ne s'en sert pas, l'a REFUSÉ — `422 unknown field: attempt`,
// sur CHAQUE vague. Conséquences, toutes silencieuses :
//
//   - aucune livraison n'était plus RÉSERVÉE aux livreurs appelés : le
//     classement par proximité ne servait plus à rien, le premier à ouvrir son
//     application prenait la course ;
//   - le PUSH aux appelés ne partait plus — il est écrit après la réservation —
//     et un livreur qui attend une course a justement rangé son téléphone ;
//   - et le transfert d'une commande ne pouvait JAMAIS passer la main, le
//     remplaçant n'étant jamais dans la liste des appelés.
//
// Rien ne le signalait : les commandes étaient quand même livrées, parce que
// le pot commun restait ouvert.
//
// Les deux verticales décodent donc leurs rappels de service avec celle-ci. Ce
// qui est VALIDÉ ne change pas : les tags `validate` s'appliquent comme avant,
// et un champ obligatoire manquant est toujours refusé.
func DecodeService(r *http.Request, dst any) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return apperr.Validation("invalid JSON body").
			WithMeta(map[string]any{"reason": "invalid_json"}).WithCause(err)
	}
	return validateStruct(dst)
}

// unknownFieldRe extrait la clé d'une erreur `json: unknown field "x"`.
// encoding/json ne l'expose pas autrement que dans le texte.
var unknownFieldRe = regexp.MustCompile(`json: unknown field "([^"]*)"`)

// Decode parses the JSON body into dst and validates it ("validate" tags).
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	// ⚠️ Une clé inconnue est REFUSÉE, pas ignorée. Une application qui
	// envoie `first_name` à une route qui ne le lit pas croirait l'avoir
	// enregistré. Mais un refus qui ne NOMME pas la clé n'apprend rien : la
	// réponse la porte dans `fields`, avec `reason: unknown_field`.
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if m := unknownFieldRe.FindStringSubmatch(err.Error()); m != nil {
			return apperr.Validation("unknown field: " + m[1]).
				WithMeta(map[string]any{"fields": []string{m[1]}, "reason": "unknown_field"}).
				WithCause(err)
		}
		return apperr.Validation("invalid JSON body").
			WithMeta(map[string]any{"reason": "invalid_json"}).WithCause(err)
	}
	return validateStruct(dst)
}

// validateStruct applique les tags `validate` et NOMME les champs fautifs.
// Partagée par `Decode` et `DecodeService` : les deux ne diffèrent que par le
// traitement des clés inconnues.
func validateStruct(dst any) error {
	if err := validate.Struct(dst); err != nil {
		var vErrs validator.ValidationErrors
		if errors.As(err, &vErrs) && len(vErrs) > 0 {
			fields := make([]string, 0, len(vErrs))
			for _, fe := range vErrs {
				fields = append(fields, fe.Field())
			}
			return apperr.Validation(fmt.Sprintf("invalid fields: %s", strings.Join(fields, ", "))).
				WithMeta(map[string]any{"fields": fields}).WithCause(err)
		}
		return apperr.Validation("invalid request").WithCause(err)
	}
	return nil
}

// Page holds cursor pagination parameters (?limit=20&cursor=<id>).
type Page struct {
	Limit  int
	Cursor string
}

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// PageFromRequest extracts pagination parameters with bounds.
func PageFromRequest(r *http.Request) Page {
	p := Page{Limit: defaultPageLimit, Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			p.Limit = min(n, maxPageLimit)
		}
	}
	return p
}

type listBody struct {
	Items      any    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// List writes the standard paginated list envelope.
func List(w http.ResponseWriter, items any, nextCursor string) {
	JSON(w, http.StatusOK, listBody{Items: items, NextCursor: nextCursor})
}

// PDF sert un document à télécharger.
//
// ⚠️ `attachment` ET NON `inline`, et un NOM DE FICHIER explicite. Un reçu
// ouvert dans l'onglet du navigateur s'appelle « receipt.pdf » dans le dossier
// de téléchargements, et vingt reçus s'appellent tous pareil : la personne qui
// cherche celui de mardi les ouvre un par un. Le nom porte donc l'opération et
// sa date.
//
// ⚠️ ET `no-store`. Un reçu porte un nom, un trajet et un montant : laissé en
// cache par un proxy ou par le navigateur d'un cybercafé, il se relit après la
// déconnexion. C'est le même raisonnement que pour un relevé bancaire.
func PDF(w http.ResponseWriter, filename string, body []byte) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(filename)+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// safeFilename retire d'un nom de fichier ce qui casserait l'en-tête.
//
// ⚠️ UN GUILLEMET OU UN RETOUR À LA LIGNE DANS CE NOM EST UNE INJECTION
// D'EN-TÊTE, pas une coquetterie : le nom vient d'une adresse ou d'un libellé
// saisi par quelqu'un. Les caractères non ASCII partent aussi — un en-tête HTTP
// ne les transporte pas sans un encodage que tous les clients ne lisent pas.
func safeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "document.pdf"
	}
	return b.String()
}
