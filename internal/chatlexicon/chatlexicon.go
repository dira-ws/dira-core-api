// Package chatlexicon : LES MOTS QUE LA PLATEFORME NE RELAIE PAS.
//
// ⚠️⚠️ AU SOCLE, ET UN SEUL POUR TOUTE LA PLATEFORME. Le filtre vit dans
// `pkg/chat`, que les deux verticales instancient chacune sur SA base — et
// c'est le bon découpage pour des conversations, qui n'ont rien à faire
// ensemble. Mais le LEXIQUE n'est pas une conversation : c'est une décision
// d'exploitation, et il n'existe aucune raison qu'un mot soit inacceptable
// dans une course et tolérable dans une livraison.
//
// Le ranger dans chaque verticale aurait obligé à le saisir deux fois. Une
// règle écrite deux fois est une règle perdue une fois : le jour où l'on
// ajoute un terme, on l'ajoute d'un côté, et la moitié de la plateforme
// continue de laisser passer ce qu'on vient d'interdire — sans que rien ne le
// signale.
//
// ⚠️ PAR PAYS, AVEC UN FOND COMMUN. « Les mots qui blessent changent d'un pays
// à l'autre » n'est pas une formule : un terme peut être une insulte ici et un
// nom de famille courant trois cents kilomètres plus loin. Le document global
// porte ce qui vaut partout ; celui d'un pays ajoute, et peut RETIRER.
//
// ⚠️ ET IL NE REMPLACE PAS LE LEXIQUE EMBARQUÉ (`chat.BaseLexicon`), il s'y
// AJOUTE. Celui-ci fait marcher la modération au premier démarrage, avant que
// quiconque ait ouvert la console — un filtre qui attend qu'on le remplisse
// est un filtre éteint, et personne ne s'en aperçoit.
package chatlexicon

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/chat"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

const Collection = "chat_lexicon"

// Document est le lexique d'un pays — ou le fond commun.
type Document struct {
	// ID : `lexicon` pour le fond commun, `lexicon:TG` pour un pays.
	ID string `bson:"_id"`
	// Terms : terme normalisé → gravité (`insult`, `threat`), ou chaîne VIDE
	// pour RETIRER un terme du fond.
	//
	// ⚠️ LA GRAVITÉ VIDE EST LA SOUPAPE, et elle n'est pas un détail : c'est
	// ce qui permet d'éteindre un terme du lexique EMBARQUÉ sans attendre un
	// déploiement. Le jour où un mot de la liste compilée est un nom de
	// famille courant dans un pays, l'exploitation doit pouvoir le neutraliser
	// en une minute — pas en ouvrant un ticket.
	Terms     map[string]string `bson:"terms"`
	UpdatedAt time.Time         `bson:"updated_at"`
	UpdatedBy string            `bson:"updated_by,omitempty"`
}

func key(code string) string {
	if code == "" {
		return "lexicon"
	}
	return "lexicon:" + strings.ToUpper(code)
}

// Entry est un terme tel que la console le lit et l'écrit.
type Entry struct {
	Term string `json:"term"`
	// Severity : `insult` (le message part, signalé), `threat` (il ne part
	// pas), ou vide pour RETIRER un terme du fond commun ou de la liste
	// embarquée.
	Severity string `json:"severity"`
	// Base dit que ce terme vient de la liste EMBARQUÉE.
	//
	// ⚠️ SERVI POUR QUE LA CONSOLE NE MENTE PAS. Sans lui, l'exploitation
	// croirait avoir saisi les quarante termes qu'elle voit, et hésiterait à
	// en retirer un « qu'elle n'a pas mis ». Dire d'où vient chaque ligne est
	// ce qui rend le retrait possible sans angoisse.
	Base bool `json:"base,omitempty"`
}

// Response est ce que la console affiche.
type Response struct {
	Country string `json:"country,omitempty"`
	// Items : le lexique EFFECTIF, fond embarqué compris, rangé.
	Items []Entry `json:"items"`
	// Own : les seuls termes écrits pour CE pays — ce que le `PUT` renvoie
	// s'il était rejoué tel quel.
	Own       []Entry   `json:"own"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

// Auditor note qui a changé quoi.
type Auditor interface {
	Record(ctx context.Context, action, resource, resourceID string, before, after any)
}

type Service struct {
	col   *mongo.Collection
	audit Auditor
}

func NewService(m *db.Mongo) *Service {
	return &Service{col: m.DB.Collection(Collection)}
}

func (s *Service) SetAuditor(a Auditor) { s.audit = a }

func (s *Service) one(ctx context.Context, id string) (Document, error) {
	var d Document
	err := s.col.FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Document{ID: id, Terms: map[string]string{}}, nil
	}
	if err != nil {
		return Document{}, err
	}
	if d.Terms == nil {
		d.Terms = map[string]string{}
	}
	return d, nil
}

// Effective rend le lexique À APPLIQUER pour un pays : la liste embarquée,
// puis le fond commun, puis le pays — chacun pouvant ajouter ou retirer.
//
// ⚠️ L'ORDRE EST LA RÈGLE : le plus LOCAL gagne. Un pays qui retire un terme
// du fond commun doit l'emporter, sinon « par pays » ne voudrait rien dire.
func (s *Service) Effective(ctx context.Context, code string) (chat.Lexicon, error) {
	base, err := s.one(ctx, key(""))
	if err != nil {
		return nil, err
	}
	out := chat.MergeLexicon(chat.BaseLexicon(), toSeverities(base.Terms))
	if code == "" {
		return out, nil
	}
	local, err := s.one(ctx, key(code))
	if err != nil {
		return nil, err
	}
	return chat.MergeLexicon(out, toSeverities(local.Terms)), nil
}

func toSeverities(in map[string]string) map[string]chat.Severity {
	out := make(map[string]chat.Severity, len(in))
	for t, sev := range in {
		out[t] = chat.Severity(sev)
	}
	return out
}

// Read rend le lexique d'un pays pour la console.
func (s *Service) Read(ctx context.Context, code string) (*Response, error) {
	eff, err := s.Effective(ctx, code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	own, err := s.one(ctx, key(code))
	if err != nil {
		return nil, apperr.Internal(err)
	}
	embedded := chat.BaseLexicon()
	out := &Response{
		Country: strings.ToUpper(code),
		Items:   make([]Entry, 0, len(eff)),
		Own:     make([]Entry, 0, len(own.Terms)),
		// ⚠️ `UpdatedAt` EST CELUI DU DOCUMENT DU PAYS, pas du fond commun :
		// l'exploitation d'un pays veut savoir quand ELLE a touché à sa liste.
		UpdatedAt: own.UpdatedAt,
		UpdatedBy: own.UpdatedBy,
	}
	for term, sev := range eff {
		_, fromBase := embedded[term]
		out.Items = append(out.Items, Entry{Term: term, Severity: string(sev), Base: fromBase})
	}
	for term, sev := range own.Terms {
		out.Own = append(out.Own, Entry{Term: term, Severity: sev})
	}
	sortEntries(out.Items)
	sortEntries(out.Own)
	return out, nil
}

// sortEntries range par gravité PUIS par terme.
//
// ⚠️ LES MENACES EN TÊTE, et ce n'est pas cosmétique : ce sont elles qui
// BLOQUENT un message. Une exploitation qui relit sa liste doit voir d'abord
// ce qui coupe une conversation, pas ce qui la signale.
func sortEntries(in []Entry) {
	rank := map[string]int{string(chat.SeverityThreat): 0, string(chat.SeverityInsult): 1}
	sort.Slice(in, func(i, j int) bool {
		ri, rj := rank[in[i].Severity], rank[in[j].Severity]
		if ri != rj {
			return ri < rj
		}
		return in[i].Term < in[j].Term
	})
}

// UpdateRequest remplace la liste PROPRE d'un pays.
//
// ⚠️ REMPLACE PLUTÔT QU'AJOUTE, et c'est le bon choix pour une console : un
// écran qui n'ajoute jamais ne permet pas de retirer, et deux routes — une
// pour ajouter, une pour ôter — auraient fait deux moitiés d'écran qu'on peut
// oublier d'appeler. On envoie la liste telle qu'on veut la voir.
type UpdateRequest struct {
	Items []Entry `json:"items" validate:"max=2000,dive"`
}

var (
	errNotAWord = apperr.New("lexicon_not_a_word",
		"a lexicon term must be ONE word: matching is on whole words",
		http.StatusUnprocessableEntity)
	errTooLong = apperr.New("lexicon_term_too_long",
		"a lexicon term is too long to be a word", http.StatusUnprocessableEntity)
	errBadSeverity = apperr.New("lexicon_bad_severity",
		"severity must be insult, threat, or empty to remove the term",
		http.StatusUnprocessableEntity)
	errEmptyTerm = apperr.New("lexicon_empty_term",
		"a lexicon term cannot be empty once normalised", http.StatusUnprocessableEntity)
)

// maxTermLen : un « mot » plus long que ça n'en est pas un. Dernier filet,
// après la règle qui compte vraiment — voir `errNotAWord`.
const maxTermLen = 40

// Save écrit la liste propre d'un pays.
func (s *Service) Save(ctx context.Context, code, actorID string, req UpdateRequest) (*Response, error) {
	terms := make(map[string]string, len(req.Items))
	for _, it := range req.Items {
		// ⚠️⚠️ UN TERME EST UN MOT, ET CE N'EST PAS UNE PRÉFÉRENCE DE SAISIE.
		// `Check` ne compare que des MOTS ENTIERS — c'est ce qui permet à
		// « con » d'être au lexique sans que *second*, *contact* ou *Conakry*
		// ne déclenchent quoi que ce soit. Un terme contenant une espace ne
		// peut donc JAMAIS correspondre à rien.
		//
		// Le refuser à l'écriture est la seule protection utile : accepté, il
		// s'afficherait dans la console comme une règle active, l'exploitation
		// croirait avoir interdit « je vais te tuer », et rien ne mordrait. Le
		// filtre le plus trompeur est celui qui a l'air de marcher.
		//
		// ⚠️ Et une longueur seule ne suffisait pas : « Je vais t'écraser »
		// fait dix-sept caractères, passait sous la borne de quarante, et ne
		// correspondait à rien. C'est la recette sur les vraies routes qui l'a
		// montré — pas la relecture du code.
		if strings.ContainsAny(it.Term, " \t\n\r") {
			return nil, errNotAWord.WithMeta(map[string]any{"reason": it.Term})
		}
		if len(it.Term) > maxTermLen {
			return nil, errTooLong.WithMeta(map[string]any{"reason": it.Term})
		}
		switch chat.Severity(it.Severity) {
		case chat.SeverityInsult, chat.SeverityThreat, "":
		default:
			return nil, errBadSeverity.WithMeta(map[string]any{"reason": it.Severity})
		}
		// ⚠️⚠️ NORMALISÉ À L'ENTRÉE, par la MÊME fonction que les messages.
		// Personne ne saisit « encule » dans un champ de console : on tape
		// « enculé », avec son accent, et on attend que ça marche. Et c'est
		// la seule façon que les deux côtés se retrouvent — un lexique tenu à
		// la main en suivant les règles d'une fonction est un lexique qui se
		// désynchronise.
		n := chat.Normalise(it.Term)
		if n == "" {
			return nil, errEmptyTerm.WithMeta(map[string]any{"reason": it.Term})
		}
		terms[n] = it.Severity
	}

	before, err := s.Read(ctx, code)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	d := Document{ID: key(code), Terms: terms, UpdatedAt: now, UpdatedBy: actorID}
	if _, err := s.col.ReplaceOne(ctx, bson.M{"_id": d.ID}, d,
		options.Replace().SetUpsert(true)); err != nil {
		return nil, apperr.Internal(err)
	}
	after, err := s.Read(ctx, code)
	if err != nil {
		return nil, err
	}
	// ⚠️ AU JOURNAL D'AUDIT. Changer ce qui bloque un message est exactement
	// le genre de geste dont on veut savoir qui l'a fait — y compris le jour
	// où quelqu'un retire discrètement le terme qui le gênait.
	if s.audit != nil {
		res := strings.ToUpper(code)
		if res == "" {
			res = "global"
		}
		s.audit.Record(ctx, "chat.lexicon", "chat_lexicon", res, before.Own, after.Own)
	}
	return after, nil
}

// CountryOf lit le pays de la requête, ou celui que la console demande.
func CountryOf(ctx context.Context, asked string) string {
	if asked != "" {
		return strings.ToUpper(asked)
	}
	return country.FromContext(ctx)
}
