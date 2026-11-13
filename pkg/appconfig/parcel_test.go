package appconfig

// LE COLIS S'ALLUME PAYS PAR PAYS, ET IL VERROUILLE.

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ UN PAYS FERMÉ NE VEND RIEN — et « fermé » est le défaut.
//
// Un registre nil, vide, un pays absent de la liste ou vide : le colis est
// fermé. Seul un pays NOMMÉ l'ouvre, et une règle de catalogue ne fait ensuite
// que le retirer d'une édition. `/app/config` dit la même chose que le verrou :
// les deux sortent de `ParcelOpen`.
func TestAClosedCountrySellsNothing(t *testing.T) {
	for _, r := range []*Registry{nil, {}, {Parcel: []string{}}} {
		for _, cc := range []string{"TG", "GA", "TD", ""} {
			for _, ed := range []string{"full", "lite"} {
				assert.False(t, r.ParcelOpen(cc, ed), "%v %s %s : rien n'est ouvert, rien ne se vend", r, cc, ed)
				assert.False(t, r.Resolve(cc, ed).Features[FeatureParcel], "%s %s : `/app/config` dit le verrou", cc, ed)
			}
		}
	}

	open := &Registry{Parcel: []string{"TG"}}
	assert.True(t, open.ParcelOpen("TG", "full"))
	assert.True(t, open.ParcelOpen(" tg ", "lite"), "la casse et les espaces ne décident pas")
	assert.True(t, open.Resolve("TG", "full").Features[FeatureParcel])
	for _, cc := range []string{"GA", "TD", "SN", ""} {
		assert.False(t, open.ParcelOpen(cc, "full"), "%s n'est pas nommé : fermé", cc)
		assert.False(t, open.Resolve(cc, "full").Features[FeatureParcel], "%s", cc)
	}

	// La règle de catalogue RETIRE d'une édition, elle n'ouvre jamais.
	sansLite := &Registry{Parcel: []string{"TG"}, Features: map[string]Rule{FeatureParcel: {Editions: []string{"lite"}}}}
	assert.True(t, sansLite.ParcelOpen("TG", "full"))
	assert.False(t, sansLite.ParcelOpen("TG", "lite"), "retiré de Lite")
	assert.False(t, sansLite.Resolve("TG", "lite").Features[FeatureParcel])
	nulleAilleurs := &Registry{Features: map[string]Rule{FeatureParcel: {Editions: []string{"lite"}, Countries: []string{"GA"}}}}
	assert.False(t, nulleAilleurs.ParcelOpen("TG", "full"), "une règle qui ne vise pas un pays ne l'ouvre pas")
}

// ⚠️⚠️ UN SOCLE MUET, SANS REGISTRE DÉJÀ LU, GARDE LE COLIS FERMÉ.
//
// C'est l'inverse de tout le reste de ce pont, où un socle muet laisse tout
// passer : un pays ne doit pas se mettre à vendre des envois parce qu'un appel
// HTTP a échoué. Mais le DERNIER REGISTRE LU passe avant ce défaut — une panne
// passagère ne ferme pas non plus ce que l'exploitation a ouvert.
func TestAnUnknownRegistryKeepsParcelClosed(t *testing.T) {
	// Personne n'écoute, rien n'a jamais été lu.
	assert.False(t, NewClient("http://127.0.0.1:1", "jeton").ParcelOpen(fullIn("TG")),
		"un socle injoignable a ouvert le colis")
	assert.False(t, NewClient("", "").ParcelOpen(fullIn("TG")), "un socle non configuré a ouvert le colis")

	// Un socle qui répond en erreur dès le départ : rien n'est connu, fermé.
	enPanne, _ := serveurDeRegistre(t, `{"parcel":["TG"]}`, http.StatusInternalServerError)
	assert.False(t, enPanne.ParcelOpen(fullIn("TG")), "un corps lisible derrière un `500` a ouvert le colis")

	// Un registre lu une fois : il fait foi, même quand le socle se tait ensuite.
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"parcel":["TG"]}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "jeton")
	require.True(t, c.ParcelOpen(fullIn("TG")), "le pays ouvert par l'exploitation")
	assert.False(t, c.ParcelOpen(fullIn("GA")), "un pays que le registre ne nomme pas")

	down.Store(true)
	c.mu.Lock()
	c.readAt = time.Now().Add(-2 * clientTTL)
	c.mu.Unlock()
	assert.True(t, c.ParcelOpen(fullIn("TG")), "une panne passagère a fermé ce que l'exploitation avait ouvert")
	assert.False(t, c.ParcelOpen(fullIn("GA")))
}

// La liste des pays se normalise : majuscules, sans doublon, triée — et un
// code qui n'a pas deux lettres est rendu pour être refusé.
func TestTheParcelCountriesAreCleaned(t *testing.T) {
	got, bad := CleanCountries([]string{" tg", "GA", "TG", "ga "})
	assert.Empty(t, bad)
	assert.Equal(t, []string{"GA", "TG"}, got)

	_, bad = CleanCountries([]string{"TG", "Togo"})
	assert.Equal(t, "Togo", bad)
	_, bad = CleanCountries([]string{"T1"})
	assert.Equal(t, "T1", bad)

	got, bad = CleanCountries(nil)
	assert.Nil(t, got, "rien d'ouvert ne range pas une liste vide")
	assert.Empty(t, bad)
}
