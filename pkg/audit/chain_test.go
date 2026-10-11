package audit

// ⚠️⚠️ CE FICHIER NE TESTE PAS « LE CHAÎNAGE MARCHE ». Il teste que les TROIS
// façons de falsifier un journal sont DÉTECTÉES — supprimer une entrée, la
// réécrire, réordonner la chaîne. C'est la seule promesse que ce mécanisme
// fait, et c'est donc la seule chose qui mérite d'être prouvée.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(n int) time.Time {
	return time.Date(2026, 10, 11, 12, 0, n, 0, time.UTC)
}

func entry(n int, action string) Entry {
	return Entry{
		Service: ServiceCore, Country: "TG",
		ActorID: "6a0000000000000000000001", ActorRole: "admin",
		Action:   action,
		Resource: Resource{Type: "user", ID: "6a0000000000000000000002"},
		Before:   map[string]any{"status": "active"},
		After:    map[string]any{"status": "suspended"},
		IP:       "10.0.0.1", CreatedAt: at(n),
	}
}

// chaîne construit une chaîne scellée de `n` entrées.
func chaine(t *testing.T, n int) []Entry {
	t.Helper()
	out := make([]Entry, 0, n)
	prev := GenesisHash
	for i := 1; i <= n; i++ {
		e, err := Link(int64(i), prev, entry(i, "user.suspend"))
		require.NoError(t, err)
		out = append(out, e)
		prev = e.Hash
	}
	return out
}

// verifie rejoue la vérification sur une tranche, comme `Verify` le fait sur un
// curseur. Rend la rupture, ou "".
func verifie(t *testing.T, entries []Entry) (reason string, atSeq int64) {
	t.Helper()
	prev := GenesisHash
	var expected int64
	for i, e := range entries {
		if i == 0 {
			expected = e.Seq
		}
		r, _, err := Step(prev, expected, e)
		require.NoError(t, err)
		if r != "" {
			return r, e.Seq
		}
		prev, expected = e.Hash, expected+1
	}
	return "", 0
}

// Une chaîne intacte passe. Si celui-ci échoue, les trois suivants ne valent
// rien.
func TestAnUntouchedChainVerifies(t *testing.T) {
	r, _ := verifie(t, chaine(t, 20))
	assert.Empty(t, r)
}

// ⚠️⚠️ SUPPRIMER UNE ENTRÉE — le cas le plus probable, parce que c'est le plus
// simple : on efface la ligne qui gêne.
func TestDeletingAnEntryIsCaught(t *testing.T) {
	c := chaine(t, 10)
	// On retire la cinquième, comme le ferait quelqu'un en base.
	amputee := append(append([]Entry{}, c[:4]...), c[5:]...)
	r, seq := verifie(t, amputee)
	assert.Equal(t, BreakMissing, r)
	assert.EqualValues(t, 6, seq, "la rupture se voit à l'entrée SUIVANTE, et elle est nommée")
}

// ⚠️⚠️ RÉÉCRIRE UNE ENTRÉE — changer ce qu'on a fait après coup.
func TestRewritingAnEntryIsCaught(t *testing.T) {
	c := chaine(t, 10)
	c[4].After = map[string]any{"status": "active"} // « je ne l'ai jamais suspendu »
	r, seq := verifie(t, c)
	assert.Equal(t, BreakContent, r)
	assert.EqualValues(t, 5, seq)
}

// ⚠️⚠️ RÉORDONNER — « il a suspendu le compte PUIS viré l'argent » ne dit pas
// la même chose que l'inverse. L'ORDRE EST L'INFORMATION.
func TestReorderingTwoEntriesIsCaught(t *testing.T) {
	c := chaine(t, 10)
	c[4], c[5] = c[5], c[4]
	r, _ := verifie(t, c)
	assert.NotEmpty(t, r, "deux entrées échangées cassent la chaîne")
}

// ⚠️ ET RECOUDRE NE SAUVE PAS : quelqu'un qui supprime une entrée puis
// renumérote les suivantes casse les EMPREINTES, parce que le numéro en fait
// partie. C'est la raison d'être de `seq` dans le hachage.
func TestRenumberingAfterADeletionStillBreaks(t *testing.T) {
	c := chaine(t, 10)
	amputee := append(append([]Entry{}, c[:4]...), c[5:]...)
	for i := 4; i < len(amputee); i++ {
		amputee[i].Seq = int64(i + 1) // on « rebouche » le trou
	}
	r, seq := verifie(t, amputee)
	// ⚠️ C'EST LE MAILLON QUI CASSE, PAS L'EMPREINTE — et c'est plus
	// informatif que ce que j'attendais en écrivant ce test. L'entrée
	// renumérotée 5 porte encore le `prev_hash` de l'entrée 5 d'origine, celle
	// qu'on vient d'effacer : le lien pend dans le vide. Le rapport dit donc
	// « la chaîne a été recousue », ce qui décrit exactement le geste.
	assert.Equal(t, BreakLink, r,
		"renuméroter comble le trou mais laisse un maillon qui pend")
	assert.EqualValues(t, 5, seq)
}

// ⚠️ ET REFABRIQUER LA CHAÎNE ENTIÈRE, ALORS ? Oui, c'est possible — et c'est
// la limite honnête de ce mécanisme, écrite ici pour qu'on ne la découvre pas
// en réunion. Quelqu'un qui a l'écriture sur la base peut tout re-sceller. Ce
// que le chaînage garantit, c'est qu'il ne peut pas toucher UNE ligne sans
// toucher TOUTES les suivantes — et qu'un ancrage externe (une empreinte
// publiée chaque jour) rendrait même cela visible.
func TestAFullyRebuiltChainVerifies(t *testing.T) {
	c := chaine(t, 5)
	c[2].After = map[string]any{"status": "active"}
	prev := GenesisHash
	for i := range c {
		e, err := Link(int64(i+1), prev, c[i])
		require.NoError(t, err)
		c[i] = e
		prev = e.Hash
	}
	r, _ := verifie(t, c)
	assert.Empty(t, r, "c'est la limite assumée : tamper-EVIDENT, pas tamper-PROOF")
}

// L'empreinte est déterministe — sinon rien de ce qui précède ne tient.
func TestTheHashIsDeterministic(t *testing.T) {
	e := entry(1, "user.suspend")
	a, err := hashOf(7, "abc", e)
	require.NoError(t, err)
	b, err := hashOf(7, "abc", e)
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.Len(t, a, 64, "sha-256 en hexadécimal")
}

// ⚠️⚠️ CHAQUE CHAMP QUI ENGAGE ENTRE DANS L'EMPREINTE. Ce test les parcourt un
// par un : si quelqu'un ajoute un champ à `Entry` sans l'ajouter à `chainable`,
// ce champ devient modifiable sans trace — et personne ne s'en apercevrait.
func TestEveryFieldThatMattersChangesTheHash(t *testing.T) {
	base := entry(1, "user.suspend")
	ref, err := hashOf(1, GenesisHash, base)
	require.NoError(t, err)

	for nom, muter := range map[string]func(Entry) Entry{
		"le service":   func(e Entry) Entry { e.Service = ServiceVTC; return e },
		"le pays":      func(e Entry) Entry { e.Country = "SN"; return e },
		"l'acteur":     func(e Entry) Entry { e.ActorID = "autre"; return e },
		"le rôle":      func(e Entry) Entry { e.ActorRole = "support"; return e },
		"l'action":     func(e Entry) Entry { e.Action = "user.delete"; return e },
		"la ressource": func(e Entry) Entry { e.Resource.ID = "autre"; return e },
		"l'avant":      func(e Entry) Entry { e.Before = map[string]any{"status": "x"}; return e },
		"l'après":      func(e Entry) Entry { e.After = map[string]any{"status": "x"}; return e },
		"l'adresse IP": func(e Entry) Entry { e.IP = "10.0.0.2"; return e },
		"l'instant":    func(e Entry) Entry { e.CreatedAt = at(2); return e },
	} {
		h, err := hashOf(1, GenesisHash, muter(base))
		require.NoError(t, err, nom)
		assert.NotEqual(t, ref, h, "modifier %s doit changer l'empreinte", nom)
	}

	// Le NUMÉRO et le MAILLON en font partie aussi — sinon réordonner ou
	// recoudre ne casserait rien.
	h, err := hashOf(2, GenesisHash, base)
	require.NoError(t, err)
	assert.NotEqual(t, ref, h, "le numéro de séquence fait partie de l'empreinte")
	h, err = hashOf(1, "autre", base)
	require.NoError(t, err)
	assert.NotEqual(t, ref, h, "le maillon précédent fait partie de l'empreinte")
}

// ⚠️ UNE DIFFÉRENCE D'UNE NANOSECONDE COMPTE. Tronquer à la seconde rendrait
// deux entrées d'une même rafale indiscernables — et leur ordre
// indémontrable.
func TestTwoEntriesInTheSameSecondAreDistinguishable(t *testing.T) {
	a := entry(1, "x")
	b := a
	b.CreatedAt = a.CreatedAt.Add(time.Nanosecond)
	ha, err := hashOf(1, GenesisHash, a)
	require.NoError(t, err)
	hb, err := hashOf(1, GenesisHash, b)
	require.NoError(t, err)
	assert.NotEqual(t, ha, hb)
}

// ⚠️ « PAS DE PRÉCÉDENT » ET « PRÉCÉDENT EFFACÉ » NE DOIVENT PAS SE RESSEMBLER
// dans un rapport : la genèse porte un mot, pas une chaîne vide.
func TestGenesisIsNamedNotEmpty(t *testing.T) {
	assert.NotEmpty(t, GenesisHash)
	assert.Equal(t, "genesis", GenesisHash)
}

// Une entrée non scellée est RÉCENTE, pas suspecte.
func TestAnUnsealedEntryIsNotABreak(t *testing.T) {
	assert.False(t, entry(1, "x").Sealed())
	sealed, err := Link(1, GenesisHash, entry(1, "x"))
	require.NoError(t, err)
	assert.True(t, sealed.Sealed())
}
