package appconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ UN REGISTRE VIDE SE COMPORTE COMME LA PLATEFORME D'AVANT.
//
// C'est la propriété qui rend cette couche sûre à ajouter : rien ne change
// pour personne tant que quelqu'un n'a pas explicitement restreint. Sans elle,
// il aurait fallu réviser chaque pays avant de déployer — et le premier oublié
// aurait perdu des fonctions sans qu'aucune erreur ne le dise.
func TestAnEmptyRegistryTakesNothingAway(t *testing.T) {
	for _, r := range []*Registry{nil, {}, {Features: map[string]Rule{}}} {
		for _, key := range FeatureKeys() {
			assert.True(t, r.Allows(key, "TD", "lite"), "%s dans Lite", key)
			assert.True(t, r.Allows(key, "TG", "full"), "%s dans l'app normale", key)
		}
		// Une clé qui n'est pas au catalogue est permise elle aussi : refuser
		// ferait d'une faute de frappe dans un APPELANT une fonction coupée.
		assert.True(t, r.Allows("jamais_vue", "TD", "lite"))
	}
}

// ⚠️⚠️ RETIRER DE LITE NE RETIRE PAS DE L'APPLICATION NORMALE — c'est toute la
// demande, et la polarité de la règle le garantit : elle nomme QUI PERD.
func TestTakingAFeatureFromLiteLeavesTheFullAppAlone(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureScheduledRides: {Editions: []string{"lite"}},
		FeatureRental:         {Editions: []string{"lite"}},
	}}
	// Lite ne les voit plus…
	assert.False(t, r.Allows(FeatureScheduledRides, "TD", "lite"))
	assert.False(t, r.Allows(FeatureRental, "TD", "lite"))
	// …et l'application normale, si — dans tous les pays.
	for _, c := range []string{"TD", "TG", "SN", "GA"} {
		assert.True(t, r.Allows(FeatureScheduledRides, c, "full"), "pays %s", c)
	}
	// Et les autres fonctions ne bougent pas.
	assert.True(t, r.Allows(FeatureTripShare, "TD", "lite"))
}

// ⚠️⚠️ NOMMER UN PAYS RÉTRÉCIT UNE COUPURE, IL N'EN CRÉE JAMAIS UNE AILLEURS.
//
// C'est l'épreuve de #245, et elle existe parce que le contraire est arrivé en
// vrai. La règle disait auparavant ce qui était PERMIS — « disponible dans ces
// éditions ET dans ces pays » — si bien que la seule écriture naturelle pour
// « pas de location dans Lite, au Gabon » :
//
//	{Editions: ["full"], Countries: ["GA"]}
//
// signifiait « la location n'existe QUE pour l'édition normale, et QUE au
// Gabon ». Mesuré sur la préprod juste après l'écriture : l'application
// NORMALE avait perdu la location au Togo, au Sénégal et en Guinée. Trois pays
// réels, par un réglage qui se lisait comme gabonais, et sans aucune erreur.
func TestNamingACountryOnlyNarrowsTheCut(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureRental: {Editions: []string{"lite"}, Countries: []string{"GA"}},
	}}
	// La coupure mord là où on l'a posée, et là seulement.
	assert.False(t, r.Allows(FeatureRental, "GA", "lite"), "la coupure demandée")

	// ⚠️ L'APPLICATION NORMALE NE PERD RIEN, NULLE PART — le défaut exact.
	for _, c := range []string{"GA", "TG", "SN", "GN", "TD"} {
		assert.True(t, r.Allows(FeatureRental, c, "full"),
			"l'app normale a perdu la location au %s : c'est le défaut de #245", c)
	}
	// Et Lite la garde partout ailleurs qu'au Gabon.
	for _, c := range []string{"TG", "SN", "GN", "TD"} {
		assert.True(t, r.Allows(FeatureRental, c, "lite"),
			"la coupure gabonaise a débordé sur Lite au %s", c)
	}
}

// Sans pays nommé, la coupure vaut partout — c'est la portée par défaut.
func TestWithoutACountryTheCutAppliesEverywhere(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureTripShare: {Editions: []string{"lite"}},
	}}
	for _, c := range []string{"TD", "TG", "SN", "GA"} {
		assert.False(t, r.Allows(FeatureTripShare, c, "lite"), "pays %s", c)
		assert.True(t, r.Allows(FeatureTripShare, c, "full"), "pays %s", c)
	}
}

// ⚠️⚠️ UNE RÈGLE SANS ÉDITION NOMMÉE NE RETIRE RIEN, et ce n'est pas un détail
// de confort : la `Rule` NULLE est ce qu'on obtient pour une clé ABSENTE de la
// carte. Si une liste d'éditions vide voulait dire « toutes », chaque fonction
// du catalogue serait coupée partout dès qu'une seule autre est réglée.
func TestARuleThatNamesNoEditionTakesNothing(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureRaffle: {Countries: []string{"TD"}},
	}}
	for _, ed := range []string{"full", "lite"} {
		assert.True(t, r.Allows(FeatureRaffle, "TD", ed), "édition %s", ed)
		// Et la clé jamais réglée, qui passe par la même règle nulle.
		assert.True(t, r.Allows(FeatureWallet, "TD", ed), "édition %s", ed)
	}
}

// ⚠️⚠️ LA CASSE NE DOIT PAS DÉCIDER. Un pays écrit `tg` dans la console et
// `TG` dans un jeton est le MÊME pays : comparer tel quel aurait retiré la
// fonction à la moitié des requêtes, de façon parfaitement reproductible et
// parfaitement inexplicable.
func TestCaseAndSpacesNeverDecide(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureRaffle: {Editions: []string{"LITE"}, Countries: []string{" tg ", "Sn"}},
	}}
	assert.False(t, r.Allows(FeatureRaffle, "TG", "lite"))
	assert.False(t, r.Allows(FeatureRaffle, "tg", "LITE"))
	assert.False(t, r.Allows(FeatureRaffle, " sn ", "Lite"))
	assert.True(t, r.Allows(FeatureRaffle, "TG", "full"))
	assert.True(t, r.Allows(FeatureRaffle, "GA", "lite"), "hors de la portée")
}

// ⚠️ UNE ÉDITION INCONNUE EST TRAITÉE COMME `full` — la même règle que partout
// ailleurs : une faute de frappe dégrade vers l'application complète, jamais
// vers une application amputée.
func TestAnUnknownEditionIsTreatedAsFull(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureChallenges: {Editions: []string{"lite"}},
	}}
	assert.True(t, r.Allows(FeatureChallenges, "TD", "léger"))
	assert.True(t, r.Allows(FeatureChallenges, "TD", ""))
	assert.False(t, r.Allows(FeatureChallenges, "TD", "lite"))

	// ⚠️⚠️ ET « TRAITÉE COMME `full` » VEUT DIRE LES DEUX SENS : une édition
	// inconnue reçoit AUSSI ce que l'application normale perd. Sans cette
	// seconde moitié, « inconnue » serait une troisième édition à qui on ne
	// retire jamais rien — une application qui envoie un mot de travers
	// obtiendrait plus que les deux vraies.
	//
	// C'est une mutation PASSÉE AU VERT qui l'a montré : retirer
	// `edition.Normalize` ne changeait rien aux trois lignes ci-dessus,
	// puisqu'une règle qui coupe `lite` ne croise de toute façon pas `léger`.
	// Il fallait une règle qui coupe `full`.
	cut := &Registry{Features: map[string]Rule{
		FeatureChallenges: {Editions: []string{"full"}},
	}}
	assert.False(t, cut.Allows(FeatureChallenges, "TD", "full"))
	assert.False(t, cut.Allows(FeatureChallenges, "TD", "léger"),
		"une faute de frappe a obtenu ce que l'application normale a perdu")
	assert.False(t, cut.Allows(FeatureChallenges, "TD", ""),
		"une édition absente a obtenu ce que l'application normale a perdu")
	assert.True(t, cut.Allows(FeatureChallenges, "TD", "lite"))
}

// ⚠️⚠️ ET LE SCHÉMA SAIT ENFIN DIRE « COUPÉE DES DEUX CÔTÉS ».
//
// L'ancienne polarité ne pouvait pas : une liste d'éditions vide voulait dire
// « toutes », si bien qu'« aucune » n'avait pas d'écriture. La console portait
// pour cela un bouton DÉSACTIVÉ avec une infobulle — on ne pouvait pas décocher
// la dernière édition, parce que le second clic rouvrait la fonction des deux
// côtés. Ce n'est plus une limite : c'est une règle comme les autres.
func TestBothEditionsCanLoseAFeature(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureRaffle: {Editions: []string{"full", "lite"}, Countries: []string{"TD"}},
	}}
	assert.False(t, r.Allows(FeatureRaffle, "TD", "full"))
	assert.False(t, r.Allows(FeatureRaffle, "TD", "lite"))
	// Et toujours pas ailleurs : la portée reste la portée.
	assert.True(t, r.Allows(FeatureRaffle, "TG", "full"))
	assert.True(t, r.Allows(FeatureRaffle, "TG", "lite"))
}

// `Restricts` est ce qui refuse un réglage mort à l'écriture.
func TestARuleKnowsWhetherItTakesAnything(t *testing.T) {
	assert.False(t, Rule{}.Restricts(false), "la règle nulle ne retire rien")
	assert.False(t, Rule{Countries: []string{"TD"}}.Restricts(false),
		"un pays sans édition ne retire rien")
	assert.True(t, Rule{Editions: []string{"lite"}}.Restricts(false))

	// ⚠️ UNE LISTE A BESOIN DE SA LISTE BLANCHE : nommer une édition sans dire
	// ce qu'elle voit ne restreint aucune valeur. C'est exactement ce que la
	// console écrivait, et le serveur servait tout.
	assert.False(t, Rule{Editions: []string{"lite"}}.Restricts(true),
		"une règle de liste sans liste blanche ne restreint rien")
	assert.True(t, Rule{Editions: []string{"lite"}, Allow: []string{"moto"}}.Restricts(true))
}

// --- LES LISTES BLANCHES ---

// ⚠️ UNE INTERSECTION, ET L'ORDRE DU MÉTIER EST CONSERVÉ. Le module range sa
// liste exprès : le mode mis en tête est celui qu'on propose d'abord. Rendre
// l'ordre de la liste blanche aurait réordonné des écrans que personne n'a
// demandé à réordonner.
func TestAWhitelistIntersectsAndKeepsTheBusinessOrder(t *testing.T) {
	r := &Registry{ListRules: map[string]Rule{
		ListVehicleModes: {Editions: []string{"lite"}, Allow: []string{"comfort", "moto", "eco"}},
	}}
	served := []string{"moto", "eco", "comfort", "van", "tricycle"}
	got := r.AllowedIn(ListVehicleModes, "TD", "lite", served)
	assert.Equal(t, []string{"moto", "eco", "comfort"}, got,
		"l'ordre du métier doit survivre à l'intersection")
	// L'application normale garde tout.
	assert.Equal(t, served, r.AllowedIn(ListVehicleModes, "TD", "full", served))
}

// ⚠️⚠️ UNE LISTE BLANCHE QUI NE CROISE RIEN REND LA LISTE ENTIÈRE.
//
// C'est le garde-fou le plus important de ce fichier. Une faute de frappe dans
// la console — `Eco` au lieu de `eco`, un mode renommé par les courses, une
// virgule restée dans la saisie — aurait rendu ZÉRO mode de véhicule : une
// application passager sans aucune voiture à commander, pour un pays entier,
// sans aucune erreur nulle part. Mieux vaut une restriction qui ne s'applique
// pas qu'un produit injouable.
func TestAWhitelistThatMatchesNothingReturnsEverything(t *testing.T) {
	served := []string{"moto", "eco", "comfort"}
	for _, allow := range [][]string{
		{"Moto2000"},            // un mode qui n'existe pas
		{"inexistant", "autre"}, // deux fautes
		{" "},                   // une saisie vide
	} {
		r := &Registry{ListRules: map[string]Rule{ListVehicleModes: {Editions: []string{"lite"}, Allow: allow}}}
		assert.Equal(t, served, r.AllowedIn(ListVehicleModes, "TD", "lite", served),
			"liste blanche %v : l'application se retrouverait sans aucun véhicule", allow)
	}
	// Et la casse ne doit pas provoquer ce repli : `Eco` doit croiser `eco`.
	r := &Registry{ListRules: map[string]Rule{ListVehicleModes: {Editions: []string{"lite"}, Allow: []string{"ECO", " Moto "}}}}
	assert.Equal(t, []string{"moto", "eco"}, r.AllowedIn(ListVehicleModes, "TD", "lite", served))
}

// ⚠️ UNE LISTE BLANCHE ABSENTE VEUT DIRE « TOUT CE QUE LE MÉTIER SERT », jamais
// « rien ». Un registre qui ne parle pas des moyens de paiement ne doit pas en
// retirer un seul.
func TestNoWhitelistMeansEverything(t *testing.T) {
	served := []string{"cash", "wallet", "online"}
	for _, r := range []*Registry{nil, {}, {ListRules: map[string]Rule{}}} {
		assert.Equal(t, served, r.AllowedIn(ListPaymentMethods, "TD", "lite", served))
	}
	// ⚠️ Une règle sans `Allow` non plus — et c'est tout le défaut de #245 :
	// la console écrivait EXACTEMENT cela pour « retirer les modes de Lite »,
	// le serveur n'y trouvait aucune valeur à retirer, et servait tout. La
	// case était décochée à l'écran. L'écriture la refuse maintenant
	// (`Restricts`), et la lecture reste inerte pour les règles déjà posées.
	r := &Registry{ListRules: map[string]Rule{ListPaymentMethods: {Editions: []string{"lite"}}}}
	assert.Equal(t, served, r.AllowedIn(ListPaymentMethods, "TD", "lite", served))
}

// --- CE QUE L'APPLICATION LIT AU DÉMARRAGE ---

// ⚠️⚠️ TOUTES LES CLÉS SONT SERVIES, Y COMPRIS CELLES À `true`.
//
// Ne servir que les fonctions coupées obligerait chaque application à
// connaître le catalogue pour deviner le reste — et une fonction ajoutée au
// serveur serait ABSENTE de la réponse, donc lue comme coupée par une
// application prudente. Une fonction nouvelle disparaîtrait ainsi des écrans
// le jour où le serveur l'annonce.
func TestResolveServesEveryKeyIncludingTheOnesLeftOn(t *testing.T) {
	r := &Registry{Features: map[string]Rule{
		FeatureScheduledRides: {Editions: []string{"lite"}},
	}}
	got := r.Resolve("TD", "lite")
	assert.Equal(t, "lite", got.Edition)
	assert.Equal(t, "TD", got.Country)
	assert.Len(t, got.Features, len(Features), "le catalogue entier doit être servi")
	assert.False(t, got.Features[FeatureScheduledRides])
	assert.True(t, got.Features[FeatureTripShare])
	for _, key := range FeatureKeys() {
		_, present := got.Features[key]
		assert.True(t, present, "la clé %s manque : une application la lirait comme coupée", key)
	}
}

// Un registre nil se résout quand même — toutes fonctions ouvertes.
func TestResolveOnANilRegistryOpensEverything(t *testing.T) {
	var r *Registry
	got := r.Resolve("td", "LITE")
	assert.Equal(t, "lite", got.Edition)
	assert.Equal(t, "TD", got.Country, "le pays est normalisé en majuscules")
	require.Len(t, got.Features, len(Features))
	for key, on := range got.Features {
		assert.True(t, on, "%s", key)
	}
	assert.Empty(t, got.Lists)
}

// Les listes blanches sont servies triées, pour une réponse stable.
func TestTheServedWhitelistsAreSorted(t *testing.T) {
	r := &Registry{ListRules: map[string]Rule{
		ListPaymentMethods: {Editions: []string{"lite"}, Allow: []string{"mobile_money", "cash"}},
	}}
	got := r.Resolve("TD", "lite")
	assert.Equal(t, []string{"cash", "mobile_money"}, got.Lists[ListPaymentMethods],
		"une carte Go se parcourt au hasard : sans tri, la réponse change à chaque appel")
}

// --- LE CATALOGUE ---

// ⚠️⚠️ UNE CLÉ INCONNUE EST REFUSÉE À L'ÉCRITURE, et c'est ce que `Known`
// existe pour faire. Sans elle, `scheduled_ride` au singulier s'enregistrait,
// personne ne le lisait, la fonction restait visible dans Lite — et on
// cherchait la panne dans l'application.
func TestAMistypedKeyIsNotInTheCatalogue(t *testing.T) {
	assert.True(t, Known(FeatureScheduledRides))
	assert.False(t, Known("scheduled_ride"), "le singulier doit être refusé")
	assert.False(t, Known(""))
	assert.False(t, Known("SCHEDULED_RIDES"), "le catalogue est en minuscules")

	assert.True(t, KnownList(ListVehicleModes))
	assert.False(t, KnownList(FeatureRental), "une fonction n'est pas une liste")
	assert.False(t, KnownList("vehicle_mode"))
}

// ⚠️ CHAQUE ENTRÉE DU CATALOGUE DIT CE QUE LA COUPER FAIT VOIR À LA PERSONNE.
// « Désactiver scheduled_rides » ne dit rien à l'opérateur qui coche la case ;
// « le client ne peut plus commander pour plus tard » dit tout. Sans cette
// phrase, personne n'oserait toucher à cet écran.
func TestEveryCatalogueEntryExplainsWhatHidingItDoes(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range append(append([]Feature{}, Features...), Lists...) {
		assert.NotEmpty(t, f.Key)
		assert.NotEmpty(t, f.Label, "%s n'a pas de libellé", f.Key)
		assert.NotEmpty(t, f.Hiding, "%s ne dit pas ce que le retirer fait voir", f.Key)
		assert.NotEmpty(t, f.Owner, "%s ne dit pas quel service l'applique", f.Key)
		assert.False(t, seen[f.Key], "%s est déclaré deux fois", f.Key)
		seen[f.Key] = true
	}
}

// ⚠️ LES CLÉS SONT RENDUES TRIÉES, et c'est une promesse de l'API du paquet.
//
// Une carte Go se parcourt dans un ordre ALÉATOIRE : un appelant qui bâtit un
// écran, une réponse ou une comparaison sur cette liste verrait l'ordre changer
// à chaque exécution. Ce n'est pas cosmétique — c'est ce qui rend une réponse
// comparable à la précédente et cachable par une application.
func TestTheCatalogueKeysComeBackSorted(t *testing.T) {
	keys := FeatureKeys()
	require.Len(t, keys, len(Features))
	for i := 1; i < len(keys); i++ {
		assert.Less(t, keys[i-1], keys[i],
			"les clés ne sont pas triées : %q vient avant %q", keys[i-1], keys[i])
	}
	// Et elles nomment bien tout le catalogue, sans trou.
	for _, f := range Features {
		assert.Contains(t, keys, f.Key)
	}
}
