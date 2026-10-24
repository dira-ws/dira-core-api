package ownfleet

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/equipment"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Fleets rend la flotte INTERNE du pays de la requête.
//
// ⚠️ DÉCLARÉE ICI, CÔTÉ CONSOMMATEUR : ce paquet dit ce dont il a besoin — un
// propriétaire pour ses véhicules — et ignore comment une flotte est faite. La
// traduction vit au câblage, comme pour le matériel.
type Fleets interface {
	EnsureInternal(ctx context.Context) (fleetID string, err error)
}

// Redevance est LE MOTEUR DE PRÉLÈVEMENT de la plateforme —
// `internal/equipment`.
//
// ⚠️⚠️ IL N'Y EN A QU'UN, et c'est la décision structurante de ce paquet (voir
// l'en-tête de `model.go`). Une attribution de véhicule ouvre un contrat de
// LOCATION chez lui ; tout ce qui concerne l'argent — échéancier, retenue sur
// les gains, plafonds, grâce, pénalité, blocage, caution, QR de remise et de
// retour — se passe là-bas et NULLE PART ICI.
type Redevance interface {
	OpenAssetRental(ctx context.Context, in equipment.AssetRentalInput) (*equipment.ContractResponse, error)
	ListContracts(ctx context.Context, f equipment.ContractFilter, page httpx.Page) ([]equipment.ContractResponse, string, error)
	MyContracts(ctx context.Context, userID string) ([]equipment.ContractResponse, error)
}

// Accounts : ce que le socle sait d'une personne.
type Accounts interface {
	CountryOf(ctx context.Context, userID string) (string, error)
	RoleOf(ctx context.Context, userID string) (string, error)
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
}

// Auditor enregistre les gestes d'exploitation. Facultatif.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Store est ce que le service attend du registre — déclaré ICI, côté
// consommateur, comme partout sur cette plateforme : le parcours complet d'une
// attribution s'éprouve alors sans Mongo.
//
// ⚠️⚠️ ET LA DOUBLURE DOIT DIRE VRAI, SURTOUT POUR `Reserve`. Une doublure qui
// réserverait toujours ferait passer l'épreuve de la double attribution en ne
// vérifiant rien : c'est le refus CONDITIONNEL qui est la règle, pas l'appel.
// Cette plateforme a déjà vu l'inverse — une doublure plus fidèle que la base,
// qui a révélé qu'un chemin n'avait jamais marché — et c'est ce qu'on veut
// reproduire ici, pas l'éviter.
type Store interface {
	Create(ctx context.Context, a *Asset) error
	ByID(ctx context.Context, id primitive.ObjectID) (*Asset, error)
	List(ctx context.Context, f Filter, cursor primitive.ObjectID, limit int) ([]Asset, error)
	ByHolder(ctx context.Context, userID primitive.ObjectID) ([]Asset, error)
	Update(ctx context.Context, id primitive.ObjectID, set bson.M) (*Asset, error)
	Reserve(ctx context.Context, assetID, contractID, holderUserID primitive.ObjectID, at time.Time) (*Asset, error)
	Release(ctx context.Context, assetID, contractID primitive.ObjectID, at time.Time) (bool, error)
}

// Service applique les règles de la flotte interne.
type Service struct {
	repo     Store
	fleets   Fleets
	rede     Redevance
	accounts Accounts
	audit    Auditor
	now      func() time.Time
}

func NewService(repo Store) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetFleets(f Fleets)       { s.fleets = f }
func (s *Service) SetRedevance(r Redevance) { s.rede = r }
func (s *Service) SetAccounts(a Accounts)   { s.accounts = a }
func (s *Service) SetAuditor(a Auditor)     { s.audit = a }

func (s *Service) record(ctx context.Context, action, id string, before, after any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "own_fleet_vehicle", id, before, after)
}

// --- lecture ---

// List rend le parc du pays, le plus récent d'abord, chaque véhicule confié
// accompagné de ce que sa redevance doit.
func (s *Service) List(ctx context.Context, f Filter, page httpx.Page) ([]AssetResponse, string, error) {
	if f.Status != "" && !contains(Statuses, f.Status) {
		return nil, "", errBadStatus
	}
	cursor := primitive.NilObjectID
	if page.Cursor != "" {
		id, err := primitive.ObjectIDFromHex(page.Cursor)
		if err != nil {
			return nil, "", apperr.Validation("cursor is not a valid id")
		}
		cursor = id
	}
	assets, err := s.repo.List(ctx, f, cursor, page.Limit)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	out := s.decorate(ctx, assets)
	// Une page incomplète est la DERNIÈRE : rendre un curseur ferait afficher
	// « charger plus » sur une liste terminée, et le clic rapporterait une page
	// vide — ce qui se lit comme une panne.
	next := ""
	if len(assets) == page.Limit && len(out) > 0 {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// Get rend un véhicule.
func (s *Service) Get(ctx context.Context, id string) (*AssetResponse, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	out := s.decorate(ctx, []Asset{*a})
	return &out[0], nil
}

// Mine est CE QUE L'AGENT VOIT : les véhicules de Dira qui lui sont confiés, et
// pour chacun sa redevance — ce qu'il doit, ce qui a été prélevé.
//
// ⚠️ UN SEUL APPEL, ET C'EST DÉLIBÉRÉ. L'échéancier complet vit déjà dans
// `GET /me/equipment` : la voiture y est un contrat de location comme un autre,
// avec ses lignes et ses paiements. Mais obliger l'application à lire deux
// routes et à les recoudre sur `asset_id` aurait fait écrire ce raccord deux
// fois — une fois par application —, et la troisième se serait trompée. Le
// serveur le fait une fois.
//
// ⚠️ ET SANS BORNE PAYS : c'est le COMPTE CONNECTÉ qui désigne ses véhicules.
// Un chauffeur en voyage doit pouvoir lire ce qu'il doit.
func (s *Service) Mine(ctx context.Context, userID string) ([]AssetResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	assets, err := s.repo.ByHolder(ctx, uid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if len(assets) == 0 {
		return []AssetResponse{}, nil
	}
	now := s.now()
	fees := map[string]*FeeResponse{}
	if s.rede != nil {
		// ⚠️ SES CONTRATS À LUI, lus SANS borne pays (`MyContracts`) — la même
		// raison que ci-dessus. Passer par la liste d'exploitation aurait rendu
		// une redevance vide à un agent qui appelle depuis l'étranger, et son
		// écran aurait dit « rien à payer ».
		cs, err := s.rede.MyContracts(ctx, userID)
		if err != nil {
			return nil, err
		}
		for i := range cs {
			if cs[i].AssetID != "" {
				fees[cs[i].AssetID] = feeOf(&cs[i])
			}
		}
	}
	out := make([]AssetResponse, 0, len(assets))
	for i := range assets {
		r := toAsset(&assets[i])
		r.Fee = fees[r.ID]
		r.Attention = attentionOf(&assets[i], r.Fee, now)
		// ⚠️ NI LE NOM DU PORTEUR, NI LES NOTES D'EXPLOITATION sur l'écran de
		// l'agent : il sait qui il est, et les notes du parc sont des échanges
		// internes (« à surveiller », « rendu sale »). Les servir aurait fait
		// lire à quelqu'un ce qu'on écrit sur lui.
		r.HolderName, r.Notes = "", ""
		out = append(out, r)
	}
	return out, nil
}

// decorate rend les véhicules avec le nom de leur porteur et l'état de leur
// redevance.
//
// ⚠️ UNE REQUÊTE POUR LA PAGE, pas une par ligne : un écran de parc est une
// liste, et cinquante allers-retours font une page qu'on n'ouvre plus.
func (s *Service) decorate(ctx context.Context, assets []Asset) []AssetResponse {
	now := s.now()
	out := make([]AssetResponse, 0, len(assets))
	ids := make([]primitive.ObjectID, 0, len(assets))
	holders := make([]string, 0, len(assets))
	for i := range assets {
		out = append(out, toAsset(&assets[i]))
		if assets[i].ContractID != nil {
			ids = append(ids, assets[i].ID)
		}
		if assets[i].HolderUserID != nil {
			holders = append(holders, assets[i].HolderUserID.Hex())
		}
	}
	if s.rede != nil && len(ids) > 0 {
		cs, _, err := s.rede.ListContracts(ctx,
			// ⚠️ `active` ET `returned` : un contrat RENDU peut garder une
			// dette. S'arrêter à `active` aurait fait disparaître de la liste
			// des impayés exactement les reprises qu'il reste à recouvrer.
			equipment.ContractFilter{AssetIDs: ids, Statuses: []string{equipment.StatusActive, equipment.StatusReturned, equipment.StatusDraft, equipment.StatusAccepted}},
			httpx.Page{Limit: len(ids) * 2})
		if err != nil {
			slog.WarnContext(ctx, "ownfleet: fees not resolved", "error", err)
		} else {
			byAsset := make(map[string]*FeeResponse, len(cs))
			for i := range cs {
				if cs[i].AssetID == "" {
					continue
				}
				// ⚠️ LE CONTRAT QUE LE VÉHICULE DÉSIGNE, et pas « le dernier
				// trouvé » : un véhicule a un historique de contrats, et
				// afficher la redevance du conducteur précédent sur la ligne du
				// suivant est précisément l'erreur qu'on ne verrait pas.
				byAsset[cs[i].AssetID+"|"+cs[i].ID] = feeOf(&cs[i])
			}
			for i := range out {
				if out[i].ContractID == "" {
					continue
				}
				out[i].Fee = byAsset[out[i].ID+"|"+out[i].ContractID]
			}
		}
	}
	if s.accounts != nil && len(holders) > 0 {
		if names, err := s.accounts.UserNames(ctx, holders); err == nil {
			for i := range out {
				out[i].HolderName = names[out[i].HolderUserID]
			}
		}
	}
	for i := range out {
		out[i].Attention = attentionOf(&assets[i], out[i].Fee, now)
	}
	return out
}

func (s *Service) load(ctx context.Context, id string) (*Asset, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errNotFound
	}
	a, err := s.repo.ByID(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if a == nil {
		return nil, errNotFound
	}
	return a, nil
}

// --- le parc ---

// Create fait entrer un véhicule dans la flotte interne.
//
// ⚠️ IL RÉSOUT LA FLOTTE INTERNE DU PAYS ET ÉCRIT SON IDENTIFIANT — c'est le
// piège maison de cette plateforme pris à l'envers. Un `fleet_id` que personne
// ne remplit est resté invisible longtemps parce que les écrans paraissaient
// normaux ; ici, aucun véhicule n'entre au parc sans propriétaire, et
// l'épreuve lit le document écrit.
func (s *Service) Create(ctx context.Context, actorID string, req AssetInput) (*AssetResponse, error) {
	if s.fleets == nil {
		return nil, errNoFleetDesk
	}
	fleetID, err := s.fleets.EnsureInternal(ctx)
	if err != nil {
		return nil, err
	}
	fid, err := primitive.ObjectIDFromHex(fleetID)
	if err != nil {
		return nil, errNoFleetDesk
	}
	plate := normalisePlate(req.Plate)
	if plate == "" {
		return nil, apperr.Validation("plate is required").
			WithMeta(map[string]any{"fields": []string{"plate"}})
	}
	a := &Asset{
		Country: country.FromContext(ctx), FleetID: fid,
		ClassKey: strings.TrimSpace(req.ClassKey), Verticals: req.Verticals,
		Plate: plate, Brand: strings.TrimSpace(req.Brand), Model: strings.TrimSpace(req.Model),
		Color: strings.TrimSpace(req.Color), Year: req.Year, Seats: req.Seats,
		OdometerKm: req.OdometerKm, Photos: req.Photos,
		AcquiredAt: req.AcquiredAt, AcquisitionXOF: req.AcquisitionXOF,
		InsuranceExpiresAt: req.InsuranceExpiresAt, TechnicalExpiresAt: req.TechnicalExpiresAt,
		// ⚠️ UN VÉHICULE ENTRE AU PARC `available`, jamais `assigned` : on ne
		// peut pas enregistrer une voiture déjà confiée sans sa redevance, car
		// ce serait une voiture dehors que rien ne facture.
		Status: StatusAvailable,
		Terms:  termsFrom(req.Terms, Terms{}),
		Notes:  strings.TrimSpace(req.Notes), CreatedBy: actorID,
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, wrap(err)
	}
	out := toAsset(a)
	s.record(ctx, "own_fleet.create", a.ID.Hex(), nil, out)
	return &out, nil
}

// Update change la fiche d'un véhicule.
func (s *Service) Update(ctx context.Context, actorID, id string, req AssetPatch) (*AssetResponse, error) {
	before, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	set, err := patchOf(before, req)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		out := toAsset(before)
		return &out, nil
	}
	after, err := s.repo.Update(ctx, before.ID, set)
	if err != nil {
		return nil, wrap(err)
	}
	if after == nil {
		return nil, errNotFound
	}
	out := toAsset(after)
	s.record(ctx, "own_fleet.update", id, toAsset(before), out)
	return &out, nil
}

// --- l'attribution ---

// Assign CONFIE le véhicule à un agent — chauffeur ou livreur — contre une
// redevance.
//
// ⚠️⚠️ L'ORDRE DES DEUX ÉCRITURES EST LA GARANTIE. On RÉSERVE d'abord le
// véhicule, dans une écriture conditionnelle qui porte déjà l'identifiant du
// contrat à venir ; on ouvre la redevance ensuite. L'inverse aurait laissé un
// contrat vivant sur un véhicule déjà parti — une dette sans voiture —, et un
// simple « est-il libre ? » avant l'écriture aurait laissé passer les deux
// attributions du matin où quelqu'un double-clique.
//
// ⚠️ ET SI LA REDEVANCE NE S'OUVRE PAS, LA RÉSERVATION EST DÉFAITE. Sans ce
// retour en arrière, un refus du moteur — un pays qui n'ouvre pas la location,
// un plan hors bornes — aurait immobilisé le véhicule pour toujours : réservé
// par un contrat qui n'existe pas, et aucun écran pour dire pourquoi.
func (s *Service) Assign(ctx context.Context, actorID, id string, req AssignInput) (*AssetResponse, error) {
	if s.rede == nil {
		return nil, errNoRedevance
	}
	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.Status == StatusRetired {
		return nil, errRetiredTerms
	}
	if !a.Assignable() {
		return nil, errBusy.WithMeta(map[string]any{"status": a.Status})
	}
	// ⚠️ LA VERTICALE DOIT ÊTRE CELLE DU VÉHICULE. Une berline confiée à un
	// livreur serait facturée sur les gains de la LIVRAISON, où elle ne
	// produit rien — et le chauffeur paierait une voiture qui ne travaille pas.
	// Un parc sans verticale déclarée sert les deux (même convention que les
	// audiences d'un article).
	if len(a.Verticals) > 0 && !contains(a.Verticals, req.Vertical) {
		return nil, apperr.Conflict("own_fleet_vertical_not_offered",
			"this vehicle is not offered to this line of business").
			WithMeta(map[string]any{"verticals": a.Verticals})
	}
	uid, err := primitive.ObjectIDFromHex(req.UserID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	// ⚠️ ON CONFIE UNE VOITURE À UN CONDUCTEUR, PAS À UN COMPTE QUELCONQUE. Le
	// rôle est `driver` pour les deux métiers — c'est la verticale qui
	// distingue le livreur du chauffeur. Sans ce contrôle, une voiture serait
	// partie à un compte client : la redevance se serait retenue sur des gains
	// qui n'existent pas, et personne n'aurait su pourquoi elle ne rentrait
	// jamais.
	if s.accounts != nil {
		role, err := s.accounts.RoleOf(ctx, req.UserID)
		if err != nil {
			return nil, err
		}
		if role != "" && role != auth.RoleDriver {
			return nil, apperr.Conflict("own_fleet_not_a_driver",
				"this account is not a driver account").
				WithMeta(map[string]any{"role": role})
		}
	}
	terms := termsFrom(termsInputOf(req), a.Terms)
	if terms.FeeXOF <= 0 {
		return nil, apperr.Validation("a fee is required: set it on the vehicle or on the assignment").
			WithMeta(map[string]any{"fields": []string{"fee_xof"}})
	}
	now := s.now()
	// ⚠️ L'IDENTIFIANT DU CONTRAT EST TIRÉ ICI, avant qu'il existe : c'est lui
	// qui scelle la réservation. Voir `Repository.Reserve`.
	contractID := primitive.NewObjectID()
	reserved, err := s.repo.Reserve(ctx, a.ID, contractID, uid, now)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if reserved == nil {
		// Rien n'a été réservé : une autre attribution est passée entre la
		// lecture et l'écriture. Le refus nomme la raison.
		return nil, errBusy
	}
	out, err := s.rede.OpenAssetRental(ctx, equipment.AssetRentalInput{
		ContractID: contractID, AssetID: a.ID, Label: labelOf(reserved),
		UserID: uid, Vertical: req.Vertical,
		FeeXOF: terms.FeeXOF, DepositXOF: terms.DepositXOF, Days: terms.Days,
		Included: terms.Included, Plan: planWithPeriod(req.Plan, terms.Period),
		Notes: strings.TrimSpace(req.Notes), ActorID: actorID,
	})
	if err != nil {
		// LE RETOUR EN ARRIÈRE.
		if _, rerr := s.repo.Release(ctx, a.ID, contractID, s.now()); rerr != nil {
			// ⚠️ BRUYANT : le véhicule reste réservé par un contrat qui n'a
			// jamais existé, et seule l'exploitation peut le recaler
			// (`POST .../release`). Le taire aurait fait disparaître une
			// voiture du parc sans trace.
			slog.ErrorContext(ctx, "ownfleet: reservation not rolled back",
				"asset_id", a.ID.Hex(), "contract_id", contractID.Hex(), "error", rerr)
		}
		return nil, err
	}
	resp := toAsset(reserved)
	resp.Fee = feeOf(out)
	resp.Attention = attentionOf(reserved, resp.Fee, now)
	if s.accounts != nil {
		if names, nerr := s.accounts.UserNames(ctx, []string{req.UserID}); nerr == nil {
			resp.HolderName = names[req.UserID]
		}
	}
	s.record(ctx, "own_fleet.assign", a.ID.Hex(), toAsset(a), resp)
	return &resp, nil
}

// Release implémente `equipment.AssetDesk` : le contrat s'est terminé, le
// véhicule revient au parc.
//
// ⚠️⚠️ C'EST LE CHEMIN INVERSE, et il est appelé par les TROIS sorties du
// contrat — rendu, impayé constaté, annulé avant la remise. Le piège maison de
// cette plateforme est un rattachement que rien ne défaisait ; ici, un véhicule
// dont le contrat est clos et qui resterait « confié » serait un véhicule que
// l'exploitation ne peut plus donner à personne.
//
// ⚠️ ET IL NE REND PAS D'ERREUR QUAND RIEN N'A ÉTÉ LIBÉRÉ. « Déjà rendu » et
// « déjà reconfié à quelqu'un d'autre » sont les deux cas, et aucun des deux
// n'est une faute : le premier est un rejeu, le second est protégé par la
// condition sur l'identifiant du contrat. Les deux se journalisent.
func (s *Service) Release(ctx context.Context, assetID, contractID, outcome string) error {
	aid, err := primitive.ObjectIDFromHex(assetID)
	if err != nil {
		return apperr.Validation("invalid asset id")
	}
	cid, err := primitive.ObjectIDFromHex(contractID)
	if err != nil {
		return apperr.Validation("invalid contract id")
	}
	freed, err := s.repo.Release(ctx, aid, cid, s.now())
	if err != nil {
		return apperr.Internal(err)
	}
	if !freed {
		slog.InfoContext(ctx, "ownfleet: nothing to release",
			"asset_id", assetID, "contract_id", contractID, "outcome", outcome)
		return nil
	}
	slog.InfoContext(ctx, "ownfleet: vehicle back in the pool",
		"asset_id", assetID, "contract_id", contractID, "outcome", outcome)
	s.record(ctx, "own_fleet.release", assetID, nil, map[string]any{
		"contract_id": contractID, "outcome": outcome,
	})
	return nil
}

// ForceRelease rend un véhicule au parc À LA MAIN — une CORRECTION.
//
// ⚠️⚠️ ELLE EXISTE PARCE QUE LE CHEMIN NORMAL PEUT ÉCHOUER. Le retour d'un
// contrat libère le véhicule « au mieux » : si le registre ne répond pas à cet
// instant, le contrat se ferme et la voiture reste confiée. Sans geste nommé, la
// seule issue connue serait une écriture directe en base — c'est exactement ce
// qui est arrivé au rattachement de flotte des courses avant qu'on lui donne son
// `release-fleet`.
//
// ⚠️ ET ELLE REFUSE DE DÉFAIRE UNE ATTRIBUTION VIVANTE. Un contrat encore
// `active` veut dire que quelqu'un roule et paie : lui retirer le véhicule du
// registre aurait laissé une redevance qui court sur une voiture que le parc
// croit libre — et le prochain exploitant l'aurait confiée à un deuxième
// chauffeur. On ferme le contrat d'abord (retour ou impayé), on recale ensuite.
func (s *Service) ForceRelease(ctx context.Context, actorID, id, reason string) (*AssetResponse, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.ContractID == nil {
		return nil, errNotAssigned
	}
	if s.rede != nil {
		cs, _, err := s.rede.ListContracts(ctx,
			equipment.ContractFilter{AssetIDs: []primitive.ObjectID{a.ID},
				Statuses: []string{equipment.StatusDraft, equipment.StatusAccepted, equipment.StatusActive}},
			httpx.Page{Limit: 5})
		if err != nil {
			return nil, err
		}
		for i := range cs {
			if cs[i].ID == a.ContractID.Hex() {
				return nil, apperr.Conflict("own_fleet_contract_open",
					"close the fee contract first (return it or record the default)").
					WithMeta(map[string]any{"contract_id": cs[i].ID, "status": cs[i].Status})
			}
		}
	}
	freed, err := s.repo.Release(ctx, a.ID, *a.ContractID, s.now())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if !freed {
		return nil, errNotAssigned
	}
	// ⚠️ JOURNALISÉ AVEC LE MOTIF : ce geste change ce que le parc montre, et
	// « pourquoi cette voiture est-elle revenue au parc ? » doit avoir une
	// réponse six mois plus tard.
	slog.InfoContext(ctx, "ownfleet: vehicle released by hand",
		"asset_id", a.ID.Hex(), "plate", a.Plate, "contract_id", a.ContractID.Hex(), "reason", reason)
	s.record(ctx, "own_fleet.force_release", a.ID.Hex(), toAsset(a), map[string]any{"reason": reason})
	return s.Get(ctx, id)
}

// --- les règles pures ---

// termsInputOf traduit les conditions d'une attribution en conditions tout
// court.
func termsInputOf(req AssignInput) *TermsInput {
	out := &TermsInput{Period: req.Period}
	if req.FeeXOF != nil {
		out.FeeXOF = *req.FeeXOF
	}
	if req.DepositXOF != nil {
		out.DepositXOF = *req.DepositXOF
	}
	if req.Days != nil {
		out.Days = *req.Days
	}
	if req.Included != nil {
		// ⚠️ UNE LISTE VIDE EXPLICITE VEUT DIRE « RIEN N'EST INCLUS », et elle
		// doit l'emporter sur les conditions du véhicule. Confondre « je n'ai
		// rien dit » avec « rien n'est inclus » aurait offert l'entretien à
		// quelqu'un qui ne l'a pas négocié — ou l'inverse, qui se découvre le
		// jour de la panne. C'est pourquoi le champ est un POINTEUR.
		out.Included = *req.Included
		if out.Included == nil {
			out.Included = []string{}
		}
	}
	return out
}

// termsFrom fait tomber les conditions en cascade : ce qui est demandé, puis le
// défaut.
//
// ⚠️ ZÉRO VEUT DIRE « NON RENSEIGNÉ » POUR UN MONTANT, et c'est acceptable ici
// parce qu'une redevance nulle n'existe pas : un véhicule prêté gratuitement se
// dit par un plan sans échéance, pas par un loyer de zéro. La garde de `Assign`
// refuse d'ailleurs une redevance nulle après la cascade.
func termsFrom(in *TermsInput, fallback Terms) Terms {
	out := fallback
	if in == nil {
		return out
	}
	if in.FeeXOF > 0 {
		out.FeeXOF = in.FeeXOF
	}
	if in.Period != "" {
		out.Period = in.Period
	}
	if in.DepositXOF > 0 {
		out.DepositXOF = in.DepositXOF
	}
	if in.Days > 0 {
		out.Days = in.Days
	}
	if in.Included != nil {
		out.Included = in.Included
	}
	if out.Period == "" {
		// ⚠️ LA SEMAINE PAR DÉFAUT, comme le matériel : c'est le pas que le
		// terrain pratique, et le même défaut que `fitPlanToMode` appliquerait
		// de toute façon. Le poser ici rend la condition LISIBLE sur la fiche
		// plutôt que devinée plus loin.
		out.Period = equipment.PeriodWeekly
	}
	return out
}

// planWithPeriod pose le pas de la redevance dans le plan du contrat.
//
// ⚠️ LA PÉRIODE DU PLAN EST CELLE DE LA REDEVANCE, et il ne peut pas y en avoir
// deux. Le plan porte le pas des échéances, la fiche porte celui du loyer :
// laisser les deux divergerait silencieusement — une redevance « mensuelle »
// prélevée chaque semaine est quatre fois trop.
func planWithPeriod(p *equipment.Plan, period string) *equipment.Plan {
	out := equipment.Plan{}
	if p != nil {
		out = *p
	}
	out.Period = period
	return &out
}

// patchOf construit l'écriture d'une fiche.
//
// ⚠️⚠️ IL NE TOUCHE JAMAIS À L'ATTRIBUTION — `contract_id`, `holder_user_id`,
// `assigned_at` — ni ne pose `assigned`. Ces champs n'ont que deux auteurs,
// `Reserve` et `Release`. Un champ qu'on peut aussi écrire depuis une fiche
// finit par dire autre chose que le contrat qu'il désigne, et c'est l'écran qui
// a l'air normal.
//
// ⚠️ ET LE STATUT D'UN VÉHICULE CONFIÉ NE SE CHANGE PAS. Poser `maintenance`
// sur une voiture dont la redevance court aurait arrêté le parc sans arrêter le
// loyer : l'agent continuerait de payer un véhicule que la console affiche au
// garage.
func patchOf(before *Asset, req AssetPatch) (bson.M, error) {
	set := bson.M{}
	if req.Status != nil {
		if *req.Status != StatusAvailable && *req.Status != StatusMaintenance && *req.Status != StatusRetired {
			return nil, errBadStatus
		}
		if before.ContractID != nil {
			return nil, apperr.Conflict("own_fleet_vehicle_assigned",
				"this vehicle is assigned: close its fee contract before changing its status").
				WithMeta(map[string]any{"contract_id": before.ContractID.Hex()})
		}
		set["status"] = *req.Status
	}
	if req.ClassKey != nil {
		v := strings.TrimSpace(*req.ClassKey)
		if v == "" {
			return nil, apperr.Validation("class_key is required").
				WithMeta(map[string]any{"fields": []string{"class_key"}})
		}
		set["class_key"] = v
	}
	if req.Verticals != nil {
		set["verticals"] = *req.Verticals
	}
	if req.Plate != nil {
		v := normalisePlate(*req.Plate)
		if v == "" {
			return nil, apperr.Validation("plate is required").
				WithMeta(map[string]any{"fields": []string{"plate"}})
		}
		set["plate"] = v
	}
	if req.Brand != nil {
		set["brand"] = strings.TrimSpace(*req.Brand)
	}
	if req.Model != nil {
		set["model"] = strings.TrimSpace(*req.Model)
	}
	if req.Color != nil {
		set["color"] = strings.TrimSpace(*req.Color)
	}
	if req.Year != nil {
		set["year"] = *req.Year
	}
	if req.Seats != nil {
		set["seats"] = *req.Seats
	}
	if req.OdometerKm != nil {
		set["odometer_km"] = *req.OdometerKm
	}
	if req.Photos != nil {
		set["photos"] = *req.Photos
	}
	if req.AcquiredAt != nil {
		set["acquired_at"] = *req.AcquiredAt
	}
	if req.AcquisitionXOF != nil {
		set["acquisition_xof"] = *req.AcquisitionXOF
	}
	if req.InsuranceExpiresAt != nil {
		set["insurance_expires_at"] = *req.InsuranceExpiresAt
	}
	if req.TechnicalExpiresAt != nil {
		set["technical_expires_at"] = *req.TechnicalExpiresAt
	}
	if req.Notes != nil {
		set["notes"] = strings.TrimSpace(*req.Notes)
	}
	if req.Terms != nil {
		// ⚠️ LES CONDITIONS SE REMPLACENT EN BLOC, et c'est assumé : elles se
		// lisent ensemble (un loyer sans son pas ne veut rien dire). Mais elles
		// ne changent QUE la fiche — les contrats déjà ouverts gardent ce qui
		// a été figé à leur ouverture. « Une écriture dont le montant bouge
		// n'est pas une écriture. »
		set["terms"] = termsFrom(req.Terms, Terms{})
	}
	return set, nil
}

// attentionOf dit ce qui demande un geste.
//
// ⚠️ CALCULÉ PAR LE SERVEUR ET SERVI AUX DEUX ÉCRANS. Deux écrans qui le
// calculent eux-mêmes ne comptent jamais pareil, et la console finirait par
// annoncer un impayé que l'application de l'agent ne montre pas — c'est à ce
// moment-là qu'il appelle le support en étant certain d'avoir payé.
func attentionOf(a *Asset, fee *FeeResponse, now time.Time) []string {
	out := []string{}
	if a.Status == StatusRetired {
		return out
	}
	if a.ContractID != nil && fee == nil {
		// ⚠️ CONFIÉ SANS REDEVANCE LISIBLE : ce n'est pas rien, c'est le signe
		// qu'un contrat manque ou qu'un retour en arrière a échoué. Le taire
		// aurait laissé une voiture dehors que rien ne facture, sur un écran
		// parfaitement normal.
		out = append(out, AttentionNoContract)
	}
	if fee != nil {
		if fee.OverdueSince != nil {
			out = append(out, AttentionOverdue)
		}
		if fee.Blocked {
			out = append(out, AttentionBlocked)
		}
		if fee.TermOver {
			out = append(out, AttentionTermOver)
		}
		if fee.HandedAt == nil && (fee.Status == equipment.StatusDraft || fee.Status == equipment.StatusAccepted) {
			out = append(out, AttentionNotHanded)
		}
	}
	if a.InsuranceExpiresAt != nil && !a.InsuranceExpiresAt.After(now) {
		out = append(out, AttentionInsurance)
	}
	if a.TechnicalExpiresAt != nil && !a.TechnicalExpiresAt.After(now) {
		out = append(out, AttentionTechnical)
	}
	return out
}

// labelOf nomme un véhicule sur une ligne et sur un contrat.
//
// ⚠️ LA PLAQUE EN DERNIER ET TOUJOURS PRÉSENTE : c'est elle qui identifie la
// voiture devant le comptoir. Une marque et un modèle manquants laissent un
// label utilisable ; une plaque manquante ne peut pas arriver (elle est
// obligatoire).
func labelOf(a *Asset) string {
	parts := make([]string, 0, 3)
	if v := strings.TrimSpace(a.Brand + " " + a.Model); v != "" {
		parts = append(parts, v)
	}
	parts = append(parts, a.Plate)
	return strings.Join(parts, " · ")
}

// normalisePlate met une plaque sous sa forme comparable.
//
// ⚠️ MAJUSCULES ET ESPACES RÉDUITS, MAIS LES SÉPARATEURS GARDÉS. L'unicité par
// pays repose sur cette forme : « ga 1234 ab » et « GA 1234 AB » sont la même
// voiture, et laisser les deux entrer aurait fait deux lignes pour un véhicule
// — l'une attribuée, l'autre « disponible » pour toujours.
func normalisePlate(raw string) string {
	return strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(raw))), " ")
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func wrap(err error) error {
	if apperr.From(err).Code != "internal" {
		return err
	}
	return apperr.Internal(err)
}
