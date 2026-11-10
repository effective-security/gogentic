package llms_test

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
)

func TestParseModelName(t *testing.T) {
	tests := []struct {
		id       string
		prefix   string
		family   string
		version  []int
		snapshot string
		stage    string
	}{
		// The family table in Documentation/llm-factory.md.
		{id: "claude-fable-5-1", family: "claude-fable", version: []int{5, 1}},
		{id: "claude-3-5-sonnet-20241022", family: "claude-sonnet", version: []int{3, 5}, snapshot: "20241022"},
		{id: "gpt-5.1-mini", family: "gpt-mini", version: []int{5, 1}},
		{id: "gpt-5.6-luna", family: "gpt-luna", version: []int{5, 6}},
		{id: "gemini-3-pro-preview", family: "gemini-pro", version: []int{3}, stage: "preview"},
		{id: "global.anthropic.claude-sonnet-4-6", prefix: "global.anthropic.", family: "claude-sonnet", version: []int{4, 6}},
		{id: "gpt-4o-mini", family: "gpt-4o-mini"},

		// Anthropic.
		{id: "claude-fable-5-2", family: "claude-fable", version: []int{5, 2}},
		{id: "claude-opus-4-8", family: "claude-opus", version: []int{4, 8}},
		{id: "claude-opus-4-10", family: "claude-opus", version: []int{4, 10}},
		{id: "claude-opus-5", family: "claude-opus", version: []int{5}},
		{id: "claude-opus-4-20250514", family: "claude-opus", version: []int{4}, snapshot: "20250514"},
		{id: "claude-sonnet-4-5-20250929", family: "claude-sonnet", version: []int{4, 5}, snapshot: "20250929"},
		{id: "claude-3-opus-latest", family: "claude-opus", version: []int{3}, stage: "latest"},

		// OpenAI.
		{id: "gpt-5.1", family: "gpt", version: []int{5, 1}},
		{id: "gpt-5", family: "gpt", version: []int{5}},
		{id: "gpt-5-pro", family: "gpt-pro", version: []int{5}},
		{id: "gpt-5.1-codex-max", family: "gpt-codex-max", version: []int{5, 1}},
		{id: "gpt-5.1-2025-11-13", family: "gpt", version: []int{5, 1}, snapshot: "2025-11-13"},
		{id: "gpt-4-0613", family: "gpt", version: []int{4}, snapshot: "0613"},
		{id: "gpt-4-1106-preview", family: "gpt", version: []int{4}, snapshot: "1106", stage: "preview"},
		{id: "o3", family: "o3"},
		{id: "o4-mini", family: "o4-mini"},

		// Google.
		{id: "gemini-2.5-pro", family: "gemini-pro", version: []int{2, 5}},
		{id: "gemini-2.5-flash-lite", family: "gemini-flash-lite", version: []int{2, 5}},
		{id: "gemini-2.0-flash-001", family: "gemini-flash", version: []int{2, 0}, snapshot: "001"},
		{id: "gemini-flash-latest", family: "gemini-flash", stage: "latest"},

		// Routed IDs keep their prefix apart from the family.
		{
			id:       "us.anthropic.claude-opus-4-20250514-v1:0",
			prefix:   "us.anthropic.",
			family:   "claude-opus",
			version:  []int{4},
			snapshot: "20250514-v1:0",
		},
		{id: "openai.gpt-5.6-luna", prefix: "openai.", family: "gpt-luna", version: []int{5, 6}},
		{id: "anthropic/claude-opus-4.8", prefix: "anthropic/", family: "claude-opus", version: []int{4, 8}},
		{id: "openai/gpt-oss-120b", prefix: "openai/", family: "gpt-oss-120b"},
		{id: "meta-llama/llama-3.1-70b-instruct", prefix: "meta-llama/", family: "llama-70b-instruct", version: []int{3, 1}},
		{id: "anthropic.claude-v2:1", prefix: "anthropic.", family: "claude", snapshot: "v2:1"},

		// IDs that follow no convention stay whole.
		{id: "fixture", family: "fixture"},
		{id: "", family: ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			name := llms.ParseModelName(tt.id)
			assert.Equal(t, tt.id, name.ID)
			assert.Equal(t, tt.prefix, name.Prefix)
			assert.Equal(t, tt.family, name.Family)
			assert.Equal(t, tt.version, name.Version)
			assert.Equal(t, tt.snapshot, name.Snapshot)
			assert.Equal(t, tt.stage, name.Stage)
			assert.Equal(t, tt.prefix+tt.family, name.Key())
			assert.Equal(t, len(tt.version) > 0, name.HasVersion())
		})
	}
}

func TestModelName_VersionComparison(t *testing.T) {
	opus48 := llms.ParseModelName("claude-opus-4-8")
	assert.True(t, opus48.VersionAtLeast(4, 6))
	assert.True(t, opus48.VersionAtLeast(4, 8))
	assert.True(t, opus48.VersionAtLeast(4))
	assert.False(t, opus48.VersionAtLeast(4, 9))
	assert.False(t, opus48.VersionAtLeast(5))
	assert.True(t, opus48.VersionBelow(5))
	assert.False(t, opus48.VersionBelow(4, 8))

	// A missing component counts as zero.
	opus5 := llms.ParseModelName("claude-opus-5")
	assert.True(t, opus5.VersionAtLeast(5, 0))
	assert.True(t, opus5.VersionBelow(5, 1))

	// Minor versions compare as numbers, not as text.
	assert.True(t, llms.ParseModelName("claude-opus-4-10").VersionAtLeast(4, 9))

	// Without a version both comparisons are false.
	unversioned := llms.ParseModelName("o4-mini")
	assert.False(t, unversioned.VersionAtLeast(1))
	assert.False(t, unversioned.VersionBelow(99))

	assert.Equal(t, 0, llms.CompareVersions([]int{5}, []int{5, 0}))
	assert.Equal(t, -1, llms.CompareVersions([]int{4, 8}, []int{5}))
	assert.Equal(t, 1, llms.CompareVersions([]int{5, 2}, []int{5, 1}))
	assert.Equal(t, 0, llms.CompareVersions(nil, nil))
}
