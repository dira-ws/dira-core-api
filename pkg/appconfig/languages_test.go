package appconfig

// LE CATALOGUE DES LANGUES — ce qu'une application reçoit, et ce qui ne sort pas.

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codes(items []LanguageItem) []string {
	out := make([]string, 0, len(items))
	for _, l := range items {
		out = append(out, l.Code)
	}
	return out
}

// LA LISTE DE DÉPART EST CELLE QUI A ÉTÉ DEMANDÉE — et elle ne promet rien
// qu'on n'ait pas : trois langues d'écran traduites, deux langues mesurées au
// direct. Une case cochée par erreur ici ouvrirait un micro facturé dans une
// langue que personne n'a écoutée, sur tous les environnements, au déploiement.
func TestTheStartingListIsTheOneAskedFor(t *testing.T) {
	var r *Registry
	block := r.LanguagesFor()
	assert.Equal(t, []string{"fr", "en", "shu", "ee", "wo", "ff", "man", "bm"}, codes(block.Items))
	assert.Equal(t, "fr", block.Default)

	var screens []string
	for _, l := range block.Items {
		assert.True(t, l.Spoken, "%s se parle", l.Code)
		if l.App {
			screens = append(screens, l.Code)
		}
	}
	assert.Equal(t, []string{"fr", "en", "shu"}, screens, "seules les langues traduites s'offrent aux écrans")
	assert.Equal(t, []string{"fr", "shu"}, r.LiveLanguages(), "seules les langues mesurées s'ouvrent au direct")
}

// ⚠️ UNE RÉPONSE PUBLIQUE, SANS JETON : l'indication au modèle et le code Whisper
// restent chez nous. L'épreuve regarde le JSON réellement servi, pas la structure.
func TestTheServedBlockCarriesNothingAboutTheModels(t *testing.T) {
	r := &Registry{Languages: []Language{{Code: "wo", Name: "Wolof", Native: "Wolof", App: true, Spoken: true,
		Live: true, Hint: "orthographe officielle", Whisper: "xx"}}}
	raw, err := json.Marshal(r.Resolve("TD", "lite").Languages)
	require.NoError(t, err)
	var got struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Len(t, got.Items, 1)
	keys := make([]string, 0, len(got.Items[0]))
	for k := range got.Items[0] {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"code", "name", "native", "rtl", "app", "spoken"}, keys)
}

// ⚠️⚠️ LA MÊME RÉPONSE PARTOUT : c'est tout l'objet du catalogue.
func TestTheCatalogueDoesNotDependOnTheCountryOrTheEdition(t *testing.T) {
	r := &Registry{Live: map[string]LiveRule{"TD": {}}}
	assert.Equal(t, r.Resolve("TD", "lite").Languages, r.Resolve("GA", "full").Languages)
}

// ⚠️ RETIRER UNE LANGUE LA RETIRE : la liste de départ ne revient pas par
// en-dessous d'un catalogue enregistré.
func TestASavedCatalogueReplacesTheStartingList(t *testing.T) {
	r := &Registry{Languages: []Language{{Code: "en", App: true, Spoken: true}}}
	assert.Equal(t, []string{"en"}, codes(r.LanguagesFor().Items))
}

// UNE LANGUE QUI NE S'OFFRE NULLE PART RESTE AU CATALOGUE, SANS ÊTRE SERVIE.
func TestALanguageOfferedNowhereIsNotServed(t *testing.T) {
	r := &Registry{Languages: []Language{
		{Code: "fr", App: true, Spoken: true},
		{Code: "kbp"},
	}}
	assert.Equal(t, []string{"fr"}, codes(r.LanguagesFor().Items))
	assert.Len(t, r.LanguageCatalogue(), 2, "elle reste à l'écran de la console")
}

// LE DÉFAUT EST LA PREMIÈRE LANGUE D'ÉCRAN — pas la première de la liste : une
// langue qui ne fait que se parler n'a pas de traduction à montrer.
func TestTheDefaultIsTheFirstScreenLanguage(t *testing.T) {
	r := &Registry{Languages: []Language{
		{Code: "wo", Spoken: true},
		{Code: "en", App: true, Spoken: true},
		{Code: "fr", App: true, Spoken: true},
	}}
	assert.Equal(t, "en", r.LanguagesFor().Default)
}

// Un catalogue corrigé à la main sans langue d'écran ne sert pas un défaut vide.
func TestADefaultIsAlwaysServed(t *testing.T) {
	r := &Registry{Languages: []Language{{Code: "wo", Spoken: true}}}
	assert.Equal(t, "fr", r.LanguagesFor().Default)
}

// ⚠️ C'EST L'ÉCRITURE QUI RETOURNE L'INTERFACE : `rtl` vient du script.
func TestTheArabicScriptReadsRightToLeft(t *testing.T) {
	var r *Registry
	for _, l := range r.LanguagesFor().Items {
		assert.Equal(t, l.Code == "shu", l.RTL, l.Code)
	}
}

// ⚠️ OUVRIR UN MICRO NE TIENT PAS À UN SEUL CONTRÔLE : une langue cochée au
// direct mais qui ne se parle pas (document corrigé à la main) reste fermée.
func TestALanguageThatIsNotSpokenStaysClosedToTheLive(t *testing.T) {
	r := &Registry{Languages: []Language{{Code: "fr", App: true, Live: true}}}
	assert.Empty(t, r.LiveLanguages())
}

// Écrire dans la réponse n'écrit pas dans le catalogue.
func TestTheCatalogueCannotBeMutatedFromOutside(t *testing.T) {
	var r *Registry
	list := r.LanguageCatalogue()
	list[0].Code = "zz"
	assert.Equal(t, "fr", r.LanguageCatalogue()[0].Code)
}
