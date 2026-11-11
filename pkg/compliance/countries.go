package compliance

// LE PAYS DES PIÈCES D'AVANT.
//
// ⚠️⚠️ BORNER LA FILE SANS RATTRAPER LES PIÈCES EXISTANTES LES AURAIT FAIT
// DISPARAÎTRE. La file et l'arbitrage lisent `country` ; une pièce déposée
// avant ce champ n'en a pas, et `country.Restrict` ne la rend à personne —
// l'exploitation aurait vu une file vide, et l'aurait crue à jour.
//
// Ce fichier demande donc à la VERTICALE le pays de chaque pièce d'avant (elle
// seule sait où vit un chauffeur, une flotte, un bien), et le pose. Il se
// rejoue sans risque : une pièce rattrapée n'est plus relue, et `SetCountry` ne
// réécrit jamais un pays déjà posé.

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Countries : le pays du porteur d'une pièce, servi par la verticale.
type Countries interface {
	// DocumentCountry rend le pays de la pièce — celui du chauffeur, de la
	// flotte ou du bien qui en répond —, ou "" quand la verticale ne le sait
	// pas (porteur disparu).
	DocumentCountry(ctx context.Context, d Document) (string, error)
}

// SetCountries branche le rattrapage (câblage).
func (s *Service) SetCountries(c Countries) { s.countries = c }

// CountryStore : ce qu'il faut au dépôt de lignes pour rattraper.
type CountryStore interface {
	DocumentsWithoutCountry(ctx context.Context, after primitive.ObjectID, limit int) ([]Document, error)
	SetCountry(ctx context.Context, id primitive.ObjectID, code string) error
}

// AdoptCountries pose le pays des pièces qui n'en ont pas, et rend combien en
// ont reçu un.
//
// ⚠️ ELLE PARCOURT TOUTE LA COLLECTION PAR PAGES, ET NE BOUCLE PAS : une pièce
// dont la verticale ne sait pas le pays est laissée — et COMPTÉE dans le
// journal —, et le curseur passe outre. Relire sans curseur aurait relu les
// mêmes pièces sans fin.
//
// ⚠️ LE CONTEXTE NE DOIT PORTER AUCUN PAYS : c'est une tâche de fond qui
// rattrape tous les pays d'un coup.
func (s *Service) AdoptCountries(ctx context.Context, page int) (int, error) {
	store, ok := s.repo.(CountryStore)
	if s.countries == nil || !ok {
		return 0, nil
	}
	if page <= 0 || page > 1000 {
		page = 500
	}
	adopted, unknown := 0, 0
	var after primitive.ObjectID
	for {
		docs, err := store.DocumentsWithoutCountry(ctx, after, page)
		if err != nil {
			return adopted, err
		}
		if len(docs) == 0 {
			break
		}
		for i := range docs {
			d := docs[i]
			after = d.ID
			code, err := s.countries.DocumentCountry(ctx, d)
			if err != nil {
				return adopted, err
			}
			code = country.Normalize(code)
			if code == "" || !country.Known(code) {
				unknown++
				continue
			}
			if err := store.SetCountry(ctx, d.ID, code); err != nil {
				return adopted, err
			}
			adopted++
		}
		if len(docs) < page {
			break
		}
	}
	if unknown > 0 {
		slog.WarnContext(ctx, "compliance: documents left without a country — invisible to the review queue",
			"count", unknown)
	}
	return adopted, nil
}
