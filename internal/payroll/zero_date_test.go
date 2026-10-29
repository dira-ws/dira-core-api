package payroll

// ⚠️ MÊME GARDE QU'`internal/equipment` ET `internal/finance`, et il est local
// exprès : un pays qui n'a jamais réglé sa paie ne doit pas se dater de l'an 1.
//
// `0001-01-01T00:00:00Z` part sur le fil comme une VRAIE date : une console
// l'affiche « modifié le 01/01/0001 », et une application qui compare
// `updated_at` pour savoir si son cache a vieilli se compare à l'an 1, donc ne
// rafraîchit jamais. Seul un champ ABSENT dit « jamais réglé ».

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDefaultSettingsCarryNoZeroDate(t *testing.T) {
	s := DefaultSettings("GA")
	raw, err := json.Marshal(toSettings(&s, "XAF"))
	if err != nil {
		t.Fatal(err)
	}
	if zeroDated(string(raw)) != "" {
		t.Fatalf("les réglages par défaut portent la date zéro : %s", raw)
	}
	// ⚠️ Et l'autre versant : des réglages écrits gardent leur date.
	s.UpdatedAt = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	raw, err = json.Marshal(toSettings(&s, "XAF"))
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
