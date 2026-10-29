package finance

// ⚠️ MÊME GARDE QU'`internal/equipment` ET `internal/payroll` : la facturation
// et le catalogue de déclarations d'un pays qui n'a rien réglé sont servis
// depuis leur DÉFAUT, et leur `updated_at` n'existe pas encore.
//
// `0001-01-01T00:00:00Z` part sur le fil comme une VRAIE date. Seul un champ
// ABSENT dit « jamais réglé » — et ici le modèle EST la réponse, donc le champ
// est un pointeur jusqu'au stockage.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCountryDefaultsCarryNoZeroDate(t *testing.T) {
	for name, v := range map[string]any{
		"billing": DefaultBilling("GA"),
		"catalog": DefaultDeclarationCatalog("GA"),
	} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if zeroDated(string(raw)) != "" {
			t.Errorf("le défaut `%s` porte la date zéro : %s", name, raw)
		}
	}

	// ⚠️ Et l'autre versant : une politique écrite garde sa date. Sans lui,
	// retirer le champ passerait l'épreuve ci-dessus.
	b := DefaultBilling("GA")
	b.UpdatedAt = ptrTime(time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC))
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "2026-10-29") {
		t.Fatalf("une facturation datée doit porter sa date : %s", raw)
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
