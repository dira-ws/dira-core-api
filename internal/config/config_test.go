package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️⚠️ LE SOCLE DÉMARRE AVEC L'ENVIRONNEMENT D'AVANT. Une fusion publie une image
// que la préprod déploie SEULE, avant que quiconque touche à son `.env` : il y
// a encore la configuration d'une verticale qui ne fait plus partie de Dira —
// ses `IMMO_*`, et peut-être une liste de jetons par service
// (`CORE_SERVICE_TOKENS`) que ce socle n'a jamais lue. Rien de tout cela ne
// doit faire tomber le démarrage, ni se glisser dans le secret de service :
// seul `CORE_SERVICE_TOKEN` ouvre les routes de service.
func TestTheCoreStartsWithTheEnvironmentOfBefore(t *testing.T) {
	t.Setenv("CORE_SERVICE_TOKEN", "le-secret-partage")
	t.Setenv("CORE_SERVICE_TOKENS", "immo=un-autre-secret,food=encore-un")
	t.Setenv("IMMO_SERVICE_TOKEN", "x")
	t.Setenv("IMMO_CORE_BASE_URL", "http://core-api:8082")
	t.Setenv("IMMO_MEILI_MASTER_KEY", "y")
	t.Setenv("IMMO_WEB_URL", "https://exemple.invalid")

	cfg, err := Load()
	require.NoError(t, err, "une variable que le socle ne lit pas ne fait pas tomber le démarrage")
	assert.Equal(t, "le-secret-partage", cfg.ServiceToken)
}
