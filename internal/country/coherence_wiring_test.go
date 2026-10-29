package country

// CHAQUE RÈGLE DE COHÉRENCE EST RÉELLEMENT APPELÉE, ET AVANT L'ÉCRITURE.
//
// ⚠️⚠️ CETTE ÉPREUVE EXISTE PARCE QU'UNE MUTATION SURVIVAIT À TOUT. En retirant
// l'appel à `checkTopupCoherence` de `UpdateTopup`, la suite entière restait
// verte : les épreuves de cohérence appellent la RÈGLE directement, jamais le
// chemin qui l'utilise. Elles prouvent que la règle sait refuser, pas qu'on la
// consulte avant d'écrire.
//
// ⚠️ ET LE MÊME TROU ÉTAIT DÉJÀ OUVERT POUR LES RETRAITS, sans que personne
// l'ait vu : `UpdatePayout` n'est pas davantage éprouvée de bout en bout. Le
// service prend un `*Repository` CONCRET — il n'y a pas de couture où glisser
// une doublure, et ajouter une base aux épreuves pour cette seule vérification
// coûterait plus qu'elle ne rapporte. Quand le défaut vit dans la FORME du
// code et non dans une valeur, l'épreuve lit le code : c'est la mécanique du
// garde des frontières pays.
//
// ⚠️⚠️ L'ORDRE COMPTE AUTANT QUE L'APPEL. Vérifier APRÈS avoir écrit laisse une
// fenêtre où le pays porte un réglage impossible — et pour la recharge, cette
// fenêtre est un pays qui n'encaisse plus.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEveryCoherenceRuleIsConsultedBeforeWriting(t *testing.T) {
	sources := map[string]string{}
	entrées, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, e := range entrées {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(".", e.Name()))
		require.NoError(t, err)
		sources[e.Name()] = string(raw)
	}
	require.NotEmpty(t, sources, "aucune source lue : le filtre est cassé, et son silence passerait pour un succès")

	// ⚠️ ON DÉCOUVRE LES RÈGLES au lieu de les énumérer : une `checkXCoherence`
	// écrite demain devra être consultée, et c'est ici qu'on s'en souviendra.
	règle := regexp.MustCompile(`func check([A-Z][A-Za-z]*)Coherence\(`)
	trouvées := 0
	for _, src := range sources {
		for _, m := range règle.FindAllStringSubmatch(src, -1) {
			sujet := m[1] // "Topup", "Payout"…
			trouvées++

			corps, ok := corpsDe(sources, "Update"+sujet)
			require.True(t, ok,
				"`check%sCoherence` existe et `Update%s` est introuvable : "+
					"une règle que personne n'appelle ne protège rien", sujet, sujet)

			// Appelée…
			appel := strings.Index(corps, "check"+sujet+"Coherence(")
			assert.Positive(t, appel+1,
				"`Update%s` n'appelle PAS `check%sCoherence` : la règle sait "+
					"refuser, et personne ne la consulte", sujet, sujet)
			if appel < 0 {
				continue
			}

			// …sur le résultat FUSIONNÉ, et non sur la requête. `{\"min\": 50000}`
			// seul suffit à rendre impossible un `max: 10000` écrit trois mois
			// plus tôt, sans que la requête le nomme.
			arg := corps[appel:]
			if fin := strings.Index(arg, ")"); fin > 0 {
				arg = arg[:fin]
			}
			assert.Contains(t, arg, "merged",
				"`Update%s` vérifie autre chose que le résultat fusionné : une "+
					"règle qui ne regarde que ce qu'on lui envoie laisse creuser "+
					"le trou par le côté", sujet)

			// …et AVANT l'écriture.
			écriture := strings.Index(corps, "s.repo.Set")
			require.Positive(t, écriture+1,
				"`Update%s` n'écrit nulle part : relisez cette épreuve", sujet)
			assert.Less(t, appel, écriture,
				"`Update%s` vérifie APRÈS avoir écrit : il reste une fenêtre où "+
					"le pays porte un réglage impossible", sujet)
		}
	}
	assert.GreaterOrEqual(t, trouvées, 2,
		"deux règles de cohérence au moins sont attendues (recharge, retrait) : "+
			"en trouver moins veut dire que la recherche ne marche plus")
}

// corpsDe extrait le corps d'une méthode par son nom, sans recopier sa
// signature : une épreuve qui épingle la signature rougit au premier argument
// ajouté, pour rien.
func corpsDe(sources map[string]string, nom string) (string, bool) {
	marque := regexp.MustCompile(`func \([^)]+\) ` + regexp.QuoteMeta(nom) + `\(`)
	for _, src := range sources {
		loc := marque.FindStringIndex(src)
		if loc == nil {
			continue
		}
		reste := src[loc[1]:]
		if fin := strings.Index(reste, "\nfunc "); fin > 0 {
			reste = reste[:fin]
		}
		return reste, true
	}
	return "", false
}
