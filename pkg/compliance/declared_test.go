package compliance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// --- LA LISTE DÉCLARÉE PAR LA VERTICALE (immo#54) ---

// ⚠️⚠️ SANS DÉCLARATION, RIEN NE BOUGE POUR LA LIVRAISON ET LES COURSES : elles
// reçoivent exactement les dix types d'avant, et refusent les trois de
// l'immobilier comme un type inconnu. C'est la promesse faite à deux
// verticales vivantes, et elle s'éprouve type par type.
func TestTransportStillReceivesExactlyItsTenKinds(t *testing.T) {
	fx := newFixture()
	driver, vehicle := fx.newDriver(t, 5)
	for _, kind := range []string{DocBusinessRegistration, DocPropertyTitle, DocMandate} {
		req := SubmitDocumentRequest{Kind: kind, FileURL: "https://f/" + kind + ".jpg"}
		if docOwner[kind] == ownerVehicle {
			req.VehicleID = vehicle
		}
		_, err := fx.svc.SubmitDocument(context.Background(), driver, req)
		require.Error(t, err, kind)
		assert.Equal(t, "unknown_document_kind", apperr.From(err).Code, kind)
	}
	assert.Len(t, TransportKinds, 10)
	for _, kind := range TransportKinds {
		req := SubmitDocumentRequest{Kind: kind, FileURL: "https://f/" + kind + ".jpg"}
		if docOwner[kind] == ownerVehicle {
			req.VehicleID = vehicle
		}
		if NeedsExpiry(kind) {
			req.ExpiresAt = inDays(90)
		}
		_, err := fx.svc.SubmitDocument(context.Background(), driver, req)
		assert.NoError(t, err, kind)
	}
}

// Une flotte non plus ne reçoit pas un papier de l'immobilier, même porté par
// un de ses véhicules.
func TestAFleetDoesNotReceiveRealEstatePapers(t *testing.T) {
	fx := newFixture()
	fleetID, vehicle := primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()
	fx.fleet.fleetOf[vehicle] = fleetID
	_, err := fx.svc.SubmitFleetDocument(context.Background(), fleetID, SubmitDocumentRequest{
		Kind: DocPropertyTitle, FileURL: "https://f/t.jpg", VehicleID: vehicle,
	})
	require.Error(t, err)
	assert.Equal(t, "unknown_document_kind", apperr.From(err).Code)
}

// ⚠️ LA RÈGLE PAR DÉFAUT EST CELLE D'AVANT, au type près et dans le même ordre :
// la relance et l'écran du chauffeur lisent `Missing`.
func TestTransportExpectationsAreTheOldRule(t *testing.T) {
	moto := VehicleRef{ID: primitive.NewObjectID().Hex(), Motorised: true}
	velo := VehicleRef{ID: primitive.NewObjectID().Hex()}
	exp, err := transport{}.Expected(context.Background(), "x", []VehicleRef{moto, velo})
	require.NoError(t, err)
	assert.Equal(t, PersonKindsFor([]VehicleRef{moto, velo}), exp.Subject)
	assert.Equal(t, KindsFor(moto), exp.Assets[moto.ID])
	assert.Equal(t, KindsFor(velo), exp.Assets[velo.ID])
}

// realEstate : une verticale qui déclare SES pièces — l'immobilier.
type realEstate struct{ agencies map[string]bool }

func (r realEstate) Accepts(kind string) bool {
	switch kind {
	case DocIDCard, DocSelfie, DocBusinessRegistration, DocPropertyTitle, DocMandate:
		return true
	}
	return false
}

func (r realEstate) Expected(_ context.Context, subject string, assets []VehicleRef) (Expectations, error) {
	out := Expectations{Subject: []string{DocIDCard, DocSelfie}, Assets: map[string][]string{}}
	if r.agencies[subject] {
		out.Subject = append(out.Subject, DocBusinessRegistration)
	}
	for _, a := range assets {
		out.Assets[a.ID] = []string{DocPropertyTitle}
		if r.agencies[subject] {
			out.Assets[a.ID] = append(out.Assets[a.ID], DocMandate)
		}
	}
	return out, nil
}

// publisherFixture : une agence et un bien, la liste déclarée.
func publisherFixture(t *testing.T, agency bool) (fx *fixture, user, subject, property string) {
	t.Helper()
	fx = newFixture()
	user, subject = primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()
	fx.fleet.byUser[user] = subject
	fx.fleet.accounts[subject] = user
	// ⚠️ MOTORISÉ EXPRÈS : la règle du transport réclamerait alors le PERMIS.
	// Si elle fuyait sous la déclaration, l'épreuve le verrait.
	property = fx.fleet.addVehicleOf(subject, true)
	fx.svc.SetRequirements(realEstate{agencies: map[string]bool{subject: agency}})
	return fx, user, subject, property
}

// ⚠️⚠️ LE DÉFAUT QUI A FAIT ÉCRIRE CE FICHIER : monté tel quel sur un annonceur,
// le paquet réclamait un CASIER JUDICIAIRE à quelqu'un qui loue son appartement.
// Sous la liste déclarée, il n'est ni réclamé ni reçu ; le permis non plus.
func TestDeclaredKindsReplaceTheTransportRule(t *testing.T) {
	fx, user, _, property := publisherFixture(t, false)

	got, err := fx.svc.Compliance(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, []string{DocIDCard, DocSelfie, DocPropertyTitle + ":" + property}, got.Missing)
	assert.False(t, got.Compliant)

	_, err = fx.svc.SubmitDocument(context.Background(), user, SubmitDocumentRequest{
		Kind: DocCriminalRecord, FileURL: "https://f/c.jpg", ExpiresAt: inDays(90),
	})
	require.Error(t, err)
	assert.Equal(t, "unknown_document_kind", apperr.From(err).Code,
		"un annonceur n'a pas de casier à déposer")
}

// Une AGENCE doit en plus son registre du commerce, et le mandat de chaque bien.
func TestAnAgencyOwesItsRegistrationAndEachMandate(t *testing.T) {
	fx, user, _, property := publisherFixture(t, true)
	got, err := fx.svc.Compliance(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, []string{
		DocIDCard, DocSelfie, DocBusinessRegistration,
		DocPropertyTitle + ":" + property, DocMandate + ":" + property,
	}, got.Missing)

	// Le dossier complet et validé est EN RÈGLE.
	admin := newAdmin()
	for _, req := range []SubmitDocumentRequest{
		{Kind: DocIDCard, FileURL: "https://f/i.jpg"},
		{Kind: DocSelfie, FileURL: "https://f/s.jpg"},
		{Kind: DocBusinessRegistration, FileURL: "https://f/r.jpg"},
		{Kind: DocPropertyTitle, FileURL: "https://f/t.jpg", VehicleID: property},
		{Kind: DocMandate, FileURL: "https://f/m.jpg", VehicleID: property},
	} {
		d := submit(t, fx, user, req)
		_, err := fx.svc.ReviewDocument(context.Background(), admin, d.ID, ReviewDocumentRequest{Status: DocValid})
		require.NoError(t, err)
	}
	got, err = fx.svc.Compliance(context.Background(), user)
	require.NoError(t, err)
	assert.Empty(t, got.Missing)
	assert.True(t, got.Compliant)
}

// Le titre et le mandat appartiennent au BIEN : sans bien, refusés.
func TestATitleNeedsItsProperty(t *testing.T) {
	fx, user, _, _ := publisherFixture(t, true)
	_, err := fx.svc.SubmitDocument(context.Background(), user, SubmitDocumentRequest{
		Kind: DocPropertyTitle, FileURL: "https://f/t.jpg",
	})
	require.Error(t, err)
	assert.Equal(t, "document_needs_vehicle", apperr.From(err).Code)
	_, err = fx.svc.SubmitDocument(context.Background(), user, SubmitDocumentRequest{
		Kind: DocBusinessRegistration, FileURL: "https://f/r.jpg", VehicleID: primitive.NewObjectID().Hex(),
	})
	require.Error(t, err, "un registre du commerce ne se rattache pas à un bien")
}

// Les libellés des trois nouveaux types existent dans les deux langues : une
// alerte « pièce à vérifier » ne doit jamais afficher `property_title`.
func TestRealEstateKindsHaveLabels(t *testing.T) {
	for _, kind := range []string{DocBusinessRegistration, DocPropertyTitle, DocMandate} {
		assert.NotEqual(t, kind, Label(kind, "fr"), kind)
		assert.NotEqual(t, kind, Label(kind, "en"), kind)
	}
}

// --- LE PAYS DES PIÈCES ---

func inCountryCtx(code string) context.Context {
	return country.WithCountry(context.Background(), code, country.SourceHeader)
}

// Une pièce porte le pays de la requête qui l'a déposée.
func TestADocumentCarriesTheCountryOfItsDeposit(t *testing.T) {
	fx := newFixture()
	driver, _ := fx.newDriver(t, 5)
	d, err := fx.svc.SubmitDocument(inCountryCtx("TG"), driver, SubmitDocumentRequest{
		Kind: DocIDCard, FileURL: "https://f/i.jpg",
	})
	require.NoError(t, err)
	stored, _ := fx.repo.DocumentByID(context.Background(), mustOID(t, d.ID))
	assert.Equal(t, "TG", stored.Country)

	up := upsertUpdate(&Document{OwnerID: primitive.NewObjectID(), Kind: DocIDCard, Country: "TG"}, time.Now())
	assert.Equal(t, "TG", up["$set"].(bson.M)["country"],
		"en `$set` : une pièce d'avant le champ le reçoit à son prochain dépôt")
	up = upsertUpdate(&Document{OwnerID: primitive.NewObjectID(), Kind: DocIDCard}, time.Now())
	assert.NotContains(t, up["$set"].(bson.M), "country", "un pays inconnu n'efface pas celui qui est posé")
}

// ⚠️⚠️ LA FILE ET L'ARBITRAGE SONT BORNÉS AU PAYS : un exploitant de Lomé ne voit
// ni n'arbitre la pièce d'identité d'un chauffeur de Dakar.
func TestTheQueueAndTheReviewStayInTheirCountry(t *testing.T) {
	fx := newFixture()
	tgDriver, _ := fx.newDriver(t, 5)
	snDriver, _ := fx.newDriver(t, 5)
	tgDoc, err := fx.svc.SubmitDocument(inCountryCtx("TG"), tgDriver, SubmitDocumentRequest{Kind: DocIDCard, FileURL: "https://f/t.jpg"})
	require.NoError(t, err)
	snDoc, err := fx.svc.SubmitDocument(inCountryCtx("SN"), snDriver, SubmitDocumentRequest{Kind: DocIDCard, FileURL: "https://f/s.jpg"})
	require.NoError(t, err)

	q, err := fx.svc.ComplianceQueue(inCountryCtx("TG"), 50)
	require.NoError(t, err)
	require.Len(t, q, 1)
	assert.Equal(t, tgDoc.ID, q[0].ID)

	_, err = fx.svc.ReviewDocument(inCountryCtx("TG"), newAdmin(), snDoc.ID, ReviewDocumentRequest{Status: DocValid})
	require.Error(t, err)
	assert.Equal(t, "document_not_found", apperr.From(err).Code,
		"hors du pays, la pièce n'existe pas — la même réponse qu'un identifiant inconnu")
	_, err = fx.svc.ReviewDocument(inCountryCtx("TG"), newAdmin(), tgDoc.ID, ReviewDocumentRequest{Status: DocValid})
	require.NoError(t, err)
}

// Les filtres du dépôt RÉEL portent la borne — c'est eux qui partent en base.
func TestTheRepositoryFiltersCarryTheCountry(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "TG", queueFilter(inCountryCtx("TG"), now)[country.Field])
	assert.Contains(t, queueFilter(inCountryCtx("TG"), now), "$or", "la borne s'AJOUTE au filtre de la file")
	id := primitive.NewObjectID()
	f := reviewFilter(inCountryCtx("SN"), id)
	assert.Equal(t, "SN", f[country.Field])
	assert.Equal(t, id, f["_id"])
	// Une tâche de fond sans pays lit tout — c'est la règle de `Restrict`.
	assert.NotContains(t, queueFilter(context.Background(), now), country.Field)
}

// countriesOf : la verticale qui sait où vit chaque porteur.
type countriesOf map[string]string

func (c countriesOf) DocumentCountry(_ context.Context, d Document) (string, error) {
	return c[d.OwnerID.Hex()], nil
}

// ⚠️⚠️ BORNER SANS RATTRAPER AURAIT VIDÉ LA FILE : les pièces d'avant n'ont pas
// de pays. Le rattrapage les pose, laisse celles dont personne ne sait le pays
// — sans boucler dessus —, et ne réécrit jamais un pays déjà posé.
func TestAdoptCountriesCatchesUpTheOldDocuments(t *testing.T) {
	fx := newFixture()
	owners := []primitive.ObjectID{primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID(), primitive.NewObjectID()}
	add := func(owner primitive.ObjectID, code string) {
		fx.repo.documents = append(fx.repo.documents, Document{
			ID: primitive.NewObjectID(), OwnerID: owner, Kind: DocIDCard, Status: DocPending, Country: code,
		})
	}
	add(owners[0], "")   // à rattraper : TG
	add(owners[1], "")   // porteur inconnu : laissé
	add(owners[2], "SN") // déjà posé : intact, même si la verticale dit autre chose
	add(owners[3], "")   // à rattraper : SN
	fx.svc.SetCountries(countriesOf{
		owners[0].Hex(): "tg", owners[2].Hex(): "GN", owners[3].Hex(): "SN",
	})

	// Une page de 1 : le curseur doit passer le porteur inconnu, pas buter dessus.
	n, err := fx.svc.AdoptCountries(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	got := map[string]string{}
	for _, d := range fx.repo.documents {
		got[d.OwnerID.Hex()] = d.Country
	}
	assert.Equal(t, "TG", got[owners[0].Hex()], "normalisé")
	assert.Empty(t, got[owners[1].Hex()])
	assert.Equal(t, "SN", got[owners[2].Hex()])
	assert.Equal(t, "SN", got[owners[3].Hex()])

	// Rejoué : rien de plus.
	n, err = fx.svc.AdoptCountries(context.Background(), 1)
	require.NoError(t, err)
	assert.Zero(t, n)
}

// Un pays qui n'est pas au catalogue n'est pas posé : il rendrait la pièce
// aussi invisible qu'aucun pays, en faisant croire qu'elle est rangée.
func TestAdoptCountriesRefusesAnUnknownCode(t *testing.T) {
	fx := newFixture()
	owner := primitive.NewObjectID()
	fx.repo.documents = append(fx.repo.documents, Document{ID: primitive.NewObjectID(), OwnerID: owner, Kind: DocIDCard})
	fx.svc.SetCountries(countriesOf{owner.Hex(): "ZZ"})
	n, err := fx.svc.AdoptCountries(context.Background(), 10)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, fx.repo.documents[0].Country)
}

func mustOID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return id
}
