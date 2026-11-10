package anthropic_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sentRequest runs one GenerateContent call and returns the request body.
func sentRequest(t *testing.T, model string, opts []anthropic.Option, callOpts ...llms.CallOption) map[string]any {
	t.Helper()
	f := &fixture{reply: textReply}
	opts = append([]anthropic.Option{
		anthropic.WithModel(model),
		anthropic.WithToken("fixture"),
		anthropic.WithHTTPClient(f.client(t)),
	}, opts...)
	m, err := anthropic.New(opts...)
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
	return f.body
}

// outputEffort returns output_config.effort from a request body.
func outputEffort(body map[string]any) (string, bool) {
	cfg, ok := body["output_config"].(map[string]any)
	if !ok {
		return "", false
	}
	effort, ok := cfg["effort"].(string)
	return effort, ok
}

// TestReasoningEffort_Unchanged pins the requests the effort refactoring must
// not change: what is sent when no level is requested.
func TestReasoningEffort_Unchanged(t *testing.T) {
	const notSent = ""
	tests := []struct {
		model      string
		wantEffort string
	}{
		// 4.6 to 4.9 models get the low output effort.
		{"claude-opus-4-8", "low"},
		{"claude-opus-4-7", "low"},
		{"claude-opus-4-6", "low"},
		{"claude-sonnet-4-6", "low"},
		{"global.anthropic.claude-sonnet-4-6", "low"},
		// Older and newer models get no effort setting.
		{"claude-sonnet-4-5", notSent},
		{"claude-haiku-4-5", notSent},
		{"claude-opus-4-20250514", notSent},
		{"claude-fable-5-2", notSent},
		{"claude-opus-5-5", notSent},
		{"claude-sonnet-5", notSent},
		{"fixture", notSent},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			body := sentRequest(t, tt.model, nil)
			assert.NotContains(t, body, "thinking")
			effort, ok := outputEffort(body)
			if tt.wantEffort == notSent {
				assert.False(t, ok, "unexpected output effort %q", effort)
				assert.NotContains(t, body, "output_config")
				return
			}
			require.True(t, ok, "output effort was not sent")
			assert.Equal(t, tt.wantEffort, effort)
		})
	}
}

// thinkingOf returns the thinking type and token budget of a request body.
// kind is empty when the request carried no thinking parameter.
func thinkingOf(body map[string]any) (kind string, budget float64) {
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		return "", 0
	}
	kind, _ = thinking["type"].(string)
	budget, _ = thinking["budget_tokens"].(float64)
	return kind, budget
}

// TestReasoningEffort_Levels covers requested levels: adaptive thinking with
// an output effort from 4.6 on, a valid thinking budget before that.
func TestReasoningEffort_Levels(t *testing.T) {
	const (
		notSent      = ""
		adaptive     = "adaptive"
		enabled      = "enabled"
		disabled     = "disabled"
		betweenTools = "between_tools"
	)
	tests := []struct {
		model        string
		effort       llms.ReasoningEffort
		wantThinking string
		wantBudget   float64
		wantEffort   string
	}{
		// 4.7 and later: adaptive thinking, the full effort ladder.
		{"claude-opus-4-8", llms.ReasoningEffortLow, adaptive, 0, "low"},
		{"claude-opus-4-8", llms.ReasoningEffortMedium, adaptive, 0, "medium"},
		{"claude-opus-4-8", llms.ReasoningEffortHigh, adaptive, 0, "high"},
		{"claude-opus-4-8", llms.ReasoningEffortXHigh, adaptive, 0, "xhigh"},
		{"claude-opus-4-7", llms.ReasoningEffortMax, adaptive, 0, "max"},
		{"claude-fable-5-2", llms.ReasoningEffortMedium, adaptive, 0, "medium"},
		{"claude-fable-5-2", llms.ReasoningEffortXHigh, adaptive, 0, "xhigh"},
		{"claude-mythos-5-2", llms.ReasoningEffortMax, adaptive, 0, "max"},
		{"claude-opus-5-5", llms.ReasoningEffortMax, adaptive, 0, "max"},
		{"claude-opus-5", llms.ReasoningEffortHigh, adaptive, 0, "high"},
		{"claude-sonnet-5-5", llms.ReasoningEffortLow, adaptive, 0, "low"},
		{"claude-sonnet-5", llms.ReasoningEffortXHigh, adaptive, 0, "xhigh"},
		{"claude-haiku-5-5", llms.ReasoningEffortHigh, adaptive, 0, "high"},
		// A version newer than the table behaves like the newest generation.
		{"claude-opus-7-1", llms.ReasoningEffortXHigh, adaptive, 0, "xhigh"},

		// 4.6: adaptive thinking, no xhigh. Opus and Sonnet both take max.
		{"claude-opus-4-6", llms.ReasoningEffortXHigh, adaptive, 0, "high"},
		{"claude-opus-4-6", llms.ReasoningEffortMax, adaptive, 0, "max"},
		{"claude-sonnet-4-6", llms.ReasoningEffortMedium, adaptive, 0, "medium"},
		{"claude-sonnet-4-6", llms.ReasoningEffortXHigh, adaptive, 0, "high"},
		{"claude-sonnet-4-6", llms.ReasoningEffortMax, adaptive, 0, "max"},
		{"global.anthropic.claude-sonnet-4-6", llms.ReasoningEffortHigh, adaptive, 0, "high"},

		// Before 4.6: a thinking budget; only Opus 4.5 takes an effort.
		{"claude-haiku-4-5", llms.ReasoningEffortLow, enabled, 1024, notSent},
		{"claude-haiku-4-5", llms.ReasoningEffortMedium, enabled, 5000, notSent},
		{"claude-haiku-4-5", llms.ReasoningEffortHigh, enabled, 10000, notSent},
		{"claude-sonnet-4-5", llms.ReasoningEffortXHigh, enabled, 10000, notSent},
		{"claude-sonnet-4-5-20250929", llms.ReasoningEffortMax, enabled, 10000, notSent},
		{"claude-opus-4-5", llms.ReasoningEffortMedium, enabled, 5000, "medium"},
		{"claude-opus-4-5", llms.ReasoningEffortMax, enabled, 10000, "high"},
		{"us.anthropic.claude-opus-4-20250514-v1:0", llms.ReasoningEffortHigh, enabled, 10000, notSent},

		// none before 5.0: no thinking parameter, which leaves thinking off,
		// and the lowest effort where there is one.
		{"claude-opus-4-8", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-sonnet-4-6", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-opus-4-5", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-haiku-4-5", llms.ReasoningEffortNone, notSent, 0, notSent},

		// none from 5.0: thinking is on unless it is turned off, in the form
		// the model accepts.
		{"claude-opus-5", llms.ReasoningEffortNone, disabled, 0, "low"},
		{"claude-sonnet-5", llms.ReasoningEffortNone, disabled, 0, "low"},
		{"claude-haiku-5-5", llms.ReasoningEffortNone, disabled, 0, "low"},
		{"claude-sonnet-5-5", llms.ReasoningEffortNone, betweenTools, 0, "low"},
		{"us.anthropic.claude-sonnet-5-5-v1:0", llms.ReasoningEffortNone, betweenTools, 0, "low"},
		// Models that always think, and versions newer than the table, take
		// the lowest effort only: they reject a disabled thinking setting.
		{"claude-opus-5-5", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-fable-5", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-fable-5-2", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-mythos-5-1", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-sonnet-5-6", llms.ReasoningEffortNone, notSent, 0, "low"},
		{"claude-opus-7-1", llms.ReasoningEffortNone, notSent, 0, "low"},

		// Models without a known reasoning control ignore every level.
		{"fixture", llms.ReasoningEffortHigh, notSent, 0, notSent},
		{"claude-3-5-sonnet-20241022", llms.ReasoningEffortHigh, notSent, 0, notSent},
	}
	for _, tt := range tests {
		t.Run(tt.model+"/"+tt.effort.String(), func(t *testing.T) {
			body := sentRequest(t, tt.model, nil, llms.WithReasoningEffort(tt.effort))

			kind, budget := thinkingOf(body)
			assert.Equal(t, tt.wantThinking, kind)
			assert.Equal(t, tt.wantBudget, budget)

			effort, ok := outputEffort(body)
			if tt.wantEffort == notSent {
				assert.False(t, ok, "unexpected output effort %q", effort)
				return
			}
			require.True(t, ok, "output effort was not sent")
			assert.Equal(t, tt.wantEffort, effort)
		})
	}
}

// A thinking budget counts against max_tokens and must stay below it.
func TestReasoningEffort_BudgetFitsMaxTokens(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
		effort    llms.ReasoningEffort
		want      float64
	}{
		{"default max_tokens is raised above the budget", 0, llms.ReasoningEffortHigh, 10000 + 4096},
		{"max_tokens equal to the budget is raised", 10000, llms.ReasoningEffortHigh, 10000 + 4096},
		{"max_tokens above the budget is kept", 20000, llms.ReasoningEffortHigh, 20000},
		{"low budget fits the default", 0, llms.ReasoningEffortLow, 4096},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callOpts := []llms.CallOption{
				llms.WithReasoningEffort(tt.effort),
			}
			if tt.maxTokens > 0 {
				callOpts = append(callOpts, llms.WithMaxTokens(tt.maxTokens))
			}
			body := sentRequest(t, "claude-haiku-4-5", nil, callOpts...)
			_, budget := thinkingOf(body)
			assert.Equal(t, tt.want, body["max_tokens"])
			assert.Less(t, budget, body["max_tokens"].(float64))
		})
	}

	// Adaptive thinking has no budget, so max_tokens is left alone.
	body := sentRequest(t, "claude-opus-4-8", nil, llms.WithReasoningEffort(llms.ReasoningEffortMax))
	assert.EqualValues(t, 4096, body["max_tokens"])
}

func TestReasoningEffort_Policy(t *testing.T) {
	const (
		notSent  = ""
		adaptive = "adaptive"
		disabled = "disabled"
	)
	tests := []struct {
		name         string
		model        string
		policy       llms.EffortPolicy
		effort       llms.ReasoningEffort
		wantThinking string
		wantEffort   string
	}{
		{
			name:         "config default is used when the call sets none",
			model:        "claude-fable-5-2",
			policy:       llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			wantThinking: adaptive,
			wantEffort:   "medium",
		},
		{
			name:         "call option wins over the config default",
			model:        "claude-fable-5-2",
			policy:       llms.EffortPolicy{Default: llms.ReasoningEffortMedium},
			effort:       llms.ReasoningEffortXHigh,
			wantThinking: adaptive,
			wantEffort:   "xhigh",
		},
		{
			name:         "ceiling lowers a requested level",
			model:        "claude-opus-4-8",
			policy:       llms.EffortPolicy{Max: llms.ReasoningEffortHigh},
			effort:       llms.ReasoningEffortMax,
			wantThinking: adaptive,
			wantEffort:   "high",
		},
		{
			name:  "ceiling lowers the config default",
			model: "claude-opus-4-8",
			policy: llms.EffortPolicy{
				Default: llms.ReasoningEffortMax,
				Max:     llms.ReasoningEffortMedium,
			},
			wantThinking: adaptive,
			wantEffort:   "medium",
		},
		{
			name:         "ceiling caps a model that thinks on its own",
			model:        "claude-fable-5-2",
			policy:       llms.EffortPolicy{Max: llms.ReasoningEffortMedium},
			wantThinking: adaptive,
			wantEffort:   "medium",
		},
		{
			name:         "ceiling of none turns thinking off where the model allows it",
			model:        "claude-opus-5",
			policy:       llms.EffortPolicy{Max: llms.ReasoningEffortNone},
			wantThinking: disabled,
			wantEffort:   "low",
		},
		{
			name:         "ceiling of none turns off a requested level too",
			model:        "claude-haiku-5-5",
			policy:       llms.EffortPolicy{Max: llms.ReasoningEffortNone},
			effort:       llms.ReasoningEffortHigh,
			wantThinking: disabled,
			wantEffort:   "low",
		},
		{
			name:       "ceiling of none lowers the effort of a model that always thinks",
			model:      "claude-opus-5-5",
			policy:     llms.EffortPolicy{Max: llms.ReasoningEffortNone},
			wantEffort: "low",
		},
		{
			name:       "ceiling at the model's own level sends nothing",
			model:      "claude-fable-5-2",
			policy:     llms.EffortPolicy{Max: llms.ReasoningEffortHigh},
			wantEffort: notSent,
		},
		{
			name:       "ceiling leaves the low default of 4.x alone",
			model:      "claude-opus-4-8",
			policy:     llms.EffortPolicy{Max: llms.ReasoningEffortMedium},
			wantEffort: "low",
		},
		{
			name:       "unknown model ignores the policy",
			model:      "fixture",
			policy:     llms.EffortPolicy{Default: llms.ReasoningEffortHigh},
			wantEffort: notSent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []anthropic.Option{
				anthropic.WithEffortPolicy(tt.policy),
			}
			var callOpts []llms.CallOption
			if tt.effort != llms.ReasoningEffortDefault {
				callOpts = append(callOpts, llms.WithReasoningEffort(tt.effort))
			}
			body := sentRequest(t, tt.model, opts, callOpts...)

			kind, _ := thinkingOf(body)
			assert.Equal(t, tt.wantThinking, kind)

			effort, ok := outputEffort(body)
			if tt.wantEffort == notSent {
				assert.False(t, ok, "unexpected output effort %q", effort)
				return
			}
			require.True(t, ok, "output effort was not sent")
			assert.Equal(t, tt.wantEffort, effort)
		})
	}
}

// A structured-output request keeps the resolved effort next to the format.
func TestReasoningEffort_KeptWithResponseFormat(t *testing.T) {
	format := &schema.ResponseFormat{
		Type: "json_schema",
		JSONSchema: &schema.ResponseFormatJSONSchema{
			Schema: &schema.ResponseFormatJSONSchemaProperty{
				Type: "object",
				Properties: map[string]*schema.ResponseFormatJSONSchemaProperty{
					"name": {Type: "string"},
				},
				Required: []string{"name"},
			},
		},
	}

	body := sentRequest(t, "claude-opus-4-8", nil,
		llms.WithResponseFormat(format),
		llms.WithReasoningEffort(llms.ReasoningEffortHigh),
	)
	cfg, ok := body["output_config"].(map[string]any)
	require.True(t, ok, "output_config was not sent")
	assert.Equal(t, "high", cfg["effort"])
	assert.Contains(t, cfg, "format")

	// Without a requested level the format still travels with the default.
	body = sentRequest(t, "claude-opus-4-8", nil, llms.WithResponseFormat(format))
	cfg, ok = body["output_config"].(map[string]any)
	require.True(t, ok, "output_config was not sent")
	assert.Equal(t, "low", cfg["effort"])
	assert.Contains(t, cfg, "format")

	// A model without an effort setting gets the format alone.
	body = sentRequest(t, "claude-haiku-4-5", nil, llms.WithResponseFormat(format))
	cfg, ok = body["output_config"].(map[string]any)
	require.True(t, ok, "output_config was not sent")
	assert.NotContains(t, cfg, "effort")
	assert.Contains(t, cfg, "format")
}

// The newer web tool version goes to the models that support it, whatever
// their version number looks like.
func TestWebToolVersionByModel(t *testing.T) {
	webSearch := []llms.Tool{{
		Type: "web_search",
	}}
	tests := []struct {
		model      string
		wantLegacy bool
	}{
		{"claude-fable-5-2", false},
		{"claude-mythos-5-1", false},
		{"claude-opus-5-5", false},
		{"claude-opus-5", false},
		{"claude-opus-4-8", false},
		{"claude-opus-4-6", false},
		{"claude-sonnet-5-5", false},
		{"claude-sonnet-5", false},
		{"claude-sonnet-4-6", false},
		{"global.anthropic.claude-sonnet-4-6", false},

		{"claude-sonnet-4-5", true},
		{"claude-sonnet-4-0", true},
		{"claude-opus-4-5", true},
		{"claude-opus-4-20250514", true},
		{"claude-haiku-5-5", true},
		{"claude-haiku-4-5", true},
		{"fixture", true},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			tools := anthropic.ToTools(webSearch, tt.model)
			require.Len(t, tools, 1)
			if tt.wantLegacy {
				assert.NotNil(t, tools[0].OfWebFetchTool20250910)
				assert.Nil(t, tools[0].OfWebFetchTool20260209)
				return
			}
			assert.NotNil(t, tools[0].OfWebFetchTool20260209)
			assert.Nil(t, tools[0].OfWebFetchTool20250910)
		})
	}
}
