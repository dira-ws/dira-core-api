package main

// LE CÂBLAGE DES BORNES DE RECHARGE — LES DEUX BOUTS (#250).
//
// ⚠️⚠️ UNE BORNE SE BRANCHE DEUX FOIS, ET N'EN BRANCHER QU'UNE EST MUET. Le
// portefeuille ANNONCE les bornes (`GET /wallet` → `topup`), le paiement les
// APPLIQUE (`POST /payments/initiate`). Les deux branchements sont des appels
// distincts dans `main.go`, et chacun est facultatif par construction — sans
// quoi un service de développement sans pays réglés perdrait ses paiements.
//
// Les deux moitiés manquantes donnent deux défauts opposés, tous deux
// silencieux :
//
//   - sans `SetTopupBounds`, l'application ne reçoit pas de bornes et garde ses
//     montants codés en dur — ceux du Togo, recopiés, dérisoires en Guinée ;
//   - sans `SetTopupLimits`, c'est #251 à l'identique : le serveur affiche une
//     règle qu'il n'applique pas. L'écran cache le bouton, un appel direct
//     passe, et personne ne s'en aperçoit avant de lire le grand livre.
//
// ⚠️ ET LES ÉPREUVES DES MODULES NE PEUVENT PAS LE VOIR : elles branchent
// elles-mêmes leur propre politique. Elles prouvent que la route refuse quand
// on lui donne des bornes, jamais que `main` lui en donne.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ ON DÉCOUVRE LES BRANCHEMENTS À BRANCHER au lieu de les énumérer : toute
// méthode `SetTopup…` d'`internal` doit être appelée par `main`. Un troisième
// consommateur des bornes — la console, une verticale — échouera ici tant qu'on
// ne l'aura pas câblé, c'est-à-dire au seul moment où quelqu'un y pensera.
func TestEveryTopupSetterIsCalledByMain(t *testing.T) {
	déclarés := setteursDéclarés(t, "../../internal")
	require.NotEmpty(t, déclarés,
		"aucun `SetTopup…` trouvé dans internal : le filtre est cassé, et son "+
			"silence se lirait comme « tout va bien »")

	main, err := os.ReadFile("main.go")
	require.NoError(t, err)

	for _, nom := range déclarés {
		appelé := regexp.MustCompile(`\.` + regexp.QuoteMeta(nom) + `\(`)
		assert.True(t, appelé.Match(main),
			"`%s` est déclaré dans internal et JAMAIS appelé par main.go : "+
				"une borne annoncée d'un côté et appliquée de l'autre ne tient "+
				"que si les DEUX sont branchés", nom)
	}

	// ⚠️ Et les deux que l'on connaît sont bien là : si l'un disparaissait du
	// code d'`internal`, la boucle ci-dessus deviendrait vide et verte.
	assert.Contains(t, déclarés, "SetTopupBounds", "le portefeuille doit ANNONCER les bornes")
	assert.Contains(t, déclarés, "SetTopupLimits", "le paiement doit les APPLIQUER")
}

// setteursDéclarés trouve les méthodes de CÂBLAGE `SetTopup…` d'`internal`.
//
// ⚠️ UNE ÉCRITURE EN BASE N'EST PAS UN CÂBLAGE, et la première version les
// confondait : `Repository.SetTopup` enregistre les bornes d'un pays, `main` ne
// l'appelle pas et n'a aucune raison de le faire. L'épreuve rougissait donc sur
// un branchement qui n'existe pas — un faux refus, exactement ce qu'on reproche
// aux refus d'ailleurs.
//
// Le discriminant est le PREMIER ARGUMENT : une méthode de câblage reçoit une
// dépendance, une méthode de base reçoit un `context.Context`. C'est vrai de
// tout `internal` et ce n'est pas un hasard — une injection n'a pas de requête.
func setteursDéclarés(t *testing.T, racine string) []string {
	t.Helper()
	déclare := regexp.MustCompile(`func \([^)]+\) (SetTopup[A-Za-z]*)\(([^)]*)\)`)
	vus := map[string]bool{}
	var out []string
	for _, src := range sourcesGo(t, racine) {
		for _, m := range déclare.FindAllStringSubmatch(src, -1) {
			if strings.HasPrefix(strings.TrimSpace(m[2]), "ctx context.Context") {
				continue
			}
			if !vus[m[1]] {
				vus[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	return out
}

// sourcesGo rend le texte de chaque source Go d'une arborescence, tests exclus.
//
// ⚠️ Une arborescence VIDE n'est pas un feu vert, et c'est l'appelant qui le
// vérifie (`require.NotEmpty`) : un analyseur qui n'a rien lu ne dit rien, et
// son silence se lit comme « tout va bien ». C'est le même piège que dans
// `pkg/country/guard`.
func sourcesGo(t *testing.T, racine string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(racine, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, string(src))
		return nil
	})
	require.NoError(t, err)
	return out
}
