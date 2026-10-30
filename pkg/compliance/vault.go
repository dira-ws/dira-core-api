package compliance

// LES PIÈCES AU DÉPÔT : RANGÉES SOUS LEUR GENRE, SERVIES SIGNÉES.
//
// ⚠️⚠️ UNE PIÈCE DE CONFORMITÉ EST CE QUE LA PLATEFORME DÉTIENT DE PLUS INTIME
// sur un agent — carte d'identité, permis, extrait de casier judiciaire. Elle
// ne doit être lisible que par ceux à qui l'API la sert, le temps de la
// regarder : une adresse qui ne périme pas est un jeton au porteur, et elle
// finit dans une capture d'écran, un ticket, un journal d'accès.
//
// Ce fichier fait deux gestes, à deux moments :
//
//   - au DÉPÔT, la pièce est RANGÉE sous le genre `document` du dépôt, quel que
//     soit le genre sous lequel l'application l'a envoyée. Le serveur est le
//     seul à pouvoir corriger un dépôt mal rangé sans attendre une nouvelle
//     version de chaque application ;
//   - à la LECTURE, l'adresse est SIGNÉE pour une durée courte. On range
//     l'adresse NUE : signée, elle vieillirait dans la base.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Vault : le dépôt d'objets vu de la conformité. Déclaré côté consommateur —
// c'est `pkg/storage.Store`.
type Vault interface {
	Key(rawURL string) (string, bool)
	Adopt(ctx context.Context, rawURL, kind, entity string) (string, error)
	SignedURL(ctx context.Context, rawURL string, ttl time.Duration) (string, error)
}

// SetVault branche le dépôt (câblage).
func (s *Service) SetVault(v Vault) { s.vault = v }

// DocumentStorageKind : le genre du dépôt où vivent les pièces.
const DocumentStorageKind = "document"

// SignedFor : la durée de vie d'une adresse de pièce servie.
//
// ⚠️ PLUS LONGUE QUE CELLE D'UN VOCAL (un quart d'heure), parce qu'une pièce se
// REGARDE — on la compare à un nom, on zoome sur une date —, mais une demi-heure
// et pas une journée : une adresse qui vit une journée est une adresse publique
// avec une date. L'écran qui la montre relit la liste quand elle a expiré.
const SignedFor = 30 * time.Minute

// errForeignFile : `file_url` ne désigne pas un objet de NOTRE dépôt.
//
// ⚠️ LA VALIDATION ÉTAIT « UNE URL », RIEN DE PLUS : une pièce pouvait pointer
// n'importe où — un hébergeur tiers, une adresse qui change de contenu après
// validation. Une pièce se dépose par `POST /uploads`.
var errForeignFile = apperr.New("file_url_foreign",
	"file_url must be an address returned by POST /uploads", http.StatusUnprocessableEntity).
	WithMeta(map[string]any{"fields": []string{"file_url"}})

// keep range une pièce déposée et rend l'adresse NUE à ranger.
//
// Sans dépôt branché : l'adresse telle quelle — le comportement d'avant.
func (s *Service) keep(ctx context.Context, raw, owner string) (string, error) {
	if s.vault == nil {
		return raw, nil
	}
	if _, ok := s.vault.Key(raw); !ok {
		return "", errForeignFile
	}
	out, err := s.vault.Adopt(ctx, raw, DocumentStorageKind, owner)
	if err != nil {
		return "", apperr.Internal(err)
	}
	return out, nil
}

// respond compose la réponse d'une pièce, son adresse SIGNÉE.
//
// ⚠️ UNE SIGNATURE QUI ÉCHOUE REND L'ADRESSE NUE, ET LE DIT. Elle ne s'affiche
// plus une fois le genre fermé — mais une liste entière refusée pour une
// signature ratée priverait l'exploitation de TOUTES les pièces d'un agent.
func (s *Service) respond(ctx context.Context, d *Document, now time.Time) DocumentResponse {
	out := toDocumentResponse(d, now)
	if s.vault == nil || out.FileURL == "" {
		return out
	}
	// Une adresse hors de notre dépôt (une pièce de démonstration, une donnée
	// d'avant) ne se signe pas : elle est servie telle quelle, sans bruit.
	if _, ours := s.vault.Key(out.FileURL); !ours {
		return out
	}
	signed, err := s.vault.SignedURL(ctx, out.FileURL, SignedFor)
	if err != nil {
		slog.WarnContext(ctx, "compliance: document address not signed — it may not display",
			"document_id", out.ID, "error", err)
		return out
	}
	out.FileURL = signed
	return out
}

// Strays : ce qu'il faut au dépôt de lignes pour ranger les pièces d'avant.
type Strays interface {
	DocumentsOutside(ctx context.Context, marker string, limit int) ([]Document, error)
	SetFileURL(ctx context.Context, d *Document, fileURL string) error
}

// AdoptStrays range les pièces DÉJÀ DÉPOSÉES hors de leur genre, et rend
// combien ont été rangées.
//
// ⚠️⚠️ RANGER LES NOUVELLES NE RANGE PAS LES ANCIENNES. Ici, chaque pièce
// déposée avant est copiée sous `document/`, sa ligne réécrite, et l'original
// retiré.
//
// ⚠️ ELLE SE REJOUE : une pièce déjà rangée n'est plus lue, et une pièce dont le
// fichier a disparu est dite et laissée — on ne réécrit pas une ligne vers un
// objet qu'on n'a pas pu copier.
func (s *Service) AdoptStrays(ctx context.Context, limit int) (int, error) {
	strays, ok := s.repo.(Strays)
	if s.vault == nil || !ok {
		return 0, nil
	}
	docs, err := strays.DocumentsOutside(ctx, "/"+DocumentStorageKind+"/", limit)
	if err != nil {
		return 0, err
	}
	moved := 0
	for i := range docs {
		d := &docs[i]
		if d.FileURL == "" || strings.Contains(d.FileURL, "/"+DocumentStorageKind+"/") {
			continue
		}
		// Une adresse étrangère au dépôt est refusée par `Adopt` lui-même : la
		// pièce est laissée telle quelle, et le journal le dit.
		owner := d.OwnerID.Hex()
		out, err := s.vault.Adopt(ctx, d.FileURL, DocumentStorageKind, owner)
		if err != nil {
			slog.WarnContext(ctx, "compliance: document not adopted", "document_id", d.ID.Hex(), "error", err)
			continue
		}
		if err := strays.SetFileURL(ctx, d, out); err != nil {
			slog.ErrorContext(ctx, "compliance: DOCUMENT COPIED BUT ITS LINE STILL POINTS TO THE OLD ADDRESS",
				"document_id", d.ID.Hex(), "error", err)
			continue
		}
		moved++
	}
	return moved, nil
}

// replacedFile : l'adresse de la pièce qu'un dépôt va REMPLACER — même
// porteur, même type, même véhicule. Vide s'il n'y en a pas.
func (s *Service) replacedFile(ctx context.Context, owner primitive.ObjectID, kind string, vehicle *primitive.ObjectID) string {
	docs, err := s.repo.DocumentsByOwner(ctx, owner)
	if err != nil {
		return ""
	}
	want := docKey(kind, vehicle)
	for _, d := range docs {
		if docKey(d.Kind, d.VehicleID) == want {
			return d.FileURL
		}
	}
	return ""
}

// dropReplaced retire du dépôt le fichier d'une pièce REMPLACÉE.
//
// ⚠️⚠️ SANS CE GESTE, L'EFFACEMENT D'UN COMPTE N'EST PAS COMPLET. Le dépôt
// remplace la LIGNE d'une pièce ; l'ancien fichier restait dans le seau, et
// plus rien ne le désignait. Le jour où le compte est effacé, l'effacement
// suit les lignes : la photo de la carte d'identité d'avant, elle, restait
// pour toujours — impossible à retrouver, impossible à justifier.
//
// ⚠️ ET UN FICHIER ENCORE DÉSIGNÉ PAR UNE AUTRE PIÈCE DU MÊME PORTEUR N'EST PAS
// RETIRÉ : une application qui joint la même photo à deux pièces ne doit pas
// en perdre une parce que l'autre a été remplacée. AU MIEUX : un retrait qui
// échoue est dit, et n'empêche pas le dépôt.
func (s *Service) dropReplaced(ctx context.Context, owner primitive.ObjectID, old string) {
	// ⚠️ LE SEUL GARDE-FOU, ET IL SUFFIT : un fichier encore désigné par une
	// pièce de ce porteur — y compris celle qu'on vient de déposer, si c'est
	// le même fichier — reste.
	if s.files == nil || old == "" {
		return
	}
	if docs, err := s.repo.DocumentsByOwner(ctx, owner); err == nil {
		for _, d := range docs {
			if storageBare(d.FileURL) == storageBare(old) {
				return
			}
		}
	}
	if err := s.files.Remove(ctx, old); err != nil {
		slog.WarnContext(ctx, "compliance: the replaced document file stays in the bucket", "error", err)
	}
}

// storageBare : une adresse sans sa signature — deux adresses du même objet,
// l'une signée, l'autre nue, désignent le même fichier.
func storageBare(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}
