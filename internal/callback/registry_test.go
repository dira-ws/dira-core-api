package callback

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func targets(cs []*Client) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Target())
	}
	return out
}

// ⚠️⚠️ UN SERVICE PRÉVENU N'EST PAS UN SERVICE PAYÉ, et confondre les deux
// aurait mis une commande payée en file morte.
//
// La table est indexée par OBJET payé : « commande », « course », « abonnement
// de courses ». L'analytique ne possède aucun de ces objets — mais elle détient
// le fil de conversation d'une personne et ses messages vocaux, et doit donc
// apprendre qu'un compte a été effacé. L'inscrire sous un `purpose` inventé
// l'aurait rendue destinataire des confirmations de paiement : elle aurait
// refusé chaque rappel, la tâche serait revenue vingt fois, et le paiement d'un
// client aurait fini en file morte.
func TestAToldServiceIsNeverPaid(t *testing.T) {
	r := NewRegistry(map[string]*Client{
		"order": New("food", "http://food:8080", "jeton-food"),
		"ride":  New("vtc", "http://vtc:8080", "jeton-vtc"),
	}).AlsoTell(New("analytics", "http://analytics:8080", "jeton-analytics"))

	// Elle est prévenue des faits qui concernent la personne.
	assert.ElementsMatch(t, []string{"analytics", "food", "vtc"}, targets(r.All()),
		"l'analytique n'apprendrait pas qu'un compte a été effacé : son fil et ses vocaux resteraient")

	// ⚠️ ET ELLE N'EST DESTINATAIRE D'AUCUN PAIEMENT.
	for _, purpose := range []string{"order", "ride", "ride_subscription", "analytics"} {
		if c := r.For(purpose); c != nil {
			assert.NotEqual(t, "analytics", c.Target(),
				"un paiement `%s` partirait chez l'analytique, qui le refuserait vingt fois", purpose)
		}
	}
	assert.NotContains(t, r.Purposes(), "analytics")
}

// ⚠️ PRÉVENU UNE FOIS, PAS DEUX. Les courses apparaissent sous deux `purpose`
// (une course, un abonnement de courses) et partagent une adresse : une annonce
// en double ferait purger deux fois — inoffensif — mais un service inscrit à la
// fois comme payé et comme prévenu serait, lui, appelé deux fois pour rien.
func TestEveryoneIsToldExactlyOnce(t *testing.T) {
	food := New("food", "http://food:8080", "jeton-food")
	r := NewRegistry(map[string]*Client{
		"order":             food,
		"ride":              New("vtc", "http://vtc:8080", "jeton-vtc"),
		"ride_subscription": New("vtc", "http://vtc:8080", "jeton-vtc"),
	}).AlsoTell(food, New("analytics", "http://analytics:8080", "jeton-analytics"))

	got := targets(r.All())
	assert.ElementsMatch(t, []string{"analytics", "food", "vtc"}, got)
	require.Len(t, got, 3)
}

// ⚠️ UN SERVICE SANS ADRESSE NI SECRET N'EST PAS PRÉVENU, et c'est pour cela
// que le démarrage CRIE plutôt que de laisser croire que c'est branché. Un
// client désactivé glissé dans la liste aurait fait échouer chaque annonce, et
// la tâche du socle serait revenue indéfiniment sur une adresse vide.
func TestAnUnconfiguredServiceIsNotTold(t *testing.T) {
	r := NewRegistry(map[string]*Client{
		"order": New("food", "http://food:8080", "jeton-food"),
	}).AlsoTell(
		New("analytics", "", "jeton"),
		New("analytics", "http://analytics:8080", ""),
		nil,
	)
	assert.Equal(t, []string{"food"}, targets(r.All()))
}

// ⚠️ ET UN REGISTRE NUL NE PANIQUE PAS : le développement tourne sans aucune
// verticale branchée, et un socle qui s'écroule là ne démarrerait jamais en
// local.
func TestANilRegistryTellsNobody(t *testing.T) {
	var r *Registry
	assert.Nil(t, r.AlsoTell(New("analytics", "http://analytics:8080", "jeton")))
	assert.Empty(t, r.All())
}
