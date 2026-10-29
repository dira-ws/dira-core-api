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
	// Kind : `text` ou `voice`. ⚠️ TOUJOURS SERVI, même pour du texte : une
	// application qui doit deviner le genre d'un message en regardant si
	// `audio_url` est là finit par traiter un vocal dont l'audio a disparu
	// comme un message texte vide.
	Kind string `json:"kind"`
	// AudioURL : où réécouter le vocal. Absent sur du texte.
	//
	// ⚠️ LISIBLE SANS EN-TÊTE : un lecteur audio ne porte pas notre
	// `Authorization`. L'adresse est donc signée et datée côté stockage, et
	// c'est elle qui protège — pas un jeton que le lecteur n'enverra jamais.
	AudioURL string `json:"audio_url,omitempty"`
	// Seconds : la durée déclarée par le téléphone de l'envoyeur, pour
	// dessiner la barre avant de télécharger le fichier. ⚠️ Déclarée, pas
	// mesurée — voir `Message.Seconds`.
	Seconds int `json:"seconds,omitempty"`
}

// VoicePolicy dit si cette conversation accepte les vocaux, et jusqu'à quelle
// durée.
//
// ⚠️⚠️ ELLE EXISTE POUR QUE L'APPLICATION NE MONTRE LE MICRO QUE LÀ OÙ IL
// MARCHE. Un bouton d'enregistrement sur un déploiement sans transcription et
// sans stockage produit un échec après que la personne a parlé — le pire
// moment pour apprendre que ça ne marche pas, surtout pour quelqu'un qui ne
// lit pas le message d'erreur.
type VoicePolicy struct {
	Enabled    bool `json:"enabled"`
	MaxSeconds int  `json:"max_seconds"`
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
	// Voice : ce que cette conversation accepte comme vocal. Absent = la
	// verticale n'a pas branché les vocaux ; l'application ne montre pas le
	// micro.
	Voice *VoicePolicy `json:"voice,omitempty"`
}

func toResponse(m Message) MessageResponse {
	out := MessageResponse{
		ID:   m.ID.Hex(),
		From: m.SenderRole,
		Body: m.Body,
		// ⚠️ `text` POUR UN MESSAGE SANS GENRE : ceux d'avant les vocaux n'en
		// portent pas, et servir une chaîne vide obligerait chaque application
		// à écrire le même `?? "text"` de son côté.
		Kind:      kindOf(m),
		AudioURL:  m.AudioURL,
		Seconds:   m.Seconds,
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
		// ⚠️⚠️ ET L'AUDIO DISPARAÎT AVEC LE CORPS. Retirer la transcription
		// d'un message tout en laissant son enregistrement réécoutable
		// n'aurait retiré que la trace écrite — le destinataire n'aurait eu
		// qu'à appuyer sur « écouter ». C'est le genre de demi-mesure qui fait
		// croire à une modération qui n'existe pas.
		out.AudioURL = ""
	}
	return out
}

// kindOf rend le genre d'un message, `text` par défaut.
func kindOf(m Message) string {
	if m.Kind == KindVoice {
		return KindVoice
	}
	return KindText
}

func toResponses(items []Message) []MessageResponse {
	out := make([]MessageResponse, 0, len(items))
	for _, m := range items {
		out = append(out, toResponse(m))
	}
	return out
}
