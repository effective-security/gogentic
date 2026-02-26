package anthropic

import (
	"testing"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessMessagesForRequest_SystemBlocks(t *testing.T) {
	t.Parallel()

	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "system-1"),
		llms.MessageFromTextParts(llms.RoleSystem, "system-2"),
		llms.MessageFromTextParts(llms.RoleHuman, "hello"),
	}

	chatMessages, systemBlocks, partLocations, err := processMessagesForRequest(messages)
	require.NoError(t, err)

	require.Len(t, systemBlocks, 2)
	assert.Equal(t, "system-1", systemBlocks[0].Text)
	assert.Equal(t, "system-2", systemBlocks[1].Text)
	require.Len(t, chatMessages, 1)

	loc, ok := partLocations[promptCachePartKey{MessageIndex: 0, PartIndex: 0}]
	require.True(t, ok)
	assert.True(t, loc.IsSystem)
	assert.Equal(t, 0, loc.SystemIndex)

	loc, ok = partLocations[promptCachePartKey{MessageIndex: 2, PartIndex: 0}]
	require.True(t, ok)
	assert.False(t, loc.IsSystem)
	assert.Equal(t, 0, loc.MessageIndex)
	assert.Equal(t, 0, loc.ContentIndex)
}

func TestApplyPromptCachePolicyToRequest(t *testing.T) {
	t.Parallel()

	params, partLocations := newAnthropicPromptCacheTestParams(t)

	opts := &llms.CallOptions{
		PromptCachePolicy: &llms.PromptCachePolicy{
			Breakpoints: []llms.PromptCacheBreakpoint{
				{
					Target: llms.PromptCacheTarget{
						Kind:         llms.PromptCacheTargetMessagePart,
						MessageIndex: 0,
						PartIndex:    0,
					},
					TTL: llms.PromptCacheTTL1h,
				},
				{
					Target: llms.PromptCacheTarget{
						Kind:         llms.PromptCacheTargetMessagePart,
						MessageIndex: 1,
						PartIndex:    0,
					},
					TTL: llms.PromptCacheTTL5m,
				},
				{
					Target: llms.PromptCacheTarget{
						Kind:      llms.PromptCacheTargetTool,
						ToolIndex: 0,
					},
				},
			},
		},
	}

	reqOpts, err := applyPromptCachePolicyToRequest(&LLM{Options: &Options{}}, &params, opts, partLocations)
	require.NoError(t, err)

	assert.Equal(t, sdkanthropic.CacheControlEphemeralTTLTTL1h, params.System[0].CacheControl.TTL)
	require.NotNil(t, params.Messages[0].Content[0].GetCacheControl())
	assert.Equal(t, sdkanthropic.CacheControlEphemeralTTLTTL5m, params.Messages[0].Content[0].GetCacheControl().TTL)
	require.NotNil(t, params.Tools[0].GetCacheControl())
	assert.Equal(t, "ephemeral", string(params.Tools[0].GetCacheControl().Type))
	assert.Len(t, reqOpts, 1)
}

func TestApplyPromptCachePolicyToRequest_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		breakpoints []llms.PromptCacheBreakpoint
		errContains string
	}{
		{
			name: "duplicate message part breakpoint",
			breakpoints: []llms.PromptCacheBreakpoint{
				{
					Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetMessagePart, MessageIndex: 1, PartIndex: 0},
				},
				{
					Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetMessagePart, MessageIndex: 1, PartIndex: 0},
				},
			},
			errContains: "duplicate prompt cache breakpoint",
		},
		{
			name: "too many breakpoints",
			breakpoints: []llms.PromptCacheBreakpoint{
				{Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetMessagePart, MessageIndex: 0, PartIndex: 0}},
				{Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetMessagePart, MessageIndex: 1, PartIndex: 0}},
				{Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetMessagePart, MessageIndex: 1, PartIndex: 1}},
				{Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetTool, ToolIndex: 0}},
				{Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetTool, ToolIndex: 1}},
			},
			errContains: "too many prompt cache breakpoints",
		},
		{
			name: "missing target",
			breakpoints: []llms.PromptCacheBreakpoint{
				{
					Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetMessagePart, MessageIndex: 9, PartIndex: 0},
				},
			},
			errContains: "prompt cache target not found",
		},
		{
			name: "unsupported ttl",
			breakpoints: []llms.PromptCacheBreakpoint{
				{
					Target: llms.PromptCacheTarget{Kind: llms.PromptCacheTargetMessagePart, MessageIndex: 0, PartIndex: 0},
					TTL:    llms.PromptCacheTTL("2h"),
				},
			},
			errContains: "unsupported prompt cache TTL",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			params, partLocations := newAnthropicPromptCacheTestParams(t)
			opts := &llms.CallOptions{
				PromptCachePolicy: &llms.PromptCachePolicy{
					Breakpoints: tt.breakpoints,
				},
			}

			_, err := applyPromptCachePolicyToRequest(&LLM{Options: &Options{}}, &params, opts, partLocations)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestPromptCacheRequestOptions(t *testing.T) {
	t.Parallel()

	t.Run("adds extended ttl beta header", func(t *testing.T) {
		t.Parallel()
		reqOpts := promptCacheRequestOptions(&LLM{Options: &Options{}}, true)
		assert.Len(t, reqOpts, 1)
	})

	t.Run("skips when beta already configured", func(t *testing.T) {
		t.Parallel()
		reqOpts := promptCacheRequestOptions(&LLM{
			Options: &Options{
				AnthropicBetaHeader: string(sdkanthropic.AnthropicBetaExtendedCacheTTL2025_04_11),
			},
		}, true)
		assert.Len(t, reqOpts, 0)
	})

	t.Run("token detection trims spaces", func(t *testing.T) {
		t.Parallel()
		assert.True(t, containsBetaHeaderToken("foo, "+string(sdkanthropic.AnthropicBetaExtendedCacheTTL2025_04_11), string(sdkanthropic.AnthropicBetaExtendedCacheTTL2025_04_11)))
	})
}

func newAnthropicPromptCacheTestParams(t *testing.T) (sdkanthropic.MessageNewParams,
	map[promptCachePartKey]promptCachePartLocation,
) {
	t.Helper()

	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "stable system"),
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextPart("stable context"),
				llms.TextPart("volatile question"),
			},
		},
	}

	chatMessages, systemBlocks, partLocations, err := processMessagesForRequest(messages)
	require.NoError(t, err)

	tools := ToTools([]llms.Tool{
		{
			Function: &llms.FunctionDefinition{
				Name:        "lookup",
				Description: "Lookup a record",
				Parameters:  &jsonschema.Schema{Type: "object"},
			},
		},
	})
	require.Len(t, tools, 1)

	params := sdkanthropic.MessageNewParams{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 256,
		Messages:  chatMessages,
		System:    systemBlocks,
		Tools:     tools,
	}

	return params, partLocations
}
