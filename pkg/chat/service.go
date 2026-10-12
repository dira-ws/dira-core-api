package chat

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

var (
	errNotParty = apperr.Forbidden("forbidden", "this conversation is not yours")
	// errNoDriver : tant que personne n'a pris la course, il n'y a personne à
	// qui écrire. Un message parti dans le vide serait pire qu'un refus — le
	// client attendrait une réponse.
	// ⚠️ Le message ne dit ni « commande » ni « course » : ce paquet sert les
	// deux, et un texte qui nommerait l'un afficherait « aucun livreur n'a pris
	// cette commande » à un passager qui attend une voiture.
	errNoDriver = apperr.Conflict("no_driver_yet", "nobody has taken this yet")
	errClosed   = apperr.Conflict("conversation_closed", "this conversation is closed")
	errEmpty    = apperr.Validation("message body is required")
	// ⚠️⚠️ UN REFUS QUI DIT CE QUI S'EST PASSÉ, et un code à lui.
	//
	// Un message bloqué en silence, ou refusé par un `validation_failed`
	// traduit en « Requête invalide », fait croire à une panne du réseau : on
	// le renvoie trois fois, puis on appelle le support. La phrase doit dire
	// que c'est un REFUS et pourquoi — sans nommer le mot, ce qui apprendrait
	// seulement à l'écrire autrement.
	errBlockedByModeration = apperr.New("message_blocked",
		"this message was not sent: it contains a threat or an insult that we do not relay",
		http.StatusUnprocessableEntity)
)

// Conversation is who may speak on an order, and until when.
//
// Un seul appel rend les trois faits : sans cela, le service enchaînerait des
// lectures dont chacune peut échouer à moitié, pour une question qui n'en est
// qu'une — « puis-je écrire ici ? ».
type Conversation struct {
	ClientID string
	// DriverUserID est vide tant que personne n'a pris la course.
	DriverUserID string
	Status       string
	// CompletedAt est l'instant où la course s'est terminée — commande livrée,
	// passager déposé. Nil tant qu'elle est en cours.
	CompletedAt *time.Time
}

// Parties resolves a conversation. Implemented at wiring time from the order
// and delivery modules — ce module n'importe ni l'un ni l'autre.
type Parties interface {
	ConversationOf(ctx context.Context, refID string) (*Conversation, error)
}

// Notifier pushes a message to whoever is listening. Facultatif : sans lui, la
// conversation fonctionne, elle se rafraîchit simplement à la lecture.
type Notifier interface {
	NotifyMessage(ctx context.Context, refID, clientID, driverUserID, senderRole, body string)
}

// Pusher atteint le DESTINATAIRE quand son application est fermée. Le socket
// ne porte que jusqu'à un écran allumé, et « je suis en bas » n'a de valeur
// que reçu tout de suite.
//
// ⚠️ Il reçoit le côté et le corps, JAMAIS une identité : la notification ne
// doit pas nommer sur l'écran verrouillé ce que la conversation protège.
type Pusher interface {
	NotifyChatMessage(ctx context.Context, recipientUserID, refID, body string)
}

type Service struct {
	repo    *Repository
	parties Parties
	// refKind est écrit sur chaque message : « order » ou « ride ». Il tient
	// dans le service et non dans l'appel, parce qu'une instance ne sert
	// qu'une verticale — le passer à chaque envoi laisserait la possibilité
	// d'écrire une course dans les conversations de commandes.
	refKind  string
	notifier Notifier
	pusher   Pusher
	// lexicon est la liste surveillée. Vide = la modération automatique est
	// ÉTEINTE, et le chat se comporte comme avant.
	//
	// ⚠️ ÉTEINTE PAR DÉFAUT DANS LE TYPE, ALLUMÉE AU CÂBLAGE : un service
	// construit dans un test ne doit pas se mettre à refuser des messages
	// parce qu'un lexique s'est invité tout seul.
	lexicon Lexicon
	// lexiconOf rend le lexique À APPLIQUER pour CETTE requête, quand il
	// dépend d'elle.
	//
	// ⚠️⚠️ UNE FONCTION ET NON UNE LISTE, pour deux raisons qu'une liste figée
	// ne couvre pas. La première est le PAYS : « les mots qui blessent
	// changent d'un pays à l'autre » n'est pas une formule — un terme peut
	// être une insulte ici et un nom de famille courant trois cents
	// kilomètres plus loin, et c'est le pays de la requête qui tranche. La
	// seconde est le TEMPS : l'exploitation ajoute un terme depuis la console
	// un mardi après-midi, et il doit mordre sans attendre un redéploiement.
	//
	// Nil = on retombe sur `lexicon`, la liste posée au câblage. C'est le cas
	// des tests, et c'est voulu : une fonction qui irait chercher un lexique
	// quelque part ferait dépendre un test de modération d'un réseau.
	lexiconOf func(ctx context.Context) Lexicon
}

// NewService builds the conversation service of ONE vertical.
//
// `refKind` dit de quoi l'on parle ici — `RefOrder` ou `RefRide`.
func NewService(repo *Repository, refKind string, parties Parties) *Service {
	return &Service{repo: repo, refKind: refKind, parties: parties}
}

// SetNotifier branche la diffusion temps réel (câblage).
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// SetPusher branche les notifications poussées (câblage).
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetLexicon allume la modération automatique (câblage).
//
// ⚠️ LA MODÉRATION SE COUPE EN VIDANT LA LISTE, pas en changeant le code : un
// pays dont le lexique se révèle mauvais doit pouvoir l'éteindre en une
// minute, sans déploiement.
func (s *Service) SetLexicon(l Lexicon) { s.lexicon = l }

// SetLexiconSource branche un lexique QUI DÉPEND DE LA REQUÊTE (câblage).
//
// ⚠️ IL PREND LE PAS SUR `SetLexicon` quand il rend quelque chose. Les deux
// coexistent parce qu'ils répondent à deux moments : la liste embarquée fait
// marcher la modération au PREMIER démarrage — un filtre qui attend qu'on le
// remplisse est un filtre éteint, et personne ne s'en aperçoit — et la source
// la remplace dès que l'exploitation a dit quelque chose.
//
// ⚠️ ET UNE SOURCE QUI REND UNE LISTE VIDE N'ÉTEINT PAS LA MODÉRATION : elle
// laisse la liste du câblage s'appliquer. Un pays qui veut vraiment éteindre
// le filtre retire ses termes un par un depuis la console — un geste visible,
// qui laisse une trace au journal — plutôt qu'en vidant un document, ce qui
// ressemblerait trop à une panne de lecture.
func (s *Service) SetLexiconSource(f func(ctx context.Context) Lexicon) {
	s.lexiconOf = f
}

// lexiconFor rend la liste à appliquer à cette requête.
func (s *Service) lexiconFor(ctx context.Context) Lexicon {
	if s.lexiconOf != nil {
		if l := s.lexiconOf(ctx); len(l) > 0 {
			return l
		}
	}
	return s.lexicon
}

// roleOf tells which side of the conversation a caller is on, or "" when they
// are on neither.
//
// L'ADMIN est un cas à part : il LIT pour le support, il n'écrit pas. Écrire
// en son nom ferait passer un message de la plateforme pour celui d'un
// livreur, et c'est exactement ce qu'un client ne doit pas avoir à démêler.
func roleOf(userID string, c *Conversation) string {
	switch {
	case c.ClientID == userID:
		return SenderClient
	case c.DriverUserID != "" && c.DriverUserID == userID:
		return SenderDriver
	default:
		return ""
	}
}

// List returns the conversation of an order, and how many messages the caller
// has not read.
func (s *Service) List(ctx context.Context, userID, role, refID, after string, limit int) ([]MessageResponse, int, error) {
	oid, _, party, err := s.access(ctx, userID, role, refID)
	if err != nil {
		return nil, 0, err
	}
	var afterID *primitive.ObjectID
	if after != "" {
		parsed, err := primitive.ObjectIDFromHex(after)
		if err != nil {
			return nil, 0, apperr.Validation("invalid cursor").WithCause(err)
		}
		afterID = &parsed
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, err := s.repo.ListByRef(ctx, oid, afterID, limit)
	if err != nil {
		return nil, 0, err
	}
	unread := 0
	if party != "" {
		if unread, err = s.repo.CountUnread(ctx, oid, party); err != nil {
			// Au mieux : un compteur perdu ne doit pas emporter la
			// conversation elle-même.
			slog.WarnContext(ctx, "chat: unread count unavailable", "ref_id", refID, "error", err)
			unread = 0
		}
	}
	return toResponses(items), unread, nil
}

// Send posts a message from the caller.
func (s *Service) Send(ctx context.Context, userID, role, refID, body string) (*MessageResponse, error) {
	oid, conv, party, err := s.access(ctx, userID, role, refID)
	if err != nil {
		return nil, err
	}
	if party == "" {
		// Un administrateur lit, il n'écrit pas.
		return nil, errNotParty
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errEmpty
	}
	if len([]rune(body)) > MaxBody {
		return nil, apperr.Validation("message is too long")
	}
	if err := writable(conv); err != nil {
		return nil, err
	}

	senderOID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	// LA MODÉRATION AUTOMATIQUE — voir `moderation.go`.
	//
	// ⚠️⚠️ ELLE S'EXÉCUTE AVANT L'ÉCRITURE POUR LES MENACES, ET APRÈS POUR LE
	// RESTE. Une menace ne doit pas exister dans la conversation ; une insulte
	// doit y être, avec sa marque — parce que la retirer empêcherait le
	// modérateur de voir ce qui s'est réellement dit, et le destinataire l'a de
	// toute façon déjà lue s'il était devant son écran.
	verdict := Check(body, s.lexiconFor(ctx))
	if verdict.Blocked() {
		return nil, errBlockedByModeration
	}

	m := &Message{
		RefID:      oid,
		RefKind:    s.refKind,
		SenderID:   senderOID,
		SenderRole: party,
		Body:       body,
		CreatedAt:  time.Now().UTC(),
	}
	if verdict.Flagged() {
		m.Flagged = true
		m.FlagTerms = verdict.Terms
		m.FlagSeverity = string(verdict.Severity)
	}
	if err := s.repo.Insert(ctx, m); err != nil {
		return nil, err
	}
	if s.notifier != nil {
		s.notifier.NotifyMessage(ctx, refID, conv.ClientID, conv.DriverUserID, party, body)
	}
	// L'AUTRE côté, et lui seul : s'envoyer une notification à soi-même est
	// le défaut classique de ce genre de branchement.
	if s.pusher != nil {
		recipient := conv.DriverUserID
		if party == SenderDriver {
			recipient = conv.ClientID
		}
		if recipient != "" {
			s.pusher.NotifyChatMessage(ctx, recipient, refID, body)
		}
	}
	resp := toResponse(*m)
	return &resp, nil
}

// SendAs posts a message ON BEHALF of one side, for the back-office
// emulators.
//
// Le message est attribué à la VRAIE partie — son identifiant d'utilisateur,
// son côté —, jamais à l'administrateur. C'est le même motif que les autres
// émulateurs de la console : ils agissent POUR un acteur, par la même méthode
// de service, et non à côté de lui.
//
// La règle qui interdit à un administrateur d'écrire EN SON NOM reste entière :
// ici il n'écrit pas en son nom, il rejoue ce que la partie aurait envoyé.
func (s *Service) SendAs(ctx context.Context, refID, as, body string) (*MessageResponse, error) {
	if as != SenderClient && as != SenderDriver {
		return nil, apperr.Validation("as must be client or driver")
	}
	conv, err := s.parties.ConversationOf(ctx, refID)
	if err != nil {
		return nil, err
	}
	userID := conv.ClientID
	if as == SenderDriver {
		userID = conv.DriverUserID
	}
	if userID == "" {
		return nil, errNoDriver
	}
	// On repasse par Send : mêmes bornes, mêmes refus, même diffusion. Une
	// seconde implémentation aurait fini par diverger de la première.
	return s.Send(ctx, userID, "", refID, body)
}

// MarkRead marks everything the other side sent as read.
func (s *Service) MarkRead(ctx context.Context, userID, role, refID string) (int, error) {
	oid, _, party, err := s.access(ctx, userID, role, refID)
	if err != nil {
		return 0, err
	}
	if party == "" {
		// Lire en tant qu'admin ne marque rien : le support n'est pas le
		// destinataire, et effacer le non-lu du client serait lui cacher un
		// message qu'il n'a jamais vu.
		return 0, nil
	}
	n, err := s.repo.MarkRead(ctx, oid, party, time.Now().UTC())
	return int(n), err
}

// access resolves the order, its conversation and the caller's side of it.
func (s *Service) access(ctx context.Context, userID, role, refID string) (primitive.ObjectID, *Conversation, string, error) {
	oid, err := primitive.ObjectIDFromHex(refID)
	if err != nil {
		return primitive.NilObjectID, nil, "", apperr.NotFound("not_found", "not found").WithCause(err)
	}
	conv, err := s.parties.ConversationOf(ctx, refID)
	if err != nil {
		return primitive.NilObjectID, nil, "", err
	}
	party := roleOf(userID, conv)
	if party == "" && role != auth.RoleAdmin {
		return primitive.NilObjectID, nil, "", errNotParty
	}
	return oid, conv, party, nil
}

// writable says whether the conversation still accepts messages.
func writable(c *Conversation) error {
	if c.DriverUserID == "" {
		return errNoDriver
	}
	if c.Status == "cancelled" {
		return errClosed.WithMeta(map[string]any{"reason": "cancelled"})
	}
	// Livrée : la conversation reste ouverte le temps de dire ce qui doit
	// l'être — puis se ferme, l'historique demeurant lisible.
	if c.CompletedAt != nil && time.Since(*c.CompletedAt) > WriteWindowAfterCompletion {
		return errClosed.WithMeta(map[string]any{"reason": "completed", "window": WriteWindowAfterCompletion.String()})
	}
	return nil
}

// PurgeOf jette tout ce qu'une personne effacée a écrit ici — la part de
// l'effacement d'un compte qui vit dans les conversations.
//
// ⚠️ SES MESSAGES, PAS LES CONVERSATIONS. Un fil appartient à deux personnes :
// ce que le chauffeur a écrit — « j'ai déposé le sac chez le gardien » — est sa
// trace à lui, et il peut en avoir besoin. Voir `Repository.PurgeOf`.
func (s *Service) PurgeOf(ctx context.Context, userID primitive.ObjectID) (int64, error) {
	return s.repo.PurgeOf(ctx, userID)
}

// --- LA MODÉRATION ---------------------------------------------------------

var (
	errMessageNotFound = apperr.NotFound("message_not_found", "this message does not exist")
	// ⚠️ ON NE SIGNALE QUE CE QU'ON A REÇU. Se signaler soi-même remplirait la
	// file de bruit — ou servirait à faire croire qu'on a été agressé.
	errReportOwnMessage = apperr.New("cannot_report_own_message",
		"you can only report a message you received", http.StatusUnprocessableEntity)
)

// Report : la personne qui a REÇU ce message le signale.
//
// ⚠️⚠️ C'EST LE GESTE LE PLUS IMPORTANT DE TOUTE CETTE MODÉRATION. Ce qu'un
// automate rate — l'ironie, la menace polie, le sous-entendu, la langue qu'on
// n'a pas mise au lexique — c'est exactement ce qu'un humain reconnaît. Sans
// ce bouton, la file ne contient que ce que la machine a su voir, c'est-à-dire
// le plus grossier.
func (s *Service) Report(ctx context.Context, userID, role, refID, messageID string) (*MessageResponse, error) {
	_, _, party, err := s.access(ctx, userID, role, refID)
	if err != nil {
		return nil, err
	}
	if party == "" {
		return nil, errNotParty
	}
	mid, err := primitive.ObjectIDFromHex(messageID)
	if err != nil {
		return nil, errMessageNotFound
	}
	m, err := s.repo.One(ctx, mid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if m == nil || m.RefID.Hex() != refIDOf(refID) {
		return nil, errMessageNotFound
	}
	if m.SenderRole == party {
		return nil, errReportOwnMessage
	}
	now := time.Now().UTC()
	if err := s.repo.SetReported(ctx, mid, now); err != nil {
		return nil, apperr.Internal(err)
	}
	m.Reported, m.ReportedAt = true, &now
	out := toResponse(*m)
	return &out, nil
}

// FlaggedResponse est la file de modération.
type FlaggedResponse struct {
	Items      []FlaggedMessage `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// FlaggedMessage est une ligne de la file.
//
// ⚠️ ELLE PORTE LE CORPS, contrairement à ce que la conversation rend d'un
// message retiré : c'est précisément ce qu'un modérateur doit lire pour
// décider. Ce qu'on cache à l'autre partie, on ne le cache pas à celui qui
// juge.
type FlaggedMessage struct {
	ID      string `json:"id"`
	RefID   string `json:"ref_id"`
	RefKind string `json:"ref_kind"`
	From    string `json:"from"`
	Body    string `json:"body"`
	// Reported : signalé par un HUMAIN. Ces lignes viennent en tête.
	Reported bool `json:"reported,omitempty"`
	// FlagTerms : les RÈGLES qui ont parlé, sous leur forme normalisée — pas
	// le texte écrit. Voir `Verdict.Terms`.
	FlagTerms    []string  `json:"flag_terms,omitempty"`
	FlagSeverity string    `json:"flag_severity,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Flagged rend la file de modération.
func (s *Service) Flagged(ctx context.Context, limit int, cursor string) (*FlaggedResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var after *primitive.ObjectID
	if cursor != "" {
		id, err := primitive.ObjectIDFromHex(cursor)
		if err != nil {
			return nil, apperr.Validation("invalid cursor")
		}
		after = &id
	}
	rows, err := s.repo.ListFlagged(ctx, limit, after)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := &FlaggedResponse{Items: make([]FlaggedMessage, 0, len(rows))}
	for _, m := range rows {
		out.Items = append(out.Items, FlaggedMessage{
			ID: m.ID.Hex(), RefID: m.RefID.Hex(), RefKind: m.RefKind,
			From: m.SenderRole, Body: m.Body,
			Reported: m.Reported, FlagTerms: m.FlagTerms,
			FlagSeverity: m.FlagSeverity, CreatedAt: m.CreatedAt,
		})
	}
	if len(rows) == limit {
		out.NextCursor = rows[len(rows)-1].ID.Hex()
	}
	return out, nil
}

// Hide retire un message de la conversation.
func (s *Service) Hide(ctx context.Context, messageID, reason string) error {
	mid, err := primitive.ObjectIDFromHex(messageID)
	if err != nil {
		return errMessageNotFound
	}
	actor, _ := auth.UserFromContext(ctx)
	by, _ := primitive.ObjectIDFromHex(actor)
	if err := s.repo.SetHidden(ctx, mid, by, strings.TrimSpace(reason), time.Now().UTC()); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// Keep sort un message de la file sans le retirer.
func (s *Service) Keep(ctx context.Context, messageID string) error {
	mid, err := primitive.ObjectIDFromHex(messageID)
	if err != nil {
		return errMessageNotFound
	}
	if err := s.repo.SetKept(ctx, mid, time.Now().UTC()); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// refIDOf rend l'identifiant tel qu'il est stocké — le message porte un
// ObjectID, l'URL porte sa forme hexadécimale.
func refIDOf(refID string) string {
	if id, err := primitive.ObjectIDFromHex(strings.TrimSpace(refID)); err == nil {
		return id.Hex()
	}
	return refID
}
