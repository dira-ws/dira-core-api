package payroll

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

var (
	errEmployeeNotFound   = apperr.NotFound("payroll_employee_not_found", "employee not found")
	errAdjustmentNotFound = apperr.NotFound("payroll_adjustment_not_found", "bonus or deduction not found")
	errRunNotFound        = apperr.NotFound("payroll_run_not_found", "payroll run not found")
	// errRunExists est le refus du DOUBLON DE PÉRIODE. Voir `Indexes`.
	errRunExists = apperr.Conflict("payroll_run_exists",
		"a payroll already covers this period for this frequency")
	// errRunMoved est le refus d'un geste posé sur un état qui a changé entre
	// la lecture et l'écriture — c'est lui qui empêche un DOUBLE VERSEMENT.
	errRunMoved = apperr.Conflict("payroll_run_moved",
		"this payroll is no longer in the expected state: reload it")
)

type Repository struct {
	employees   *mongo.Collection
	adjustments *mongo.Collection
	runs        *mongo.Collection
	settings    *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{
		employees:   m.Collection(collectionEmployees),
		adjustments: m.Collection(collectionAdjustments),
		runs:        m.Collection(collectionRuns),
		settings:    m.Collection(collectionSettings),
	}
}

// ⚠️ LES INDEX DE CE MODULE SONT DÉCLARÉS DANS `internal/indexes`, et NULLE
// PART AILLEURS. Les recopier ici aurait fait deux listes : celle qui est
// réellement posée au démarrage, et celle qu'on relit en se demandant pourquoi
// une requête est lente. Les quatre collections de la paie y ont leur section,
// avec le verrou de période et son explication.

// --- salariés ---

func (r *Repository) InsertEmployee(ctx context.Context, e *Employee) error {
	e.ID = primitive.NewObjectID()
	now := time.Now().UTC()
	e.CreatedAt, e.UpdatedAt = now, now
	if _, err := r.employees.InsertOne(ctx, e); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperr.Conflict("payroll_account_taken",
				"this account already has a payroll record")
		}
		return fmt.Errorf("payroll: insert employee: %w", err)
	}
	return nil
}

func (r *Repository) EmployeeByID(ctx context.Context, id primitive.ObjectID) (*Employee, error) {
	var e Employee
	if err := r.employees.FindOne(ctx, country.Restrict(ctx, bson.M{"_id": id})).Decode(&e); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errEmployeeNotFound
		}
		return nil, fmt.Errorf("payroll: find employee: %w", err)
	}
	return &e, nil
}

func (r *Repository) SaveEmployee(ctx context.Context, e *Employee) error {
	e.UpdatedAt = time.Now().UTC()
	res, err := r.employees.ReplaceOne(ctx, bson.M{"_id": e.ID}, e)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperr.Conflict("payroll_account_taken",
				"this account already has a payroll record")
		}
		return fmt.Errorf("payroll: save employee: %w", err)
	}
	if res.MatchedCount == 0 {
		return errEmployeeNotFound
	}
	return nil
}

// EmployeeFilter borne une liste de salariés.
type EmployeeFilter struct {
	Status    string
	Frequency string
	Q         string
}

func (r *Repository) ListEmployees(ctx context.Context, f EmployeeFilter, limit int, cursor string) ([]Employee, string, error) {
	filter := country.Restrict(ctx, bson.M{})
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Frequency != "" {
		filter["frequency"] = f.Frequency
	}
	if f.Q != "" {
		// Recherche sur le nom et le poste. `$options: "i"` plutôt qu'un index
		// textuel : la table est petite (le personnel d'une entreprise), et un
		// index textuel aurait imposé une langue.
		filter["$or"] = []bson.M{
			{"full_name": bson.M{"$regex": primitive.Regex{Pattern: escapeRegex(f.Q), Options: "i"}}},
			{"position": bson.M{"$regex": primitive.Regex{Pattern: escapeRegex(f.Q), Options: "i"}}},
		}
	}
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			if err := db.AfterNewest(ctx, r.employees, filter, cid); err != nil {
				return nil, "", err
			}
		}
	}
	cur, err := r.employees.Find(ctx, filter,
		options.Find().SetSort(db.NewestFirst).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", fmt.Errorf("payroll: list employees: %w", err)
	}
	var out []Employee
	if err := cur.All(ctx, &out); err != nil {
		return nil, "", fmt.Errorf("payroll: list employees decode: %w", err)
	}
	if len(out) <= limit {
		return out, "", nil
	}
	out = out[:limit]
	return out, out[limit-1].ID.Hex(), nil
}

// PayableEmployees rend les salariés ACTIFS d'une fréquence — ceux qu'une paie
// doit couvrir.
//
// ⚠️ SANS PAGINATION, et c'est un choix borné : il s'agit du personnel d'une
// entreprise dans UN pays, pas d'une population d'utilisateurs. Une limite
// haute protège d'une erreur de données sans découper une paie en deux, ce qui
// aurait été bien pire — la moitié des gens payés.
func (r *Repository) PayableEmployees(ctx context.Context, frequency string) ([]Employee, error) {
	filter := country.Restrict(ctx, bson.M{"frequency": frequency, "status": StatusActive})
	cur, err := r.employees.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "full_name", Value: 1}}).SetLimit(2000))
	if err != nil {
		return nil, fmt.Errorf("payroll: payable employees: %w", err)
	}
	var out []Employee
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("payroll: payable employees decode: %w", err)
	}
	return out, nil
}

// --- primes et retenues ---

func (r *Repository) InsertAdjustment(ctx context.Context, a *Adjustment) error {
	a.ID = primitive.NewObjectID()
	now := time.Now().UTC()
	a.CreatedAt, a.UpdatedAt = now, now
	if _, err := r.adjustments.InsertOne(ctx, a); err != nil {
		return fmt.Errorf("payroll: insert adjustment: %w", err)
	}
	return nil
}

func (r *Repository) AdjustmentByID(ctx context.Context, id primitive.ObjectID) (*Adjustment, error) {
	var a Adjustment
	if err := r.adjustments.FindOne(ctx, country.Restrict(ctx, bson.M{"_id": id})).Decode(&a); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errAdjustmentNotFound
		}
		return nil, fmt.Errorf("payroll: find adjustment: %w", err)
	}
	return &a, nil
}

func (r *Repository) SaveAdjustment(ctx context.Context, a *Adjustment) error {
	a.UpdatedAt = time.Now().UTC()
	res, err := r.adjustments.ReplaceOne(ctx, bson.M{"_id": a.ID}, a)
	if err != nil {
		return fmt.Errorf("payroll: save adjustment: %w", err)
	}
	if res.MatchedCount == 0 {
		return errAdjustmentNotFound
	}
	return nil
}

// AdjustmentsOfEmployee rend les primes et retenues d'une personne, les plus
// anciennes d'abord — l'ordre dans lequel elles passent sur une paie.
func (r *Repository) AdjustmentsOfEmployee(ctx context.Context, employeeID primitive.ObjectID, status string) ([]Adjustment, error) {
	filter := country.Restrict(ctx, bson.M{"employee_id": employeeID})
	if status != "" {
		filter["status"] = status
	}
	cur, err := r.adjustments.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(500))
	if err != nil {
		return nil, fmt.Errorf("payroll: adjustments of employee: %w", err)
	}
	var out []Adjustment
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("payroll: adjustments of employee decode: %w", err)
	}
	return out, nil
}

// ClaimAdjustments RÉSERVE pour une paie les primes et retenues ouvertes des
// salariés donnés, et rend celles qu'elle a effectivement obtenues.
//
// ⚠️ C'EST UNE RÉSERVATION, ET C'EST CE QUI EMPÊCHE DE PAYER UNE PRIME DEUX
// FOIS. Sans elle, deux préparations lancées côte à côte — deux onglets, un
// double clic — mettraient la même prime de 50 000 F sur deux paies, et les
// deux seraient versées : chacune serait juste, prise séparément.
//
// ⚠️ UNE SEULE ÉCRITURE CONDITIONNELLE. Le filtre exige `run_id` ABSENT ; la
// seconde préparation ne trouve donc rien à réserver, au lieu de constater
// trop tard que quelqu'un l'a précédée. Lire puis écrire en deux temps aurait
// laissé la fenêtre grande ouverte, et un double clic suffit à la trouver.
//
// L'annulation d'une paie LIBÈRE ses réservations (`ReleaseAdjustments`) : une
// prime retenue par un brouillon abandonné serait perdue pour toujours.
func (r *Repository) ClaimAdjustments(ctx context.Context, runID primitive.ObjectID, employees []primitive.ObjectID, periodEnd time.Time) ([]Adjustment, error) {
	if len(employees) == 0 {
		return nil, nil
	}
	filter := country.Restrict(ctx, bson.M{
		"employee_id": bson.M{"$in": employees},
		"status":      AdjustmentOpen,
		"run_id":      bson.M{"$exists": false},
		// La date d'effet se compare à la FIN de période — voir `eligible`.
		"$or": []bson.M{
			{"effective_from": bson.M{"$exists": false}},
			{"effective_from": bson.M{"$lte": periodEnd}},
		},
	})
	if _, err := r.adjustments.UpdateMany(ctx, filter,
		bson.M{"$set": bson.M{"run_id": runID, "updated_at": time.Now().UTC()}}); err != nil {
		return nil, fmt.Errorf("payroll: claim adjustments: %w", err)
	}
	// Relire CE QUI EST RÉSERVÉ PAR CETTE PAIE : la mise à jour ne rend pas les
	// documents, et calculer la fiche depuis ce qu'on croyait réserver ferait
	// entrer une prime qu'une autre paie avait déjà prise.
	cur, err := r.adjustments.Find(ctx, bson.M{"run_id": runID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("payroll: claimed adjustments: %w", err)
	}
	var out []Adjustment
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("payroll: claimed adjustments decode: %w", err)
	}
	return out, nil
}

// AdjustmentsOfRun rend les primes et retenues RÉSERVÉES par une paie.
//
// ⚠️ BORNÉE PAR `run_id` ET PAR RIEN D'AUTRE : l'identifiant de la paie est
// plus étroit qu'un pays, et la paie, elle, a déjà été lue avec sa borne.
func (r *Repository) AdjustmentsOfRun(ctx context.Context, runID primitive.ObjectID) ([]Adjustment, error) {
	cur, err := r.adjustments.Find(ctx, bson.M{"run_id": runID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("payroll: adjustments of run: %w", err)
	}
	var out []Adjustment
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("payroll: adjustments of run decode: %w", err)
	}
	return out, nil
}

// ReleaseAdjustments rend à la file ce qu'une paie avait réservé.
func (r *Repository) ReleaseAdjustments(ctx context.Context, runID primitive.ObjectID) error {
	_, err := r.adjustments.UpdateMany(ctx, bson.M{"run_id": runID, "status": AdjustmentOpen},
		bson.M{"$unset": bson.M{"run_id": ""}, "$set": bson.M{"updated_at": time.Now().UTC()}})
	if err != nil {
		return fmt.Errorf("payroll: release adjustments: %w", err)
	}
	return nil
}

// ConsumeAdjustments inscrit sur chaque prime et chaque retenue ce que le
// VERSEMENT en a fait.
//
// ⚠️ AU VERSEMENT, ET NON À LA PRÉPARATION. Un brouillon peut être abandonné :
// une retenue comptée comme « prise » par une paie jamais versée aurait disparu
// des suivantes, et la dette se serait effacée toute seule.
// incOf rend ce qu'un passage de paie incrémente sur son ajustement.
//
// ⚠️ UNE ÉCHÉANCE NE SE CONSOMME QUE SI ELLE A PRIS QUELQUE CHOSE. Le compteur
// `runs` est ce qui épuise une retenue en pourcentage (`eligible`) et ce qui
// répartit le reste d'un échéancier (`plannedShare`) : l'incrémenter sur un
// passage borné à zéro par le plancher faisait disparaître une retenue qui
// n'avait rien prélevé — voir `settledAfter`.
func incOf(ap Applied) bson.M {
	inc := bson.M{"taken": ap.Amount}
	if ap.Amount > 0 {
		inc["runs"] = 1
	}
	return inc
}

func (r *Repository) ConsumeAdjustments(ctx context.Context, applied []Applied, at time.Time) error {
	for _, ap := range applied {
		set := bson.M{"updated_at": at}
		if ap.Settled {
			set["status"] = AdjustmentSettled
			set["settled_at"] = at
		}
		update := bson.M{
			"$inc": incOf(ap),
			"$set": set,
			// La réservation est consommée : l'ajustement n'appartient plus à
			// une paie en cours.
			"$unset": bson.M{"run_id": ""},
		}
		if _, err := r.adjustments.UpdateOne(ctx, bson.M{"_id": ap.AdjustmentID}, update); err != nil {
			return fmt.Errorf("payroll: consume adjustment %s: %w", ap.AdjustmentID.Hex(), err)
		}
	}
	return nil
}

// --- paies ---

func (r *Repository) InsertRun(ctx context.Context, run *Run) error {
	run.ID = primitive.NewObjectID()
	now := time.Now().UTC()
	run.CreatedAt, run.UpdatedAt = now, now
	if run.Slips == nil {
		run.Slips = []Payslip{}
	}
	if _, err := r.runs.InsertOne(ctx, run); err != nil {
		// ⚠️ LE DOUBLON SE LIT ICI AUSSI, pas seulement dans le contrôle qui
		// précède : entre « est-ce déjà préparé ? » et l'insertion, une autre
		// console a eu le temps de le faire. L'index est le verrou, ce code en
		// est la traduction lisible.
		if mongo.IsDuplicateKeyError(err) {
			return errRunExists
		}
		return fmt.Errorf("payroll: insert run: %w", err)
	}
	return nil
}

func (r *Repository) RunByID(ctx context.Context, id primitive.ObjectID) (*Run, error) {
	var run Run
	if err := r.runs.FindOne(ctx, country.Restrict(ctx, bson.M{"_id": id})).Decode(&run); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errRunNotFound
		}
		return nil, fmt.Errorf("payroll: find run: %w", err)
	}
	return &run, nil
}

// CountRunsForPeriod dit si une paie couvre DÉJÀ cette période.
//
// ⚠️ ELLE COMPTE, ET ELLE EST POSÉE EN CLAIR AVANT D'ÉCRIRE. C'est la leçon de
// l'incident des rapports : s'appuyer sur un upsert filtré laissait insérer un
// doublon quand le filtre ne couvrait pas l'état d'entrée. Ici, la question est
// posée pour pouvoir RÉPONDRE proprement — l'index reste le verrou.
func (r *Repository) CountRunsForPeriod(ctx context.Context, frequency string, periodStart time.Time) (int64, error) {
	n, err := r.runs.CountDocuments(ctx, country.Restrict(ctx, bson.M{
		"frequency":    frequency,
		"period_start": periodStart,
		"status":       bson.M{"$in": []string{RunDraft, RunApproved, RunPaid}},
	}))
	if err != nil {
		return 0, fmt.Errorf("payroll: count runs for period: %w", err)
	}
	return n, nil
}

// SaveRunSlips écrit les fiches et les totaux d'une paie EN BROUILLON.
//
// ⚠️ LE FILTRE PORTE LE STATUT. Une paie approuvée ou versée n'est jamais
// réécrite : sans cette condition, une préparation qui se termine en retard
// pourrait écraser des montants déjà versés.
func (r *Repository) SaveRunSlips(ctx context.Context, run *Run) error {
	res, err := r.runs.UpdateOne(ctx,
		bson.M{"_id": run.ID, "status": RunDraft},
		bson.M{"$set": bson.M{
			"slips": run.Slips, "gross": run.Gross, "bonuses": run.Bonuses,
			"deductions": run.Deductions, "net": run.Net, "carried": run.Carried,
			"headcount": run.Headcount, "updated_at": time.Now().UTC(),
		}})
	if err != nil {
		return fmt.Errorf("payroll: save run slips: %w", err)
	}
	if res.MatchedCount == 0 {
		return errRunMoved
	}
	return nil
}

// MoveRun fait passer une paie d'un statut à un autre, EN UNE SEULE ÉCRITURE.
//
// ⚠️ C'EST LA PIÈCE QUI EMPÊCHE UN DOUBLE VERSEMENT, et c'est pour cela qu'elle
// ne ressemble pas à un `Save`. Lire la paie, constater qu'elle est
// « approuvée », puis l'écrire « versée » laisse passer DEUX versements quand
// deux doigts appuient en même temps — et le second ne se voit nulle part,
// puisque chaque écriture est valide. Le filtre porte donc le statut ATTENDU :
// la seconde tentative ne trouve plus rien et reçoit un refus.
func (r *Repository) MoveRun(ctx context.Context, id primitive.ObjectID, from, to string, set bson.M) (*Run, error) {
	if set == nil {
		set = bson.M{}
	}
	set["status"] = to
	set["updated_at"] = time.Now().UTC()
	var run Run
	err := r.runs.FindOneAndUpdate(ctx,
		country.Restrict(ctx, bson.M{"_id": id, "status": from}),
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&run)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return nil, errRunMoved
	case err != nil:
		return nil, fmt.Errorf("payroll: move run %s→%s: %w", from, to, err)
	}
	return &run, nil
}

// RunFilter borne une liste de paies.
type RunFilter struct {
	Status     string
	Frequency  string
	EmployeeID *primitive.ObjectID
}

func (r *Repository) ListRuns(ctx context.Context, f RunFilter, limit int, cursor string) ([]Run, string, error) {
	filter := country.Restrict(ctx, bson.M{})
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Frequency != "" {
		filter["frequency"] = f.Frequency
	}
	if f.EmployeeID != nil {
		filter["slips.employee_id"] = *f.EmployeeID
	}
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			if err := db.AfterNewest(ctx, r.runs, filter, cid); err != nil {
				return nil, "", err
			}
		}
	}
	cur, err := r.runs.Find(ctx, filter,
		options.Find().SetSort(db.NewestFirst).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", fmt.Errorf("payroll: list runs: %w", err)
	}
	var out []Run
	if err := cur.All(ctx, &out); err != nil {
		return nil, "", fmt.Errorf("payroll: list runs decode: %w", err)
	}
	if len(out) <= limit {
		return out, "", nil
	}
	out = out[:limit]
	return out, out[limit-1].ID.Hex(), nil
}

// --- réglages ---

func (r *Repository) Settings(ctx context.Context, code string) (*Settings, error) {
	var s Settings
	err := r.settings.FindOne(ctx, bson.M{"_id": code}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		d := DefaultSettings(code)
		return &d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("payroll: settings: %w", err)
	}
	return &s, nil
}

func (r *Repository) SaveSettings(ctx context.Context, s *Settings) error {
	s.UpdatedAt = time.Now().UTC()
	_, err := r.settings.ReplaceOne(ctx, bson.M{"_id": s.Country}, s, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("payroll: save settings: %w", err)
	}
	return nil
}

// escapeRegex neutralise ce qu'une recherche tapée pourrait porter de
// significatif pour Mongo.
//
// ⚠️ SANS ELLE, UN NOM AVEC UNE PARENTHÈSE FAIT ÉCHOUER LA RECHERCHE, et une
// expression tapée exprès fait travailler la base pour rien. Une recherche de
// console doit chercher du TEXTE, pas exécuter un motif.
func escapeRegex(s string) string {
	const special = `\.+*?()|[]{}^$`
	out := make([]rune, 0, len(s)+8)
	for _, r := range s {
		for _, bad := range special {
			if r == bad {
				out = append(out, '\\')
				break
			}
		}
		out = append(out, r)
	}
	return string(out)
}
