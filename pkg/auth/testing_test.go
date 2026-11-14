package auth

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ UN COMPTE D'ESSAI LE DIT DANS SON JETON, jusqu'au contexte de la requête :
// c'est là que la verticale le lit quand le client commande, sans demander au
// socle — c'est ce qui marque la commande, puis sa course, ses faits, ses gains.
func TestATestingAccountSaysSoInItsToken(t *testing.T) {
	m := NewManager("s3cret", time.Minute, time.Hour)
	tok, err := m.Issue(Grant{UserID: "u1", Role: RoleClient, Country: "TG", Testing: true})
	require.NoError(t, err)

	c, err := m.Verify(tok)
	require.NoError(t, err)
	assert.True(t, c.Testing, "le marqueur traverse le jeton")

	ctx := WithClaims(context.Background(), c)
	assert.True(t, TestingFromContext(ctx), "et le contexte de la requête")
	code, _ := CountryFromContext(ctx)
	assert.Equal(t, "TG", code, "sans rien retirer au reste")

	refresh, err := m.IssueRefresh(Grant{UserID: "u1", Role: RoleClient, Testing: true})
	require.NoError(t, err)
	rc, err := m.Verify(refresh)
	require.NoError(t, err)
	assert.True(t, rc.Testing, "le rafraîchissement le garde aussi")
}

// ⚠️ UN JETON RÉEL EST CELUI D'AVANT : pas de `tst` du tout, plutôt qu'un
// `tst: false` qu'un lecteur futur pourrait prendre pour une règle.
func TestARealAccountsTokenCarriesNoTrialMarker(t *testing.T) {
	m := NewManager("s3cret", time.Minute, time.Hour)
	tok, err := m.Issue(Grant{UserID: "u1", Role: RoleClient, Country: "TG"})
	require.NoError(t, err)

	parts := strings.Split(tok, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	assert.NotContains(t, string(payload), `"tst"`)

	c, err := m.Verify(tok)
	require.NoError(t, err)
	assert.False(t, c.Testing)
	assert.False(t, TestingFromContext(WithClaims(context.Background(), c)))
	assert.False(t, TestingFromContext(context.Background()), "sans jeton, rien n'est un essai")
}
