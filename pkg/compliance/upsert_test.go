package compliance

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ⚠️⚠️ UN CHAMP SUR LEQUEL UNE LECTURE FILTRE EST UN CHAMP QU'UNE ÉCRITURE DOIT
// POSER. C'est la règle que ce fichier existe pour tenir, et voici le défaut qui
// l'a fait écrire.
//
// `fleet_id` marque la pièce déposée par le PROPRIÉTAIRE d'un véhicule.
// `SubmitFleetDocument` le posait sur la structure, `FleetDocumentsOfVehicles`
// le réclamait (`fleet_id: {$exists: true}`) — et l'écriture ne le recopiait
// dans AUCUN opérateur. La pièce partait en base sans marque de société, et
// l'écran de la société ne la retrouvait jamais : un dépôt accepté, invisible,
// à refaire. Éprouvé sur staging, où six pièces déposées ont disparu.
//
// Rien ne pouvait le voir : le dépôt réel n'est pas testé (c'est le rôle de
// l'interface `Store`), et la doublure rangeait la structure REÇUE telle quelle
// — donc PLUS FIDÈLE que la chose qu'elle double. C'est pour ça que la décision
// est extraite ici, et que la doublure ne recopie plus que ce que la base garde.
func TestAFleetDocumentCarriesItsFleetMark(t *testing.T) {
	fleet, vehicle := primitive.NewObjectID(), primitive.NewObjectID()
	now := time.Now().UTC()

	up := upsertUpdate(&Document{
		OwnerID: fleet, FleetID: &fleet, VehicleID: &vehicle, Kind: DocInsurance,
	}, now)
	set, ok := up["$set"].(bson.M)
	require.True(t, ok)
	assert.Equal(t, fleet, set["fleet_id"],
		"sans cette marque, la lecture du parc ne retrouve pas la pièce qu'on vient de déposer")

	// ⚠️ EN `$set` ET NON EN `$setOnInsert` : une pièce déposée avant la
	// correction existe déjà sans marque, et seul un `$set` la répare au
	// prochain dépôt.
	onInsert, ok := up["$setOnInsert"].(bson.M)
	require.True(t, ok)
	assert.NotContains(t, onInsert, "fleet_id",
		"en `$setOnInsert`, les pièces déjà déposées resteraient invisibles pour toujours")

	// Et le dépôt d'une PERSONNE ne porte pas de marque de société : son
	// `fleet_id` absent est ce qui distingue les deux dossiers.
	driver := primitive.NewObjectID()
	up = upsertUpdate(&Document{OwnerID: driver, VehicleID: &vehicle, Kind: DocInsurance}, now)
	set, _ = up["$set"].(bson.M)
	assert.NotContains(t, set, "fleet_id")
}

// ⚠️⚠️ TOUT TYPE QUE LA PLATEFORME EXIGE DOIT POUVOIR ÊTRE DÉPOSÉ, et ce test
// existe parce que ça n'a pas été le cas.
//
// Le DTO portait sa propre liste, écrite à la main — `licence id_card
// registration insurance`, QUATRE — quand `PersonKinds` + `VehicleKinds` + le
// permis en exigent NEUF. `inspection`, `selfie`, `criminal_record` et les
// trois photos étaient donc réclamés par `Missing`, redits tous les trois jours
// par la relance, et REFUSÉS À L'ENVOI en « Requête invalide ».
//
// Personne n'était bloqué pour autant — rien ne coupe le travail sur
// `compliant` —, ce qui explique la durée : la seule trace visible était une
// liste de papiers manquants qu'aucune application ne pouvait vider.
//
// ⚠️ LE TEST LIT LE DTO DANS LA SOURCE, parce que c'est le seul endroit qui peut
// voir le désaccord : les deux listes compilent, et aucune ne mentionne l'autre.
func TestEveryRequiredDocumentKindCanBeSubmitted(t *testing.T) {
	src, err := os.ReadFile("service.go")
	require.NoError(t, err)

	kind := regexp.MustCompile("Kind\\s+string\\s+`json:\"kind\"\\s+validate:\"([^\"]*)\"`")
	m := kind.FindSubmatch(src)
	require.NotNil(t, m, "le champ `kind` du DTO n'a plus la forme attendue : ce test ne vérifie plus rien")
	rules := string(m[1])

	required := append([]string{DocLicence}, PersonKinds...)
	required = append(required, VehicleKinds...)
	for _, k := range required {
		assert.True(t, docOwner[k] != "",
			"`%s` est exigé et n'est pas dans `docOwner` : `CheckOwner` le refuserait", k)
		if strings.Contains(rules, "oneof=") {
			assert.Contains(t, rules, k,
				"`%s` est EXIGÉ par la plateforme et absent du `oneof` du DTO : "+
					"il sera réclamé par `missing` et refusé à l'envoi", k)
		}
	}
	// ⚠️ ET UN TYPE INCONNU EST REFUSÉ PAR LE PRODUIT, AVEC SA PHRASE. Retirer
	// la liste du DTO sans cela aurait remplacé un défaut par un autre : un
	// refus muet (« Requête invalide ») au lieu d'un refus qui se comprend.
	err2 := CheckOwner("carte_de_fidelite", nil)
	require.Error(t, err2)
	e := apperr.From(err2)
	assert.Equal(t, "unknown_document_kind", e.Code)
	assert.NotEqual(t, "validation_failed", e.Code)
}
