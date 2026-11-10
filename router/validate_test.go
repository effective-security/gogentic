package router

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	toolLookup       = "lookup"
	toolOther        = "other"
	callID           = "call1"
	emptyArgs        = `{}`
	objectSchema     = `{"type":"object","properties":{}}`
	stringSchema     = `{"type":"string"}`
	rootSchemaErr    = "schema root must be an object"
	invalidToolCall  = "invalid or duplicate tool call"
	orphanToolResult = "orphan or duplicate tool result"
	textOnly         = "text blocks carry text only"
	refusalOnly      = "refusal blocks belong to assistant messages and carry text only"
	reasoningOnly    = "reasoning blocks belong to assistant messages"
	schemaModeErr    = "model does not support the requested schema mode"
	toolChoiceErr    = "model violated tool choice"
)

func ptr[T any](v T) *T { return &v }

func textBlock(text string) llms.InferenceBlock {
	return llms.InferenceBlock{
		Type: llms.BlockText,
		Text: text,
	}
}

func message(role llms.InferenceRole, blocks ...llms.InferenceBlock) llms.InferenceMessage {
	return llms.InferenceMessage{
		Role:    role,
		Content: blocks,
	}
}

func userText(text string) llms.InferenceMessage {
	return message(llms.InferenceRoleUser, textBlock(text))
}

func callBlock(id, name, args string) llms.InferenceBlock {
	return llms.InferenceBlock{
		Type:      llms.BlockFunctionCall,
		ID:        id,
		Name:      name,
		Arguments: args,
	}
}

func outputBlock(id, name, text string) llms.InferenceBlock {
	return llms.InferenceBlock{
		Type: llms.BlockFunctionOutput,
		ID:   id,
		Name: name,
		Text: text,
	}
}

func lookupTool() llms.InferenceTool {
	return llms.InferenceTool{
		Name:       toolLookup,
		Parameters: json.RawMessage(objectSchema),
	}
}

// requireRouterError asserts the exact kind, parameter and client-safe message.
func requireRouterError(t *testing.T, err error, kind Kind, param, msg string) *Error {
	t.Helper()
	require.Error(t, err)
	var re *Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, kind, re.Kind)
	assert.Equal(t, param, re.Param)
	assert.Equal(t, msg, re.Message)
	return re
}

func TestValidateRequestAcceptsFullProfile(t *testing.T) {
	req := request()
	req.Metadata = map[string]string{
		"tenant": "t1",
	}
	req.Input.Temperature = ptr(2.0)
	req.Input.TopP = ptr(1.0)
	req.Input.MaxTokens = ptr(maxOutputTokens)
	req.Input.Stop = []string{"END", "STOP"}
	req.Input.Tools = []llms.InferenceTool{lookupTool()}
	req.Input.ToolChoice = &llms.InferenceToolChoice{
		Mode: llms.ToolChoiceFunction,
		Name: toolLookup,
	}
	req.Input.ParallelToolCalls = ptr(false)
	req.Input.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "answer",
		Schema: json.RawMessage(objectSchema),
	}
	req.Input.Reasoning = &llms.InferenceReasoning{
		Effort:       llms.EffortMinimal,
		BudgetTokens: ptr(0),
	}
	req.Input.Messages = []llms.InferenceMessage{
		message(llms.InferenceRoleSystem, textBlock("be brief")),
		message(llms.InferenceRoleDeveloper, textBlock("dev")),
		userText("hi"),
		message(llms.InferenceRoleAssistant,
			llms.InferenceBlock{
				Type:   llms.BlockReasoning,
				Text:   "thinking",
				Opaque: json.RawMessage(`{"s":1}`),
				Source: "a",
			},
			llms.InferenceBlock{
				Type: llms.BlockRefusal,
				Text: "partly",
			},
			callBlock(callID, toolLookup, ` {"n":1}`),
			callBlock("call2", toolLookup, emptyArgs),
		),
		message(llms.InferenceRoleTool,
			outputBlock(callID, "", "one"),
			outputBlock("call2", toolLookup, "two"),
		),
	}
	require.NoError(t, validateRequest(&req))
	outputs := req.Input.Messages[4].Content
	assert.Equal(t, toolLookup, outputs[0].Name, "missing output name is filled from the call")
	assert.Equal(t, toolLookup, outputs[1].Name)

	for _, mode := range []llms.ToolChoiceMode{llms.ToolChoiceAuto, llms.ToolChoiceNone, llms.ToolChoiceRequired} {
		req.Input.ToolChoice = &llms.InferenceToolChoice{Mode: mode}
		assert.NoError(t, validateRequest(&req), mode)
	}
	for _, typ := range []llms.FormatType{llms.FormatText, llms.FormatJSONObject} {
		req.Input.Format = &llms.InferenceFormat{Type: typ}
		assert.NoError(t, validateRequest(&req), typ)
	}
}

func TestValidateRequestRejections(t *testing.T) {
	longText := strings.Repeat("x", maxModelNameBytes+1)
	tooManyMessages := make([]llms.InferenceMessage, maxMessages+1)
	for i := range tooManyMessages {
		tooManyMessages[i] = userText("hi")
	}
	tooManyTools := make([]llms.InferenceTool, maxTools+1)
	tooManyBlocks := make([]llms.InferenceBlock, maxBlocksPerMessage+1)
	for i := range tooManyBlocks {
		tooManyBlocks[i] = textBlock("x")
	}
	tooManyMetadata := map[string]string{}
	for i := range maxMetadataFields + 1 {
		tooManyMetadata[strings.Repeat("k", i+1)] = "v"
	}
	withTools := func(r *Request) {
		r.Input.Tools = []llms.InferenceTool{lookupTool()}
	}
	cases := []struct {
		name   string
		mutate func(*Request)
		param  string
		msg    string
	}{
		{
			name:   "model too long",
			mutate: func(r *Request) { r.Model = longText },
			param:  paramModel,
			msg:    "model name exceeds 256 bytes",
		},
		{
			name:   "no messages",
			mutate: func(r *Request) { r.Input.Messages = nil },
			param:  paramMessages,
			msg:    "expected 1 to 1024 messages",
		},
		{
			name:   "too many messages",
			mutate: func(r *Request) { r.Input.Messages = tooManyMessages },
			param:  paramMessages,
			msg:    "expected 1 to 1024 messages",
		},
		{
			name:   "too many tools",
			mutate: func(r *Request) { r.Input.Tools = tooManyTools },
			param:  paramTools,
			msg:    "at most 128 tools are supported",
		},
		{
			name:   "too many metadata fields",
			mutate: func(r *Request) { r.Metadata = tooManyMetadata },
			param:  paramMetadata,
			msg:    "at most 16 metadata fields are supported",
		},
		{
			name:   "empty metadata key",
			mutate: func(r *Request) { r.Metadata = map[string]string{"": "v"} },
			param:  paramMetadata,
			msg:    "metadata keys are 1-64 bytes and values at most 512 bytes",
		},
		{
			name:   "metadata key too long",
			mutate: func(r *Request) { r.Metadata = map[string]string{strings.Repeat("k", maxMetadataKeyBytes+1): "v"} },
			param:  paramMetadata,
			msg:    "metadata keys are 1-64 bytes and values at most 512 bytes",
		},
		{
			name:   "metadata value too long",
			mutate: func(r *Request) { r.Metadata = map[string]string{"k": strings.Repeat("v", maxMetadataValueBytes+1)} },
			param:  paramMetadata,
			msg:    "metadata keys are 1-64 bytes and values at most 512 bytes",
		},
		{
			name:   "temperature NaN",
			mutate: func(r *Request) { r.Input.Temperature = ptr(math.NaN()) },
			param:  paramTemperature,
			msg:    "temperature must be between 0 and 2",
		},
		{
			name:   "temperature negative",
			mutate: func(r *Request) { r.Input.Temperature = ptr(-0.1) },
			param:  paramTemperature,
			msg:    "temperature must be between 0 and 2",
		},
		{
			name:   "temperature too high",
			mutate: func(r *Request) { r.Input.Temperature = ptr(2.1) },
			param:  paramTemperature,
			msg:    "temperature must be between 0 and 2",
		},
		{
			name:   "top_p NaN",
			mutate: func(r *Request) { r.Input.TopP = ptr(math.NaN()) },
			param:  paramTopP,
			msg:    "top_p must be between 0 and 1",
		},
		{
			name:   "top_p negative",
			mutate: func(r *Request) { r.Input.TopP = ptr(-0.5) },
			param:  paramTopP,
			msg:    "top_p must be between 0 and 1",
		},
		{
			name:   "top_p too high",
			mutate: func(r *Request) { r.Input.TopP = ptr(1.5) },
			param:  paramTopP,
			msg:    "top_p must be between 0 and 1",
		},
		{
			name:   "max tokens zero",
			mutate: func(r *Request) { r.Input.MaxTokens = ptr(0) },
			param:  paramMaxTokens,
			msg:    "token limit must be between 1 and 1048576",
		},
		{
			name:   "max tokens too high",
			mutate: func(r *Request) { r.Input.MaxTokens = ptr(maxOutputTokens + 1) },
			param:  paramMaxTokens,
			msg:    "token limit must be between 1 and 1048576",
		},
		{
			name:   "too many stop strings",
			mutate: func(r *Request) { r.Input.Stop = []string{"a", "b", "c", "d", "e"} },
			param:  paramStop,
			msg:    "at most 4 stop strings are supported",
		},
		{
			name:   "empty stop string",
			mutate: func(r *Request) { r.Input.Stop = []string{""} },
			param:  paramStop,
			msg:    "stop strings are 1-1024 bytes",
		},
		{
			name:   "stop string too long",
			mutate: func(r *Request) { r.Input.Stop = []string{strings.Repeat("s", maxStopBytes+1)} },
			param:  paramStop,
			msg:    "stop strings are 1-1024 bytes",
		},
		{
			name: "reserved tool name",
			mutate: func(r *Request) {
				r.Input.Tools = []llms.InferenceTool{{
					Name:       string(llms.ToolChoiceRequired),
					Parameters: json.RawMessage(objectSchema),
				}}
			},
			param: paramTools,
			msg:   "invalid, reserved or duplicate tool name",
		},
		{
			name: "invalid tool name",
			mutate: func(r *Request) {
				r.Input.Tools = []llms.InferenceTool{{
					Name:       "1bad",
					Parameters: json.RawMessage(objectSchema),
				}}
			},
			param: paramTools,
			msg:   "invalid, reserved or duplicate tool name",
		},
		{
			name:   "duplicate tool name",
			mutate: func(r *Request) { r.Input.Tools = []llms.InferenceTool{lookupTool(), lookupTool()} },
			param:  paramTools,
			msg:    "invalid, reserved or duplicate tool name",
		},
		{
			name: "tool description too long",
			mutate: func(r *Request) {
				tool := lookupTool()
				tool.Description = strings.Repeat("d", maxToolDescriptionBytes+1)
				r.Input.Tools = []llms.InferenceTool{tool}
			},
			param: paramToolDesc,
			msg:   "description exceeds 4096 bytes",
		},
		{
			name: "tool schema too large",
			mutate: func(r *Request) {
				tool := lookupTool()
				tool.Parameters = json.RawMessage(strings.Repeat(" ", maxSchemaBytes+1))
				r.Input.Tools = []llms.InferenceTool{tool}
			},
			param: paramToolParameters,
			msg:   "schema exceeds 65536 bytes",
		},
		{
			name: "tool choice without tools",
			mutate: func(r *Request) {
				r.Input.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceAuto}
			},
			param: paramTools,
			msg:   "tool controls require tools",
		},
		{
			name:   "parallel tool calls without tools",
			mutate: func(r *Request) { r.Input.ParallelToolCalls = ptr(true) },
			param:  paramTools,
			msg:    "tool controls require tools",
		},
		{
			name: "tool name with auto mode",
			mutate: func(r *Request) {
				withTools(r)
				r.Input.ToolChoice = &llms.InferenceToolChoice{
					Mode: llms.ToolChoiceAuto,
					Name: toolLookup,
				}
			},
			param: paramToolChoice,
			msg:   "tool name is only valid with function mode",
		},
		{
			name: "unknown named function",
			mutate: func(r *Request) {
				withTools(r)
				r.Input.ToolChoice = &llms.InferenceToolChoice{
					Mode: llms.ToolChoiceFunction,
					Name: toolOther,
				}
			},
			param: paramToolChoice,
			msg:   "unknown function",
		},
		{
			name: "unsupported tool choice mode",
			mutate: func(r *Request) {
				withTools(r)
				r.Input.ToolChoice = &llms.InferenceToolChoice{Mode: "any"}
			},
			param: paramToolChoice,
			msg:   "unsupported tool choice mode",
		},
		{
			name: "text format with schema",
			mutate: func(r *Request) {
				r.Input.Format = &llms.InferenceFormat{
					Type:   llms.FormatText,
					Schema: json.RawMessage(objectSchema),
				}
			},
			param: paramFormat,
			msg:   "schema controls require json_schema",
		},
		{
			name: "json object with strict",
			mutate: func(r *Request) {
				r.Input.Format = &llms.InferenceFormat{
					Type:   llms.FormatJSONObject,
					Strict: true,
				}
			},
			param: paramFormat,
			msg:   "schema controls require json_schema",
		},
		{
			name: "json object with name",
			mutate: func(r *Request) {
				r.Input.Format = &llms.InferenceFormat{
					Type: llms.FormatJSONObject,
					Name: "answer",
				}
			},
			param: paramFormat,
			msg:   "schema controls require json_schema",
		},
		{
			name: "json schema with invalid name",
			mutate: func(r *Request) {
				r.Input.Format = &llms.InferenceFormat{
					Type:   llms.FormatJSONSchema,
					Name:   "bad name",
					Schema: json.RawMessage(objectSchema),
				}
			},
			param: paramFormat,
			msg:   "schema name must match [a-zA-Z_][a-zA-Z0-9_-]{0,63}",
		},
		{
			name: "json schema too large",
			mutate: func(r *Request) {
				r.Input.Format = &llms.InferenceFormat{
					Type:   llms.FormatJSONSchema,
					Name:   "answer",
					Schema: json.RawMessage(strings.Repeat(" ", maxSchemaBytes+1)),
				}
			},
			param: paramFormat,
			msg:   "schema exceeds 65536 bytes",
		},
		{
			name: "unsupported format",
			mutate: func(r *Request) {
				r.Input.Format = &llms.InferenceFormat{Type: "grammar"}
			},
			param: paramFormat,
			msg:   "unsupported response format",
		},
		{
			name: "unsupported reasoning effort",
			mutate: func(r *Request) {
				r.Input.Reasoning = &llms.InferenceReasoning{Effort: "extreme"}
			},
			param: paramReasoningCfg,
			msg:   "unsupported reasoning effort",
		},
		{
			name: "negative reasoning budget",
			mutate: func(r *Request) {
				r.Input.Reasoning = &llms.InferenceReasoning{BudgetTokens: ptr(-1)}
			},
			param: paramReasoningCfg,
			msg:   "reasoning budget must be between 0 and 1048576",
		},
		{
			name: "reasoning budget too high",
			mutate: func(r *Request) {
				r.Input.Reasoning = &llms.InferenceReasoning{BudgetTokens: ptr(maxOutputTokens + 1)}
			},
			param: paramReasoningCfg,
			msg:   "reasoning budget must be between 0 and 1048576",
		},
		{
			name:   "message without blocks",
			mutate: func(r *Request) { r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser)} },
			param:  paramContent,
			msg:    "expected 1 to 256 content blocks",
		},
		{
			name: "message with too many blocks",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, tooManyBlocks...)}
			},
			param: paramContent,
			msg:   "expected 1 to 256 content blocks",
		},
		{
			name: "user message before tool results",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, callBlock(callID, toolLookup, emptyArgs)),
					userText("again"),
				}
			},
			param: paramMessages,
			msg:   "all tool calls require a result before continuing",
		},
		{
			name: "system after user",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleSystem, textBlock("late")),
				}
			},
			param: paramMessages,
			msg:   "instructions must precede conversational messages",
		},
		{
			name: "unsupported role",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message("critic", textBlock("x"))}
			},
			param: paramMessagesRole,
			msg:   "unsupported role",
		},
		{
			name: "block ID too long",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, llms.InferenceBlock{
					Type: llms.BlockText,
					ID:   longText,
				})}
			},
			param: paramToolCallID,
			msg:   "tool call ID exceeds 256 bytes",
		},
		{
			name: "text block in tool message",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleTool, textBlock("x"))}
			},
			param: paramContent,
			msg:   textOnly,
		},
		{
			name: "text block with name",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, llms.InferenceBlock{
					Type: llms.BlockText,
					Text: "x",
					Name: toolLookup,
				})}
			},
			param: paramContent,
			msg:   textOnly,
		},
		{
			name: "text block with opaque state",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, llms.InferenceBlock{
					Type:   llms.BlockText,
					Text:   "x",
					Opaque: json.RawMessage(emptyArgs),
				})}
			},
			param: paramContent,
			msg:   textOnly,
		},
		{
			name: "refusal in user message",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, llms.InferenceBlock{
					Type: llms.BlockRefusal,
					Text: "no",
				})}
			},
			param: paramContent,
			msg:   refusalOnly,
		},
		{
			name: "refusal with arguments",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, llms.InferenceBlock{
						Type:      llms.BlockRefusal,
						Arguments: emptyArgs,
					}),
				}
			},
			param: paramContent,
			msg:   refusalOnly,
		},
		{
			name: "reasoning in user message",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, llms.InferenceBlock{
					Type: llms.BlockReasoning,
					Text: "x",
				})}
			},
			param: paramReasoning,
			msg:   reasoningOnly,
		},
		{
			name: "reasoning with function name",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, llms.InferenceBlock{
						Type: llms.BlockReasoning,
						Name: toolLookup,
					}),
				}
			},
			param: paramReasoning,
			msg:   reasoningOnly,
		},
		{
			name: "reasoning state too large",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, llms.InferenceBlock{
						Type:   llms.BlockReasoning,
						Opaque: json.RawMessage(strings.Repeat(" ", maxOpaqueBytes+1)),
						Source: "a",
					}),
				}
			},
			param: paramReasoning,
			msg:   "reasoning state exceeds 262144 bytes",
		},
		{
			name: "reasoning state is not JSON",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, llms.InferenceBlock{
						Type:   llms.BlockReasoning,
						Opaque: json.RawMessage(`{`),
						Source: "a",
					}),
				}
			},
			param: paramReasoning,
			msg:   "reasoning state requires a valid source and JSON payload",
		},
		{
			name: "function call in user message",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, callBlock(callID, toolLookup, emptyArgs))}
			},
			param: paramToolCalls,
			msg:   invalidToolCall,
		},
		{
			name: "function call without ID",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, callBlock("", toolLookup, emptyArgs)),
				}
			},
			param: paramToolCalls,
			msg:   invalidToolCall,
		},
		{
			name: "function call with invalid name",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, callBlock(callID, "1bad", emptyArgs)),
				}
			},
			param: paramToolCalls,
			msg:   invalidToolCall,
		},
		{
			name: "duplicate function call ID",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant,
						callBlock(callID, toolLookup, emptyArgs),
						callBlock(callID, toolLookup, emptyArgs),
					),
				}
			},
			param: paramToolCalls,
			msg:   invalidToolCall,
		},
		{
			name: "function arguments are not an object",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, callBlock(callID, toolLookup, `[]`)),
				}
			},
			param: paramToolCalls + ".arguments",
			msg:   "arguments must be a JSON object",
		},
		{
			name: "orphan tool result",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleTool, outputBlock(callID, "", "r")),
				}
			},
			param: paramToolCallID,
			msg:   orphanToolResult,
		},
		{
			name: "tool result with arguments",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, callBlock(callID, toolLookup, emptyArgs)),
					message(llms.InferenceRoleTool, llms.InferenceBlock{
						Type:      llms.BlockFunctionOutput,
						ID:        callID,
						Arguments: emptyArgs,
					}),
				}
			},
			param: paramToolCallID,
			msg:   orphanToolResult,
		},
		{
			name: "tool result name mismatch",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, callBlock(callID, toolLookup, emptyArgs)),
					message(llms.InferenceRoleTool, outputBlock(callID, toolOther, "r")),
				}
			},
			param: paramToolCallID,
			msg:   "tool result name mismatch",
		},
		{
			name: "unsupported block type",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleUser, llms.InferenceBlock{Type: "image"})}
			},
			param: paramContent + ".type",
			msg:   "unsupported content block type",
		},
		{
			name: "missing tool results",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, callBlock(callID, toolLookup, emptyArgs)),
				}
			},
			param: paramMessages,
			msg:   "missing tool results",
		},
		{
			name: "instructions only",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{message(llms.InferenceRoleSystem, textBlock("be brief"))}
			},
			param: paramMessages,
			msg:   "conversation requires a user or assistant message",
		},
		{
			name: "assistant prefill",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, textBlock("Sure,")),
				}
			},
			param: paramMessages,
			msg:   "assistant prefilling is outside the portable profile",
		},
		{
			name: "content too large",
			mutate: func(r *Request) {
				r.Input.Messages = []llms.InferenceMessage{userText(strings.Repeat("x", maxContentBytes+1))}
			},
			param: paramMessages,
			msg:   "content exceeds 2097152 bytes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := request()
			tc.mutate(&req)
			err := validateRequest(&req)
			re := requireRouterError(t, err, KindInvalidRequest, tc.param, tc.msg)
			assert.NoError(t, re.Cause)
		})
	}
}

func TestValidateRequestSchemaCauses(t *testing.T) {
	req := request()
	tool := lookupTool()
	tool.Parameters = json.RawMessage(stringSchema)
	req.Input.Tools = []llms.InferenceTool{tool}
	err := validateRequest(&req)
	re := requireRouterError(t, err, KindInvalidRequest, paramToolParameters, "unsupported or invalid tool schema")
	require.EqualError(t, re.Cause, rootSchemaErr)

	req = request()
	req.Input.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "answer",
		Schema: json.RawMessage(stringSchema),
	}
	err = validateRequest(&req)
	re = requireRouterError(t, err, KindInvalidRequest, paramFormat, "unsupported or invalid output schema")
	require.EqualError(t, re.Cause, rootSchemaErr)
}

func TestSupports(t *testing.T) {
	all := Features{
		Tools:           true,
		JSON:            true,
		JSONSchema:      true,
		StrictSchema:    true,
		StrictTools:     true,
		SchemaWithTools: true,
		DeveloperRole:   true,
		Reasoning:       true,
		Seed:            true,
	}
	toolHistory := []llms.InferenceMessage{
		userText("hi"),
		message(llms.InferenceRoleAssistant, callBlock(callID, toolLookup, emptyArgs)),
		message(llms.InferenceRoleTool, outputBlock(callID, toolLookup, "r")),
	}
	strictTool := lookupTool()
	strictTool.Strict = ptr(true)
	cases := []struct {
		name      string
		features  Features
		maxOutput int
		mutate    func(*llms.InferenceRequest)
		param     string
		msg       string
	}{
		{
			name:      "output limit",
			features:  all,
			maxOutput: 10,
			mutate:    func(in *llms.InferenceRequest) { in.MaxTokens = ptr(11) },
			param:     paramMaxTokens,
			msg:       "model output limit exceeded",
		},
		{
			name:   "function tools",
			mutate: func(in *llms.InferenceRequest) { in.Tools = []llms.InferenceTool{lookupTool()} },
			param:  paramTools,
			msg:    "model does not support function tools",
		},
		{
			name:   "reasoning controls",
			mutate: func(in *llms.InferenceRequest) { in.Reasoning = &llms.InferenceReasoning{} },
			param:  paramReasoningCfg,
			msg:    "model does not support reasoning controls",
		},
		{
			name:   "seed",
			mutate: func(in *llms.InferenceRequest) { in.Seed = ptr(int64(7)) },
			param:  paramSeed,
			msg:    "model does not support seeds",
		},
		{
			name: "developer role",
			mutate: func(in *llms.InferenceRequest) {
				in.Messages = append([]llms.InferenceMessage{message(llms.InferenceRoleDeveloper, textBlock("dev"))}, in.Messages...)
			},
			param: paramMessagesRole,
			msg:   "model does not support the developer role",
		},
		{
			name:   "tool history",
			mutate: func(in *llms.InferenceRequest) { in.Messages = toolHistory },
			param:  paramMessages,
			msg:    "model does not support tool history",
		},
		{
			name: "foreign reasoning state",
			features: Features{
				Tools: true,
			},
			mutate: func(in *llms.InferenceRequest) {
				in.Messages = []llms.InferenceMessage{
					userText("hi"),
					message(llms.InferenceRoleAssistant, llms.InferenceBlock{
						Type:   llms.BlockReasoning,
						Opaque: json.RawMessage(emptyArgs),
						Source: "b",
					}),
					userText("again"),
				}
			},
			param: paramReasoning,
			msg:   "reasoning state was produced by a different model",
		},
		{
			name: "strict tools",
			features: Features{
				Tools: true,
			},
			mutate: func(in *llms.InferenceRequest) { in.Tools = []llms.InferenceTool{strictTool} },
			param:  paramToolsStrict,
			msg:    "model does not support strict tools",
		},
		{
			name: "json mode",
			mutate: func(in *llms.InferenceRequest) {
				in.Format = &llms.InferenceFormat{Type: llms.FormatJSONObject}
			},
			param: paramFormat,
			msg:   "model does not support JSON mode",
		},
		{
			name: "json schema",
			mutate: func(in *llms.InferenceRequest) {
				in.Format = &llms.InferenceFormat{Type: llms.FormatJSONSchema}
			},
			param: paramFormat,
			msg:   schemaModeErr,
		},
		{
			name: "strict json schema",
			features: Features{
				JSONSchema: true,
			},
			mutate: func(in *llms.InferenceRequest) {
				in.Format = &llms.InferenceFormat{
					Type:   llms.FormatJSONSchema,
					Strict: true,
				}
			},
			param: paramFormat,
			msg:   schemaModeErr,
		},
		{
			name: "json with tools",
			features: Features{
				Tools: true,
				JSON:  true,
			},
			mutate: func(in *llms.InferenceRequest) {
				in.Tools = []llms.InferenceTool{lookupTool()}
				in.Format = &llms.InferenceFormat{Type: llms.FormatJSONObject}
			},
			param: paramFormat,
			msg:   "model does not support JSON output with tools",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := request().Input
			tc.mutate(&in)
			tgt := Target{
				ID:              "a",
				Features:        tc.features,
				MaxOutputTokens: tc.maxOutput,
			}
			requireRouterError(t, supports(tgt, in), KindUnsupported, tc.param, tc.msg)
		})
	}

	in := request().Input
	in.Messages = []llms.InferenceMessage{message(llms.InferenceRoleDeveloper, textBlock("dev"))}
	in.Messages = append(in.Messages, toolHistory...)
	in.Messages = append(in.Messages, userText("more"))
	in.Tools = []llms.InferenceTool{strictTool}
	in.Reasoning = &llms.InferenceReasoning{}
	in.Seed = ptr(int64(1))
	in.MaxTokens = ptr(10)
	in.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Strict: true,
	}
	assert.NoError(t, supports(Target{ID: "a", Features: all, MaxOutputTokens: 10}, in))
}

func TestNormalizeResponseRejections(t *testing.T) {
	tools := []llms.InferenceTool{
		lookupTool(),
		{
			Name:       toolOther,
			Parameters: json.RawMessage(objectSchema),
		},
	}
	withCalls := func(choice *llms.InferenceToolChoice) llms.InferenceRequest {
		return llms.InferenceRequest{
			Tools:      tools,
			ToolChoice: choice,
		}
	}
	callResponse := func(blocks ...llms.InferenceBlock) *llms.InferenceResponse {
		return &llms.InferenceResponse{
			Content:      blocks,
			FinishReason: llms.FinishToolCalls,
		}
	}
	cases := []struct {
		name string
		in   llms.InferenceRequest
		out  *llms.InferenceResponse
		err  string
	}{
		{
			name: "nil response",
			err:  "nil inference response",
		},
		{
			name: "unknown finish reason",
			out: &llms.InferenceResponse{
				Content:      []llms.InferenceBlock{textBlock("x")},
				FinishReason: "paused",
			},
			err: `unknown finish reason "paused"`,
		},
		{
			name: "invalid reasoning state",
			out: &llms.InferenceResponse{
				Content: []llms.InferenceBlock{{
					Type:   llms.BlockReasoning,
					Opaque: json.RawMessage(`{`),
				}},
				FinishReason: llms.FinishStop,
			},
			err: "reasoning state is not valid JSON",
		},
		{
			name: "unknown tool",
			in:   withCalls(nil),
			out:  callResponse(callBlock(callID, "missing", emptyArgs)),
			err:  `invalid returned tool call "missing"`,
		},
		{
			name: "call without ID",
			in:   withCalls(nil),
			out:  callResponse(callBlock("", toolLookup, emptyArgs)),
			err:  `invalid returned tool call "lookup"`,
		},
		{
			name: "tool choice none",
			in:   withCalls(&llms.InferenceToolChoice{Mode: llms.ToolChoiceNone}),
			out:  callResponse(callBlock(callID, toolLookup, emptyArgs)),
			err:  toolChoiceErr,
		},
		{
			name: "named tool choice mismatch",
			in: withCalls(&llms.InferenceToolChoice{
				Mode: llms.ToolChoiceFunction,
				Name: toolOther,
			}),
			out: callResponse(callBlock(callID, toolLookup, emptyArgs)),
			err: toolChoiceErr,
		},
		{
			name: "arguments are not an object",
			in:   withCalls(nil),
			out:  callResponse(callBlock(callID, toolLookup, `"x"`)),
			err:  "returned tool arguments must be a JSON object",
		},
		{
			name: "unsupported output block",
			in:   withCalls(nil),
			out:  callResponse(outputBlock(callID, toolLookup, "r")),
			err:  `unsupported output block type "function_call_output"`,
		},
		{
			name: "parallel calls disabled",
			in: llms.InferenceRequest{
				Tools:             tools,
				ParallelToolCalls: ptr(false),
			},
			out: callResponse(
				callBlock(callID, toolLookup, emptyArgs),
				callBlock("call2", toolOther, emptyArgs),
			),
			err: "model violated parallel tool setting",
		},
		{
			name: "tool calls finish without calls",
			out:  callResponse(textBlock("x")),
			err:  "missing output tool calls",
		},
		{
			name: "required tool not called",
			in:   withCalls(&llms.InferenceToolChoice{Mode: llms.ToolChoiceRequired}),
			out:  textResponse("x"),
			err:  "model did not call a required tool",
		},
		{
			name: "json object output is an array",
			in: llms.InferenceRequest{
				Format: &llms.InferenceFormat{Type: llms.FormatJSONObject},
			},
			out: textResponse(`[1]`),
			err: "JSON output must be an object",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := normalizeResponse(tc.in, tc.out)
			require.EqualError(t, err, tc.err)
			assert.Nil(t, res)
		})
	}
}

func TestNormalizeResponseAccepts(t *testing.T) {
	jsonObject := llms.InferenceRequest{
		Format: &llms.InferenceFormat{Type: llms.FormatJSONObject},
	}
	res, err := normalizeResponse(jsonObject, textResponse(`{"ok":true}`))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishStop, res.FinishReason)

	truncated := textResponse(`{"ok":`)
	truncated.FinishReason = llms.FinishLength
	res, err = normalizeResponse(jsonObject, truncated)
	require.NoError(t, err, "truncated output skips format validation")
	assert.Equal(t, llms.FinishLength, res.FinishReason)

	filtered := textResponse("")
	filtered.FinishReason = llms.FinishContentFilter
	res, err = normalizeResponse(llms.InferenceRequest{}, filtered)
	require.NoError(t, err)
	assert.Equal(t, llms.FinishContentFilter, res.FinishReason)

	reasoning := &llms.InferenceResponse{
		Content: []llms.InferenceBlock{
			{
				Type:   llms.BlockReasoning,
				Text:   "thought",
				Opaque: json.RawMessage(`{"s":1}`),
			},
			textBlock("ok"),
		},
		FinishReason: llms.FinishStop,
		UsageKnown:   true,
		Usage: llms.Usage{
			InputTokens:     10,
			OutputTokens:    4,
			TotalTokens:     14,
			CacheReadTokens: 3,
			ReasoningTokens: 2,
		},
	}
	res, err = normalizeResponse(llms.InferenceRequest{}, reasoning)
	require.NoError(t, err)
	assert.True(t, res.UsageKnown, "consistent usage is preserved")
	assert.Equal(t, reasoning.Usage, res.Usage)
	assert.JSONEq(t, `{"s":1}`, string(res.Content[0].Opaque))
}

func TestUsageConsistent(t *testing.T) {
	cases := []struct {
		name  string
		usage llms.Usage
		want  bool
	}{
		{
			name: "consistent",
			usage: llms.Usage{
				InputTokens:      10,
				OutputTokens:     5,
				TotalTokens:      15,
				CacheReadTokens:  4,
				CacheWriteTokens: 6,
				ReasoningTokens:  5,
			},
			want: true,
		},
		{
			name: "total mismatch",
			usage: llms.Usage{
				InputTokens:  10,
				OutputTokens: 5,
				TotalTokens:  16,
			},
		},
		{
			name: "cache exceeds input",
			usage: llms.Usage{
				InputTokens:     1,
				OutputTokens:    1,
				TotalTokens:     2,
				CacheReadTokens: 2,
			},
		},
		{
			name: "reasoning exceeds output",
			usage: llms.Usage{
				InputTokens:     1,
				OutputTokens:    1,
				TotalTokens:     2,
				ReasoningTokens: 2,
			},
		},
		{
			name: "counter overflow",
			usage: llms.Usage{
				InputTokens: maxAccountingTokens + 1,
				TotalTokens: maxAccountingTokens + 1,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, usageConsistent(tc.usage))
		})
	}
}
