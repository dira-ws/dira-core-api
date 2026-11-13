package compliance

// CE QU'UNE VERTICALE ATTEND, DÉCLARÉ PAR ELLE.
//
// ⚠️⚠️ LA LISTE DES PIÈCES ÉTAIT CELLE DU TRANSPORT, ÉCRITE DANS LE PAQUET.
// Monté tel quel sur un métier qui n'a ni véhicule ni dispatch, il aurait
// réclamé un permis et un extrait de casier à des gens qui n'en ont que faire.
// Ce n'était pas un câblage manquant : la liste vivait au mauvais endroit.
//
// La verticale DÉCLARE donc ce qu'elle reçoit et ce qu'elle attend, parmi les
// types que ce paquet connaît (`docOwner`) ; le paquet garde ce qui est
// commun — le cycle d'une pièce, qui la regarde, quand elle cesse de couvrir.
//
// ⚠️ ET SANS DÉCLARATION, RIEN NE BOUGE : la livraison et les courses ne
// branchent rien, et reçoivent exactement la règle d'avant (`transport`) —
// `PersonKindsFor`, `KindsFor`, et les dix types de `TransportKinds`.

import (
	"context"
	"slices"
)

// Requirements : les pièces qu'une verticale reçoit et attend.
type Requirements interface {
	// Accepts dit si la verticale REÇOIT ce type. Un type refusé l'est par
	// `unknown_document_kind`, comme un type qui n'existe pas : pour celui qui
	// dépose, les deux reviennent au même — cette pièce n'a pas sa place ici.
	Accepts(kind string) bool
	// Expected rend les pièces attendues d'un porteur et de chacun de ses biens.
	//
	// ⚠️ UN CONTEXTE ET UNE ERREUR, parce que la réponse peut dépendre de ce
	// que la verticale est seule à savoir — le statut d'un dossier, le genre
	// d'un bien — et qu'elle le lit dans sa base.
	Expected(ctx context.Context, subjectID string, assets []VehicleRef) (Expectations, error)
}

// Expectations : ce qui est attendu d'un dossier.
type Expectations struct {
	// Subject : les pièces du PORTEUR (personne ou société).
	Subject []string
	// Assets : les pièces de chaque bien, par identifiant. Un bien absent de
	// la carte n'attend RIEN — on ne réclame pas un papier qu'aucune règle ne
	// demande.
	Assets map[string][]string
}

// transport est la règle d'avant, intacte : celle de la livraison et des
// courses.
type transport struct{}

func (transport) Accepts(kind string) bool { return slices.Contains(TransportKinds, kind) }

func (transport) Expected(_ context.Context, _ string, assets []VehicleRef) (Expectations, error) {
	out := Expectations{Subject: PersonKindsFor(assets), Assets: make(map[string][]string, len(assets))}
	for _, v := range assets {
		out.Assets[v.ID] = KindsFor(v)
	}
	return out, nil
}

// SetRequirements branche la liste déclarée par la verticale (câblage).
// Facultatif : sans elle, la règle du transport.
func (s *Service) SetRequirements(r Requirements) { s.req = r }

func (s *Service) requirements() Requirements {
	if s.req == nil {
		return transport{}
	}
	return s.req
}

// accepts refuse un type que la verticale ne reçoit pas, par le code d'un type
// inconnu.
func (s *Service) accepts(kind string) error {
	if !s.requirements().Accepts(kind) {
		return errUnknownDocKind.WithMeta(map[string]any{"kind": kind})
	}
	return nil
}
