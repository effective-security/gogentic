package bedrockclient

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	anthropicModelID   = "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
	anthropicToolName  = "lookup"
	anthropicToolID    = "toolu_1"
	anthropicToolInput = `{"city":"Paris"}`
	noInputToolName    = "now"
	noInputToolID      = "toolu_2"
	emptyToolInput     = "{}"
	throttledMessage   = "slow down"
	streamWaitTimeout  = 5 * time.Second
)

func lookupTool() llms.Tool {
	props := jsonschema.NewProperties()
	props.Set("city", &jsonschema.Schema{
		Type: "string",
	})
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        anthropicToolName,
			Description: "Look up the weather",
			Parameters: &jsonschema.Schema{
				Type:       "object",
				Properties: props,
				Required:   []string{"city"},
			},
		},
	}
}

func TestCreateAnthropicCompletionRequest(t *testing.T) {
	const response = `{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	tests := []struct {
		name     string
		messages []Message
		options  llms.CallOptions
		request  string
	}{
		{
			name: "defaults",
			messages: []Message{
				textMessage(llms.RoleGeneric, "hi"),
			},
			options: llms.CallOptions{},
			request: `{
				"anthropic_version": "bedrock-2023-05-31",
				"max_tokens": 2048,
				"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]
			}`,
		},
		{
			name: "full conversation with tools and sampling",
			messages: []Message{
				textMessage(llms.RoleSystem, "be "),
				textMessage(llms.RoleSystem, "brief"),
				textMessage(llms.RoleHuman, "describe"),
				{
					Role:     llms.RoleHuman,
					Content:  "PNG",
					Type:     AnthropicMessageTypeImage,
					MimeType: "image/png",
				},
				textMessage(llms.RoleAI, "let me check"),
				{
					Role:       llms.RoleAI,
					Content:    anthropicToolID,
					Type:       AnthropicMessageTypeToolUse,
					ToolCallID: anthropicToolID,
					ToolName:   anthropicToolName,
					ToolInput:  anthropicToolInput,
				},
				{
					Role:       llms.RoleTool,
					Content:    "sunny",
					Type:       AnthropicMessageTypeToolResult,
					ToolCallID: anthropicToolID,
				},
				textMessage(llms.RoleHuman, "thanks"),
			},
			options: llms.CallOptions{
				MaxTokens:   256,
				Temperature: 0.5,
				TopP:        0.9,
				TopK:        40,
				StopWords:   []string{"STOP"},
				Tools: []llms.Tool{
					lookupTool(),
				},
			},
			request: `{
				"anthropic_version": "bedrock-2023-05-31",
				"max_tokens": 256,
				"system": "be brief",
				"messages": [
					{"role": "user", "content": [
						{"type": "text", "text": "describe"},
						{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "UE5H"}}
					]},
					{"role": "assistant", "content": [
						{"type": "text", "text": "let me check"},
						{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": {"city": "Paris"}}
					]},
					{"role": "user", "content": [
						{"type": "tool_result", "tool_use_id": "toolu_1", "content": "sunny"}
					]},
					{"role": "user", "content": [{"type": "text", "text": "thanks"}]}
				],
				"temperature": 0.5,
				"top_p": 0.9,
				"top_k": 40,
				"stop_sequences": ["STOP"],
				"tools": [{
					"name": "lookup",
					"description": "Look up the weather",
					"input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
				}]
			}`,
		},
		{
			name: "tool without properties and tool use without input",
			messages: []Message{
				{
					Role:       llms.RoleAI,
					Type:       AnthropicMessageTypeToolUse,
					ToolCallID: anthropicToolID,
					ToolName:   "ping",
				},
			},
			options: llms.CallOptions{
				Tools: []llms.Tool{
					{
						Type: "function",
						Function: &llms.FunctionDefinition{
							Name:        "ping",
							Description: "Ping the service",
							Parameters: &jsonschema.Schema{
								Type: "object",
							},
						},
					},
				},
			},
			request: `{
				"anthropic_version": "bedrock-2023-05-31",
				"max_tokens": 2048,
				"messages": [{"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_1", "name": "ping"}]}],
				"tools": [{"name": "ping", "description": "Ping the service", "input_schema": {"type": "object"}}]
			}`,
		},
		{
			// F-019: a function without Parameters used to panic.
			name: "tool without parameters",
			messages: []Message{
				textMessage(llms.RoleHuman, "hi"),
			},
			options: llms.CallOptions{
				Tools: []llms.Tool{
					{
						Type: "function",
						Function: &llms.FunctionDefinition{
							Name:        "now",
							Description: "Current time",
						},
					},
				},
			},
			request: `{
				"anthropic_version": "bedrock-2023-05-31",
				"max_tokens": 2048,
				"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}],
				"tools": [{"name": "now", "description": "Current time", "input_schema": {"type": "object"}}]
			}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, response)
			_, err := s.client().CreateCompletion(context.Background(), anthropicModelID, tt.messages, tt.options)
			require.NoError(t, err)
			calls := s.requests()
			require.Len(t, calls, 1)
			assert.Equal(t, fmt.Sprintf(invokePathFmt, anthropicModelID), calls[0].path)
			assert.Equal(t, acceptAny, calls[0].accept)
			assert.Equal(t, contentTypeJSON, calls[0].contentType)
			assert.JSONEq(t, tt.request, calls[0].body)
		})
	}
}

// F-019: a tool without a function definition used to panic.
func TestCreateAnthropicCompletionToolWithoutFunction(t *testing.T) {
	s := newStub(t, `{}`)
	resp, err := s.client().CreateCompletion(context.Background(), anthropicModelID, []Message{
		textMessage(llms.RoleHuman, "hi"),
	}, llms.CallOptions{
		Tools: []llms.Tool{
			lookupTool(),
			{
				Type: "web_search",
			},
		},
	})
	require.ErrorIs(t, err, llms.ErrMissingFunctionDefinition)
	assert.EqualError(t, err, `bedrock anthropic: tool [1]: tool of type "web_search": tool has no function definition`)
	assert.Nil(t, resp)
	assert.Empty(t, s.requests(), "no request is sent for rejected tools")
}

func TestCreateAnthropicCompletionResponse(t *testing.T) {
	usage := llms.Usage{
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
	}
	toolCalls := []llms.ToolCall{
		{
			ID:   anthropicToolID,
			Type: llms.ToolTypeFunction,
			FunctionCall: &llms.FunctionCall{
				Name:      anthropicToolName,
				Arguments: anthropicToolInput,
			},
		},
	}
	tests := []struct {
		name     string
		response string
		expected []*llms.ContentChoice
	}{
		{
			name:     "text blocks are concatenated",
			response: `{"content":[{"type":"text","text":"Hello"},{"type":"text","text":" world"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`,
			expected: []*llms.ContentChoice{
				{
					Content:    "Hello world",
					StopReason: AnthropicCompletionReasonEndTurn,
					Usage:      usage,
				},
			},
		},
		{
			name:     "tool use only",
			response: `{"content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"city":"Paris"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
			expected: []*llms.ContentChoice{
				{
					ToolCalls:  toolCalls,
					StopReason: AnthropicCompletionReasonToolUse,
					Usage:      usage,
				},
			},
		},
		{
			name:     "text and tool use become separate choices",
			response: `{"content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"city":"Paris"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
			expected: []*llms.ContentChoice{
				{
					Content:    "checking",
					StopReason: AnthropicCompletionReasonToolUse,
					Usage:      usage,
				},
				{
					ToolCalls:  toolCalls,
					StopReason: AnthropicCompletionReasonToolUse,
					Usage:      usage,
				},
			},
		},
		{
			name:     "no text and no tool calls yield one empty choice",
			response: `{"content":[{"type":"text","text":""}],"stop_reason":"stop_sequence","stop_sequence":"STOP","usage":{"input_tokens":10,"output_tokens":5}}`,
			expected: []*llms.ContentChoice{
				{
					Content:    "",
					StopReason: AnthropicCompletionReasonStopSequence,
					Usage:      usage,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, tt.response)
			res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, []Message{
				textMessage(llms.RoleHuman, "hi"),
			}, llms.CallOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, res.Choices)
			for _, choice := range res.Choices {
				for _, tc := range choice.ToolCalls {
					// F-001: callers read the tool call through these accessors.
					assert.Equal(t, anthropicToolName, tc.GetFunctionCallName())
					assert.Equal(t, anthropicToolInput, tc.GetFunctionCallArguments())
				}
			}
		})
	}
}

func TestCreateAnthropicCompletionResponseErrors(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "hi"),
	}
	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			name:     "no content",
			response: `{"content":[],"stop_reason":"end_turn"}`,
			expected: "no results",
		},
		{
			name:     "truncated by max tokens",
			response: `{"content":[{"type":"text","text":"partial"}],"stop_reason":"max_tokens"}`,
			expected: "completed due to max_tokens. Maybe try increasing max tokens",
		},
		{
			name:     "invalid json",
			response: invalidJSONBody,
			expected: "invalid character 'n' looking for beginning of object key string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, tt.response)
			res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{})
			require.EqualError(t, err, tt.expected)
			assert.Nil(t, res)
		})
	}

	s := newStub(t).fail()
	res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{})
	requireValidationError(t, err)
	assert.Nil(t, res)
}

func TestCreateAnthropicCompletionInvalidMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		expected string
	}{
		{
			name: "unsupported role",
			messages: []Message{
				textMessage(llms.Role("robot"), "beep"),
			},
			expected: "role not supported",
		},
		{
			name: "multiple system prompts",
			messages: []Message{
				textMessage(llms.RoleSystem, "first"),
				textMessage(llms.RoleHuman, "hi"),
				textMessage(llms.RoleSystem, "second"),
			},
			expected: "multiple system prompts",
		},
		{
			name: "system prompt must be text",
			messages: []Message{
				{
					Role:     llms.RoleSystem,
					Content:  "PNG",
					Type:     AnthropicMessageTypeImage,
					MimeType: "image/png",
				},
			},
			expected: "system prompt must be text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t)
			res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, tt.messages, llms.CallOptions{})
			require.EqualError(t, err, tt.expected)
			assert.Nil(t, res)
			assert.Empty(t, s.requests(), "invalid input never reaches bedrock")
		})
	}
}

func TestGetAnthropicRole(t *testing.T) {
	tests := []struct {
		role     llms.Role
		expected string
	}{
		{
			role:     llms.RoleSystem,
			expected: AnthropicSystem,
		},
		{
			role:     llms.RoleAI,
			expected: AnthropicRoleAssistant,
		},
		{
			role:     llms.RoleGeneric,
			expected: AnthropicRoleUser,
		},
		{
			role:     llms.RoleHuman,
			expected: AnthropicRoleUser,
		},
		{
			role:     llms.RoleTool,
			expected: AnthropicRoleUser,
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			role, err := getAnthropicRole(tt.role)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, role)
		})
	}

	role, err := getAnthropicRole("")
	require.EqualError(t, err, "role not supported")
	assert.Empty(t, role)
}

// recordChunks returns a StreamingFunc that appends every chunk to dst.
func recordChunks(dst *[]string) func(context.Context, []byte) error {
	return func(_ context.Context, chunk []byte) error {
		*dst = append(*dst, string(chunk))
		return nil
	}
}

func TestCreateAnthropicCompletionStreaming(t *testing.T) {
	s := newStub(t, eventStream(
		chunkEvent(t, `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"usage":{"input_tokens":12,"output_tokens":1}}}`),
		chunkEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		chunkEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`),
		typedEvent("futureEvent", `{"ignored":true}`),
		chunkEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`),
		chunkEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":7}}`),
		chunkEvent(t, `{"type":"message_stop","amazon-bedrock-invocationMetrics":{"inputTokenCount":12,"outputTokenCount":7}}`),
	))
	s.contentType = contentTypeEvents

	var chunks []string
	res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, []Message{
		textMessage(llms.RoleHuman, "hi"),
	}, llms.CallOptions{
		MaxTokens:     32,
		StreamingFunc: recordChunks(&chunks),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Hel", "lo"}, chunks)
	assert.Equal(t, []*llms.ContentChoice{
		{
			Content:    "Hello",
			StopReason: AnthropicCompletionReasonEndTurn,
			Usage: llms.Usage{
				InputTokens:  12,
				OutputTokens: 7,
				TotalTokens:  19,
			},
		},
	}, res.Choices)

	calls := s.requests()
	require.Len(t, calls, 1)
	assert.Equal(t, fmt.Sprintf(invokeStreamPathFmt, anthropicModelID), calls[0].path)
	assert.Equal(t, acceptAny, calls[0].streamAccept)
	assert.JSONEq(t, `{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens": 32,
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]
	}`, calls[0].body)
}

// F-020: a streamed message carries the same choices as the non-streaming one,
// tool calls and total usage included.
func TestCreateAnthropicCompletionStreamingToolCalls(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "weather in Paris?"),
	}
	usage := llms.Usage{
		InputTokens:  12,
		OutputTokens: 9,
		TotalTokens:  21,
	}
	expected := []*llms.ContentChoice{
		{
			Content:    "Checking",
			StopReason: AnthropicCompletionReasonToolUse,
			Usage:      usage,
		},
		{
			ToolCalls: []llms.ToolCall{
				{
					ID:   anthropicToolID,
					Type: llms.ToolTypeFunction,
					FunctionCall: &llms.FunctionCall{
						Name:      anthropicToolName,
						Arguments: anthropicToolInput,
					},
				},
				{
					ID:   noInputToolID,
					Type: llms.ToolTypeFunction,
					FunctionCall: &llms.FunctionCall{
						Name:      noInputToolName,
						Arguments: emptyToolInput,
					},
				},
			},
			StopReason: AnthropicCompletionReasonToolUse,
			Usage:      usage,
		},
	}

	s := newStub(t, eventStream(
		chunkEvent(t, `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"usage":{"input_tokens":12,"output_tokens":1}}}`),
		chunkEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		chunkEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Checking"}}`),
		chunkEvent(t, `{"type":"content_block_stop","index":0}`),
		chunkEvent(t, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`),
		chunkEvent(t, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":""}}`),
		chunkEvent(t, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`),
		// A delta for a block that never started is dropped.
		chunkEvent(t, `{"type":"content_block_delta","index":9,"delta":{"type":"input_json_delta","partial_json":"{\"x\":1}"}}`),
		chunkEvent(t, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Paris\"}"}}`),
		chunkEvent(t, `{"type":"content_block_stop","index":1}`),
		// A tool without parameters receives no input deltas.
		chunkEvent(t, `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_2","name":"now","input":{}}}`),
		chunkEvent(t, `{"type":"content_block_stop","index":2}`),
		chunkEvent(t, `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":9}}`),
		chunkEvent(t, `{"type":"message_stop","amazon-bedrock-invocationMetrics":{"inputTokenCount":12,"outputTokenCount":9}}`),
	))
	s.contentType = contentTypeEvents

	var chunks []string
	streamed, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{
		StreamingFunc: recordChunks(&chunks),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Checking"}, chunks, "only text deltas reach the streaming func")
	assert.Equal(t, expected, streamed.Choices)

	// The same message answered without streaming.
	s = newStub(t, `{"content":[{"type":"text","text":"Checking"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"city":"Paris"}},{"type":"tool_use","id":"toolu_2","name":"now","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":12,"output_tokens":9}}`)
	whole, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{})
	require.NoError(t, err)
	assert.Equal(t, expected, whole.Choices)
}

func TestCreateAnthropicCompletionStreamingErrors(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "hi"),
	}
	delta := chunkEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`)

	t.Run("streaming func error stops the stream", func(t *testing.T) {
		callbackErr := errors.New("client went away")
		s := newStub(t, eventStream(delta, delta))
		s.contentType = contentTypeEvents
		calls := 0
		res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{
			StreamingFunc: func(context.Context, []byte) error {
				calls++
				return callbackErr
			},
		})
		require.ErrorIs(t, err, callbackErr)
		assert.Nil(t, res)
		assert.Equal(t, 1, calls)
	})

	t.Run("invalid chunk json", func(t *testing.T) {
		s := newStub(t, eventStream(chunkEvent(t, invalidJSONBody)))
		s.contentType = contentTypeEvents
		var chunks []string
		res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{
			StreamingFunc: recordChunks(&chunks),
		})
		require.EqualError(t, err, "bedrock anthropic: decode stream chunk: invalid character 'n' looking for beginning of object key string")
		var syntaxErr *json.SyntaxError
		assert.ErrorAs(t, err, &syntaxErr)
		assert.Nil(t, res)
		assert.Empty(t, chunks)
	})

	t.Run("exception event before any chunk", func(t *testing.T) {
		s := newStub(t, eventStream(exceptionEvent("throttlingException", throttledMessage)))
		s.contentType = contentTypeEvents
		var chunks []string
		res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{
			StreamingFunc: recordChunks(&chunks),
		})
		var te *types.ThrottlingException
		require.ErrorAs(t, err, &te)
		assert.Equal(t, throttledMessage, te.ErrorMessage())
		assert.Nil(t, res)
		assert.Empty(t, chunks)
	})

	t.Run("exception event after delivered chunks", func(t *testing.T) {
		s := newStub(t, eventStream(delta, delta, exceptionEvent("throttlingException", throttledMessage)))
		s.contentType = contentTypeEvents
		calls := 0
		res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{
			StreamingFunc: func(context.Context, []byte) error {
				calls++
				// The SDK reader closes the body only after it has recorded the
				// exception, so the next received event observes stream.Err().
				select {
				case <-s.bodyClosed:
				case <-time.After(streamWaitTimeout):
					t.Error("event stream reader did not finish")
				}
				return nil
			},
		})
		var te *types.ThrottlingException
		require.ErrorAs(t, err, &te)
		assert.Equal(t, throttledMessage, te.ErrorMessage())
		assert.Nil(t, res)
		// The SDK decodes events on its own goroutine, so the failure may already
		// be visible when the first chunk arrives; it is never seen later than
		// the second one, which is dropped.
		assert.LessOrEqual(t, calls, 1)
	})

	t.Run("http error", func(t *testing.T) {
		s := newStub(t).fail()
		var chunks []string
		res, err := s.client().CreateCompletion(context.Background(), anthropicModelID, msgs, llms.CallOptions{
			StreamingFunc: recordChunks(&chunks),
		})
		requireValidationError(t, err)
		assert.Nil(t, res)
		assert.Empty(t, chunks)
	})
}
