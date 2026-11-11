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

	tools, err := ToTools([]llms.Tool{
		{
			Function: &llms.FunctionDefinition{
				Name:        "lookup",
				Description: "Lookup a record",
				Parameters:  &jsonschema.Schema{Type: "object"},
			},
		},
	}, "claude-sonnet-4-5")
	require.NoError(t, err)
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

func TestProcessMessagesForRequest_Locations(t *testing.T) {
	t.Parallel()

	messages := []llms.Message{
		{
			Role: llms.RoleSystem,
			Parts: []llms.ContentPart{
				llms.TextPart("system-1"),
				llms.TextPart("system-2"),
			},
		},
		{
			Role: llms.RoleHuman,
		},
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextPart("question"),
				llms.BinaryPart("image/png", []byte("png")),
			},
		},
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				llms.TextPart("checking"),
				llms.ToolCall{
					ID: "call_1",
					FunctionCall: &llms.FunctionCall{
						Name:      "lookup",
						Arguments: `{}`,
					},
				},
			},
		},
		{
			Role: llms.RoleTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: "call_1",
					Content:    "found",
				},
			},
		},
		llms.MessageFromTextParts(llms.RoleGeneric, "generic"),
	}

	chatMessages, systemBlocks, partLocations, err := processMessagesForRequest(messages)
	require.NoError(t, err)

	require.Len(t, systemBlocks, 2)
	assert.Equal(t, "system-1", systemBlocks[0].Text)
	assert.Equal(t, "system-2", systemBlocks[1].Text)

	roles := make([]sdkanthropic.MessageParamRole, 0, len(chatMessages))
	for _, m := range chatMessages {
		roles = append(roles, m.Role)
	}
	assert.Equal(t, []sdkanthropic.MessageParamRole{
		sdkanthropic.MessageParamRoleUser,
		sdkanthropic.MessageParamRoleAssistant,
		sdkanthropic.MessageParamRoleUser,
		sdkanthropic.MessageParamRoleAssistant,
	}, roles)

	assert.Equal(t, map[promptCachePartKey]promptCachePartLocation{
		{MessageIndex: 0, PartIndex: 0}: {IsSystem: true, SystemIndex: 0},
		{MessageIndex: 0, PartIndex: 1}: {IsSystem: true, SystemIndex: 1},
		{MessageIndex: 2, PartIndex: 0}: {MessageIndex: 0, ContentIndex: 0},
		{MessageIndex: 2, PartIndex: 1}: {MessageIndex: 0, ContentIndex: 1},
		{MessageIndex: 3, PartIndex: 0}: {MessageIndex: 1, ContentIndex: 0},
		{MessageIndex: 3, PartIndex: 1}: {MessageIndex: 1, ContentIndex: 1},
		{MessageIndex: 4, PartIndex: 0}: {MessageIndex: 2, ContentIndex: 0},
		{MessageIndex: 5, PartIndex: 0}: {MessageIndex: 3, ContentIndex: 0},
	}, partLocations)
}

func TestProcessMessagesForRequest_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message llms.Message
		wantErr string
		wantIs  error
	}{
		{
			name: "system message with binary part",
			message: llms.Message{
				Role: llms.RoleSystem,
				Parts: []llms.ContentPart{
					llms.TextPart("ok"),
					llms.BinaryPart("image/png", []byte("png")),
				},
			},
			wantErr: "anthropic: failed to handle system message: anthropic: for system message: anthropic: invalid content type",
			wantIs:  ErrInvalidContentType,
		},
		{
			name: "human message with unsupported binary",
			message: llms.Message{
				Role: llms.RoleHuman,
				Parts: []llms.ContentPart{
					llms.BinaryPart("application/pdf", []byte("pdf")),
				},
			},
			wantErr: "anthropic: failed to handle human message: anthropic: unsupported binary content type: application/pdf",
		},
		{
			name: "AI message with invalid tool arguments",
			message: llms.Message{
				Role: llms.RoleAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID: "call_1",
						FunctionCall: &llms.FunctionCall{
							Name:      "lookup",
							Arguments: "{invalid",
						},
					},
				},
			},
			wantErr: "anthropic: failed to handle AI message: anthropic: failed to unmarshal tool call arguments: invalid character 'i' looking for beginning of object key string",
		},
		{
			name:    "tool message with text part",
			message: llms.MessageFromTextParts(llms.RoleTool, "not a tool response"),
			wantErr: "anthropic: failed to handle tool message: anthropic: for tool message part type: llms.TextContent: anthropic: invalid content type",
			wantIs:  ErrInvalidContentType,
		},
		{
			name:    "unsupported role",
			message: llms.MessageFromTextParts(llms.Role("narrator"), "once upon a time"),
			wantErr: "anthropic: narrator: anthropic: unsupported message type",
			wantIs:  ErrUnsupportedMessageType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			chatMessages, systemBlocks, partLocations, err := processMessagesForRequest([]llms.Message{tt.message})
			require.EqualError(t, err, tt.wantErr)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
			assert.Nil(t, chatMessages)
			assert.Nil(t, systemBlocks)
			assert.Nil(t, partLocations)
		})
	}
}

func TestApplyPromptCachePolicyToRequest_NoPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts *llms.CallOptions
	}{
		{
			name: "nil options",
		},
		{
			name: "no policy",
			opts: &llms.CallOptions{},
		},
		{
			name: "no breakpoints",
			opts: &llms.CallOptions{
				PromptCachePolicy: &llms.PromptCachePolicy{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			params, partLocations := newAnthropicPromptCacheTestParams(t)
			want, _ := newAnthropicPromptCacheTestParams(t)

			reqOpts, err := applyPromptCachePolicyToRequest(&LLM{Options: &Options{}}, &params, tt.opts, partLocations)
			require.NoError(t, err)
			assert.Nil(t, reqOpts)
			assert.Equal(t, want, params, "params are not changed")
		})
	}
}

func TestApplyPromptCachePolicyToRequest_InvalidTargets(t *testing.T) {
	t.Parallel()

	messagePart := func(message, part int) llms.PromptCacheBreakpoint {
		return llms.PromptCacheBreakpoint{
			Target: llms.PromptCacheTarget{
				Kind:         llms.PromptCacheTargetMessagePart,
				MessageIndex: message,
				PartIndex:    part,
			},
		}
	}
	tool := func(index int) llms.PromptCacheBreakpoint {
		return llms.PromptCacheBreakpoint{
			Target: llms.PromptCacheTarget{
				Kind:      llms.PromptCacheTargetTool,
				ToolIndex: index,
			},
		}
	}
	// humanPart is the key of the first part of the human message.
	humanPart := promptCachePartKey{
		MessageIndex: 1,
		PartIndex:    0,
	}

	tests := []struct {
		name        string
		breakpoints []llms.PromptCacheBreakpoint
		// corrupt alters the converted request before the policy applies.
		corrupt func(*sdkanthropic.MessageNewParams, map[promptCachePartKey]promptCachePartLocation)
		wantErr string
	}{
		{
			name:        "negative message index",
			breakpoints: []llms.PromptCacheBreakpoint{messagePart(-1, 0)},
			wantErr:     "anthropic: invalid prompt cache message_part target: message=-1 part=0",
		},
		{
			name:        "negative part index",
			breakpoints: []llms.PromptCacheBreakpoint{messagePart(1, -1)},
			wantErr:     "anthropic: invalid prompt cache message_part target: message=1 part=-1",
		},
		{
			name:        "negative tool index",
			breakpoints: []llms.PromptCacheBreakpoint{tool(-1)},
			wantErr:     "anthropic: invalid prompt cache tool target: tool=-1",
		},
		{
			name:        "duplicate tool",
			breakpoints: []llms.PromptCacheBreakpoint{tool(0), tool(0)},
			wantErr:     "anthropic: duplicate prompt cache breakpoint for tool[0]",
		},
		{
			name:        "tool out of range",
			breakpoints: []llms.PromptCacheBreakpoint{tool(1)},
			wantErr:     "anthropic: prompt cache tool target out of range: tool[1]",
		},
		{
			name: "unsupported target kind",
			breakpoints: []llms.PromptCacheBreakpoint{
				{
					Target: llms.PromptCacheTarget{
						Kind: llms.PromptCacheTargetKind("request"),
					},
				},
			},
			wantErr: `anthropic: unsupported prompt cache target kind: "request"`,
		},
		{
			name:        "system index out of range",
			breakpoints: []llms.PromptCacheBreakpoint{messagePart(0, 0)},
			corrupt: func(params *sdkanthropic.MessageNewParams, _ map[promptCachePartKey]promptCachePartLocation) {
				params.System = nil
			},
			wantErr: "anthropic: invalid system prompt cache target index: 0",
		},
		{
			name:        "message index out of range",
			breakpoints: []llms.PromptCacheBreakpoint{messagePart(1, 0)},
			corrupt: func(_ *sdkanthropic.MessageNewParams, locations map[promptCachePartKey]promptCachePartLocation) {
				locations[humanPart] = promptCachePartLocation{
					MessageIndex: 3,
				}
			},
			wantErr: "anthropic: invalid message prompt cache target index: 3",
		},
		{
			name:        "content index out of range",
			breakpoints: []llms.PromptCacheBreakpoint{messagePart(1, 0)},
			corrupt: func(_ *sdkanthropic.MessageNewParams, locations map[promptCachePartKey]promptCachePartLocation) {
				locations[humanPart] = promptCachePartLocation{
					ContentIndex: 2,
				}
			},
			wantErr: "anthropic: invalid message content prompt cache target index: 2",
		},
		{
			name:        "content block without cache control",
			breakpoints: []llms.PromptCacheBreakpoint{messagePart(1, 0)},
			corrupt: func(params *sdkanthropic.MessageNewParams, _ map[promptCachePartKey]promptCachePartLocation) {
				params.Messages[0].Content[0] = sdkanthropic.NewThinkingBlock("signature", "thinking")
			},
			wantErr: "anthropic: prompt cache unsupported for message[1].part[0]",
		},
		{
			name:        "tool without cache control",
			breakpoints: []llms.PromptCacheBreakpoint{tool(0)},
			corrupt: func(params *sdkanthropic.MessageNewParams, _ map[promptCachePartKey]promptCachePartLocation) {
				params.Tools[0] = sdkanthropic.ToolUnionParam{}
			},
			wantErr: "anthropic: prompt cache unsupported for tool[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			params, partLocations := newAnthropicPromptCacheTestParams(t)
			if tt.corrupt != nil {
				tt.corrupt(&params, partLocations)
			}
			opts := &llms.CallOptions{
				PromptCachePolicy: &llms.PromptCachePolicy{
					Breakpoints: tt.breakpoints,
				},
			}

			reqOpts, err := applyPromptCachePolicyToRequest(&LLM{Options: &Options{}}, &params, opts, partLocations)
			require.EqualError(t, err, tt.wantErr)
			assert.Nil(t, reqOpts)
		})
	}
}

func TestPromptCacheRequestOptions_Conditions(t *testing.T) {
	t.Parallel()

	assert.Nil(t, promptCacheRequestOptions(nil, true), "no model")
	assert.Nil(t, promptCacheRequestOptions(&LLM{}, true), "no options")
	assert.Nil(t, promptCacheRequestOptions(&LLM{Options: &Options{}}, false), "short TTL")
	assert.Len(t, promptCacheRequestOptions(&LLM{
		Options: &Options{
			AnthropicBetaHeader: " other-beta ",
		},
	}, true), 1, "the configured beta lacks the extended TTL")
}
