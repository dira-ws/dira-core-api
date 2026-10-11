package sos

// LA MESURE DES FAUSSES ALERTES — et les règles qui ne se voient pas.
//
// ⚠️ CE QU'ON MESURE ICI EST L'ABSENCE : un seuil trop HAUT ne déclenche jamais
// et ne laisse AUCUNE trace. Pas d'alerte, pas de faux, pas de plainte — juste
// une détection éteinte que tout le monde croit allumée. Les règles ci-dessous
// existent pour que la mesure, elle, reste lisible.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func norm(t *testing.T, it DetectionItem) (Detection, bool) {
	t.Helper()
	return normaliseDetection(it, primitive.NewObjectID(), "TG", "driver", testNow())
}

func testNow() time.Time {
	return time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
}

// ⚠️⚠️ LE BOUTON N'EST PAS UNE DÉTECTION : il n'a rien PROPOSÉ, il a été
// pressé. Compter ses « annulations » fausserait tous les taux — et c'est le
// taux d'annulation qui sert à régler les seuils.
func TestAPressedButtonIsNotAMeasurement(t *testing.T) {
	_, ok := norm(t, DetectionItem{Source: SourceButton, Outcome: OutcomeConfirmed})
	assert.False(t, ok)
	// Vide devient `button` (voir SanitiseSource) : donc jeté aussi.
	_, ok = norm(t, DetectionItem{Source: "   ", Outcome: OutcomeCancelled})
	assert.False(t, ok)
}

// ⚠️ UNE LIGNE INUTILISABLE EST JETÉE, PAS REFUSÉE. C'est une mesure : la
// perdre coûte une statistique, refuser l'envoi coûterait les 49 autres.
func TestAnInventedOutcomeIsDroppedNotRefused(t *testing.T) {
	_, ok := norm(t, DetectionItem{Source: SourceNoise, Outcome: "peut-etre"})
	assert.False(t, ok)
	_, ok = norm(t, DetectionItem{Source: SourceNoise, Outcome: ""})
	assert.False(t, ok)

	d, ok := norm(t, DetectionItem{Source: SourceNoise, Outcome: "CANCELLED"})
	require.True(t, ok, "la casse ne doit rien changer")
	assert.Equal(t, OutcomeCancelled, d.Outcome)
}

// ⚠️ UNE SOURCE INCONNUE EST GARDÉE, comme partout ailleurs dans ce module :
// `noise` était un mot inconnu de ce serveur hier.
func TestAnUnknownSourceIsStillMeasured(t *testing.T) {
	d, ok := norm(t, DetectionItem{Source: "Cri_Prolongé", Outcome: OutcomeExpired})
	require.True(t, ok)
	assert.Equal(t, "cri_prolong", d.Source, "gardée, seulement mise en forme")
}

// ⚠️⚠️ UNE DATE DANS L'AVENIR EST RAMENÉE À MAINTENANT. Un téléphone mal réglé
// ne doit ni se ranger en tête d'un classement par date, ni — bien pire —
// survivre à la purge pour toujours : `expires_at` se calcule depuis `at`.
func TestAPhoneWithAWrongClockCannotOutliveTheRetention(t *testing.T) {
	d, ok := norm(t, DetectionItem{
		Source: SourceShake, Outcome: OutcomeCancelled,
		At: "2099-01-01T00:00:00Z",
	})
	require.True(t, ok)
	assert.Equal(t, testNow(), d.At)
	assert.Equal(t, testNow().Add(DetectionRetention), d.ExpiresAt)

	// Une date passée plausible est gardée telle quelle.
	d, ok = norm(t, DetectionItem{
		Source: SourceShake, Outcome: OutcomeCancelled,
		At: "2026-10-11T09:12:03Z",
	})
	require.True(t, ok)
	assert.Equal(t, 2026, d.At.Year())
	assert.Equal(t, d.At.Add(DetectionRetention), d.ExpiresAt)

	// Une date illisible ne jette pas la mesure : on prend maintenant.
	d, ok = norm(t, DetectionItem{Source: SourceShake, Outcome: OutcomeCancelled, At: "hier"})
	require.True(t, ok)
	assert.Equal(t, testNow(), d.At)
}

// ⚠️ LE MODÈLE, JAMAIS L'IDENTIFIANT — et borné. Il sert à comprendre pourquoi
// un seuil de bruit se comporte autrement sur un téléphone bas de gamme, pas à
// suivre quelqu'un.
func TestTheDeviceModelIsBoundedAndNeverAnIdentifier(t *testing.T) {
	d, ok := norm(t, DetectionItem{
		Source: SourceNoise, Outcome: OutcomeCancelled,
		DeviceModel: strings.Repeat("X", 200),
	})
	require.True(t, ok)
	assert.Len(t, d.DeviceModel, 40)
}

// ⚠️ NI SON, NI POSITION, NI IDENTIFIANT DE COURSE : cette route mesure des
// SEUILS, pas des gens. Ce test lit la source, parce qu'un champ en trop ne se
// voit qu'une fois qu'il s'est rempli — puis gardé, puis servi à autre chose.
func TestTheStoredShapeCarriesNothingAboutThePerson(t *testing.T) {
	src := mustReadFile(t, "detections.go")
	start := strings.Index(src, "type Detection struct {")
	require.Positive(t, start)
	shape := src[start : start+strings.Index(src[start:], "\n}")]
	for _, forbidden := range []string{"Lat", "Lng", "Audio", "RideID", "DeliveryID", "Phone"} {
		assert.NotContains(t, shape, forbidden+" ",
			"`%s` n'a rien à faire dans une mesure de seuil", forbidden)
	}
}

// ⚠️⚠️ CETTE ROUTE NE PASSE PAS PAR `httpx.Decode`, ET C'EST LA SEULE DU DÉPÔT.
//
// `httpx.Decode` REFUSE une clé inconnue — bonne règle presque partout. Ici
// elle se retourne contre nous : ces comptes rendus partent en arrière-plan,
// SANS réessai, et personne ne lit la réponse. Le jour où une application
// ajoute un champ, tout serveur plus ancien jetterait LE LOT ENTIER en
// répondant `204` — donc sans que rien ne le signale. On perdrait précisément
// la mesure qui dit qu'un seuil ne déclenche plus.
//
// Ce test lit la source, parce que le défaut est invisible : la route répond
// `204` dans les deux cas.
func TestTheDetectionRouteToleratesAnUnknownField(t *testing.T) {
	src := mustReadFile(t, "handler.go")
	start := strings.Index(src, "func (h *Handler) detections(")
	require.Positive(t, start)
	body := src[start : start+strings.Index(src[start:], "\n}")]
	// ⚠️ ON NE LIT QUE LES LIGNES DE CODE : le commentaire a le droit de citer
	// `httpx.Decode` pour expliquer pourquoi il n'est pas appelé. (Le piège
	// s'est refermé une fois sur ce test même.)
	var code []string
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); !strings.HasPrefix(t, "//") {
			code = append(code, line)
		}
	}
	joined := strings.Join(code, "\n")
	assert.NotContains(t, joined, "httpx.Decode",
		"un champ en trop doit être IGNORÉ, jamais faire tomber le lot entier")
	assert.Contains(t, joined, "json.NewDecoder")
}

// Les bornes du contrat, figées.
func TestTheMeasurementLimitsAreTheOnesWeAnnounced(t *testing.T) {
	assert.Equal(t, 50, DetectionBatchMax)
	assert.Equal(t, 200, DetectionDailyCap)
	assert.Equal(t, 90*24*time.Hour, DetectionRetention)
}

// ⚠️ PAS DE TAUX SANS PROPOSITION. « 0 % d'annulation » se lit « la détection
// est parfaitement réglée » — l'inverse exact de « elle n'a jamais
// déclenché », qui est la panne silencieuse qu'on cherche.
func TestNoProposalMeansNoRateAtAll(t *testing.T) {
	var s DetectionStat
	assert.Nil(t, s.CancelRate)
}

// Les modèles les plus bavards, les cinq premiers, à égalité par ordre stable.
func TestTheTalkativeDevicesComeFirst(t *testing.T) {
	got := topModels([]string{"A", "B", "A", "C", "A", "B", "", ""})
	require.Len(t, got, 3)
	assert.Equal(t, DeviceCount{Model: "A", N: 3}, got[0])
	assert.Equal(t, DeviceCount{Model: "B", N: 2}, got[1])
	assert.Empty(t, topModels([]string{"", ""}), "un modèle vide n'est pas un modèle")
}

func mustReadFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	require.NoError(t, err)
	return string(b)
}
