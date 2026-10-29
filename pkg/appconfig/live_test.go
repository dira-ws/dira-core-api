package appconfig

// CE QUI ALLUME — ET CE QUI N'ALLUME PAS — L'ASSISTANT EN DIRECT.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ LE DRAPEAU ET LE BLOC NE PEUVENT PAS SE CONTREDIRE, et c'est la
// propriété qui compte : l'application a besoin des deux, et c'est elle qui
// aurait tranché si nous avions servi l'un sans l'autre.
func TestTheFlagAndTheBlockAlwaysAgree(t *testing.T) {
	cases := []struct {
		name string
		reg  *Registry
		ed   string
		want bool
	}{
		{"aucun réglage", &Registry{}, "lite", false},
		{"registre nil", nil, "lite", false},
		{"un autre pays", &Registry{Live: map[string]LiveRule{"GA": {Languages: []string{"fr"}}}}, "lite", false},
		{"le pays, sans langue", &Registry{Live: map[string]LiveRule{"TD": {}}}, "lite", false},
		{"le pays et une langue", &Registry{Live: map[string]LiveRule{"TD": {Languages: []string{"fr"}}}}, "lite", true},
		{"retiré de cette édition", &Registry{
			Live:     map[string]LiveRule{"TD": {Languages: []string{"fr"}}},
			Features: map[string]Rule{FeatureAssistantLive: {Editions: []string{"lite"}, Countries: []string{"TD"}}},
		}, "lite", false},
		{"retiré de l'AUTRE édition", &Registry{
			Live:     map[string]LiveRule{"TD": {Languages: []string{"fr"}}},
			Features: map[string]Rule{FeatureAssistantLive: {Editions: []string{"lite"}, Countries: []string{"TD"}}},
		}, "full", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.reg.Resolve("TD", c.ed)
			assert.Equal(t, c.want, got.Features[FeatureAssistantLive], "le drapeau")
			assert.Equal(t, c.want, got.Live != nil, "le bloc")
		})
	}
}

// ⚠️ LA CASSE DU PAYS NE DÉCIDE PAS. Un opérateur écrit `td` dans la console,
// un jeton porte `TD` : c'est le même pays, et l'avoir comparé tel quel aurait
// éteint le direct pour une requête sur deux.
func TestTheCountryCaseDoesNotDecide(t *testing.T) {
	r := &Registry{Live: map[string]LiveRule{"TD": {Languages: []string{"fr"}}}}
	_, on := r.LiveFor("td", "lite")
	assert.True(t, on)
}

// ⚠️ LES DÉFAUTS SONT SERVIS, pas laissés à zéro : une application ne doit pas
// connaître nos défauts pour savoir quand couper son micro. Un `0` servi aurait
// voulu dire « coupe tout de suite » ou « ne coupe jamais », selon le lecteur.
func TestTheServedBlockNeverHasAZeroLimit(t *testing.T) {
	r := &Registry{Live: map[string]LiveRule{"TD": {Languages: []string{"fr"}}}}
	block, on := r.LiveFor("TD", "lite")
	require.True(t, on)
	assert.Equal(t, LiveMaxSessionSecondsDefault, block.MaxSessionSeconds)
	assert.Equal(t, LiveIdleSecondsDefault, block.IdleTimeoutSeconds)
	assert.Equal(t, LiveInputSampleRate, block.Input.SampleRate)
	assert.NotEmpty(t, block.Input.Encodings, "sans encodage annoncé, l'application ne sait pas quoi envoyer")
}

// Les réglages de l'exploitation l'emportent sur les défauts.
func TestTheOperatorsLimitsWin(t *testing.T) {
	r := &Registry{Live: map[string]LiveRule{"TD": {
		Languages: []string{"fr"}, MaxSessionSeconds: 120, IdleTimeoutSeconds: 10,
	}}}
	block, _ := r.LiveFor("TD", "lite")
	assert.Equal(t, 120, block.MaxSessionSeconds)
	assert.Equal(t, 10, block.IdleTimeoutSeconds)
}

// ⚠️ LES LANGUES SONT TRIÉES ET DÉDOUBLONNÉES : la réponse de `/app/config` est
// mise en cache, et un ordre qui change à chaque lecture l'invalide sans
// qu'aucun réglage ait bougé.
func TestTheLanguagesAreSortedAndDeduplicated(t *testing.T) {
	r := &Registry{Live: map[string]LiveRule{"TD": {Languages: []string{"shu", " FR ", "shu", ""}}}}
	block, on := r.LiveFor("TD", "lite")
	require.True(t, on)
	assert.Equal(t, []string{"fr", "shu"}, block.Languages)
}

// ⚠️⚠️ LE BLOC N'ANNONCE QUE DES ENCODAGES QUE LE RELAIS SAIT DÉCODER. La
// liste est dans le code pour cette raison : annoncer `opus` sans décodeur
// donnerait un micro qui part et un serveur qui n'entend rien — une panne
// muette côté application, pour un réglage posé côté serveur.
func TestOnlyTheRelaysOwnEncodingsAreAnnounced(t *testing.T) {
	r := &Registry{Live: map[string]LiveRule{"TD": {Languages: []string{"fr"}}}}
	block, _ := r.LiveFor("TD", "lite")
	assert.Equal(t, LiveInputEncodings, block.Input.Encodings)
	for _, e := range block.Input.Encodings {
		assert.NotEmpty(t, e)
	}
}

// ⚠️ ET LA LISTE SERVIE EST UNE COPIE. Rendre la tranche du paquet aurait
// laissé n'importe quel appelant écraser les capacités du relais pour tout le
// processus — un `block.Input.Encodings[0] = "opus"` quelque part, et le socle
// annonce un format que personne ne décode.
func TestTheAnnouncedEncodingsCannotBeMutatedFromOutside(t *testing.T) {
	r := &Registry{Live: map[string]LiveRule{"TD": {Languages: []string{"fr"}}}}
	block, _ := r.LiveFor("TD", "lite")
	block.Input.Encodings[0] = "opus"
	assert.Equal(t, LiveEncodingPCM16, LiveInputEncodings[0],
		"les capacités du relais ont été réécrites depuis la réponse servie")
}
