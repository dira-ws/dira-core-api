package appconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kgtech-org/dira-core-api/pkg/voice"
)

// ⚠️ LES LIMITES D'UN VOCAL SONT SERVIES PARTOUT (core#265) — chaque pays,
// chaque édition, et même sans registre : une application qui ne les lit pas
// fait parler quelqu'un pour un refus.
func TestTheVoiceLimitsAreServedEverywhere(t *testing.T) {
	var none *Registry
	for _, got := range []Resolved{
		none.Resolve("TD", "lite"),
		(&Registry{}).Resolve("TG", "full"),
	} {
		assert.Equal(t, voice.Served(), got.Voice)
		assert.Equal(t, 60, got.Voice.MaxSeconds)
		assert.Equal(t, 2<<20, got.Voice.MaxBytes)
		assert.Contains(t, got.Voice.Formats, "audio/ogg")
	}
}
