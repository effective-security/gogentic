package googleai

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sentThinkingConfig runs one GenerateContent call and returns the thinking
// configuration found in the request body, or nil when the request carried
// none.
func sentThinkingConfig(t *testing.T, model string, opts []Option, callOpts ...llms.CallOption) map[string]any {
	t.Helper()
	f := &fixture{t: t, response: textResponse}
	opts = append([]Option{
		WithDefaultModel(model),
		WithAPIKey("fixture"),
		WithHTTPClient(f.client()),
	}, opts...)
	m, err := New(context.Background(), opts...)
	require.NoError(t, err)

	messages := []llms.Message{{
		Role: llms.RoleHuman,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "hi"},
		},
	}}
	_, err = m.GenerateContent(context.Background(), messages, callOpts...)
	require.NoError(t, err)
	require.Len(t, f.requests, 1)

	thinking, _ := generation(f.requests[0])["thinkingConfig"].(map[string]any)
	return thinking
}

// sentThinkingLevel runs one GenerateContent call and returns the thinking
// level found in the request body. ok is false when the request carried no
// thinking configuration. A configuration that asks for anything but a level
// fails the test.
func sentThinkingLevel(t *testing.T, model string, opts []Option, callOpts ...llms.CallOption) (level string, ok bool) {
	t.Helper()
	thinking := sentThinkingConfig(t, model, opts, callOpts...)
	if thinking == nil {
		return "", false
	}
	level, ok = thinking["thinkingLevel"].(string)
	require.True(t, ok, "thinking configuration without a level: %v", thinking)
	assert.NotContains(t, thinking, "thinkingBudget")
	return level, true
}

// sentThinkingBudget runs one GenerateContent call and returns the thinking
// budget found in the request body. ok is false when the request carried no
// thinking configuration. A configuration that asks for anything but a budget
// fails the test.
func sentThinkingBudget(t *testing.T, model string, opts []Option, callOpts ...llms.CallOption) (budget float64, ok bool) {
	t.Helper()
	thinking := sentThinkingConfig(t, model, opts, callOpts...)
	if thinking == nil {
		return 0, false
	}
	budget, ok = thinking["thinkingBudget"].(float64)
	require.True(t, ok, "thinking configuration without a budget: %v", thinking)
	assert.NotContains(t, thinking, "thinkingLevel")
	return budget, true
}

// effortLabel names a level for subtest titles.
func effortLabel(e llms.ReasoningEffort) string {
	if e == llms.ReasoningEffortDefault {
		return "unset"
	}
	return e.String()
}

// TestReasoningEffort_Unchanged pins the requests the effort refactoring must
// not change: nothing is sent when no level is requested, and low and high
// are passed through as thinking levels. Gemini 2.5 is the exception, covered
// by TestReasoningEffort_Budget: it rejects a thinking level.
func TestReasoningEffort_Unchanged(t *testing.T) {
	const notSent = ""
	tests := []struct {
		model  string
		effort llms.ReasoningEffort
		want   string
	}{
		{"gemini-2.5-pro", llms.ReasoningEffortDefault, notSent},
		{"gemini-3-pro-preview", llms.ReasoningEffortDefault, notSent},
		{"gemini-3.6-flash", llms.ReasoningEffortDefault, notSent},
		{"fixture", llms.ReasoningEffortDefault, notSent},

		{"gemini-2.5-flash", llms.ReasoningEffortDefault, notSent},
		{"gemini-2.5-flash-lite", llms.ReasoningEffortDefault, notSent},
		{"gemini-2.0-flash", llms.ReasoningEffortDefault, notSent},

		{"gemini-3-pro-preview", llms.ReasoningEffortLow, "LOW"},
		{"gemini-3-pro-preview", llms.ReasoningEffortMedium, "LOW"},
		{"gemini-3-pro-preview", llms.ReasoningEffortHigh, "HIGH"},
		{"gemini-3.6-flash", llms.ReasoningEffortHigh, "HIGH"},
		{"fixture", llms.ReasoningEffortLow, "LOW"},
		{"fixture", llms.ReasoningEffortHigh, "HIGH"},
	}
	for _, tt := range tests {
		t.Run(tt.model+"/"+effortLabel(tt.effort), func(t *testing.T) {
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			level, ok := sentThinkingLevel(t, tt.model, nil, callOpts...)
			if tt.want == notSent {
				assert.False(t, ok, "unexpected thinking level %q", level)
				return
			}
			require.True(t, ok, "thinking level was not sent")
			assert.Equal(t, tt.want, level)
		})
	}
}

// TestReasoningEffort_Levels covers the levels that were not mapped before:
// each is sent as the nearest thinking level the model accepts.
func TestReasoningEffort_Levels(t *testing.T) {
	tests := []struct {
		model  string
		effort llms.ReasoningEffort
		want   string
	}{
		// medium where the model has it.
		{"gemini-3.1-pro-preview", llms.ReasoningEffortMedium, "MEDIUM"},
		{"gemini-3.6-flash", llms.ReasoningEffortMedium, "MEDIUM"},
		{"gemini-4-argon", llms.ReasoningEffortMedium, "MEDIUM"},
		{"fixture", llms.ReasoningEffortMedium, "MEDIUM"},

		// Levels above high are sent as high.
		{"gemini-3.1-pro-preview", llms.ReasoningEffortXHigh, "HIGH"},
		{"gemini-3-pro-preview", llms.ReasoningEffortMax, "HIGH"},
		{"gemini-3.6-flash", llms.ReasoningEffortMax, "HIGH"},

		// none is the model's floor: minimal, or the lowest level.
		{"gemini-3-flash-preview", llms.ReasoningEffortNone, "MINIMAL"},
		{"gemini-3.6-flash", llms.ReasoningEffortNone, "MINIMAL"},
		{"gemini-3.5-flash-lite", llms.ReasoningEffortNone, "MINIMAL"},
		{"gemini-3.8-flash", llms.ReasoningEffortNone, "LOW"},
		{"gemini-3-pro-preview", llms.ReasoningEffortNone, "LOW"},
		{"gemini-4-argon", llms.ReasoningEffortNone, "LOW"},
	}
	for _, tt := range tests {
		t.Run(tt.model+"/"+effortLabel(tt.effort), func(t *testing.T) {
			level, ok := sentThinkingLevel(t, tt.model, nil, llms.WithReasoningEffort(tt.effort))
			require.True(t, ok, "thinking level was not sent")
			assert.Equal(t, tt.want, level)
		})
	}
}

// Gemini 2.5 takes a thinking token budget and rejects a thinking level.
func TestReasoningEffort_Budget(t *testing.T) {
	const notSent = -1
	tests := []struct {
		name   string
		model  string
		policy llms.EffortPolicy
		effort llms.ReasoningEffort
		want   float64
	}{
		{
			name:   "low",
			model:  "gemini-2.5-pro",
			effort: llms.ReasoningEffortLow,
			want:   1024,
		},
		{
			name:   "medium",
			model:  "gemini-2.5-pro",
			effort: llms.ReasoningEffortMedium,
			want:   8192,
		},
		{
			name:   "high",
			model:  "gemini-2.5-flash",
			effort: llms.ReasoningEffortHigh,
			want:   24576,
		},
		{
			name:   "levels above high are sent as high",
			model:  "gemini-2.5-flash-lite",
			effort: llms.ReasoningEffortMax,
			want:   24576,
		},
		{
			name:   "snapshot of a 2.5 model",
			model:  "gemini-2.5-flash-preview-09-2025",
			effort: llms.ReasoningEffortMedium,
			want:   8192,
		},
		{
			name:   "none turns thinking off on flash",
			model:  "gemini-2.5-flash",
			effort: llms.ReasoningEffortNone,
			want:   0,
		},
		{
			name:   "none turns thinking off on flash-lite",
			model:  "gemini-2.5-flash-lite",
			effort: llms.ReasoningEffortNone,
			want:   0,
		},
		{
			name:   "none is the lowest budget on pro, which cannot stop thinking",
			model:  "gemini-2.5-pro",
			effort: llms.ReasoningEffortNone,
			want:   1024,
		},
		{
			name:   "config default is used when the call sets none",
			model:  "gemini-2.5-pro",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			want:   8192,
		},
		{
			name:   "call option wins over the config default",
			model:  "gemini-2.5-pro",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			effort: llms.ReasoningEffortHigh,
			want:   24576,
		},
		{
			name:   "ceiling caps a model that decides how much to think",
			model:  "gemini-2.5-flash",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   1024,
		},
		{
			name:   "ceiling of none turns thinking off",
			model:  "gemini-2.5-flash",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortNone},
			want:   0,
		},
		{
			name:   "ceiling leaves a model that does not think alone",
			model:  "gemini-2.5-flash-lite",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
		{
			name:   "model before 2.5 has no thinking control",
			model:  "gemini-2.0-flash",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			effort: llms.ReasoningEffortHigh,
			want:   notSent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []Option{
				WithEffortPolicy(tt.policy),
			}
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			budget, ok := sentThinkingBudget(t, tt.model, opts, callOpts...)
			if tt.want == notSent {
				assert.False(t, ok, "unexpected thinking budget %v", budget)
				return
			}
			require.True(t, ok, "thinking budget was not sent")
			assert.Equal(t, tt.want, budget)
		})
	}
}

// The thinking control is the one of the model the request is sent to. A
// model named by a call option is not forwarded, so it selects nothing.
func TestReasoningEffort_UsesTheClientModel(t *testing.T) {
	// gemini-3-pro has no minimal level; gemini-3-flash has.
	level, ok := sentThinkingLevel(t, "gemini-3-pro-preview", nil,
		llms.WithModel("gemini-3-flash-preview"),
		llms.WithReasoningEffort(llms.ReasoningEffortNone),
	)
	require.True(t, ok, "thinking level was not sent")
	assert.Equal(t, "LOW", level)

	level, ok = sentThinkingLevel(t, "gemini-3-flash-preview", nil,
		llms.WithModel("gemini-3-pro-preview"),
		llms.WithReasoningEffort(llms.ReasoningEffortNone),
	)
	require.True(t, ok, "thinking level was not sent")
	assert.Equal(t, "MINIMAL", level)

	// A 2.5 client keeps its budget whatever the call names.
	budget, ok := sentThinkingBudget(t, "gemini-2.5-pro", nil,
		llms.WithModel("gemini-3-flash-preview"),
		llms.WithReasoningEffort(llms.ReasoningEffortMedium),
	)
	require.True(t, ok, "thinking budget was not sent")
	assert.Equal(t, float64(8192), budget)
}

func TestReasoningEffort_Policy(t *testing.T) {
	const notSent = ""
	tests := []struct {
		name   string
		model  string
		policy llms.EffortPolicy
		effort llms.ReasoningEffort
		want   string
	}{
		{
			name:   "config default is used when the call sets none",
			model:  "gemini-3.1-pro-preview",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			want:   "MEDIUM",
		},
		{
			name:   "call option wins over the config default",
			model:  "gemini-3.1-pro-preview",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			effort: llms.ReasoningEffortHigh,
			want:   "HIGH",
		},
		{
			name:   "ceiling lowers a requested level",
			model:  "gemini-3.6-flash",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			effort: llms.ReasoningEffortHigh,
			want:   "LOW",
		},
		{
			name:   "ceiling caps a model that thinks at high on its own",
			model:  "gemini-3-pro-preview",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   "LOW",
		},
		{
			name:   "ceiling caps a model that thinks at medium on its own",
			model:  "gemini-3.6-flash",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   "LOW",
		},
		{
			name:   "ceiling at the model's own level sends nothing",
			model:  "gemini-3.6-flash",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortMedium},
			want:   notSent,
		},
		{
			name:   "ceiling leaves a model that barely thinks alone",
			model:  "gemini-3.5-flash-lite",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []Option{
				WithEffortPolicy(tt.policy),
			}
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			level, ok := sentThinkingLevel(t, tt.model, opts, callOpts...)
			if tt.want == notSent {
				assert.False(t, ok, "unexpected thinking level %q", level)
				return
			}
			require.True(t, ok, "thinking level was not sent")
			assert.Equal(t, tt.want, level)
		})
	}
}
