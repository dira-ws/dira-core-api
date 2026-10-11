package chat

// ⚠️⚠️ LE TEST QUI COMPTE DANS CE FICHIER EST CELUI DE CONAKRY.
//
// Une liste de mots interdits appliquée en sous-chaîne est la façon la plus
// courante de rater une modération : elle censure des mots innocents, les gens
// l'apprennent, et le jour où elle a raison personne ne la croit. En français
// le piège est partout — « con » est dans *second*, *contact*, *connaître*, et
// dans **Conakry**, la capitale du pays que ce contrat vise.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lex() Lexicon { return BaseLexicon() }

// ⚠️⚠️ LE PIÈGE DE SCUNTHORPE, EN FRANÇAIS. Si ce test tombe, la modération
// bloque un livreur qui écrit « je suis à Conakry » — et c'est la fin de la
// confiance dans tout le reste.
func TestInnocentWordsThatContainABannedWordArePassed(t *testing.T) {
	for _, phrase := range []string{
		"je suis à Conakry",
		"je vous contacte dans deux minutes",
		"c'est la seconde rue à droite",
		"je ne connais pas cette adresse",
		"le concert est fini, je sors",
		"j'arrive au rond-point de la Concorde",
		"Scunthorpe",
		"analysis of the route",     // « anal »
		"I'll be there in a second", // « con »
		"classic bassinet",          // « ass »
	} {
		v := Check(phrase, lex())
		assert.Equal(t, VerdictPass, v.Verdict,
			"« %s » doit passer — déclenché par %v", phrase, v.Terms)
	}
}

// Et le vrai mot, lui, est attrapé.
func TestAnActualInsultIsFlagged(t *testing.T) {
	v := Check("tu es un connard", lex())
	assert.Equal(t, VerdictFlag, v.Verdict)
	assert.Equal(t, SeverityInsult, v.Severity)
	assert.Contains(t, v.Terms, "conard", "la règle, sous sa forme normalisée")
}

// ⚠️⚠️ UNE INSULTE PASSE QUAND MÊME, ET C'EST DÉLIBÉRÉ. Deux personnes qui
// s'énervent sur un prix s'insultent ; bloquer ne calme personne et fait croire
// à une panne de réseau — on renvoie le message trois fois. Il part, et un
// humain voit le motif réel, qui est presque toujours ailleurs.
func TestAnInsultStillReachesTheOtherSide(t *testing.T) {
	v := Check("espèce d'abruti", lex())
	assert.False(t, v.Blocked(), "une insulte ne se bloque pas")
	assert.True(t, v.Flagged(), "elle se signale")
}

// ⚠️ UNE MENACE, ELLE, NE PART PAS. Ce qui est en jeu n'est plus le confort de
// la conversation.
func TestAThreatDoesNotGoThrough(t *testing.T) {
	v := Check("je vais te tuer", lex())
	assert.True(t, v.Blocked())
	assert.Equal(t, SeverityThreat, v.Severity)
}

// ⚠️ LA PLUS GRAVE L'EMPORTE : un message qui contient une insulte ET une
// menace est une menace.
func TestTheWorstSeverityWins(t *testing.T) {
	v := Check("connard je vais te frapper", lex())
	assert.True(t, v.Blocked())
	assert.Equal(t, SeverityThreat, v.Severity)
	assert.Len(t, v.Terms, 2)

	// Et l'ordre d'apparition ne change rien.
	v = Check("je vais te frapper connard", lex())
	assert.True(t, v.Blocked())
	assert.Equal(t, SeverityThreat, v.Severity)
}

// ⚠️ LES CONTOURNEMENTS ORDINAIRES SONT FERMÉS : accents, majuscules, chiffres
// qui imitent des lettres, lettres répétées.
func TestTheUsualDodgesAreClosed(t *testing.T) {
	for _, dodge := range []string{
		"ENCULÉ", "encule", "ENCULE", "encul3",
		"salooooope", "SALOPE", "s4lope",
		"c0nnard", "C0NN4RD",
	} {
		v := Check("tu es un "+dodge, lex())
		assert.NotEqual(t, VerdictPass, v.Verdict, "« %s » ne doit pas passer", dodge)
	}
}

// ⚠️ ET ON S'ARRÊTE LÀ. « e n c u l é » passe, et c'est assumé : retirer les
// espaces internes collerait « rue du con grès » en un seul mot. Un filtre qui
// gagne un contournement rare en créant un faux positif fréquent a perdu.
func TestSpacedOutLettersGetThroughAndThatIsTheTrade(t *testing.T) {
	v := Check("e n c u l é", lex())
	assert.Equal(t, VerdictPass, v.Verdict,
		"assumé : l'humain de la file voit ce que l'automate ne voit pas")
}

// Un message ordinaire de course ne déclenche rien — c'est 99 % du trafic.
func TestAnOrdinaryMessagePasses(t *testing.T) {
	for _, m := range []string{
		"je suis au portail bleu",
		"j'arrive dans deux minutes",
		"le client ne répond pas, j'attends encore 5 min",
		"I'm at the gate, the blue one",
		"merci beaucoup, bonne route",
	} {
		assert.Equal(t, VerdictPass, Check(m, lex()).Verdict, m)
	}
}

// ⚠️ L'EXPLOITATION PEUT RETIRER UN TERME EMBARQUÉ — c'est la soupape. Le jour
// où un mot de la liste est un nom de famille courant dans un pays, il faut
// pouvoir l'éteindre sans attendre un déploiement.
func TestOperationsCanTurnOffABuiltInTerm(t *testing.T) {
	l := MergeLexicon(lex(), map[string]Severity{"con": ""})
	assert.Equal(t, VerdictPass, Check("quel con", l).Verdict)
	// Les autres restent.
	assert.Equal(t, VerdictFlag, Check("quel connard", l).Verdict)
}

// ⚠️ ET ELLE PEUT EN AJOUTER — avec ses accents, comme on les tape.
func TestOperationsCanAddATermWithItsAccents(t *testing.T) {
	l := MergeLexicon(lex(), map[string]Severity{"Brêle": SeverityInsult})
	assert.Equal(t, VerdictFlag, Check("espèce de brele", l).Verdict)
	assert.Equal(t, VerdictFlag, Check("espèce de BRÊLE", l).Verdict)
}

// Un lexique vide laisse tout passer — la modération se coupe en vidant la
// liste, pas en changeant le code.
func TestAnEmptyLexiconPassesEverything(t *testing.T) {
	assert.Equal(t, VerdictPass, Check("connard", Lexicon{}).Verdict)
	assert.Equal(t, VerdictPass, Check("connard", nil).Verdict)
}

// Le même terme répété ne compte qu'une fois : la file lit des motifs, pas des
// occurrences.
func TestARepeatedTermIsReportedOnce(t *testing.T) {
	v := Check("connard connard connard", lex())
	require.Len(t, v.Terms, 1)
	// ⚠️ LA FORME NORMALISÉE, pas le texte écrit : c'est la RÈGLE qui est
	// nommée, pour qu'on puisse la retirer si elle se trompe trop souvent.
	assert.Equal(t, "conard", v.Terms[0])
}

// ⚠️ LES DEUX LANGUES SONT APPLIQUÉES ENSEMBLE : la même conversation mêle le
// français et l'anglais, et demander « quelle langue est-ce ? » avant de
// filtrer ajouterait une devinette là où l'union suffit.
func TestBothLanguagesApplyAtOnce(t *testing.T) {
	l := lex()
	assert.Equal(t, VerdictFlag, Check("you idiot", l).Verdict)
	assert.Equal(t, VerdictFlag, Check("quel connard", l).Verdict)
	assert.True(t, Check("I will kill you", l).Blocked())
}

// La normalisation, dans le détail.
func TestNormalise(t *testing.T) {
	assert.Equal(t, "encule", Normalise("ENCULÉ"))
	assert.Equal(t, "salope", Normalise("salooooope"))
	// ⚠️ « bonne » DEVIENT « bone », et c'est sans conséquence : le lexique
	// passe par la même fonction, donc les deux côtés se déforment pareil.
	assert.Equal(t, "bone", Normalise("bonne"))
	assert.Equal(t, "salope", Normalise("sallope"), "le contournement le plus courant")
	assert.Equal(t, "ca va", Normalise("Ça va"))
	// ⚠️ `@` et `4` SONT DES « a », `0` est un « o » : c'est tout l'objet du
	// repliement. « À@ » donne donc « aa » — deux lettres identiques, que la
	// règle des répétitions conserve.
	assert.Equal(t, "a", Normalise("À@"), "À et @ se replient tous deux en « a », puis la répétition tombe")
	assert.Equal(t, "salope", Normalise("s4l0pe"))
	assert.Equal(t, "encule", Normalise("3ncul3"))
}
