package llms_test

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReasoningEffort_ParseAndString(t *testing.T) {
	levels := map[string]llms.ReasoningEffort{
		"none":   llms.ReasoningEffortNone,
		"low":    llms.ReasoningEffortLow,
		"medium": llms.ReasoningEffortMedium,
		"high":   llms.ReasoningEffortHigh,
		"xhigh":  llms.ReasoningEffortXHigh,
		"max":    llms.ReasoningEffortMax,
	}
	for name, level := range levels {
		t.Run(name, func(t *testing.T) {
			parsed, err := llms.ParseReasoningEffort(name)
			require.NoError(t, err)
			assert.Equal(t, level, parsed)
			assert.Equal(t, name, level.String())
		})
	}

	t.Run("case and spaces are ignored", func(t *testing.T) {
		parsed, err := llms.ParseReasoningEffort("  XHigh ")
		require.NoError(t, err)
		assert.Equal(t, llms.ReasoningEffortXHigh, parsed)
	})

	for _, invalid := range []string{"", "default", "minimal", "hihg", "5"} {
		t.Run("invalid "+invalid, func(t *testing.T) {
			parsed, err := llms.ParseReasoningEffort(invalid)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid reasoning effort")
			assert.Equal(t, llms.ReasoningEffortDefault, parsed)
		})
	}

	assert.Empty(t, llms.ReasoningEffortDefault.String())
	assert.Empty(t, llms.ReasoningEffort(99).String())
}

func TestReasoningEffort_Order(t *testing.T) {
	ordered := []llms.ReasoningEffort{
		llms.ReasoningEffortDefault,
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	}
	for i := 1; i < len(ordered); i++ {
		assert.Less(t, ordered[i-1], ordered[i])
	}
}

func TestEffortSet(t *testing.T) {
	lowToHigh := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	highOnly := llms.NewEffortSet(llms.ReasoningEffortHigh)
	gapped := llms.NewEffortSet(llms.ReasoningEffortNone, llms.ReasoningEffortHigh, llms.ReasoningEffortMax)
	var empty llms.EffortSet

	t.Run("membership", func(t *testing.T) {
		assert.True(t, lowToHigh.Has(llms.ReasoningEffortMedium))
		assert.False(t, lowToHigh.Has(llms.ReasoningEffortXHigh))
		assert.False(t, lowToHigh.Has(llms.ReasoningEffortDefault))
		assert.False(t, lowToHigh.Has(llms.ReasoningEffort(99)))
		assert.True(t, empty.Empty())
		assert.False(t, lowToHigh.Empty())
		assert.Equal(t, []llms.ReasoningEffort{
			llms.ReasoningEffortNone,
			llms.ReasoningEffortHigh,
			llms.ReasoningEffortMax,
		}, gapped.Levels())
		assert.Empty(t, empty.Levels())
	})

	t.Run("default and unknown values are not levels", func(t *testing.T) {
		set := llms.NewEffortSet(llms.ReasoningEffortDefault, llms.ReasoningEffort(99))
		assert.True(t, set.Empty())
	})

	tests := []struct {
		name string
		set  llms.EffortSet
		ask  llms.ReasoningEffort
		want llms.ReasoningEffort
	}{
		{"supported level is kept", lowToHigh, llms.ReasoningEffortMedium, llms.ReasoningEffortMedium},
		{"above the top snaps down", lowToHigh, llms.ReasoningEffortXHigh, llms.ReasoningEffortHigh},
		{"max snaps down", lowToHigh, llms.ReasoningEffortMax, llms.ReasoningEffortHigh},
		{"below the floor snaps up", lowToHigh, llms.ReasoningEffortNone, llms.ReasoningEffortLow},
		{"single level from below", highOnly, llms.ReasoningEffortMedium, llms.ReasoningEffortHigh},
		{"single level from above", highOnly, llms.ReasoningEffortMax, llms.ReasoningEffortHigh},
		{"gap prefers the level below", gapped, llms.ReasoningEffortMedium, llms.ReasoningEffortNone},
		{"gap between high and max", gapped, llms.ReasoningEffortXHigh, llms.ReasoningEffortHigh},
		{"empty set", empty, llms.ReasoningEffortHigh, llms.ReasoningEffortDefault},
		{"default is not a level", lowToHigh, llms.ReasoningEffortDefault, llms.ReasoningEffortDefault},
		{"unknown value is not a level", lowToHigh, llms.ReasoningEffort(99), llms.ReasoningEffortDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.set.Nearest(tt.ask))
		})
	}
}

func TestEffortPolicy_IsZero(t *testing.T) {
	assert.True(t, llms.EffortPolicy{}.IsZero())
	for _, policy := range []llms.EffortPolicy{
		{Default: llms.ReasoningEffortLow},
		{Max: llms.ReasoningEffortHigh},
		{Supported: llms.NewEffortSet(llms.ReasoningEffortLow)},
		{Implicit: llms.ReasoningEffortMedium},
	} {
		assert.False(t, policy.IsZero(), "%+v", policy)
	}
}

func TestEffortSet_With(t *testing.T) {
	low := llms.NewEffortSet(llms.ReasoningEffortLow)
	extended := low.With(llms.ReasoningEffortNone, llms.ReasoningEffortHigh)
	assert.Equal(t, []llms.ReasoningEffort{
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortHigh,
	}, extended.Levels())
	// The receiver is not changed, and a value that is no level adds nothing.
	assert.Equal(t, []llms.ReasoningEffort{llms.ReasoningEffortLow}, low.Levels())
	assert.Equal(t, low, low.With(llms.ReasoningEffortDefault))
}

func TestEffortPolicy_Resolve(t *testing.T) {
	lowToHigh := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	lowToMax := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
	highOnly := llms.NewEffortSet(llms.ReasoningEffortHigh)

	tests := []struct {
		name      string
		policy    llms.EffortPolicy
		requested llms.ReasoningEffort
		supported llms.EffortSet
		implicit  llms.ReasoningEffort
		want      llms.ReasoningEffort
	}{
		// The rows of the table in Documentation/llm-factory.md, "Reasoning effort".
		{
			name:      "xhigh on a model that stops at high",
			requested: llms.ReasoningEffortXHigh,
			supported: lowToHigh,
			want:      llms.ReasoningEffortHigh,
		},
		{
			name:      "xhigh under a high ceiling",
			policy:    llms.EffortPolicy{Max: llms.ReasoningEffortHigh},
			requested: llms.ReasoningEffortXHigh,
			supported: lowToMax,
			want:      llms.ReasoningEffortHigh,
		},
		{
			name:      "none on a model that starts at low",
			requested: llms.ReasoningEffortNone,
			supported: lowToHigh,
			want:      llms.ReasoningEffortLow,
		},
		{
			name:      "medium on a high-only model",
			requested: llms.ReasoningEffortMedium,
			supported: highOnly,
			want:      llms.ReasoningEffortHigh,
		},
		{
			name:      "config default when the call sets none",
			policy:    llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			supported: lowToHigh,
			want:      llms.ReasoningEffortMedium,
		},
		{
			name:      "nothing requested and nothing configured",
			supported: lowToHigh,
			want:      llms.ReasoningEffortDefault,
		},
		// Precedence and ceiling details.
		{
			name:      "call option wins over the config default",
			policy:    llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			requested: llms.ReasoningEffortLow,
			supported: lowToHigh,
			want:      llms.ReasoningEffortLow,
		},
		{
			name: "config default above the ceiling is lowered",
			policy: llms.EffortPolicy{
				Default: llms.ReasoningEffortMax,
				Max:     llms.ReasoningEffortMedium,
			},
			supported: lowToMax,
			want:      llms.ReasoningEffortMedium,
		},
		{
			name:      "ceiling below the model floor uses the floor",
			policy:    llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			requested: llms.ReasoningEffortMedium,
			supported: highOnly,
			want:      llms.ReasoningEffortHigh,
		},
		{
			name:      "ceiling does not raise a lower request",
			policy:    llms.EffortPolicy{Max: llms.ReasoningEffortXHigh},
			requested: llms.ReasoningEffortLow,
			supported: lowToMax,
			want:      llms.ReasoningEffortLow,
		},
		{
			name:      "implicit level above the ceiling is capped",
			policy:    llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			supported: lowToHigh,
			implicit:  llms.ReasoningEffortHigh,
			want:      llms.ReasoningEffortLow,
		},
		{
			name:      "implicit level is left to the provider when the floor is not below it",
			policy:    llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			supported: highOnly,
			implicit:  llms.ReasoningEffortMedium,
			want:      llms.ReasoningEffortDefault,
		},
		{
			name:      "implicit level within the ceiling is left to the provider",
			policy:    llms.EffortPolicy{Max: llms.ReasoningEffortHigh},
			supported: lowToHigh,
			implicit:  llms.ReasoningEffortMedium,
			want:      llms.ReasoningEffortDefault,
		},
		{
			name:      "implicit level without a ceiling is left to the provider",
			supported: lowToHigh,
			implicit:  llms.ReasoningEffortHigh,
			want:      llms.ReasoningEffortDefault,
		},
		{
			name:      "model without a reasoning control",
			policy:    llms.EffortPolicy{Default: llms.ReasoningEffortHigh},
			requested: llms.ReasoningEffortMax,
			want:      llms.ReasoningEffortDefault,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.policy.Resolve(tt.requested, tt.supported, tt.implicit))
		})
	}

	assert.True(t, llms.EffortPolicy{}.IsZero())
	assert.False(t, llms.EffortPolicy{Max: llms.ReasoningEffortLow}.IsZero())
	assert.False(t, llms.EffortPolicy{Default: llms.ReasoningEffortLow}.IsZero())
}
