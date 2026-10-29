package finance

// LE SERVICE DES DÉCLARATIONS : déclarer, lister, contre-passer, synthétiser,
// et régler le catalogue des catégories du pays.

import (
	"context"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

var (
	errDeclarationAlreadyReversed = apperr.Conflict("declaration_not_posted", "declaration is already reversed")
	errCorrectsNotReversed        = apperr.Conflict("corrects_not_reversed", "the corrected declaration must be reversed first")
	errCorrectsAlreadyCorrected   = apperr.Conflict("corrects_already_corrected", "that declaration has already been corrected")
)

// decl rend le stockage des déclarations — la doublure des épreuves, ou le
// dépôt Mongo.
func (s *Service) decl() declarationStore {
	if s.declStore != nil {
		return s.declStore
	}
	return s.repo
}

// --- le catalogue des catégories ---

// DeclarationCategories rend le catalogue du pays de la requête.
func (s *Service) DeclarationCategories(ctx context.Context) (*DeclarationCatalog, error) {
	cc := country.FromContext(ctx)
	c, err := s.decl().Catalog(ctx, cc)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if len(c.Categories) == 0 {
		c.Categories = DefaultDeclarationCategories()
	}
	c.Country = cc
	return c, nil
}

// SaveDeclarationCategories REMPLACE le catalogue du pays.
//
// ⚠️ IL REMPLACE, IL NE FUSIONNE PAS, et l'écriture est donc toujours la
// liste ENTIÈRE. Un réglage « partiel » sur une liste n'a pas de sens — on ne
// saurait pas dire si une catégorie absente est inchangée ou supprimée — et
// la plateforme s'est déjà fait effacer trois prix par un PUT partiel ailleurs.
// Une catégorie dont on ne veut plus se DÉSACTIVE (`disabled`) : la supprimer
// ne retire rien aux déclarations déjà faites (elles portent leur libellé et
// leur compte), mais rend la console muette sur ce qu'elles étaient.
func (s *Service) SaveDeclarationCategories(ctx context.Context, actorID string, cats []DeclarationCategory) (*DeclarationCatalog, error) {
	cc := country.FromContext(ctx)
	if cc == "" {
		return nil, apperr.Validation("a country is required")
	}
	clean, err := validateCategories(cats)
	if err != nil {
		return nil, err
	}
	before, err := s.decl().Catalog(ctx, cc)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	next := &DeclarationCatalog{Country: cc, Categories: clean, UpdatedBy: actorID, UpdatedAt: ptrTime(s.now())}
	if err := s.decl().SaveCatalog(ctx, next); err != nil {
		return nil, apperr.Internal(err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, "finance.declaration_categories.update", "declaration_categories", cc, before, next)
	}
	return next, nil
}

// validateCategories relit une liste de catégories.
func validateCategories(cats []DeclarationCategory) ([]DeclarationCategory, error) {
	bad := func(reason string) error {
		return apperr.Validation("invalid declaration categories").WithMeta(map[string]any{"reason": reason})
	}
	if len(cats) == 0 {
		return nil, bad("at least one category is required")
	}
	if len(cats) > 60 {
		return nil, bad("at most 60 categories")
	}
	seen := map[string]bool{}
	out := make([]DeclarationCategory, 0, len(cats))
	for _, c := range cats {
		c.Code = strings.ToLower(strings.TrimSpace(c.Code))
		c.Label = strings.TrimSpace(c.Label)
		c.Account = strings.TrimSpace(c.Account)
		if !validCategoryCode(c.Code) {
			return nil, bad("a category code is 2 to 40 characters of a-z, 0-9 and _ : " + c.Code)
		}
		if seen[c.Code] {
			return nil, bad("duplicate category code: " + c.Code)
		}
		seen[c.Code] = true
		if c.Label == "" || len([]rune(c.Label)) > 80 {
			return nil, bad("a category needs a label of at most 80 characters: " + c.Code)
		}
		switch c.Direction {
		case DeclarationIn, DeclarationOut, DeclarationBoth:
		default:
			return nil, bad("direction must be in, out or both: " + c.Code)
		}
		// ⚠️ LA RÈGLE 1 EST ICI, à l'écriture du catalogue. La tenir au moment
		// de déclarer aurait laissé entrer une catégorie fautive et n'aurait
		// crié qu'au premier usage — devant quelqu'un qui saisit une facture.
		if !declarableAccounts[c.Account] {
			return nil, bad("account is not declarable (see GET /admin/finance/declaration-categories): " + c.Account)
		}
		out = append(out, c)
	}
	return out, nil
}

func validCategoryCode(code string) bool {
	if n := len(code); n < 2 || n > 40 {
		return false
	}
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}

// --- déclarer ---

// DeclareInput : ce qu'une déclaration demande.
//
// ⚠️ NI `source` NI `account` NE SONT ICI, et c'est volontaire. Le compte
// vient de la CATÉGORIE (que l'exploitation règle une fois, sous contrôle) et
// la provenance vient du chemin d'appel : laisser l'un ou l'autre au corps de
// la requête aurait permis de poser une écriture sur n'importe quel compte
// depuis la console, ou de faire passer une saisie manuelle pour une écriture
// de la paie.
type DeclareInput struct {
	Direction     string `json:"direction" validate:"required,oneof=in out"`
	Category      string `json:"category" validate:"required,max=40"`
	Means         string `json:"means" validate:"required,oneof=cash bank mobile_money"`
	Amount        int    `json:"amount" validate:"required,min=1"`
	Currency      string `json:"currency" validate:"omitempty,len=3"`
	ValueDate     string `json:"value_date" validate:"required"`
	Counterparty  string `json:"counterparty" validate:"required,min=2,max=160"`
	Reference     string `json:"reference" validate:"omitempty,max=60"`
	Justification string `json:"justification" validate:"required,min=5,max=600"`
	AttachmentURL string `json:"attachment_url" validate:"omitempty,url,max=500"`
	// Corrects : la déclaration contre-passée que celle-ci remplace.
	Corrects string `json:"corrects" validate:"omitempty,len=24"`
}

// Declare : une déclaration SAISIE par l'exploitation.
func (s *Service) Declare(ctx context.Context, actorID string, in DeclareInput) (*Declaration, error) {
	return s.declare(ctx, actorID, SourceConsole, "", in)
}

// DeclareFromModule : LA COUTURE laissée aux autres modules de la plateforme.
//
// ⚠️ ELLE EXISTE POUR QUE LA PAIE N'AIT RIEN À SAVOIR DE LA COMPTABILITÉ, et
// la comptabilité rien à savoir de la paie. Un module de paie qui verse un
// salaire appelle ceci avec `source = SourcePayroll`, `sourceRef =
// <identifiant de son bulletin>` et les faits du mouvement (sortie, catégorie
// `salaries`, moyen, montant, date de valeur, bénéficiaire, justificatif) ; il
// obtient une déclaration ORDINAIRE — même écriture, même console, même
// contre-passation. Aucun import croisé, aucune table partagée.
//
// ⚠️ `sourceRef` EST LA GARANTIE D'IDEMPOTENCE : un index unique (pays,
// source, source_ref) refuse la seconde déclaration du même bulletin, et
// l'appelant reçoit `declaration_duplicate`. Ce n'est pas un échec de son
// point de vue — le mouvement est déjà déclaré. Un module qui appellerait
// sans `sourceRef` perdrait cette garantie, donc il est exigé.
func (s *Service) DeclareFromModule(ctx context.Context, source, sourceRef, actorID string, in DeclareInput) (*Declaration, error) {
	source = strings.TrimSpace(source)
	sourceRef = strings.TrimSpace(sourceRef)
	if source == "" || source == SourceConsole {
		return nil, apperr.Validation("a module source is required, and it is not " + SourceConsole)
	}
	if sourceRef == "" {
		return nil, apperr.Validation("a source_ref is required: it is what makes the declaration idempotent")
	}
	return s.declare(ctx, actorID, source, sourceRef, in)
}

func (s *Service) declare(ctx context.Context, actorID, source, sourceRef string, in DeclareInput) (*Declaration, error) {
	cc := country.FromContext(ctx)
	if cc == "" {
		return nil, apperr.Validation("a country is required")
	}
	bad := func(reason string) error {
		return apperr.Validation("invalid declaration").WithMeta(map[string]any{"reason": reason})
	}
	cat, err := s.categoryFor(ctx, in.Category)
	if err != nil {
		return nil, err
	}
	if !cat.Allows(in.Direction) {
		return nil, bad("category " + cat.Code + " does not accept direction " + in.Direction)
	}
	if cat.Disabled {
		return nil, bad("category " + cat.Code + " is disabled")
	}
	cur := currencyOf(cc)
	// ⚠️ La monnaie ANNONCÉE est vérifiée, pas acceptée. Une console restée
	// sur un autre pays enverrait des francs guinéens à écrire dans un pays
	// en francs CFA : le montant serait quatorze fois trop grand, et rien
	// dans le journal ne dirait lequel des deux chiffres est le bon.
	if in.Currency != "" && country.NormalizeCurrency(in.Currency) != cur {
		return nil, apperr.Validation("currency does not match the country").WithMeta(map[string]any{
			"reason": "the amount must be in " + cur + ", the currency of " + cc, "expected": cur,
		})
	}
	valueDate := parseTime(in.ValueDate)
	if valueDate.IsZero() {
		return nil, bad("value_date must be a date (2026-10-24) or an RFC3339 instant")
	}
	valueDate = valueDate.UTC().Truncate(24 * time.Hour)
	now := s.now()
	// ⚠️ PAS DE DATE DE VALEUR DANS LE FUTUR. Déclarer un loyer qu'on paiera
	// le mois prochain écrirait au journal un argent qui n'a pas bougé : la
	// charge serait constatée, la caisse vidée, et le rapprochement avec le
	// relevé impossible. Une dépense à venir est un engagement, pas un
	// mouvement — et ce module ne tient pas les engagements.
	if valueDate.After(now) {
		return nil, bad("value_date is in the future: declare the movement once the money has moved")
	}
	d := &Declaration{
		ID:            primitive.NewObjectID(),
		Country:       cc,
		Direction:     in.Direction,
		Category:      cat.Code,
		CategoryLabel: cat.Label,
		Account:       cat.Account,
		Means:         in.Means,
		Amount:        in.Amount,
		Currency:      cur,
		ValueDate:     valueDate,
		Counterparty:  strings.TrimSpace(in.Counterparty),
		Reference:     strings.TrimSpace(in.Reference),
		Justification: strings.TrimSpace(in.Justification),
		AttachmentURL: strings.TrimSpace(in.AttachmentURL),
		Status:        DeclarationPosted,
		Source:        source,
		SourceRef:     sourceRef,
		DeclaredBy:    actorID,
		CreatedAt:     now,
	}
	// La CORRECTION d'une déclaration contre-passée : les deux sens du lien.
	var corrected *Declaration
	if in.Corrects != "" {
		if corrected, err = s.correctedTarget(ctx, in.Corrects); err != nil {
			return nil, err
		}
		d.Corrects = &corrected.ID
	}
	entry := EntryForDeclaration(*d)
	if entry == nil {
		// Impossible si la validation ci-dessus a fait son travail ; le dire
		// plutôt que d'écrire une déclaration sans écriture.
		return nil, bad("this declaration produces no accounting entry")
	}
	if err := s.decl().InsertDeclaration(ctx, d, entry); err != nil {
		return nil, apperr.From(err)
	}
	if corrected != nil {
		if err := s.decl().LinkCorrection(ctx, corrected.ID, d.ID); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	s.recordDeclaration(ctx, "finance.declaration.create", d)
	return d, nil
}

// categoryFor rend la catégorie du catalogue du pays.
func (s *Service) categoryFor(ctx context.Context, code string) (DeclarationCategory, error) {
	cat, err := s.DeclarationCategories(ctx)
	if err != nil {
		return DeclarationCategory{}, err
	}
	c, ok := cat.Find(strings.ToLower(strings.TrimSpace(code)))
	if !ok {
		return DeclarationCategory{}, apperr.Validation("unknown category").WithMeta(map[string]any{
			"reason": "the category is not in this country's catalogue (GET /admin/finance/declaration-categories)",
		})
	}
	// ⚠️ Le compte de la catégorie est revérifié ICI, et pas seulement à
	// l'écriture du catalogue : un document écrit avant cette règle, ou à la
	// main dans la base, pointerait sinon un compte de portefeuille.
	if !declarableAccounts[c.Account] {
		return DeclarationCategory{}, apperr.Validation("category points at a non-declarable account").WithMeta(map[string]any{
			"reason": "account " + c.Account + " is not declarable", "category": c.Code,
		})
	}
	return c, nil
}

func (s *Service) correctedTarget(ctx context.Context, id string) (*Declaration, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errDeclarationNotFound
	}
	prev, err := s.decl().FindDeclaration(ctx, oid)
	if err != nil {
		return nil, err
	}
	// ⚠️ ON NE CORRIGE QUE CE QUI A ÉTÉ CONTRE-PASSÉ. Laisser corriger une
	// déclaration encore vivante aurait compté la dépense DEUX FOIS : la
	// fautive et sa correction, toutes deux au journal.
	if prev.Status != DeclarationReversed {
		return nil, errCorrectsNotReversed
	}
	if prev.CorrectedBy != nil {
		return nil, errCorrectsAlreadyCorrected
	}
	return prev, nil
}

// --- lire ---

func (s *Service) Declaration(ctx context.Context, id string) (*Declaration, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errDeclarationNotFound
	}
	return s.decl().FindDeclaration(ctx, oid)
}

func (s *Service) Declarations(ctx context.Context, f DeclarationFilter, limit int, cursor string) ([]Declaration, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, next, err := s.decl().ListDeclarations(ctx, f, limit, cursor)
	if err != nil {
		return nil, "", apperr.From(err)
	}
	return items, next, nil
}

// --- contre-passer ---

// ReverseDeclaration ANNULE une déclaration en posant sa contre-passation.
//
// ⚠️ RIEN N'EST SUPPRIMÉ NI MODIFIÉ. La déclaration reste, son écriture reste,
// et une SECONDE écriture — mêmes comptes, côtés échangés — la neutralise.
// C'est la seule façon d'annuler qui laisse lisible qu'on s'est trompé, et le
// motif est donc obligatoire : « annulé » sans raison ne se relit pas six mois
// plus tard.
func (s *Service) ReverseDeclaration(ctx context.Context, actorID, id, reason string) (*Declaration, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < 5 {
		return nil, apperr.Validation("a reversal reason is required (at least 5 characters)")
	}
	d, err := s.Declaration(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.Status != DeclarationPosted {
		return nil, errDeclarationAlreadyReversed
	}
	now := s.now()
	entry := ReversalEntryForDeclaration(*d, now)
	if entry == nil {
		return nil, apperr.Internal(nil)
	}
	d.ReversalReason, d.ReversedBy, d.ReversedAt = reason, actorID, &now
	if err := s.decl().ReverseDeclaration(ctx, d, entry); err != nil {
		return nil, apperr.From(err)
	}
	d.Status = DeclarationReversed
	s.recordDeclaration(ctx, "finance.declaration.reverse", d)
	return d, nil
}

// recordDeclaration porte le geste au journal d'audit — l'auteur, le pays, et
// de quoi comprendre sans ouvrir la base.
func (s *Service) recordDeclaration(ctx context.Context, action string, d *Declaration) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "declaration", d.ID.Hex(), nil, map[string]any{
		"direction": d.Direction, "category": d.Category, "account": d.Account,
		"amount": d.Amount, "currency": d.Currency, "means": d.Means,
		"value_date": d.ValueDate, "counterparty": d.Counterparty,
		"reference": d.Reference, "justification": d.Justification,
		"status": d.Status, "source": d.Source, "reversal_reason": d.ReversalReason,
	})
}

// --- la synthèse ---

// DeclarationSummary : l'état de synthèse d'une période.
type DeclarationSummary struct {
	Country  string    `json:"country"`
	Currency string    `json:"currency"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	// In, Out : ce qui est ENTRÉ et ce qui est SORTI sur la période, net des
	// contre-passations datées dans la période.
	In  int `json:"in"`
	Out int `json:"out"`
	Net int `json:"net"`
	// Count : le nombre de déclarations qui TIENNENT (une contre-passation
	// datée dans la période en retire une).
	Count int `json:"count"`
	// Reversals : les contre-passations posées dans la période.
	Reversals  int                        `json:"reversals"`
	Categories []DeclarationCategoryTotal `json:"categories"`
	Months     []DeclarationMonthTotal    `json:"months"`
	// Truncated : le plafond de lecture a été atteint, les totaux sont donc
	// incomplets. ⚠️ Dit plutôt que tu : un faux total vaut moins que pas de
	// total.
	Truncated bool `json:"truncated"`
}

// DeclarationCategoryTotal : ce qu'une catégorie a pesé.
type DeclarationCategoryTotal struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Account  string `json:"account"`
	In       int    `json:"in"`
	Out      int    `json:"out"`
	Net      int    `json:"net"`
	Count    int    `json:"count"`
}

// DeclarationMonthTotal : un mois de la période.
type DeclarationMonthTotal struct {
	Month string `json:"month"` // `2026-10`
	In    int    `json:"in"`
	Out   int    `json:"out"`
	Net   int    `json:"net"`
}

// DeclarationSummaryFor plie des déclarations en un état de synthèse.
//
// ⚠️ UNE DÉCLARATION CONTRE-PASSÉE COMPTE DEUX FAITS, PAS ZÉRO. L'exclure
// aurait réécrit le passé : son écriture est toujours au journal, à sa date de
// valeur, et seule la contre-passation l'annule — à SA date, celle du jour où
// on s'en est aperçu. Une déclaration de janvier annulée en mars laisse donc
// janvier tel qu'il était et retire la somme de mars, exactement comme le
// journal. C'est ce qui fait que la synthèse et le journal disent le même
// chiffre sur n'importe quelle période — et c'est éprouvé.
//
// Fonction PURE : c'est la règle qu'on peut se tromper en écrivant, pas la
// requête Mongo qui la nourrit.
func DeclarationSummaryFor(ds []Declaration, from, to time.Time) DeclarationSummary {
	out := DeclarationSummary{From: from, To: to}
	inWindow := func(t time.Time) bool {
		if t.IsZero() {
			return false
		}
		if !from.IsZero() && t.Before(from) {
			return false
		}
		if !to.IsZero() && !t.Before(to) {
			return false
		}
		return true
	}
	cats := map[string]*DeclarationCategoryTotal{}
	months := map[string]*DeclarationMonthTotal{}
	add := func(d Declaration, sign int, at time.Time) {
		c := cats[d.Category]
		if c == nil {
			c = &DeclarationCategoryTotal{Category: d.Category, Label: d.CategoryLabel, Account: d.Account}
			cats[d.Category] = c
		}
		key := at.UTC().Format("2006-01")
		m := months[key]
		if m == nil {
			m = &DeclarationMonthTotal{Month: key}
			months[key] = m
		}
		amount := sign * d.Amount
		if d.Direction == DeclarationIn {
			out.In += amount
			c.In += amount
			m.In += amount
		} else {
			out.Out += amount
			c.Out += amount
			m.Out += amount
		}
		c.Count += sign
		out.Count += sign
	}
	for _, d := range ds {
		if d.Status != DeclarationPosted && d.Status != DeclarationReversed {
			continue
		}
		if inWindow(d.ValueDate) {
			add(d, 1, d.ValueDate)
		}
		if d.Status == DeclarationReversed && d.ReversedAt != nil && inWindow(*d.ReversedAt) {
			add(d, -1, *d.ReversedAt)
			out.Reversals++
		}
	}
	out.Net = out.In - out.Out
	out.Categories = make([]DeclarationCategoryTotal, 0, len(cats))
	for _, c := range cats {
		c.Net = c.In - c.Out
		out.Categories = append(out.Categories, *c)
	}
	// Le plus lourd d'abord : c'est la question qu'on pose à un état de
	// synthèse (« où part l'argent ? »).
	sort.Slice(out.Categories, func(i, j int) bool {
		a, b := out.Categories[i], out.Categories[j]
		if a.In+a.Out != b.In+b.Out {
			return a.In+a.Out > b.In+b.Out
		}
		return a.Category < b.Category
	})
	out.Months = make([]DeclarationMonthTotal, 0, len(months))
	for _, m := range months {
		m.Net = m.In - m.Out
		out.Months = append(out.Months, *m)
	}
	sort.Slice(out.Months, func(i, j int) bool { return out.Months[i].Month < out.Months[j].Month })
	return out
}

// DeclarationSummary lit la période et rend l'état de synthèse du pays.
func (s *Service) DeclarationSummary(ctx context.Context, from, to time.Time) (*DeclarationSummary, error) {
	cc := country.FromContext(ctx)
	if to.IsZero() {
		// Jusqu'à la fin de la journée en cours : une déclaration faite ce
		// matin doit apparaître dans la synthèse de ce matin.
		to = s.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 1)
	}
	if from.IsZero() {
		from = to.AddDate(0, -1, 0)
	}
	ds, err := s.decl().DeclarationsInWindow(ctx, from, to)
	if err != nil {
		return nil, apperr.From(err)
	}
	sum := DeclarationSummaryFor(ds, from, to)
	sum.Country, sum.Currency = cc, currencyOf(cc)
	sum.Truncated = len(ds) >= declarationSummaryMax
	return &sum, nil
}
