package chatlexicon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/chat"
)

// LE LEXIQUE EST UNE DÉCISION D'EXPLOITATION, pas une constante compilée.
//
// ⚠️⚠️ ET CES TESTS NE TOUCHENT PAS LA BASE. Ce qui mérite d'être prouvé ici,
// c'est la FUSION — qui gagne entre la liste embarquée, le fond commun et le
// pays — et la normalisation à l'entrée. Les deux sont des fonctions pures ;
// les tester à travers Mongo n'aurait rien prouvé de plus et aurait rendu le
// test lent et fragile.

func merged(base, local map[string]string) chat.Lexicon {
	out := chat.MergeLexicon(chat.BaseLexicon(), toSeverities(base))
	return chat.MergeLexicon(out, toSeverities(local))
}

// La liste embarquée marche AVANT que quiconque ait ouvert la console. Un
// filtre qui attend qu'on le remplisse est un filtre éteint, et personne ne
// s'en aperçoit.
func TestTheEmbeddedListWorksBeforeAnyoneHasSaidAnything(t *testing.T) {
	l := merged(nil, nil)
	assert.Equal(t, chat.SeverityThreat, l[chat.Normalise("tuer")])
	assert.Equal(t, chat.SeverityInsult, l[chat.Normalise("connard")])
	assert.NotEmpty(t, l)
}

// ⚠️ LE PLUS LOCAL GAGNE : sinon « par pays » ne voudrait rien dire.
func TestACountryOverridesTheCommonGround(t *testing.T) {
	l := merged(
		map[string]string{"gredin": string(chat.SeverityInsult)},
		map[string]string{"gredin": string(chat.SeverityThreat)},
	)
	assert.Equal(t, chat.SeverityThreat, l["gredin"], "le pays l'emporte sur le fond commun")
}

// ⚠️⚠️ LA SOUPAPE : une gravité VIDE RETIRE un terme — y compris un terme de
// la liste EMBARQUÉE. C'est ce qui permet d'éteindre un mot devenu un nom de
// famille courant quelque part, sans attendre un déploiement.
func TestACountryCanTurnOffAnEmbeddedTerm(t *testing.T) {
	term := chat.Normalise("con")
	require.Equal(t, chat.SeverityInsult, merged(nil, nil)[term],
		"il est bien dans la liste embarquée")

	l := merged(nil, map[string]string{term: ""})
	_, still := l[term]
	assert.False(t, still, "le pays l'a éteint")
	assert.Equal(t, chat.SeverityThreat, l[chat.Normalise("tuer")],
		"et il n'a éteint QUE celui-là")
}

// Et le fond commun peut l'éteindre partout, qu'un pays peut rallumer.
func TestTheCommonGroundCanTurnOffWhatACountryBringsBack(t *testing.T) {
	term := chat.Normalise("con")
	l := merged(map[string]string{term: ""}, map[string]string{term: string(chat.SeverityThreat)})
	assert.Equal(t, chat.SeverityThreat, l[term])
}

// ⚠️⚠️ NORMALISÉ À L'ENTRÉE, PAR LA MÊME FONCTION QUE LES MESSAGES. Personne
// ne saisit « encule » dans un champ de console : on tape « enculé », avec son
// accent, et on attend que ça marche. Un lexique tenu à la main en suivant les
// règles d'une fonction est un lexique qui se désynchronise — c'est déjà
// arrivé une fois, quand `kill` est resté `kill` dans la liste pendant que
// « kill » devenait `kil` dans les messages.
func TestWhatIsTypedWithAccentsStillMatches(t *testing.T) {
	saved := map[string]string{}
	for _, typed := range []string{"Enculé", "SALOPE", "crétin"} {
		n := chat.Normalise(typed)
		require.NotEmpty(t, n)
		saved[n] = string(chat.SeverityInsult)
	}
	l := merged(nil, saved)
	for _, written := range []string{"espèce d'enculé", "SALOPE !", "petit crétin"} {
		v := chat.Check(written, l)
		assert.NotEmpty(t, v.Terms, "« %s » doit déclencher", written)
	}
}

// ⚠️ LES MENACES EN TÊTE DE LISTE, et ce n'est pas cosmétique : ce sont elles
// qui BLOQUENT un message. Une exploitation qui relit sa liste doit voir
// d'abord ce qui coupe une conversation.
func TestThreatsAreListedFirst(t *testing.T) {
	in := []Entry{
		{Term: "zebre", Severity: string(chat.SeverityInsult)},
		{Term: "alpha", Severity: string(chat.SeverityThreat)},
		{Term: "abeille", Severity: string(chat.SeverityInsult)},
		{Term: "zoulou", Severity: string(chat.SeverityThreat)},
	}
	sortEntries(in)
	assert.Equal(t, []string{"alpha", "zoulou", "abeille", "zebre"},
		[]string{in[0].Term, in[1].Term, in[2].Term, in[3].Term})
}

// La clé du document : le fond commun n'a pas de pays, un pays est en
// majuscules — sans quoi `tg` et `TG` seraient deux lexiques.
func TestTheDocumentKeyIsStable(t *testing.T) {
	assert.Equal(t, "lexicon", key(""))
	assert.Equal(t, "lexicon:TG", key("TG"))
	assert.Equal(t, "lexicon:TG", key("tg"))
}
