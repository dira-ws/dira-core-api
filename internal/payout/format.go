package payout

import (
	"encoding/json"
	"fmt"
	"strings"
)

// formatXOF écrit un montant comme les écrans l'écrivent.
//
// ⚠️ IL SERT LES PHRASES DE REFUS, et c'est pour ça qu'il est ici : « le
// minimum est 1000 » se lit mal, « le minimum est 1 000 F » se lit. Un refus
// qui annonce un seuil doit l'annoncer lisiblement, sinon il est relu deux
// fois.
func formatXOF(v int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			// ⚠️ ESPACE INSÉCABLE (U+00A0), PAS UNE ESPACE ORDINAIRE — et elle
			// est invisible dans ce fichier, donc écrite ici pour que personne
			// ne la « corrige ». Avec une espace ordinaire, « 1 000 F » se
			// coupe en fin de ligne et un agent lit « 1 » puis « 000 F » sur
			// la ligne suivante, dans un refus qui parle de son argent.
			b.WriteString(" ")
		}
		b.WriteRune(r)
	}
	out := b.String() + " F"
	if neg {
		return "-" + out
	}
	return out
}

// decodeMockEvent lit la notification du bouchon.
//
// ⚠️ LE BOUCHON PARLE LE MÊME DIALECTE QUE CE QU'ON ATTEND D'UN VRAI
// AGRÉGATEUR : notre identifiant, le sien, un état, une raison. Un bouchon qui
// invente sa propre forme ne prouve rien du jour où l'on branche le vrai.
func decodeMockEvent(payload []byte) (Event, error) {
	var in struct {
		PayoutID      string `json:"payout_id"`
		ProviderRef   string `json:"provider_ref"`
		Status        string `json:"status"`
		FailureReason string `json:"failure_reason"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return Event{}, fmt.Errorf("payout: bad webhook payload: %w", err)
	}
	return Event{
		PayoutID: in.PayoutID, ProviderRef: in.ProviderRef,
		Status: in.Status, FailureReason: in.FailureReason,
	}, nil
}
