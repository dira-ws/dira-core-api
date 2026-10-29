package main

// LE CONTRÔLE DE CÂBLAGE DU FILTRE DES ÉDITIONS.
//
// ⚠️⚠️ CE TEST EXISTE PARCE QUE LE CORRECTIF DE #251 POUVAIT ÊTRE NEUTRALISÉ
// EN SILENCE. Mesuré : en remplaçant `appConfigSvc` par `nil` dans l'un des
// trois `Mount` de `main.go`, la suite ENTIÈRE restait verte — 0 sur 4. Les
// épreuves des routes passent leur propre porte : elles prouvent que la route
// REFUSE quand on lui donne une porte qui refuse, jamais que `main` lui en
// donne une.
//
// C'est #251 d'un cran plus haut. Le défaut d'origine était « le service sait
// refuser, personne ne l'appelle » ; celui-ci serait « l'intergiciel est monté,
// mais sur une porte nulle qui laisse tout passer ». Le symptôme est identique
// et tout aussi muet : `/app/config` annonce `wallet: false`, `GET /wallet`
// rend le solde, et rien n'échoue nulle part.
//
// ⚠️ ET ON FERME LA CLASSE, PAS LE CAS. L'épreuve ne liste pas les trois
// poignées : elle les DÉCOUVRE en cherchant qui réclame une
// `middleware.FeatureGate`, puis exige que `main` lui en passe une vraie. Une
// quatrième poignée qui réclamera ce garde demain échouera ici tant qu'on ne
// l'aura pas branchée — le seul moment où quelqu'un y pensera.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poignéeAvecGarde trouve les paquets d'`internal` dont `Mount` réclame une
// porte.
func poignéeAvecGarde(t *testing.T) []string {
	t.Helper()
	// ⚠️ ON CHERCHE LE PARAMÈTRE, PAS LA SIGNATURE ENTIÈRE : un `Mount` porte
	// déjà un `func(http.Handler) http.Handler`, dont la parenthèse fermante
	// coupe court à toute lecture « jusqu'au bout des arguments ». Première
	// version : zéro poignée trouvée, et l'épreuve qui se croyait verte.
	réclame := regexp.MustCompile(`gate middleware\.FeatureGate`)
	var out []string
	err := filepath.Walk("../../internal", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if réclame.Match(src) {
			out = append(out, filepath.Base(filepath.Dir(path)))
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// ⚠️⚠️ CHAQUE POIGNÉE QUI RÉCLAME UNE PORTE EN REÇOIT UNE VRAIE.
func TestEveryGatedHandlerGetsARealGate(t *testing.T) {
	poignées := poignéeAvecGarde(t)
	require.NotEmpty(t, poignées,
		"aucune poignée trouvée : le motif de lecture est cassé, pas le câblage")

	raw, err := os.ReadFile("main.go")
	require.NoError(t, err)
	main := string(raw)

	// ⚠️ LE CŒUR DE L'ÉPREUVE : une porte NULLE compile, monte, et ne refuse
	// rien. C'est le seul « câblage » qui ait l'air d'un câblage sans en être
	// un, et c'est donc le seul qu'il faut nommer.
	nulle := regexp.MustCompile(`\.Mount\([^)]*,\s*nil\)`)
	assert.Empty(t, nulle.FindString(main),
		"une poignée est montée sur une porte NULLE : elle laisse tout passer, "+
			"et `/app/config` annoncera des coupures qui ne coupent rien")

	for _, p := range poignées {
		assert.Contains(t, main, "appConfigSvc",
			"le paquet `%s` réclame une porte et `main` n'en construit aucune", p)
	}
	assert.Equal(t, len(poignées), strings.Count(main, ", appConfigSvc)"),
		"il y a %d poignée(s) à garder et %d câblage(s) : l'une d'elles n'a pas sa porte",
		len(poignées), strings.Count(main, ", appConfigSvc)"))
}

// ⚠️ ET L'INTERGICIEL QUI POSE L'ÉDITION SUR LA REQUÊTE EST MONTÉ. Sans lui,
// `edition.FromContext` rend `full` pour tout le monde : les portes seraient
// branchées, correctes, et n'auraient jamais rien à refuser.
func TestTheEditionMiddlewareIsMounted(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	require.NoError(t, err)
	assert.Contains(t, string(raw), "middleware.Edition()",
		"l'édition n'est jamais lue sur la requête : tout le monde est `full`")
}
