package llmfactory

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRefSpec(t *testing.T) {
	tests := []struct {
		raw        string
		wantPath   string
		wantEffort llms.ReasoningEffort
	}{
		// The reference examples in Documentation/llm-factory.md.
		{"gpt-5.1", "gpt-5.1", llms.ReasoningEffortDefault},
		{"AZURE/gpt-5.1,medium", "AZURE/gpt-5.1", llms.ReasoningEffortMedium},
		{"ANTHROPIC/claude-opus-*,high", "ANTHROPIC/claude-opus-*", llms.ReasoningEffortHigh},
		{"GEMINI/gemini-*-flash", "GEMINI/gemini-*-flash", llms.ReasoningEffortDefault},
		{"openrouter/anthropic/claude-sonnet-4", "openrouter/anthropic/claude-sonnet-4", llms.ReasoningEffortDefault},
		{"openai/gpt-oss-120b", "openai/gpt-oss-120b", llms.ReasoningEffortDefault},

		// Every level, case and spaces around the suffix.
		{"m,none", "m", llms.ReasoningEffortNone},
		{"m,low", "m", llms.ReasoningEffortLow},
		{"m,xhigh", "m", llms.ReasoningEffortXHigh},
		{"m,max", "m", llms.ReasoningEffortMax},
		{"AZURE/gpt-5.1, Medium", "AZURE/gpt-5.1", llms.ReasoningEffortMedium},
		{"  gpt-5.1 , HIGH  ", "gpt-5.1", llms.ReasoningEffortHigh},

		// IDs that already contain separators keep them.
		{"us.anthropic.claude-opus-4-20250514-v1:0,low", "us.anthropic.claude-opus-4-20250514-v1:0", llms.ReasoningEffortLow},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			spec, err := parseRefSpec(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, spec.path)
			assert.Equal(t, tt.wantEffort, spec.effort)
		})
	}

	invalid := []struct {
		raw     string
		wantErr string
	}{
		{"", "model reference is empty"},
		{"   ", "model reference is empty"},
		{"gpt-5.1,hihg", `model reference "gpt-5.1,hihg": invalid reasoning effort: "hihg"`},
		{"gpt-5.1,", `model reference "gpt-5.1,": invalid reasoning effort: ""`},
		{"gpt-5.1,minimal", `model reference "gpt-5.1,minimal": invalid reasoning effort: "minimal"`},
		{"a,b,high,x", `model reference "a,b,high,x": invalid reasoning effort: "x"`},
		{",high", `model reference ",high" has no model`},
	}
	for _, tt := range invalid {
		t.Run("invalid "+tt.raw, func(t *testing.T) {
			_, err := parseRefSpec(tt.raw)
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}

	// An entry that is only an effort level is what a YAML flow sequence
	// leaves behind after splitting "MODEL,effort" at the comma.
	for _, level := range []string{"none", "low", "medium", "high", "xhigh", "max", " Medium "} {
		t.Run("bare level "+level, func(t *testing.T) {
			_, err := parseRefSpec(level)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "is only an effort level")
			assert.Contains(t, err.Error(), "block style or quoted")
		})
	}
}

func TestModelMatcher(t *testing.T) {
	tests := []struct {
		pattern   string
		isPattern bool
		matches   []string
		rejects   []string
	}{
		{
			pattern: "gpt-5.1",
			matches: []string{"gpt-5.1"},
			// A dot is literal, and an exact ID never matches a longer one.
			rejects: []string{"gpt-5x1", "gpt-5.1-mini", "GPT-5.1", ""},
		},
		{
			pattern:   "claude-opus-*",
			isPattern: true,
			matches:   []string{"claude-opus-4-8", "claude-opus-5", "claude-opus-"},
			rejects:   []string{"claude-opus", "claude-sonnet-4-8", "x-claude-opus-4-8"},
		},
		{
			pattern:   "gpt-*-mini",
			isPattern: true,
			matches:   []string{"gpt-5.1-mini", "gpt-5-codex-mini"},
			rejects:   []string{"gpt-5.1", "gpt-mini", "gpt-5.1-mini-2025"},
		},
		{
			pattern:   "*-preview",
			isPattern: true,
			matches:   []string{"gemini-3-pro-preview", "-preview"},
			rejects:   []string{"gemini-3-pro", "preview"},
		},
		{
			pattern:   "gpt-5.?",
			isPattern: true,
			matches:   []string{"gpt-5.1", "gpt-5.5"},
			rejects:   []string{"gpt-5.", "gpt-5.10", "gpt-5x1x"},
		},
		{
			pattern:   "*",
			isPattern: true,
			matches:   []string{"anything", "vendor/model:free", ""},
		},
		{
			// Regular expression syntax is literal text.
			pattern:   "a.b+(c)*",
			isPattern: true,
			matches:   []string{"a.b+(c)", "a.b+(c)xyz"},
			rejects:   []string{"axb+(c)", "a.bb(c)"},
		},
		{
			pattern:   "anthropic/claude-*",
			isPattern: true,
			matches:   []string{"anthropic/claude-sonnet-4"},
			rejects:   []string{"openai/claude-sonnet-4"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			matcher := newModelMatcher(tt.pattern)
			assert.Equal(t, tt.isPattern, matcher.isPattern())
			for _, id := range tt.matches {
				assert.True(t, matcher.match(id), "%q should match %q", tt.pattern, id)
			}
			for _, id := range tt.rejects {
				assert.False(t, matcher.match(id), "%q should not match %q", tt.pattern, id)
			}
		})
	}
}

func TestRankModels(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "higher version first",
			in:   []string{"claude-fable-5-1", "claude-fable-5-2", "claude-fable-5"},
			want: []string{"claude-fable-5-2", "claude-fable-5-1", "claude-fable-5"},
		},
		{
			name: "versions compare as numbers",
			in:   []string{"claude-opus-4-9", "claude-opus-4-10"},
			want: []string{"claude-opus-4-10", "claude-opus-4-9"},
		},
		{
			name: "generally available before preview, whatever the version",
			in:   []string{"gemini-3-pro-preview", "gemini-2.5-pro"},
			want: []string{"gemini-2.5-pro", "gemini-3-pro-preview"},
		},
		{
			name: "floating ID before a snapshot, newer snapshot first",
			in:   []string{"gpt-5.1-2025-11-13", "gpt-5.1", "gpt-5.1-2025-12-01"},
			want: []string{"gpt-5.1", "gpt-5.1-2025-12-01", "gpt-5.1-2025-11-13"},
		},
		{
			name: "fewer name parts first at the same version",
			in:   []string{"gpt-5.5-pro", "gpt-5.5", "gpt-5.1-mini"},
			want: []string{"gpt-5.5", "gpt-5.5-pro", "gpt-5.1-mini"},
		},
		{
			name: "alphabetical as the last resort",
			in:   []string{"gpt-5.6-terra", "gpt-5.6-luna"},
			want: []string{"gpt-5.6-luna", "gpt-5.6-terra"},
		},
		{
			name: "empty",
			in:   nil,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := append([]string(nil), tt.in...)
			assert.Equal(t, tt.want, rankModels(tt.in, nil))
			assert.Equal(t, in, tt.in, "the input must not be reordered")
		})
	}
}
