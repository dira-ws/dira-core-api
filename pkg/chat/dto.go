package chat

import "time"

// SendMessageRequest posts one line to the conversation.
type SendMessageRequest struct {
	Body string `json:"body" validate:"required,max=1000"`
}

// SendAsRequest posts a message on behalf of one side (back-office emulators).
type SendAsRequest struct {
	As   string `json:"as" validate:"required,oneof=client driver"`
	Body string `json:"body" validate:"required,max=1000"`
}

// MessageResponse is one message as both sides read it.
//
// L'EXPÉDITEUR n'est identifié que par son RÔLE, jamais par son identifiant
// d'utilisateur ni son téléphone : toute la raison d'être de cette
// conversation est que les deux puissent se parler sans échanger leurs
// coordonnées.
type MessageResponse struct {
	ID   string `json:"id"`
	From string `json:"from"` // "client" | "driver"
	Body string `json:"body"`
	// ReadAt dit que l'AUTRE l'a lu. Absent = pas encore.
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	// Hidden : un modérateur a retiré ce message. `body` est alors VIDE.
	//
	// ⚠️ L'APPLICATION AFFICHE « message retiré », pas un blanc : la ligne
	// garde sa place et son heure, parce que l'autre partie a vu qu'il y avait
	// quelque chose là.
	Hidden bool `json:"hidden,omitempty"`
}

// ConversationResponse is a page of the conversation.
type ConversationResponse struct {
	Items []MessageResponse `json:"items"`
	// Unread est ce que l'APPELANT n'a pas lu. Zéro pour un administrateur :
	// il n'est destinataire de rien.
	Unread int `json:"unread"`
	// NextCursor : l'identifiant du dernier message servi. Le renvoyer demande
	// la suite — c'est aussi le rattrapage après une coupure.
	NextCursor string `json:"next_cursor,omitempty"`
}

func toResponse(m Message) MessageResponse {
	out := MessageResponse{
		ID:        m.ID.Hex(),
		From:      m.SenderRole,
		Body:      m.Body,
		ReadAt:    m.ReadAt,
		CreatedAt: m.CreatedAt,
	}
	// ⚠️⚠️ UN MESSAGE RETIRÉ NE REND PAS SON CORPS, et c'est le seul endroit
	// où cette règle peut tenir : toutes les lectures passent par ici.
	//
	// ⚠️ MAIS LA LIGNE RESTE, avec son heure et son auteur. L'effacer de la
	// conversation ferait disparaître un trou que l'autre partie a vu se
	// créer — « il m'a écrit quelque chose et ça a disparu » est pire que
	// « message retiré par la modération ». L'application affiche la mention,
	// elle n'invente pas un silence.
	if m.Hidden {
		out.Body = ""
		out.Hidden = true
	}
	return out
}

func toResponses(items []Message) []MessageResponse {
	out := make([]MessageResponse, 0, len(items))
	for _, m := range items {
		out = append(out, toResponse(m))
	}
	return out
}
