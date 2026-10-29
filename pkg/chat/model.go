// Package chat carries the conversation between a client and the driver who
// serves them — celui qui apporte leur commande, ou celui qui vient les
// chercher.
//
// Elle existe pour une raison précise : permettre à ces deux-là de se parler
// SANS échanger leurs numéros. « Je suis au portail bleu », « je monte dans
// cinq minutes » — des phrases qui n'ont de sens que pendant la course, et qui
// ne justifient pas de donner son téléphone à un inconnu.
//
// ⚠️ CE PAQUET EST UNE BIBLIOTHÈQUE, PAS UN SERVICE, et le choix est délibéré.
//
// Les deux verticales ont besoin du même COMPORTEMENT — qui peut écrire,
// jusqu'à quand, comment se marque un message lu — mais pas des mêmes DONNÉES :
// une conversation de commande et une conversation de course ne se lisent
// jamais ensemble. En faire un service aurait coûté un TROISIÈME socket dans
// les applications, et une synchronisation d'état entre services dont l'échec
// se serait vu comme « aucun chauffeur assigné » sans que personne comprenne
// pourquoi.
//
// Chaque verticale câble donc sa propre collection, ses propres participants et
// son propre canal temps réel. Ce qui est partagé, c'est la règle.
package chat

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Ce à quoi une conversation se rattache.
//
// Le COUPLE (genre, identifiant) fait l'identité, comme au grand livre du
// portefeuille : deux verticales peuvent porter le même identifiant sans se
// confondre — deux collections, deux compteurs d'ObjectID, aucune garantie
// d'unicité entre elles.
const (
	RefOrder = "order"
	RefRide  = "ride"
)

// Rôles d'un participant. FIGÉS à l'envoi : un livreur qui perdrait la course
// ne doit pas voir ses messages passés changer de camp.
const (
	SenderClient = "client"
	SenderDriver = "driver"
)

// MaxBody bounds one message. Au-delà, ce n'est plus un mot au livreur.
const MaxBody = 1000

// WriteWindowAfterCompletion is how long the conversation stays writable once
// the course is over — commande livrée, passager déposé.
//
// ⚠️ Réglage produit, à valider. Ni fermée à l'arrivée — « vous avez laissé le
// sac chez le voisin », « j'ai oublié mon téléphone sur la banquette » se
// disent dans la minute qui suit —, ni ouverte pour toujours : un mois plus
// tard, le chauffeur n'a plus ni contexte ni raison de répondre, et l'autre
// attendrait une réponse qui ne viendra pas.
const WriteWindowAfterCompletion = 2 * time.Hour

// Message is one line of a conversation.
// Les deux genres de message.
const (
	KindText  = "text"
	KindVoice = "voice"
)

// MaxVoiceSeconds borne un message vocal.
//
// ⚠️ SOIXANTE SECONDES, et c'est une borne de CONVERSATION, pas de stockage.
// Un vocal d'une minute se réécoute ; un vocal de cinq minutes ne se réécoute
// pas, et le destinataire — souvent au volant — ne l'écoutera pas du tout.
const MaxVoiceSeconds = 60

type Message struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
	// RefID et RefKind disent SUR QUOI l'on parle : une commande de repas, une
	// course. Le genre est écrit même quand une collection n'en contient qu'un
	// seul — une ligne qui ne dit pas de quoi elle parle oblige à connaître sa
	// collection pour se lire.
	RefID   primitive.ObjectID `bson:"ref_id"`
	RefKind string             `bson:"ref_kind"`
	// SenderID est l'utilisateur qui écrit ; SenderRole dit de quel côté.
	SenderID   primitive.ObjectID `bson:"sender_id"`
	SenderRole string             `bson:"sender_role"`
	Body       string             `bson:"body"`
	// Kind : `text` (défaut, et absent sur les messages d'avant) ou `voice`.
	//
	// ⚠️ ABSENT VAUT `text`, et c'est pour ça que ce champ est `omitempty` :
	// les messages écrits avant les vocaux n'en portent pas, et les réécrire
	// pour poser une valeur par défaut aurait touché toute la collection pour
	// une information qu'on sait déduire.
	Kind string `bson:"kind,omitempty"`
	// AudioURL et Seconds : le vocal. Vides sur un message texte.
	//
	// ⚠️ LA DURÉE VIENT DU TÉLÉPHONE, et c'est assumé : le serveur ne décode
	// pas l'audio pour la mesurer. Elle sert à dessiner la barre avant de
	// télécharger le fichier — une barre un peu fausse vaut mieux qu'un écran
	// qui attend le fichier pour savoir quoi dessiner. C'est la même décision
	// que le guichet vocal, où un « 0:01 » pour cinq secondes mesurées l'a
	// montré : on sert ce que le téléphone dit, et on ne le présente pas comme
	// une vérité.
	AudioURL  string    `bson:"audio_url,omitempty"`
	Seconds   int       `bson:"seconds,omitempty"`
	CreatedAt time.Time `bson:"created_at"`
	// ReadAt est l'instant où le DESTINATAIRE l'a lu. Porté par le message et
	// non par un compteur sur la conversation : un compteur se désynchronise,
	// et personne ne sait plus lequel des deux dit vrai.
	ReadAt *time.Time `bson:"read_at,omitempty"`

	// LA MODÉRATION — voir `moderation.go`.
	//
	// ⚠️ UN MESSAGE SIGNALÉ EST UN MESSAGE ENVOYÉ. Le drapeau ne retient rien :
	// il pose le message dans une file que des humains relisent. Ce qui ne
	// devait pas partir n'est jamais arrivé jusqu'ici.
	Flagged      bool     `bson:"flagged,omitempty"`
	FlagTerms    []string `bson:"flag_terms,omitempty"`
	FlagSeverity string   `bson:"flag_severity,omitempty"`
	// Reported : signalé par la PERSONNE qui l'a reçu.
	//
	// ⚠️ DISTINCT DU DRAPEAU AUTOMATIQUE, et plus important que lui : ce que
	// l'automate rate — l'ironie, la menace polie, le sous-entendu — c'est
	// exactement ce qu'un humain signale. Un signalement passe donc devant
	// dans la file.
	Reported   bool       `bson:"reported,omitempty"`
	ReportedAt *time.Time `bson:"reported_at,omitempty"`
	// Hidden : un modérateur l'a retiré de la conversation.
	//
	// ⚠️⚠️ RETIRÉ, PAS SUPPRIMÉ, et c'est une décision. L'autre partie l'a déjà
	// lu si elle était devant son écran : l'effacer réécrirait une histoire que
	// quelqu'un a vécue, et priverait l'enquête de la seule pièce qui compte.
	// Le corps reste en base, l'écran affiche « message retiré ».
	Hidden       bool                `bson:"hidden,omitempty"`
	HiddenAt     *time.Time          `bson:"hidden_at,omitempty"`
	HiddenBy     *primitive.ObjectID `bson:"hidden_by,omitempty"`
	HiddenReason string              `bson:"hidden_reason,omitempty"`
}
