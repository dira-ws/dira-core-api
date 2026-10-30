package voice

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Les limites servies SONT les constantes — et la liste est stable.
func TestServedLimitsAreTheConstants(t *testing.T) {
	l := Served()
	assert.Equal(t, MaxSeconds, l.MaxSeconds)
	assert.Equal(t, MaxBytes, l.MaxBytes)
	assert.Len(t, l.Formats, len(Types))
	assert.IsIncreasing(t, l.Formats, "une liste qui change d'ordre d'un appel à l'autre fait recharger les écrans")
	assert.Equal(t, l, Served())
}

// ⚠️ PAS DE WAV : vingt secondes y pèsent un demi-mégaoctet.
func TestNoWAV(t *testing.T) {
	for _, wav := range []string{"audio/wav", "audio/x-wav", "audio/wave"} {
		assert.NotContains(t, Types, wav)
	}
}
