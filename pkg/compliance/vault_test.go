package compliance

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

const ours = "https://files.dira.llc/dira-media/"

// fakeVault REPRODUIT le dépôt : n'accepte que ses adresses (nues ou
// signées), range sous le genre demandé, signe en ajoutant une requête.
type fakeVault struct {
	adopted  map[string]string
	signFail bool
	adoptErr error
}

func newVault() *fakeVault { return &fakeVault{adopted: map[string]string{}} }

func (v *fakeVault) Key(raw string) (string, bool) {
	bare, _, _ := strings.Cut(raw, "?")
	if !strings.HasPrefix(bare, ours) || len(bare) == len(ours) {
		return "", false
	}
	return strings.TrimPrefix(bare, ours), true
}

func (v *fakeVault) Adopt(_ context.Context, raw, kind, entity string) (string, error) {
	if v.adoptErr != nil {
		return "", v.adoptErr
	}
	key, ok := v.Key(raw)
	if !ok {
		return "", errors.New("étrangère")
	}
	if strings.HasPrefix(key, kind+"/") {
		return ours + key, nil
	}
	out := ours + kind + "/" + entity + "/rangee-" + key[strings.LastIndex(key, "/")+1:]
	v.adopted[raw] = out
	return out, nil
}

func (v *fakeVault) SignedURL(_ context.Context, raw string, ttl time.Duration) (string, error) {
	if v.signFail {
		return "", errors.New("signature impossible")
	}
	bare, _, _ := strings.Cut(raw, "?")
	return bare + "?sig=" + ttl.String(), nil
}

// ⚠️⚠️ UNE PIÈCE DÉPOSÉE SOUS UN AUTRE GENRE EST RANGÉE SOUS `document/`,
// l'adresse NUE est gardée, et la réponse est SIGNÉE.
func TestASubmittedDocumentIsAdoptedStoredBareAndServedSigned(t *testing.T) {
	fx := newFixture()
	vault := newVault()
	fx.svc.SetVault(vault)
	driver, _ := fx.newDriver(t, 5)

	// L'application renvoie l'adresse que `/uploads` lui a servie, sous le
	// genre qu'elle a choisi.
	doc := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence,
		FileURL: ours + "vehicle/permis.jpg", ExpiresAt: inDays(400)})

	require.Len(t, fx.repo.documents, 1)
	stored := fx.repo.documents[0].FileURL
	assert.True(t, strings.HasPrefix(stored, ours+"document/"), "la pièce n'a pas été rangée : %s", stored)
	assert.NotContains(t, stored, "?", "une adresse SIGNÉE a été rangée : elle expirerait dans la base")
	assert.Equal(t, stored+"?sig=30m0s", doc.FileURL, "la réponse n'est pas signée")
}

// ⚠️ UNE ADRESSE ÉTRANGÈRE EST REFUSÉE, avec son code et le champ.
func TestAForeignFileIsRefused(t *testing.T) {
	fx := newFixture()
	fx.svc.SetVault(newVault())
	driver, _ := fx.newDriver(t, 5)
	_, err := fx.svc.SubmitDocument(context.Background(), driver,
		SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://ailleurs.example/permis.jpg", ExpiresAt: inDays(400)})
	require.Error(t, err)
	assert.Equal(t, "file_url_foreign", apperr.From(err).Code)
	assert.Empty(t, fx.repo.documents, "une pièce étrangère a été rangée")
}

// ⚠️ SANS DÉPÔT BRANCHÉ, RIEN NE CHANGE — le comportement d'avant.
func TestWithoutAVaultNothingChanges(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	doc := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: "https://f/1.jpg", ExpiresAt: inDays(400)})
	assert.Equal(t, "https://f/1.jpg", doc.FileURL)
}

// ⚠️ UNE SIGNATURE QUI ÉCHOUE REND L'ADRESSE NUE, sans priver la liste.
func TestASigningFailureKeepsTheList(t *testing.T) {
	fx := newFixture()
	vault := newVault()
	fx.svc.SetVault(vault)
	driver, _ := fx.newDriver(t, 5)
	submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: ours + "document/u/p.jpg", ExpiresAt: inDays(400)})
	vault.signFail = true
	resp, err := fx.svc.Compliance(context.Background(), driver)
	require.NoError(t, err, "une signature ratée a fait échouer toute la liste")
	require.NotEmpty(t, resp.Documents)
	assert.Equal(t, ours+"document/u/p.jpg", resp.Documents[0].FileURL)
}

// ⚠️⚠️ TOUTES LES LECTURES SIGNENT : la file, la fiche d'un porteur, la
// relecture d'une décision.
func TestEveryReadIsSigned(t *testing.T) {
	fx := newFixture()
	fx.svc.SetVault(newVault())
	driver, _ := fx.newDriver(t, 5)
	ctx := context.Background()
	doc := submit(t, fx, driver, SubmitDocumentRequest{Kind: DocLicence, FileURL: ours + "vehicle/a.jpg", ExpiresAt: inDays(400)})

	mine, err := fx.svc.Compliance(ctx, driver)
	require.NoError(t, err)
	for _, d := range mine.Documents {
		assert.Contains(t, d.FileURL, "?sig=", "la liste du porteur n'est pas signée")
	}
	queue, err := fx.svc.ComplianceQueue(ctx, 50)
	require.NoError(t, err)
	require.NotEmpty(t, queue)
	for _, q := range queue {
		assert.Contains(t, q.FileURL, "?sig=", "la file de l'exploitation n'est pas signée")
	}
	reviewed, err := fx.svc.ReviewDocument(ctx, newAdmin(), doc.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)
	assert.Contains(t, reviewed.FileURL, "?sig=")
}

// ⚠️⚠️ LE GARDE LIT LE CODE : aucune réponse de pièce ne se compose hors de
// `respond`. Un chemin de lecture qui l'oublierait servirait une adresse nue —
// morte, une fois le genre fermé.
func TestNoDocumentResponseEscapesTheSigner(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		n := strings.Count(string(src), "toDocumentResponse(")
		switch name {
		case "service.go":
			assert.Equal(t, 1, n, "service.go compose une réponse sans la signer (seule la définition est permise)")
		case "vault.go":
			assert.Equal(t, 1, n, "vault.go : seul `respond` compose")
		default:
			assert.Zero(t, n, "%s compose une réponse de pièce sans la signer", name)
		}
	}
}

// --- les pièces d'avant ---

type strayStore struct {
	*fakeStore
	rewritten map[string]string
}

func (s *strayStore) DocumentsOutside(_ context.Context, marker string, _ int) ([]Document, error) {
	var out []Document
	for _, d := range s.documents {
		if d.FileURL != "" && !strings.Contains(d.FileURL, marker) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *strayStore) SetFileURL(_ context.Context, d *Document, fileURL string) error {
	for i := range s.documents {
		if s.documents[i].ID == d.ID && s.documents[i].FileURL == d.FileURL {
			s.documents[i].FileURL = fileURL
			s.rewritten[d.ID.Hex()] = fileURL
		}
	}
	return nil
}

// ⚠️⚠️ LES PIÈCES D'AVANT SONT RANGÉES AUSSI.
func TestStraysAreAdoptedAndTheRestIsLeft(t *testing.T) {
	base := &fakeStore{}
	store := &strayStore{fakeStore: base, rewritten: map[string]string{}}
	svc := NewService(store, newFleet())
	vault := newVault()
	svc.SetVault(vault)
	ctx := context.Background()
	mk := func(url string) {
		base.documents = append(base.documents, Document{ID: primitive.NewObjectID(), OwnerID: primitive.NewObjectID(), Kind: DocLicence, FileURL: url})
	}
	mk(ours + "vehicle/a.jpg")             // à ranger
	mk(ours + "document/u/b.jpg")          // déjà rangée
	mk("https://ailleurs.example/c.jpg")   // étrangère : laissée, et dite
	mk(ours + "vehicle/d.pdf?X-Amz-Sig=1") // signée par erreur : rangée nue

	moved, err := svc.AdoptStrays(ctx, 100)
	require.NoError(t, err)
	assert.Equal(t, 2, moved)
	for _, d := range base.documents {
		if strings.HasPrefix(d.FileURL, ours) {
			assert.Contains(t, d.FileURL, "/document/", "une pièce de notre dépôt est restée hors de son genre")
		}
	}
	assert.Equal(t, "https://ailleurs.example/c.jpg", base.documents[2].FileURL)

	// ⚠️ ELLE SE REJOUE SANS RIEN REFAIRE.
	again, err := svc.AdoptStrays(ctx, 100)
	require.NoError(t, err)
	assert.Zero(t, again)

	// ⚠️ UN RANGEMENT QUI ÉCHOUE NE RÉÉCRIT PAS LA LIGNE.
	mk(ours + "vehicle/e.jpg")
	vault.adoptErr = errors.New("dépôt muet")
	moved, err = svc.AdoptStrays(ctx, 100)
	require.NoError(t, err)
	assert.Zero(t, moved)
	assert.Equal(t, ours+"vehicle/e.jpg", base.documents[4].FileURL)
}

// ⚠️ LES PAPIERS D'UNE SOCIÉTÉ PASSENT PAR LE MÊME COFFRE : rangés sous
// `document/<flotte>/`, servis signés.
func TestAFleetDocumentIsAdoptedToo(t *testing.T) {
	f := newFixture()
	f.svc.SetVault(newVault())
	_, _, fleetID, vehicleID := f.newFleetDriver(t)
	doc, err := f.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
		Kind: DocRegistration, FileURL: ours + "vehicle/carte-grise.jpg", VehicleID: vehicleID,
	})
	require.NoError(t, err)
	var stored string
	for _, d := range f.repo.documents {
		if d.ID.Hex() == doc.ID {
			stored = d.FileURL
		}
	}
	assert.True(t, strings.HasPrefix(stored, ours+"document/"+fleetID+"/"), "papier de société non rangé : %s", stored)
	assert.Contains(t, doc.FileURL, "?sig=")
}
