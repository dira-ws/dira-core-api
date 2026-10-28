package appconfig

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconf "github.com/kgtech-org/dira-core-api/pkg/appconfig"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/edition"
)

type fakeStore struct {
	reg   *pkgconf.Registry
	err   error
	loads int
	saved *pkgconf.Registry
	actor string
}

func (f *fakeStore) Load(context.Context) (*pkgconf.Registry, error) {
	f.loads++
	if f.err != nil {
		// ⚠️ NIL *ET* L'ERREUR, comme le vrai dépôt. Une doublure qui rend le
		// registre ET l'erreur ferait passer au vert un service qui ignore
		// l'erreur : il lirait le registre quand même, et l'épreuve du repli
		// ne prouverait plus rien. (`Repository.Load` rend `nil, err`.)
		return nil, f.err
	}
	return f.reg, nil
}
func (f *fakeStore) Save(_ context.Context, reg pkgconf.Registry, actor string) error {
	f.saved, f.actor = &reg, actor
	return nil
}
func (f *fakeStore) Meta(context.Context) (*time.Time, string) { return nil, "" }

// requestFrom fabrique le contexte qu'un intergiciel aurait posé.
func requestFrom(code, ed string) context.Context {
	ctx := country.WithCountry(context.Background(), code, country.SourceHeader)
	return edition.WithEdition(ctx, ed, "", "")
}

// ⚠️⚠️ UNE LECTURE QUI ÉCHOUE OUVRE TOUT, ELLE NE COUPE RIEN.
//
// Si Mongo ne répond pas, les applications voient TOUT — c'est-à-dire se
// comportent comme avant cette fonctionnalité. L'autre sens aurait coupé des
// fonctions à cause d'une panne de base : une panne de base ne doit pas
// devenir une panne de produit, et celle-là se serait vue comme « Lite a perdu
// la moitié de ses menus » sans qu'aucune alerte ne la relie à Mongo.
func TestAFailedReadOpensEverything(t *testing.T) {
	svc := NewService(&fakeStore{err: errors.New("mongo muet")})
	ctx := requestFrom("TD", "lite")
	for _, key := range pkgconf.FeatureKeys() {
		assert.True(t, svc.Allows(ctx, key), "%s devrait rester ouverte", key)
	}
	assert.NoError(t, svc.Require(ctx, pkgconf.FeatureScheduledRides))
	// Et une liste n'est pas vidée non plus.
	served := []string{"cash", "wallet"}
	assert.Equal(t, served, svc.AllowedIn(ctx, pkgconf.ListPaymentMethods, served))
}

// ⚠️ ET LE DERNIER REGISTRE CONNU VAUT MIEUX QUE RIEN : une panne passagère de
// Mongo ne doit pas rouvrir des fonctions qu'on venait de couper.
func TestAFailedReadKeepsTheLastKnownRegistry(t *testing.T) {
	store := &fakeStore{reg: &pkgconf.Registry{Features: map[string]pkgconf.Rule{
		pkgconf.FeatureRaffle: {Editions: []string{"full"}},
	}}}
	svc := NewService(store)
	ctx := requestFrom("TD", "lite")
	assert.False(t, svc.Allows(ctx, pkgconf.FeatureRaffle))

	// Mongo tombe, et le cache expire.
	store.err = errors.New("mongo muet")
	svc.mu.Lock()
	svc.at = time.Now().Add(-time.Hour)
	svc.mu.Unlock()
	assert.False(t, svc.Allows(ctx, pkgconf.FeatureRaffle),
		"la tombola s'est rouverte à cause d'une panne de base")
}

// ⚠️⚠️ LE CACHE EST COURT, ET VIDÉ À L'ÉCRITURE.
//
// Un opérateur qui vient de couper une fonction et ne la voit pas disparaître
// la coupe une seconde fois, puis appelle pour dire que l'écran ne marche pas.
// Quinze secondes de cache sont un arbitrage — mille clients ne doivent pas
// faire mille lectures par minute — mais une écriture doit prendre effet tout
// de suite.
func TestSavingClearsTheCacheAtOnce(t *testing.T) {
	store := &fakeStore{reg: &pkgconf.Registry{}}
	svc := NewService(store)
	ctx := requestFrom("TD", "lite")
	assert.True(t, svc.Allows(ctx, pkgconf.FeatureRaffle))
	assert.Equal(t, 1, store.loads)

	// Le cache sert la deuxième lecture : pas de second appel à la base.
	assert.True(t, svc.Allows(ctx, pkgconf.FeatureRaffle))
	assert.Equal(t, 1, store.loads, "le cache ne sert à rien")

	// Après une écriture, la base est relue.
	store.reg = &pkgconf.Registry{Features: map[string]pkgconf.Rule{
		pkgconf.FeatureRaffle: {Editions: []string{"full"}},
	}}
	require.NoError(t, svc.Save(ctx, *store.reg, "op-1"))
	assert.False(t, svc.Allows(ctx, pkgconf.FeatureRaffle),
		"la coupure ne prend pas effet : l'opérateur va la refaire")
	assert.Equal(t, 2, store.loads)
}

// ⚠️⚠️ UNE CLÉ INCONNUE EST REFUSÉE À L'ÉCRITURE, et c'est le seul moment où
// on peut le faire. Sans ce contrôle, `scheduled_ride` au singulier
// s'enregistrait, personne ne le lisait, la fonction restait visible dans Lite
// — et on cherchait la panne dans l'application.
func TestAMistypedKeyIsRefusedWhenItIsSaved(t *testing.T) {
	svc := NewService(&fakeStore{reg: &pkgconf.Registry{}})
	ctx := requestFrom("TD", "full")

	err := svc.Save(ctx, pkgconf.Registry{Features: map[string]pkgconf.Rule{
		"scheduled_ride": {Editions: []string{"full"}},
	}}, "op-1")
	require.Error(t, err, "le singulier a été accepté")
	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "scheduled_ride", appErr.Meta["feature"],
		"le refus doit NOMMER la clé fautive : sinon l'opérateur relit toute la grille")

	// Une LISTE inconnue aussi.
	err = svc.Save(ctx, pkgconf.Registry{ListRules: map[string]pkgconf.Rule{
		"vehicle_mode": {Allow: []string{"moto"}},
	}}, "op-1")
	require.Error(t, err)

	// Et une fonction rangée comme liste n'est pas une liste.
	err = svc.Save(ctx, pkgconf.Registry{ListRules: map[string]pkgconf.Rule{
		pkgconf.FeatureRental: {Allow: []string{"x"}},
	}}, "op-1")
	require.Error(t, err)
}

// ⚠️⚠️ UNE LISTE BLANCHE POSÉE SUR UNE FONCTION NE VEUT RIEN DIRE, et
// l'accepter en silence est le pire des trois cas.
//
// Une fonction est allumée ou éteinte ; elle n'a pas de contenu à restreindre.
// Un opérateur qui pose `allow: ["moto"]` sur « Location » croit avoir réduit
// quelque chose : la règle s'enregistre, ne fait rien, et il repart persuadé
// que c'est réglé. Le refus lui dit où mettre sa liste.
func TestAWhitelistOnAFeatureIsRefused(t *testing.T) {
	svc := NewService(&fakeStore{reg: &pkgconf.Registry{}})
	ctx := requestFrom("TD", "full")
	err := svc.Save(ctx, pkgconf.Registry{Features: map[string]pkgconf.Rule{
		pkgconf.FeatureRental: {Allow: []string{"moto", "eco"}},
	}}, "op-1")
	require.Error(t, err, "une liste blanche sur une fonction a été acceptée sans rien faire")
	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Contains(t, appErr.Meta["fields"], "allow",
		"le refus doit montrer OÙ est le problème")

	// La même liste blanche sur une LISTE passe, elle.
	require.NoError(t, svc.Save(ctx, pkgconf.Registry{ListRules: map[string]pkgconf.Rule{
		pkgconf.ListVehicleModes: {Allow: []string{"moto", "eco"}},
	}}, "op-1"))
}

// ⚠️ UNE ÉDITION INVENTÉE EST REFUSÉE À L'ÉCRITURE, là où un APPEL la pardonne.
//
// Même distinction que `Known` / `Normalize` : sur le chemin d'un appel, une
// faute de frappe ne doit rien casser ; dans un réglage, elle produit une règle
// qui ne s'applique à rien et que personne ne pourra expliquer.
func TestAnInventedEditionIsRefusedWhenItIsSaved(t *testing.T) {
	svc := NewService(&fakeStore{reg: &pkgconf.Registry{}})
	ctx := requestFrom("TD", "full")
	for _, bad := range []string{"léger", "lite2", "LIGHT", ""} {
		err := svc.Save(ctx, pkgconf.Registry{Features: map[string]pkgconf.Rule{
			pkgconf.FeatureRental: {Editions: []string{bad}},
		}}, "op-1")
		require.Error(t, err, "« %s » a été accepté comme édition", bad)
	}
	// Et les bonnes passent, normalisées.
	require.NoError(t, svc.Save(ctx, pkgconf.Registry{Features: map[string]pkgconf.Rule{
		pkgconf.FeatureRental: {Editions: []string{" FULL "}},
	}}, "op-1"))
	require.NotNil(t, svc.repo.(*fakeStore).saved)
	assert.Equal(t, []string{"full"},
		svc.repo.(*fakeStore).saved.Features[pkgconf.FeatureRental].Editions)
}

// ⚠️ UN PAYS MAL ÉCRIT EST REFUSÉ : « Tchad » n'est pas un code pays, et une
// règle posée dessus ne s'appliquerait jamais.
func TestABadCountryCodeIsRefused(t *testing.T) {
	svc := NewService(&fakeStore{reg: &pkgconf.Registry{}})
	ctx := requestFrom("TD", "full")
	for _, bad := range []string{"Tchad", "T", "TCD3", ""} {
		err := svc.Save(ctx, pkgconf.Registry{Features: map[string]pkgconf.Rule{
			pkgconf.FeatureRental: {Countries: []string{bad}},
		}}, "op-1")
		require.Error(t, err, "« %s » a été accepté comme pays", bad)
	}
	require.NoError(t, svc.Save(ctx, pkgconf.Registry{Features: map[string]pkgconf.Rule{
		pkgconf.FeatureRental: {Countries: []string{" td "}},
	}}, "op-1"))
	assert.Equal(t, []string{"TD"},
		svc.repo.(*fakeStore).saved.Features[pkgconf.FeatureRental].Countries)
}

// ⚠️⚠️ LE REFUS NOMME LA FONCTION, pas seulement « interdit ».
//
// « 403 » tout court se lit comme un problème de droits : l'application
// proposerait de se reconnecter, et la personne appellerait le support pour un
// compte qui n'a rien. `meta.feature` dit laquelle, pour que l'écran la cache
// au prochain démarrage plutôt que de reproposer un bouton qui échoue.
func TestTheRefusalNamesTheFeature(t *testing.T) {
	svc := NewService(&fakeStore{reg: &pkgconf.Registry{Features: map[string]pkgconf.Rule{
		pkgconf.FeatureScheduledRides: {Editions: []string{"full"}},
	}}})
	err := svc.Require(requestFrom("TD", "lite"), pkgconf.FeatureScheduledRides)
	require.Error(t, err)
	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "feature_unavailable_in_edition", appErr.Code)
	assert.Equal(t, 403, appErr.HTTPStatus)
	assert.Equal(t, pkgconf.FeatureScheduledRides, appErr.Meta["feature"])
	// ⚠️ Et la phrase ne parle NI d'édition NI de pays : la personne n'a qu'une
	// application, elle ne sait pas qu'il en existe deux, et elle ne peut rien
	// faire de cette information.
	assert.NotContains(t, appErr.Message, "lite")
	assert.NotContains(t, appErr.Message, "edition")
}

// Le pays et l'édition viennent du CONTEXTE, celui que l'intergiciel a posé.
func TestTheCountryAndEditionComeFromTheRequest(t *testing.T) {
	svc := NewService(&fakeStore{reg: &pkgconf.Registry{Features: map[string]pkgconf.Rule{
		pkgconf.FeatureTripShare: {Editions: []string{"full"}, Countries: []string{"TG"}},
	}}})
	assert.True(t, svc.Allows(requestFrom("TG", "full"), pkgconf.FeatureTripShare))
	assert.False(t, svc.Allows(requestFrom("TG", "lite"), pkgconf.FeatureTripShare))
	assert.False(t, svc.Allows(requestFrom("TD", "full"), pkgconf.FeatureTripShare))

	got := svc.Resolve(requestFrom("tg", "lite"))
	assert.Equal(t, "TG", got.Country)
	assert.Equal(t, "lite", got.Edition)
	assert.False(t, got.Features[pkgconf.FeatureTripShare])
}
