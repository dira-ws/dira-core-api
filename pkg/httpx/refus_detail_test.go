package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeRefus joue un refus dans l'enveloppe et rend ce qui part sur le fil.
func decodeRefus(t *testing.T, err error) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	Error(rec, httptest.NewRequest(http.MethodGet, "/x", nil), err)
	var body struct {
		Error map[string]any `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error
}

// ⚠️⚠️ LE DÉTAIL D'UN REFUS DOIT ARRIVER, et c'est une affaire de FIL, pas de
// structure Go.
//
// Les trois clés ci-dessous étaient POSÉES par le code et PROMISES par les
// contrats ; `Error` n'en recopiait que deux autres, et jetait celles-là en
// silence. Mesuré en direct avant le correctif : `POST /rides/{id}/share`
// depuis Dira Lite rendait `feature_unavailable_in_edition` SANS dire quelle
// fonction — l'application ne pouvait pas cacher le bouton, qui est tout
// l'objet du refus (#252).
//
// ⚠️ ET LES ÉPREUVES DU CODE N'Y VOYAIENT RIEN : elles regardaient
// `appErr.Meta["feature"]`, c'est-à-dire l'erreur Go, jamais la réponse HTTP.
// C'est la troisième fois aujourd'hui qu'une épreuve sur une structure laisse
// passer un défaut de sérialisation.
func TestARefusalCarriesTheFeatureToHide(t *testing.T) {
	got := decodeRefus(t, apperr.Forbidden("feature_unavailable_in_edition", "nope").
		WithMeta(map[string]any{"feature": "rental"}))
	assert.Equal(t, "rental", got["feature"],
		"l'application ne sait pas quel menu cacher, et reproposera le bouton")
	assert.NotContains(t, got, "meta")
}

// ⚠️⚠️ ET LES CHIFFRES, EUX, RESTENT DANS LA PHRASE. C'est la règle posée de
// cette enveloppe, et elle tient : un chiffre, une personne le LIT — la phrase
// est déjà traduite, et l'y recopier à côté ferait deux sources pour une seule
// information. Seul ce que l'application doit FAIRE voyage en clair :
// `fields` (quelles cases souligner), `reason`, `feature` (quel menu cacher).
//
// Sans cette épreuve, l'enveloppe gagnerait une clé par refus un peu bavard,
// et le contrat deviendrait « tout ce qui traîne dans la carte ».
func TestNumbersStayInTheSentence(t *testing.T) {
	got := decodeRefus(t, apperr.Conflict("statement_too_large", "nope").
		WithMeta(map[string]any{"max_rows": 500}))
	assert.NotContains(t, got, "max_rows")

	got = decodeRefus(t, apperr.Conflict("promo_code_wrong_vertical", "nope").
		WithMeta(map[string]any{"valid_for": []string{"food"}}))
	assert.NotContains(t, got, "valid_for")
}

// ⚠️ ET RIEN N'EST SERVI QUAND RIEN N'EST POSÉ : les `omitempty` doivent taire
// ces clés sur les refus ordinaires, sinon chaque erreur de la plateforme
// porterait trois champs vides et les applications les liraient comme
// significatifs.
func TestAnOrdinaryRefusalCarriesNoDetail(t *testing.T) {
	got := decodeRefus(t, apperr.NotFound("ride_not_found", "nope"))
	for _, k := range []string{"feature", "valid_for", "max_rows", "fields", "reason"} {
		assert.NotContains(t, got, k, "clé %s", k)
	}
	assert.Equal(t, "ride_not_found", got["code"])
}

// ⚠️⚠️ ET IL N'Y A PAS DE `meta` SUR LE FIL — jamais, pour aucun refus.
//
// `apperr.Meta` sert aussi aux journaux et aux causes internes : le servir en
// bloc publierait ce que les modules y mettent pour eux-mêmes. Les clés du
// CONTRAT sont à plat, déclarées une par une dans `errorDetail`.
func TestTheWireNeverCarriesMeta(t *testing.T) {
	got := decodeRefus(t, apperr.Validation("nope").WithMeta(map[string]any{
		"feature": "rental", "cause": "un détail interne",
		"ride_id": "6ae2…", "fields": []string{"cars"},
	}))
	assert.NotContains(t, got, "meta")
	assert.NotContains(t, got, "cause", "un détail de journal est parti sur le fil")
	assert.NotContains(t, got, "ride_id", "un identifiant interne est parti sur le fil")
	assert.Equal(t, "rental", got["feature"], "la clé du contrat, elle, doit arriver")
}

// ⚠️⚠️ ET ON FERME LA CLASSE, PAS LE CAS : AUCUN CONTRAT NE PROMET DE `meta`.
//
// C'est une épreuve qui LIT LES SPECS, et c'est assumé. Le défaut qu'on
// interdit ici ne vit pas dans le code : il vit dans la PROSE d'un contrat qui
// promet une clé que le serveur ne sert pas. Rien d'autre ne relie les deux —
// et c'est précisément pour cela que c'est arrivé SEPT fois.
//
// Les specs disaient d'ailleurs les deux choses à la fois : « il n'y a pas de
// `meta` sur le fil » à un endroit, et `meta.feature` / `meta.valid_for` /
// `meta.max_rows` / `error.meta.reason` à cinq autres. Une équipe mobile lit
// celui qui l'arrange, et écrit du code qui lit `undefined`.
// sousAudit : les clés `meta.X` encore écrites dans les contrats, et pourquoi
// elles sont tolérées ICI et pas ailleurs.
//
// ⚠️⚠️ CE N'EST PAS UNE DISPENSE, C'EST UNE DETTE DATÉE. Aucun payload de la
// plateforme ne porte de `meta` : l'unique `json:"meta"` de tout le code est un
// DTO de paiement, étranger à ces passages. Les clés ci-dessous existent pour
// la plupart — mais **à plat**, sur la réponse ou sur l'appel, pas sous un
// `meta`. Les corriger demande de vérifier, site par site, SUR QUEL PAYLOAD
// chacune voyage : une réécriture en bloc remplacerait une phrase fausse par
// une autre. Voir #252 pour le tableau complet (27 clés, 65 passages).
//
// Ce qui compte : cette liste est FERMÉE. Une clé qui n'y est pas fait échouer
// l'épreuve — donc on ne peut plus EN INVENTER une nouvelle, et les deux que
// l'on vient de vérifier (`feature`, `reason`) ne peuvent plus revenir sous
// cette forme.
var sousAudit = map[string]string{
	"open_instead":       "jamais servie, et les contrats le disent déjà",
	"valid_for":          "jamais servie : la phrase traduite porte la verticale",
	"valid_in":           "jamais servie : la phrase traduite porte le pays",
	"min_amount_xof":     "à plat ailleurs ; à vérifier sur CE refus",
	"max_uses_per_user":  "à plat ailleurs ; à vérifier sur CE refus",
	"max_rows":           "jamais servie, et la phrase ne porte pas le nombre non plus",
	"cooldown_s":         "les contrats disent déjà qu'elle n'a jamais été servie",
	"allowed":            "les contrats disent déjà qu'elle n'a jamais été servie",
	"max_cars":           "la phrase interpole `{{.max_cars}}` — c'est là qu'elle est",
	"max_rides":          "à vérifier : refus de simultanéité",
	"ride_ids":           "à vérifier : refus de simultanéité",
	"active":             "à vérifier : refus de simultanéité",
	"max":                "à vérifier : trajets suivis",
	"cars":               "à vérifier : refus de solde",
	"fare_xof":           "servie À PLAT sur l'appel du chauffeur",
	"driver_xof":         "servie À PLAT sur l'appel du chauffeur",
	"token_cost":         "servie À PLAT sur la course",
	"cash_required_xof":  "servie À PLAT sur l'appel",
	"transfer":           "servie À PLAT sur l'appel",
	"rider_on_board":     "à vérifier sur l'appel",
	"meet_courier":       "à vérifier sur l'appel",
	"stops":              "servie À PLAT sur l'appel",
	"mode":               "servie À PLAT",
	"rider_rating_avg":   "à vérifier sur l'appel",
	"rider_rating_count": "à vérifier sur l'appel",
	"distance_m":         "servie À PLAT sur la course",
	"max_distance_m":     "à vérifier sur CE refus",
}

// ⚠️⚠️ ON FERME LA CLASSE, PAS LE CAS : AUCUN CONTRAT N'INVENTE DE `meta`.
//
// C'est une épreuve qui LIT LES SPECS, et c'est assumé. Le défaut qu'on
// interdit ici ne vit pas dans le code : il vit dans la PROSE d'un contrat qui
// promet une clé que le serveur ne sert pas. Rien d'autre ne relie les deux —
// et c'est précisément pour cela que c'est arrivé SEPT fois, dont quatre que
// les contrats racontent eux-mêmes (« ce champ n'a jamais été servi »).
//
// Les specs disaient d'ailleurs les deux choses à la fois : « il n'y a pas de
// `meta` sur le fil » à un endroit, et `meta.feature` à cinq autres. Une équipe
// mobile lit celui qui l'arrange, et écrit du code qui lit `undefined`.
// explicatif reconnaît une ligne qui EXPLIQUE l'absence d'un champ au lieu de
// le promettre.
//
// ⚠️ IL EN FAUT UN, et ce n'est pas une échappatoire : les contrats racontent
// eux-mêmes quatre de ces erreurs (« ce champ n'a jamais été servi »,
// « `error.reason` ET NON `error.meta` »). Interdire de NOMMER la mauvaise
// forme effacerait l'avertissement qui empêche de la réécrire.
func explicatif() *regexp.Regexp {
	return regexp.MustCompile(`pas de ` + "`?" + `meta|n'existe pas|n'a jamais été servi|` +
		`que l'enveloppe ne sert pas|ET NON|et non `)
}

func TestNoContractInventsANewMetaKey(t *testing.T) {
	explique := explicatif()
	promet := regexp.MustCompile("`?(?:error\\.)?meta\\.([a-z_]+)`?")

	dir := filepath.Join("..", "..", "docs", "frontend")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "les contrats sont introuvables : ajustez le chemin")

	seen := 0
	for _, e := range entries {
		// ⚠️ LES RAPPORTS `SERVEUR-*` SONT HORS JEU : ce sont des échanges
		// datés avec l'équipe, pas des contrats, et ils citent l'état du
		// serveur AU MOMENT où ils ont été écrits.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), "SERVEUR-") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		seen++
		for i, line := range strings.Split(string(raw), "\n") {
			if explique.MatchString(line) {
				continue
			}
			for _, m := range promet.FindAllStringSubmatch(line, -1) {
				_, toléré := sousAudit[m[1]]
				assert.True(t, toléré,
					"%s:%d promet `meta.%s`, et AUCUN payload de la plateforme ne "+
						"porte de `meta` : le détail actionnable d'un refus est à plat "+
						"sur `error` (`fields`, `reason`, `feature`), et les CHIFFRES "+
						"sont dans la phrase traduite.\n  %s",
					e.Name(), i+1, m[1], strings.TrimSpace(line))
			}
		}
	}
	require.Positive(t, seen, "aucun contrat lu : le filtre de noms est cassé")
}

// ⚠️ ET CHAQUE `error.X` QU'UN CONTRAT PROMET EXISTE DANS L'ENVELOPPE.
//
// L'autre moitié de la même tenaille : interdire `meta.X` ne suffit pas si on
// peut promettre `error.quelque_chose` qui n'existe pas davantage. Les deux
// épreuves ensemble font que la prose et la structure ne peuvent plus diverger.
func TestEveryPromisedErrorKeyExistsInTheEnvelope(t *testing.T) {
	served := map[string]bool{}
	rt := reflect.TypeOf(errorDetail{})
	for i := 0; i < rt.NumField(); i++ {
		tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			served[tag] = true
		}
	}
	require.Contains(t, served, "feature", "l'enveloppe a changé : relisez cette épreuve")

	promet := regexp.MustCompile("`error\\.([a-z_]+)`")
	dir := filepath.Join("..", "..", "docs", "frontend")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), "SERVEUR-") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		explique := explicatif()
		for i, line := range strings.Split(string(raw), "\n") {
			if explique.MatchString(line) {
				continue
			}
			for _, m := range promet.FindAllStringSubmatch(line, -1) {
				assert.True(t, served[m[1]],
					"%s:%d promet `error.%s`, que l'enveloppe ne sert pas :\n  %s",
					e.Name(), i+1, m[1], strings.TrimSpace(line))
			}
		}
	}
}
