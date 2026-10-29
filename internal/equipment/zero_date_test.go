package equipment

// ⚠️⚠️ UN PAYS QUI N'A RIEN RÉGLÉ NE DOIT PAS SE DATER DE L'AN 1.
//
// `DefaultSettings` est le plancher servi tant qu'un pays n'a rien écrit —
// c'est-à-dire le cas ORDINAIRE, pas le cas limite. Son `UpdatedAt` était un
// `time.Time` nu, donc `0001-01-01T00:00:00Z` partait sur le fil comme une
// vraie date.
//
// ⚠️ ET ON LE SAIT PARCE QUE QUELQU'UN L'A DÉJÀ PAYÉ. La console de Dira porte
// le contournement écrit à la main le jour où elle l'a découvert :
//
//	settings.updated_at && !settings.updated_at.startsWith("0001")
//	  — dira-admin/src/pages/Equipment.tsx
//
// Un défaut corrigé dans l'écran qui l'affiche reste un défaut sur le fil, pour
// tous les autres lecteurs. Celui-ci est corrigé à la source.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDefaultSettingsCarryNoZeroDate(t *testing.T) {
	raw, err := json.Marshal(toSettings(&[]Settings{DefaultSettings("GA")}[0]))
	if err != nil {
		t.Fatal(err)
	}
	if zeroDated(string(raw)) != "" {
		t.Fatalf("les réglages par défaut portent la date zéro : %s\n"+
			"« jamais réglé » et « réglé le 1er janvier de l'an 1 » sont deux faits "+
			"différents, et seul un champ ABSENT dit le premier", raw)
	}
}

// ⚠️ ET DES RÉGLAGES ÉCRITS PORTENT LEUR DATE. Sans ce versant, retirer le
// champ passerait l'épreuve ci-dessus — et la console ne saurait plus jamais
// quand un pays a changé sa politique.
func TestWrittenSettingsKeepTheirDate(t *testing.T) {
	s := DefaultSettings("GA")
	s.UpdatedAt = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(toSettings(&s))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "2026-10-29") {
		t.Fatalf("des réglages datés doivent porter leur date : %s", raw)
	}
}

// zeroDated nomme la forme fautive trouvée dans un corps, ou rend "".
//
// ⚠️ DEUX FORMES, PAS UNE. La date zéro d'un `time.Time` nu
// (`0001-01-01T00:00:00Z`) part sur le fil comme une vraie date. Et un
// pointeur nil SANS `omitempty` rend `"updated_at": null` — ce qui ne contient
// pas la date zéro, donc échappait à un contrôle qui ne cherchait qu'elle :
// `null` est moins mensonger que l'an 1, mais un lecteur qui fait
// `new Date(x)` dessus obtient le 1er janvier 1970, et le problème n'a
// reculé que de dix-neuf siècles. Seul un champ ABSENT dit « jamais réglé ».
func zeroDated(body string) string {
	switch {
	case strings.Contains(body, "0001-01-01"):
		return "la date zéro (0001-01-01)"
	case strings.Contains(body, `"updated_at":null`):
		return "`updated_at` présent et nul"
	}
	return ""
}
