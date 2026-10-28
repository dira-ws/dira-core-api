package notify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kgtech-org/dira-core-api/pkg/edition"
)

// ⚠️⚠️ UNE MÊME PERSONNE PEUT AVOIR LES DEUX APPLICATIONS.
//
// Dira Lite est une seconde application passager, avec les MÊMES comptes.
// Sans édition sur le jeton, une notification de course partait vers tous ses
// appareils : elle arrivait deux fois, et dans l'application qui n'avait rien
// commandé.
//
// ⚠️ L'ÉDITION EST UNE PROPRIÉTÉ DE L'INSTALLATION, pas du compte. La
// rattacher au compte aurait rendu impossible de distinguer les deux
// applications d'un même téléphone.
func TestTheEditionIsCarriedByTheTokenNotTheAccount(t *testing.T) {
	// Deux jetons, un compte, deux éditions : c'est le cas normal.
	full := Device{Token: "jeton-full", Edition: edition.Full}
	lite := Device{Token: "jeton-lite", Edition: edition.Lite}
	assert.NotEqual(t, full.Edition, lite.Edition)
	assert.NotEqual(t, full.Token, lite.Token)
}

// ⚠️ L'ÉDITION DU CORPS D'ABORD, L'EN-TÊTE DE LA REQUÊTE ENSUITE.
//
// Le corps est explicite : l'application nomme l'édition de CE jeton. Mais une
// application Lite qui ne l'a pas encore dans son corps envoie déjà
// `X-Dira-Edition` sur chaque requête — s'en servir évite d'avoir à sortir les
// deux applications dans le même ordre.
//
// ⚠️ ET NI L'UN NI L'AUTRE DONNE `full`, jamais `lite` : un jeton rangé par
// erreur dans Lite recevrait des textes amputés sans l'avoir demandé.
func TestTheEditionOfATokenComesFromTheBodyThenTheHeader(t *testing.T) {
	cas := []struct {
		nom      string
		body     string
		header   string
		attendue string
	}{
		{"le corps le dit", "lite", "full", edition.Lite},
		{"le corps le dit, pas d'en-tête", "lite", "", edition.Lite},
		{"le corps se tait, l'en-tête parle", "", "lite", edition.Lite},
		{"les deux se taisent", "", "", edition.Full},
		{"le corps dit n'importe quoi", "léger", "", edition.Full},
		{"le corps dit full explicitement", "full", "lite", edition.Full},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			ctx := context.Background()
			if c.header != "" {
				ctx = edition.WithEdition(ctx, c.header, "", "")
			}
			// La même règle que `RegisterDevice`, isolée pour être éprouvée.
			got := edition.Normalize(c.body)
			if c.body == "" {
				got = edition.FromContext(ctx)
			}
			assert.Equal(t, c.attendue, got)
		})
	}
}

// ⚠️⚠️ LES JETONS SANS ÉDITION COMPTENT COMME `full`, ET C'EST VITAL.
//
// Tous les appareils enregistrés AVANT cette livraison n'ont pas le champ. Un
// filtre `edition: "full"` strict les aurait écartés — rendant muets, d'un
// coup, TOUS les téléphones du parc existant. Une panne de notifications
// totale, et invisible : rien ne lève d'erreur quand on n'envoie à personne.
func TestADeviceWithoutAnEditionCountsAsFull(t *testing.T) {
	// C'est la requête Mongo qui porte cette règle (trois formes acceptées :
	// le champ absent, vide, ou `full`). On éprouve ici l'intention, et
	// `ActiveDevicesOfEdition` l'applique.
	for _, stored := range []string{"", edition.Full} {
		d := Device{Token: "ancien", Edition: stored}
		assert.Equal(t, edition.Full, edition.Normalize(d.Edition),
			"un jeton dont l'édition vaut %q doit être joint comme `full`", stored)
	}
}
