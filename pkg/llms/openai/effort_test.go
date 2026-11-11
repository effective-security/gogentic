package openai_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sentEffort runs one GenerateContent call on the Responses API and returns
// the reasoning effort found in the request body. ok is false when the request
// carried no reasoning setting.
func sentEffort(t *testing.T, model string, opts []openai.Option, callOpts ...llms.CallOption) (effort string, ok bool) {
	t.Helper()
	f := &fixture{reply: responsesTextReply}
	opts = append([]openai.Option{
		openai.WithModel(model),
		openai.WithToken("fixture"),
		openai.WithHTTPClient(f.client(t)),
	}, opts...)
	m, err := openai.New(opts...)
	require.NoError(t, err)

	messages := []llms.Message{{
		Role: llms.RoleHuman,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "hi"},
		},
	}}
	_, err = m.GenerateContent(context.Background(), messages, callOpts...)
	require.NoError(t, err)
	require.Equal(t, 1, f.calls)

	reasoning, found := f.body["reasoning"].(map[string]any)
	if !found {
		return "", false
	}
	effort, ok = reasoning["effort"].(string)
	return effort, ok
}

// TestReasoningEffort_Unchanged pins the requests that the effort refactoring
// must not change: what is sent when no level is requested, and the levels
// that were already passed through.
func TestReasoningEffort_Unchanged(t *testing.T) {
	const notSent = ""
	tests := []struct {
		model  string
		effort llms.ReasoningEffort
		want   string
	}{
		// No level requested: low, or none on gpt-5.1.
		{"gpt-5", llms.ReasoningEffortDefault, "low"},
		{"gpt-5-mini", llms.ReasoningEffortDefault, "low"},
		{"gpt-5.1", llms.ReasoningEffortDefault, "none"},
		{"gpt-5.1-mini", llms.ReasoningEffortDefault, "none"},
		{"gpt-5.1-codex", llms.ReasoningEffortDefault, "none"},
		{"gpt-5.2", llms.ReasoningEffortDefault, "low"},
		{"gpt-5.3-codex", llms.ReasoningEffortDefault, "low"},
		{"gpt-5.4-mini", llms.ReasoningEffortDefault, "low"},
		{"gpt-5.5", llms.ReasoningEffortDefault, "low"},
		{"gpt-5.6-luna", llms.ReasoningEffortDefault, "low"},
		{"o3", llms.ReasoningEffortDefault, "low"},
		{"o4-mini", llms.ReasoningEffortDefault, "low"},

		// Requested levels are passed through.
		{"gpt-5", llms.ReasoningEffortLow, "low"},
		{"gpt-5", llms.ReasoningEffortMedium, "medium"},
		{"gpt-5", llms.ReasoningEffortHigh, "high"},
		{"gpt-5.1", llms.ReasoningEffortLow, "low"},
		{"gpt-5.1", llms.ReasoningEffortNone, "none"},
		{"gpt-5.5", llms.ReasoningEffortMedium, "medium"},
		{"o4-mini", llms.ReasoningEffortHigh, "high"},

		// Models that never run below high.
		{"gpt-5-pro", llms.ReasoningEffortDefault, "high"},
		{"gpt-5-pro", llms.ReasoningEffortLow, "high"},
		{"gpt-5-pro", llms.ReasoningEffortHigh, "high"},
		{"gpt-5.6-sol", llms.ReasoningEffortDefault, "high"},
		{"gpt-5.6-sol", llms.ReasoningEffortNone, "high"},
		{"gpt-5.6-sol", llms.ReasoningEffortLow, "high"},
		{"gpt-5.6-sol", llms.ReasoningEffortMedium, "high"},
		{"gpt-5.6-sol", llms.ReasoningEffortHigh, "high"},

		// Models without a reasoning control.
		{"gpt-4o", llms.ReasoningEffortDefault, notSent},
		{"gpt-4o", llms.ReasoningEffortHigh, notSent},
		{"gpt-4.1-mini", llms.ReasoningEffortMedium, notSent},

		// Without a requested level, nothing is sent for IDs that carry a
		// vendor prefix or belong to a generation the client does not know.
		{"openai.gpt-5.6-luna", llms.ReasoningEffortDefault, notSent},
		{"openai/gpt-5.1", llms.ReasoningEffortDefault, notSent},
		{"gpt-6-astra", llms.ReasoningEffortDefault, notSent},
	}
	for _, tt := range tests {
		t.Run(tt.model+"/"+effortLabel(tt.effort), func(t *testing.T) {
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			effort, ok := sentEffort(t, tt.model, nil, callOpts...)
			if tt.want == notSent {
				assert.False(t, ok, "unexpected reasoning effort %q", effort)
				return
			}
			require.True(t, ok, "reasoning effort was not sent")
			assert.Equal(t, tt.want, effort)
		})
	}
}

// effortLabel names a level for subtest titles.
func effortLabel(e llms.ReasoningEffort) string {
	if e == llms.ReasoningEffortDefault {
		return "unset"
	}
	return e.String()
}

// TestReasoningEffort_Levels covers the levels that were not mapped before:
// each is sent as the nearest level the model accepts.
func TestReasoningEffort_Levels(t *testing.T) {
	tests := []struct {
		model  string
		effort llms.ReasoningEffort
		want   string
	}{
		// xhigh and max where the model has them, the top level elsewhere.
		{"gpt-5.2", llms.ReasoningEffortXHigh, "xhigh"},
		{"gpt-5.2", llms.ReasoningEffortMax, "xhigh"},
		{"gpt-5.2-codex", llms.ReasoningEffortXHigh, "xhigh"},
		{"gpt-5.3-codex", llms.ReasoningEffortMax, "xhigh"},
		{"gpt-5.4", llms.ReasoningEffortXHigh, "xhigh"},
		{"gpt-5.4-mini", llms.ReasoningEffortMax, "xhigh"},
		{"gpt-5.5", llms.ReasoningEffortXHigh, "xhigh"},
		{"gpt-5.5", llms.ReasoningEffortMax, "xhigh"},
		{"gpt-5.6-luna", llms.ReasoningEffortXHigh, "xhigh"},
		{"gpt-5.6-terra", llms.ReasoningEffortMax, "max"},
		{"gpt-5.6-sol", llms.ReasoningEffortXHigh, "xhigh"},
		{"gpt-5.6-sol", llms.ReasoningEffortMax, "max"},
		{"gpt-5.1", llms.ReasoningEffortXHigh, "high"},
		{"gpt-5", llms.ReasoningEffortMax, "high"},
		{"o3", llms.ReasoningEffortXHigh, "high"},
		{"gpt-5-pro", llms.ReasoningEffortMax, "high"},

		// none is the model's floor: none, minimal, or the lowest level.
		{"gpt-5.2", llms.ReasoningEffortNone, "none"},
		{"gpt-5.5", llms.ReasoningEffortNone, "none"},
		{"gpt-5.6-luna", llms.ReasoningEffortNone, "none"},
		{"gpt-5.3-codex", llms.ReasoningEffortNone, "low"},
		{"gpt-5", llms.ReasoningEffortNone, "minimal"},
		{"gpt-5-mini", llms.ReasoningEffortNone, "minimal"},
		{"o4-mini", llms.ReasoningEffortNone, "low"},

		// The pro tier from 5.2 on reasons at medium, high or xhigh: a client
		// default of low was rejected before.
		{"gpt-5.2-pro", llms.ReasoningEffortDefault, "medium"},
		{"gpt-5.2-pro", llms.ReasoningEffortNone, "medium"},
		{"gpt-5.4-pro", llms.ReasoningEffortLow, "medium"},
		{"gpt-5.4-pro", llms.ReasoningEffortHigh, "high"},
		{"gpt-5.5-pro", llms.ReasoningEffortMax, "xhigh"},

		// A generation the table does not know accepts low, medium and high.
		{"gpt-6-astra", llms.ReasoningEffortNone, "low"},
		{"gpt-6-astra", llms.ReasoningEffortMedium, "medium"},
		{"gpt-6.1-sol", llms.ReasoningEffortMax, "high"},

		// Routed IDs accept a requested level.
		{"openai.gpt-5.6-luna", llms.ReasoningEffortHigh, "high"},
		{"openai.gpt-5.6-terra", llms.ReasoningEffortMax, "max"},
		{"openai/gpt-5.1", llms.ReasoningEffortLow, "low"},

		// A router variant is the same model: it keeps the model's levels.
		{"openai/gpt-5.5:batch", llms.ReasoningEffortMax, "xhigh"},
		{"openai/gpt-5-pro:batch", llms.ReasoningEffortLow, "high"},
		{"openai/gpt-5:batch", llms.ReasoningEffortNone, "minimal"},
	}
	for _, tt := range tests {
		t.Run(tt.model+"/"+effortLabel(tt.effort), func(t *testing.T) {
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			effort, ok := sentEffort(t, tt.model, nil, callOpts...)
			require.True(t, ok, "reasoning effort was not sent")
			assert.Equal(t, tt.want, effort)
		})
	}

	t.Run("models without a reasoning control ignore every level", func(t *testing.T) {
		for _, model := range []string{"gpt-4o", "gpt-35-turbo", "anthropic/claude-opus-4.8", "omni-moderation-latest", "o3x-preview"} {
			effort, ok := sentEffort(t, model, nil, llms.WithReasoningEffort(llms.ReasoningEffortMax))
			assert.False(t, ok, "model %s: unexpected reasoning effort %q", model, effort)
		}
	})
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
			model:  "gpt-5.5",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortHigh},
			want:   "high",
		},
		{
			name:   "call option wins over the config default",
			model:  "gpt-5.5",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortHigh},
			effort: llms.ReasoningEffortLow,
			want:   "low",
		},
		{
			name:   "config default is snapped to the model",
			model:  "gpt-5.1",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMax},
			want:   "high",
		},
		{
			name:   "ceiling lowers a requested level",
			model:  "gpt-5.6-luna",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortMedium},
			effort: llms.ReasoningEffortMax,
			want:   "medium",
		},
		{
			name:  "ceiling lowers the config default",
			model: "gpt-5.6-luna",
			policy: llms.EffortPolicy{
				Default: llms.ReasoningEffortXHigh,
				Max:     llms.ReasoningEffortHigh,
			},
			want: "high",
		},
		{
			name:   "ceiling leaves a lower request alone",
			model:  "gpt-5.6-luna",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortHigh},
			effort: llms.ReasoningEffortLow,
			want:   "low",
		},
		{
			name:   "ceiling leaves the client default alone when it is lower",
			model:  "gpt-5.5",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortHigh},
			want:   "low",
		},
		{
			name:   "ceiling below the client default lowers it",
			model:  "gpt-5.5",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortNone},
			want:   "none",
		},
		{
			name:   "ceiling cannot go below the model floor",
			model:  "gpt-5-pro",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			effort: llms.ReasoningEffortMedium,
			want:   "high",
		},
		{
			name:   "config default reaches a routed ID",
			model:  "openai.gpt-5.6-luna",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			want:   "medium",
		},
		{
			name:   "ceiling caps a routed ID that reasons at medium on its own",
			model:  "openai/o3",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   "low",
		},
		{
			name:   "ceiling leaves a routed ID alone when its own level is within it",
			model:  "openai/o3",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortMedium},
			want:   notSent,
		},
		{
			name:   "ceiling leaves a routed ID that does not reason on its own alone",
			model:  "openai/gpt-5.1",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
		{
			name:   "ceiling caps a generation the client sends nothing to",
			model:  "gpt-6-astra",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   "low",
		},
		{
			name:   "model without a reasoning control ignores the policy",
			model:  "gpt-4o",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortHigh},
			want:   notSent,
		},
		{
			name:   "model without a reasoning control ignores the ceiling",
			model:  "gpt-4o",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []openai.Option{
				openai.WithEffortPolicy(tt.policy),
			}
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			effort, ok := sentEffort(t, tt.model, opts, callOpts...)
			if tt.want == notSent {
				assert.False(t, ok, "unexpected reasoning effort %q", effort)
				return
			}
			require.True(t, ok, "reasoning effort was not sent")
			assert.Equal(t, tt.want, effort)
		})
	}
}

// The levels and the default level a provider's model list reports for the
// client's model take the place of the table's.
func TestReasoningEffort_ReportedByProvider(t *testing.T) {
	const notSent = ""
	lowToMax := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
	noneToMax := lowToMax.With(llms.ReasoningEffortNone)
	lowToHigh := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	tests := []struct {
		name   string
		model  string
		policy llms.EffortPolicy
		effort llms.ReasoningEffort
		want   string
	}{
		{
			name:   "a generation the table does not know gets its real top level",
			model:  "openai/gpt-6-sol",
			policy: llms.EffortPolicy{Supported: noneToMax},
			effort: llms.ReasoningEffortMax,
			want:   "max",
		},
		{
			name:   "without the list the same request is cut to high",
			model:  "openai/gpt-6-sol",
			effort: llms.ReasoningEffortMax,
			want:   "high",
		},
		{
			name:   "a reported none is sent as none",
			model:  "openai/gpt-6-sol",
			policy: llms.EffortPolicy{Supported: noneToMax},
			effort: llms.ReasoningEffortNone,
			want:   "none",
		},
		{
			name:   "a model without none gets its lowest level",
			model:  "openai/gpt-6-astra",
			policy: llms.EffortPolicy{Supported: lowToMax},
			effort: llms.ReasoningEffortNone,
			want:   "low",
		},
		{
			name:   "a model of another vendor, which the table has no levels for",
			model:  "anthropic/claude-opus-4.8",
			policy: llms.EffortPolicy{Supported: noneToMax},
			effort: llms.ReasoningEffortXHigh,
			want:   "xhigh",
		},
		{
			name:   "the minimal floor of gpt-5 is kept, though the list cannot name it",
			model:  "openai/gpt-5",
			policy: llms.EffortPolicy{Supported: lowToHigh},
			effort: llms.ReasoningEffortNone,
			want:   "minimal",
		},
		{
			name:   "reported levels narrower than the table win",
			model:  "openai/gpt-5.5",
			policy: llms.EffortPolicy{Supported: lowToHigh},
			effort: llms.ReasoningEffortXHigh,
			want:   "high",
		},
		{
			name:  "the reported default level is what a ceiling is checked against",
			model: "openai/gpt-5.4",
			policy: llms.EffortPolicy{
				Max:      llms.ReasoningEffortLow,
				Implicit: llms.ReasoningEffortMedium,
			},
			want: "low",
		},
		{
			name:   "without it the table's default of none leaves the ceiling unused",
			model:  "openai/gpt-5.4",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
		{
			name:  "a reported default within the ceiling sends nothing",
			model: "openai/gpt-5.4",
			policy: llms.EffortPolicy{
				Max:      llms.ReasoningEffortHigh,
				Implicit: llms.ReasoningEffortMedium,
			},
			want: notSent,
		},
		{
			name:   "nothing requested and nothing configured sends nothing",
			model:  "openai/gpt-6-sol",
			policy: llms.EffortPolicy{Supported: noneToMax, Implicit: llms.ReasoningEffortMedium},
			want:   notSent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []openai.Option{
				openai.WithEffortPolicy(tt.policy),
			}
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			effort, ok := sentEffort(t, tt.model, opts, callOpts...)
			if tt.want == notSent {
				assert.False(t, ok, "unexpected reasoning effort %q", effort)
				return
			}
			require.True(t, ok, "reasoning effort was not sent")
			assert.Equal(t, tt.want, effort)
		})
	}

	// What is reported describes the client's model only: another model named
	// in a call keeps the levels of the table.
	t.Run("another model named in the call", func(t *testing.T) {
		opts := []openai.Option{
			openai.WithEffortPolicy(llms.EffortPolicy{Supported: noneToMax}),
		}
		effort, ok := sentEffort(t, "openai/gpt-6-sol", opts,
			llms.WithModel("gpt-5.1"),
			llms.WithReasoningEffort(llms.ReasoningEffortMax),
		)
		require.True(t, ok, "reasoning effort was not sent")
		assert.Equal(t, "high", effort)

		effort, ok = sentEffort(t, "openai/gpt-6-sol", opts,
			llms.WithModel("gpt-4o"),
			llms.WithReasoningEffort(llms.ReasoningEffortMax),
		)
		assert.False(t, ok, "unexpected reasoning effort %q", effort)
	})
}

// The level is looked up for the model named in the call, not for the model
// the client was built with.
func TestReasoningEffort_UsesTheRequestModel(t *testing.T) {
	effort, ok := sentEffort(t, "gpt-4o", nil,
		llms.WithModel("gpt-5.6-luna"),
		llms.WithReasoningEffort(llms.ReasoningEffortMax),
	)
	require.True(t, ok, "reasoning effort was not sent")
	assert.Equal(t, "max", effort)

	effort, ok = sentEffort(t, "gpt-5.6-luna", nil,
		llms.WithModel("gpt-4o"),
		llms.WithReasoningEffort(llms.ReasoningEffortMax),
	)
	assert.False(t, ok, "unexpected reasoning effort %q", effort)
}

// Chat Completions sends an effort only when a call or the configuration asks
// for one, or when the model's own level is above the configured ceiling.
func TestReasoningEffort_ChatCompletions(t *testing.T) {
	const notSent = ""
	tests := []struct {
		name   string
		model  string
		policy llms.EffortPolicy
		effort llms.ReasoningEffort
		want   string
	}{
		{
			name:  "nothing is sent without a policy or a request",
			model: "gpt-5.5",
			want:  notSent,
		},
		{
			name:   "requested level is snapped to the model",
			model:  "gpt-5.5",
			effort: llms.ReasoningEffortMax,
			want:   "xhigh",
		},
		{
			name:   "config default is used when the call sets none",
			model:  "gpt-5.5",
			policy: llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			want:   "medium",
		},
		{
			name:   "ceiling lowers a requested level",
			model:  "gpt-5.5",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			effort: llms.ReasoningEffortHigh,
			want:   "low",
		},
		{
			name:   "ceiling caps a model that reasons at medium on its own",
			model:  "o3",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   "low",
		},
		{
			name:   "ceiling caps gpt-5 at its minimal level",
			model:  "gpt-5",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortNone},
			want:   "minimal",
		},
		{
			name:   "ceiling at the model's own level sends nothing",
			model:  "o3",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortMedium},
			want:   notSent,
		},
		{
			name:   "ceiling leaves a model that does not reason on its own alone",
			model:  "gpt-5.2",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
		{
			name:   "ceiling below the model floor sends nothing",
			model:  "gpt-5.6-sol",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
		{
			name:   "model without a reasoning control ignores the ceiling",
			model:  "gpt-4o",
			policy: llms.EffortPolicy{Max: llms.ReasoningEffortLow},
			want:   notSent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{reply: chatTextReply}
			// Azure at its default API version uses Chat Completions.
			m, err := openai.New(
				openai.WithProvider(llms.ProviderAzure),
				openai.WithBaseURL("https://chat.test"),
				openai.WithModel(tt.model),
				openai.WithEmbeddingModel("embeddings"),
				openai.WithToken("fixture"),
				openai.WithHTTPClient(f.client(t)),
				openai.WithEffortPolicy(tt.policy),
			)
			require.NoError(t, err)
			messages := []llms.Message{{
				Role: llms.RoleHuman,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "hi"},
				},
			}}
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			_, err = m.GenerateContent(context.Background(), messages, callOpts...)
			require.NoError(t, err)
			assert.Contains(t, f.path, "chat/completions")

			effort, ok := f.body["reasoning_effort"].(string)
			if tt.want == notSent {
				assert.False(t, ok, "unexpected reasoning effort %q", effort)
				return
			}
			require.True(t, ok, "reasoning effort was not sent")
			assert.Equal(t, tt.want, effort)
		})
	}
}
