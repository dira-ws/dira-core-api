package user

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// CHANGER SON MOT DE PASSE.
//
// ⚠️⚠️ CETTE ROUTE N'EXISTAIT PAS, ET C'ÉTAIT UN TROU DE SÉCURITÉ ORDINAIRE.
// Les comptes à mot de passe de cette plateforme sont créés PAR QUELQU'UN
// D'AUTRE, qui choisit le mot de passe et le transmet de vive voix : le
// propriétaire d'une flotte ouvre l'accès de son gérant, l'exploitation ouvre
// celui d'un partenaire. Sans ce geste, celui qui a créé le compte connaît le
// mot de passe POUR TOUJOURS — et la personne qui s'en sert n'a aucun moyen de
// reprendre la main sur son propre accès.

// ChangePasswordRequest : l'ancien, le nouveau.
//
// ⚠️ L'ANCIEN EST EXIGÉ. C'est ce qui fait la différence entre « changer son
// mot de passe » et « prendre un compte » : un jeton volé ou un écran laissé
// ouvert suffirait sinon à verrouiller quelqu'un hors de son propre compte.
//
// ⚠️⚠️ ET LA LONGUEUR N'EST PAS DANS LE TAG `validate:`. Elle y était, et c'était
// un défaut : `min=8` lève `validation_failed`, que `httpx.Message` traduit PAR
// LE CODE — la personne recevait « Requête invalide » en choisissant son mot de
// passe, sans jamais apprendre qu'il en faut huit. Une règle qu'on impose doit
// se DIRE, surtout celle-là : c'est le seul endroit de la plateforme où
// quelqu'un invente un mot de passe, et il le fait une seule fois.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewPassword     string `json:"new_password" validate:"required"`
}

// La longueur exigée d'un mot de passe choisi par son propriétaire.
//
// ⚠️ LE MAXIMUM N'EST PAS UNE COQUETTERIE : argon2 hache ce qu'on lui donne,
// donc une phrase d'un mégaoctet se hacherait vraiment — et occuperait le
// serveur le temps de le faire.
const (
	passwordMinLen = 8
	passwordMaxLen = 120
)

var (
	// ⚠️ UN CODE À LUI, et pas `validation_failed` : la traduction se fait par
	// le code, donc un refus en `validation_failed` sort en « Requête
	// invalide » — et la personne ne sait pas lequel des deux champs corriger.
	errWrongCurrent = apperr.New("current_password_wrong",
		"the current password is not right: check it, or ask whoever opened your access",
		http.StatusUnprocessableEntity)
	errSamePassword = apperr.New("password_unchanged",
		"the new password is the same as the old one: choose a different one",
		http.StatusUnprocessableEntity)
	// ⚠️ UN COMPTE SANS MOT DE PASSE N'EN CHANGE PAS. Les clients entrent par
	// code (voir `otp.go`) : leur proposer ce geste, puis leur refuser, les
	// enverrait chercher un mot de passe qu'ils n'ont jamais eu.
	errNoPassword = apperr.New("account_has_no_password",
		"this account signs in with a code, not a password",
		http.StatusUnprocessableEntity)
	// ⚠️ LA PHRASE PORTE LE NOMBRE. « Trop court » envoie essayer au hasard.
	errPasswordTooShort = apperr.New("password_too_short",
		"a password needs at least 8 characters: pick a longer one",
		http.StatusUnprocessableEntity)
	errPasswordTooLong = apperr.New("password_too_long",
		"a password cannot be longer than 120 characters",
		http.StatusUnprocessableEntity)
)

// ChangePassword remplace le mot de passe du compte appelant.
func (s *Service) ChangePassword(ctx context.Context, userID string, req ChangePasswordRequest) error {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if u.PasswordHash == "" {
		return errNoPassword
	}
	if len(req.NewPassword) < passwordMinLen {
		return errPasswordTooShort
	}
	if len(req.NewPassword) > passwordMaxLen {
		return errPasswordTooLong
	}
	ok, err := VerifyPassword(req.CurrentPassword, u.PasswordHash)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errWrongCurrent
	}
	// ⚠️ ON REFUSE LE MÊME MOT DE PASSE. Le geste n'aurait rien changé, et la
	// personne repartirait convaincue d'avoir repris la main sur son accès.
	if same, _ := VerifyPassword(req.NewPassword, u.PasswordHash); same {
		return errSamePassword
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	u.PasswordHash = hash
	if err := s.repo.UpdateUser(ctx, u); err != nil {
		return err
	}
	// ⚠️ JOURNALISÉ SANS LE MOT DE PASSE, évidemment — mais journalisé : « quand
	// mon mot de passe a-t-il changé ? » est la première question d'un compte
	// dont quelqu'un pense avoir perdu le contrôle.
	slog.InfoContext(ctx, "user: password changed", "user_id", userID, "role", u.Role)
	return nil
}

// POST /me/password {current_password, new_password} → 204
//
// ⚠️ RIEN N'EST RENDU. Une réponse qui porterait le compte à jour donnerait
// l'occasion d'y glisser un jour l'empreinte ; un 204 ne peut rien dire de
// trop.
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req ChangePasswordRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.ChangePassword(r.Context(), userID, req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
