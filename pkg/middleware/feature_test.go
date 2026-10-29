package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// passé répond `204` : un code qu'aucun refus ne produit, donc le voir prouve
// que la requête est ALLÉE AU BOUT.
//
// ⚠️ ET C'EST LE POINT DE CE FICHIER. Une épreuve qui se contenterait de
// « ce n'est pas un 403 » accepterait une PANIQUE comme un succès — et c'est
// exactement ce qui est arrivé : la mutation qui retirait le garde de la porte
// nulle passait au vert, parce que `gate.Require` sur une interface nulle
// panique, que le filet du routeur changeait ça en `500`, et que `500 != 403`.
var passé = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
})

func joue(t *testing.T, gate FeatureGate, feature string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	RequireFeature(gate, feature)(passé).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	var body struct {
		Error map[string]any `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body.Error
}

type porteQuiRefuse struct{ feature string }

func (p porteQuiRefuse) Require(_ context.Context, feature string) error {
	if feature != p.feature {
		return nil
	}
	return apperr.Forbidden("feature_unavailable_in_edition", "nope").
		WithMeta(map[string]any{"feature": feature})
}

type porteOuverte struct{}

func (porteOuverte) Require(context.Context, string) error { return nil }

// ⚠️⚠️ UNE PORTE NULLE LAISSE PASSER, ET NE PANIQUE PAS.
//
// Un service monté sans registre — un développement sans socle, une verticale
// dont le pont n'est pas configuré — ne doit pas refuser TOUT : le défaut de
// cette couche est « disponible », partout et toujours. Et il ne doit surtout
// pas PANIQUER : une interface nulle qu'on déréférence fait tomber la requête
// en `500`, ce qui ressemble à une panne du service plutôt qu'à un câblage
// absent.
func TestANilGateLetsEverythingThrough(t *testing.T) {
	code, _ := joue(t, nil, "wallet")
	assert.Equal(t, http.StatusNoContent, code,
		"une porte nulle refuse, ou panique : un service sans registre servirait une plateforme amputée")
}

// Une porte ouverte laisse passer aussi.
func TestAnOpenGateLetsEverythingThrough(t *testing.T) {
	code, _ := joue(t, porteOuverte{}, "wallet")
	assert.Equal(t, http.StatusNoContent, code)
}

// ⚠️ ET LE REFUS REMONTE TEL QUEL : son code est ce que les applications
// lisent pour cacher un bouton, et `feature` dit lequel. L'aplatir ferait
// proposer une reconnexion pour un compte qui n'a rien.
func TestTheRefusalKeepsItsCodeAndItsFeature(t *testing.T) {
	code, errBody := joue(t, porteQuiRefuse{feature: "wallet"}, "wallet")
	require.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "feature_unavailable_in_edition", errBody["code"])
	assert.Equal(t, "wallet", errBody["feature"],
		"le refus ne dit pas quoi cacher : l'application reproposera le bouton")
}

// Et la porte n'est interrogée que sur LA fonction qu'on lui nomme.
func TestOnlyTheNamedFeatureIsAsked(t *testing.T) {
	code, _ := joue(t, porteQuiRefuse{feature: "wallet"}, "challenges")
	assert.Equal(t, http.StatusNoContent, code,
		"une route a été refusée pour une fonction qui n'est pas la sienne")
}

// ⚠️ UN REFUS SANS CODE NE DOIT PAS DEVENIR UN `403` MUET : il remonte comme
// la panne qu'il est, plutôt que de se faire passer pour une fonction coupée.
func TestAPlainErrorIsNotDisguisedAsACutFeature(t *testing.T) {
	code, errBody := joue(t, porteCassée{}, "wallet")
	assert.GreaterOrEqual(t, code, 500)
	assert.NotEqual(t, "feature_unavailable_in_edition", errBody["code"])
}

type porteCassée struct{}

func (porteCassée) Require(context.Context, string) error { return errors.New("registre illisible") }
