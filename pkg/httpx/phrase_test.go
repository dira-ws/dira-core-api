package httpx

// ⚠️⚠️ UN VOCABULAIRE SERVI PAR LE SERVEUR DOIT SE LIRE.
//
// `GET /deliveries/transfer-reasons` et ses jumelles rendaient des CODES et
// rien d'autre : chaque application portait sa propre table de phrases, donc un
// motif ajouté côté serveur s'affichait comme un code brut jusqu'à la prochaine
// publication sur les magasins — des semaines, pendant lesquelles un livreur en
// panne choisit « courier_breakdown » dans une liste (core#228 §5).

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ⚠️ SANS TRADUCTEUR, LE REPLI PART TEL QUEL — jamais une chaîne vide. Un écran
// qui reçoit un libellé vide affiche un bouton muet, et personne ne sait sur
// quoi il appuie ; le code brut est au moins diagnosticable.
//
// ⚠️ Et ce cas-là n'est pas théorique : `SetTranslator` n'est appelé qu'au
// démarrage d'un service, et un binaire d'essai ou une tâche de fond n'en a pas.
func TestAPhraseFallsBackInsteadOfGoingSilent(t *testing.T) {
	before := translator
	translator = nil
	defer func() { translator = before }()

	assert.Equal(t, "courier_breakdown",
		Phrase(context.Background(), "transfer_reasons.courier_breakdown", "courier_breakdown"))
	// Et une clé inconnue, traducteur présent ou non, rend aussi le repli.
	assert.Equal(t, "repli", Phrase(context.Background(), "clé.qui.n.existe.pas", "repli"))
}
