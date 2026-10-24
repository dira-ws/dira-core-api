package api

// ⚠️ LE CONTRAT EST SERVI OCTET POUR OCTET, ET RIEN NE LE LISAIT.
//
// `openapi.yaml` est embarqué puis renvoyé tel quel par `pkg/docs` : un YAML
// invalide compile, démarre, répond 200 — et ne casse QUE dans le navigateur de
// celui qui ouvre la documentation. C'est-à-dire chez l'équipe mobile, un jour
// où personne ne pense au commit de la veille.
//
// La matrice d'autorisation (`internal/authzmatrix`) lit bien ce fichier, mais
// elle est INERTE sans son adresse de recette : rien, dans une construction
// ordinaire, n'ouvrait le contrat.
//
// Le défaut est facile à écrire et invisible à la relecture : dans une table en
// ligne (`{description: …}`), une virgule ou un deux-points non protégés
// ferment la table au mauvais endroit, et l'analyseur s'arrête quarante lignes
// plus loin sur une accolade qui n'a rien fait. Cette épreuve l'attrape en une
// seconde.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTheEmbeddedContractIsValidYAML(t *testing.T) {
	var spec struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
		Tags    []struct {
			Name string `yaml:"name"`
		} `yaml:"tags"`
	}
	require.NoError(t, yaml.Unmarshal(OpenAPISpec, &spec),
		"openapi.yaml ne s'analyse pas : la documentation serait servie cassée")

	assert.NotEmpty(t, spec.OpenAPI, "le contrat doit déclarer sa version OpenAPI")
	// ⚠️ UN CONTRAT VIDE S'ANALYSE PARFAITEMENT. Sans ce compte, l'épreuve
	// passerait au vert sur un fichier tronqué par une mauvaise fusion — et le
	// silence se lirait comme « tout va bien ».
	assert.Greater(t, len(spec.Paths), 50, "le contrat a perdu ses chemins")
	assert.NotEmpty(t, spec.Tags, "le contrat a perdu ses rubriques")

	// Chaque chemin porte au moins une méthode : une clé de chemin laissée sans
	// corps est le résidu habituel d'un conflit de fusion mal résolu, et elle
	// s'analyse sans broncher.
	for path, methods := range spec.Paths {
		assert.NotEmpty(t, methods, "%s ne déclare aucune méthode", path)
	}
}
