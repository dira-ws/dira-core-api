package diramaps

// LA CLÉ DU SIG, ET CE QU'ELLE N'EST PAS (dira-maps#67).
//
// ⚠️⚠️ CE FICHIER EXISTE POUR QU'UN OUBLI NE PUISSE PLUS DURER DES MOIS. Le
// SIG identifie ses appelants par clé, avec quota et décompte ; le détail de
// l'exploitation est suivi dans son dépôt privé (dira-maps#67).
//
// Ce qui se joue ici : un client qui ne porte pas sa clé ne provoque AUCUNE
// panne visible, parce que le SIG est AU MIEUX — les distances retombent à vol
// d'oiseau et rien ne se lève. Une note en commentaire ne suffit donc pas : il
// y en avait déjà une. Ce qu'il faut, c'est une épreuve qui regarde ce que le
// serveur REÇOIT.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sigQuiNote rend un SIG qui enregistre les en-têtes reçus, et répond le code
// demandé.
func sigQuiNote(t *testing.T, code int) (*httptest.Server, *http.Header) {
	t.Helper()
	var vu http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vu = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"routes":[{"duration_s":600,
		  "geometry":{"type":"LineString","coordinates":[[1.22,6.13],[1.25,6.16]]}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &vu
}

func TestTheKeyTravelsOnEveryRoute(t *testing.T) {
	srv, vu := sigQuiNote(t, http.StatusOK)

	_, err := New(srv.URL, "cle-du-socle").Route(context.Background(), "lome", tournée)
	require.NoError(t, err)
	assert.Equal(t, "cle-du-socle", vu.Get(HeaderAPIKey),
		"sans cet en-tête, le SIG ne peut ni identifier, ni compter, ni borner l'appelant")
}

// ⚠️⚠️ UNE CLÉ VIDE N'ENVOIE PAS UN EN-TÊTE VIDE. Côté SIG, l'ABSENCE d'en-tête
// et un en-tête VIDE ne suivent pas le même chemin : le second est traité comme
// une clé inconnue et refusé en `401` quelle que soit la politique en vigueur.
// Un service dont la clé n'est pas encore configurée serait donc rangé parmi
// les clés révoquées — et la transition serait cassée par le client même qui la
// prépare.
func TestAnEmptyKeySendsNoHeaderAtAll(t *testing.T) {
	for _, cle := range []string{"", "   ", "\t\n"} {
		srv, vu := sigQuiNote(t, http.StatusOK)

		_, err := New(srv.URL, cle).Route(context.Background(), "lome", tournée)
		require.NoError(t, err, "clé %q", cle)

		_, présent := (*vu)[http.CanonicalHeaderKey(HeaderAPIKey)]
		assert.False(t, présent,
			"clé %q : l'en-tête ne doit pas exister, pas même vide", cle)
	}
}

// ⚠️ UN REFUS DE CLÉ SE DIT. « status 401 » dans un journal n'apprend rien à qui
// n'a pas le code sous les yeux — et c'est le jour où le SIG fermera sa porte
// qu'il faudra comprendre vite. C'est la sixième fois sur cette plateforme qu'un
// refus illisible coûte du temps.
func TestTheSIGRefusalNamesTheKeyAndNotAStatusCode(t *testing.T) {
	for _, c := range []struct {
		code   int
		attend string
	}{
		{http.StatusUnauthorized, "clé API refusée"},
		{http.StatusForbidden, "itineraire"},
	} {
		srv, _ := sigQuiNote(t, c.code)

		_, err := New(srv.URL, "cle").Route(context.Background(), "lome", tournée)
		require.Error(t, err, "code %d", c.code)
		assert.Contains(t, err.Error(), c.attend,
			"code %d : le message doit nommer la cause, pas le code", c.code)
		assert.NotContains(t, err.Error(), "status 401")
		assert.NotContains(t, err.Error(), "status 403")
	}
}

// Et la clé ne change RIEN au contrat existant : un SIG absent reste silencieux,
// et la durée arrive comme avant.
func TestTheKeyChangesNothingElse(t *testing.T) {
	r, err := New("", "cle").Route(context.Background(), "lome", tournée)
	require.NoError(t, err)
	assert.Zero(t, r.DurationS, "un SIG absent reste inerte, clé ou pas")

	srv, _ := sigQuiNote(t, http.StatusOK)
	r, err = New(srv.URL, "cle").Route(context.Background(), "lome", tournée)
	require.NoError(t, err)
	assert.Equal(t, 600, r.DurationS)
}
