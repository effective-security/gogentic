package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/invopop/jsonschema"
	orderedmap "github.com/pb33f/ordered-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	llmTestToken     = "test-token"
	llmTestModel     = "gpt-test"
	llmTestEmbedding = "embed-test"
	chatPath         = "/chat/completions"
	responsesPath    = "/responses"
	embeddingsPath   = "/embeddings"
	toolLookup       = "lookup"
	toolCallID       = "call_1"
	imageURL         = "https://example.test/cat.png"
)

// captureServer is an httptest server that records each request and serves
// one canned reply.
type captureServer struct {
	t *testing.T

	mu     sync.Mutex
	paths  []string
	header http.Header
	body   []byte

	status int
	reply  string
}

func newCaptureServer(t *testing.T, status int, reply string) (*captureServer, *httptest.Server) {
	t.Helper()
	s := &captureServer{
		t:      t,
		status: status,
		reply:  reply,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.header = r.Header.Clone()
		s.body = body
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, err = w.Write([]byte(s.reply))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

// requestPaths returns the paths requested so far.
func (s *captureServer) requestPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// lastJSON decodes the body of the last request.
func (s *captureServer) lastJSON() map[string]any {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out map[string]any
	require.NoError(s.t, json.Unmarshal(s.body, &out))
	return out
}

// lastHeader returns the headers of the last request.
func (s *captureServer) lastHeader() http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header
}

// newChatLLM builds an LLM on a provider that uses Chat Completions.
func newChatLLM(t *testing.T, baseURL string, opts ...Option) *LLM {
	t.Helper()
	opts = append([]Option{
		WithProvider(llms.ProviderPerplexity),
		WithToken(llmTestToken),
		WithModel(llmTestModel),
		WithBaseURL(baseURL),
	}, opts...)
	llm, err := New(opts...)
	require.NoError(t, err)
	require.False(t, llm.client.SupportsResponsesAPI())
	return llm
}

// newResponsesLLM builds an LLM on a provider that uses the Responses API.
func newResponsesLLM(t *testing.T, baseURL string, opts ...Option) *LLM {
	t.Helper()
	opts = append([]Option{
		WithProvider(llms.ProviderOpenAI),
		WithToken(llmTestToken),
		WithModel(llmTestModel),
		WithBaseURL(baseURL),
	}, opts...)
	llm, err := New(opts...)
	require.NoError(t, err)
	require.True(t, llm.client.SupportsResponsesAPI())
	return llm
}

func objectSchema(props ...orderedmap.Pair[string, *jsonschema.Schema]) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:       "object",
		Properties: orderedmap.New[string, *jsonschema.Schema](orderedmap.WithInitialData(props...)),
	}
}

func lookupFunctionTool(strict bool) llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        toolLookup,
			Description: "look things up",
			Parameters: objectSchema(orderedmap.Pair[string, *jsonschema.Schema]{
				Key: "q",
				Value: &jsonschema.Schema{
					Type: "string",
				},
			}),
			Strict: strict,
		},
	}
}

func toolCallPart(id, name, args string) llms.ToolCall {
	return llms.ToolCall{
		ID:   id,
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

func toolResponseMessage(id, content string) llms.Message {
	return llms.Message{
		Role: llms.RoleTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: id,
				Content:    content,
			},
		},
	}
}

func TestGenerateContentFromChat(t *testing.T) {
	t.Parallel()

	const reply = `{"id":"chatcmpl-1","choices":[` +
		`{"index":0,"message":{"role":"assistant","content":"first"},"finish_reason":"stop"},` +
		`{"index":1,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_9","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}` +
		`],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18,"completion_tokens_details":{"reasoning_tokens":3}}}`
	srvState, srv := newCaptureServer(t, http.StatusOK, reply)
	llm := newChatLLM(t, srv.URL)

	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "be brief"),
		llms.MessageFromTextParts(llms.RoleHuman, "hello"),
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				toolCallPart(toolCallID, toolLookup, `{"q":"y"}`),
			},
		},
		toolResponseMessage(toolCallID, "found y"),
		llms.MessageFromTextParts(llms.RoleGeneric, "and now?"),
	}
	resp, err := llm.GenerateContent(context.Background(), messages,
		llms.WithTools([]llms.Tool{lookupFunctionTool(false)}),
		llms.WithN(2),
		llms.WithSeed(5),
	)
	require.NoError(t, err)

	usage := llms.Usage{
		OutputTokens:    7,
		InputTokens:     11,
		TotalTokens:     18,
		ReasoningTokens: 3,
	}
	call := &llms.FunctionCall{
		Name:      toolLookup,
		Arguments: `{"q":"x"}`,
	}
	assert.Equal(t, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content:    "first",
				StopReason: "stop",
				Usage:      usage,
			},
			{
				StopReason: "tool_calls",
				Usage:      usage,
				FuncCall:   call,
				ToolCalls: []llms.ToolCall{
					{
						ID:           "call_9",
						Type:         "function",
						FunctionCall: call,
					},
				},
			},
		},
	}, resp)

	assert.Equal(t, []string{chatPath}, srvState.requestPaths())
	assert.Equal(t, llmTestToken, srvState.lastHeader().Get("api-key"), "perplexity authenticates with api-key")
	body := srvState.lastJSON()
	assert.Equal(t, llmTestModel, body["model"])
	assert.Equal(t, float64(2), body["n"])
	assert.Equal(t, float64(5), body["seed"])
	assert.Equal(t, []any{
		map[string]any{
			"role":    "system",
			"content": "be brief",
		},
		map[string]any{
			"role":    "user",
			"content": "hello",
		},
		map[string]any{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				map[string]any{
					"id":   toolCallID,
					"type": "function",
					"function": map[string]any{
						"name":      toolLookup,
						"arguments": `{"q":"y"}`,
					},
				},
			},
		},
		map[string]any{
			"role":         "tool",
			"content":      "found y",
			"tool_call_id": toolCallID,
		},
		map[string]any{
			"role":    "user",
			"content": "and now?",
		},
	}, body["messages"])
	assert.Equal(t, []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        toolLookup,
				"description": "look things up",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"q": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
	}, body["tools"])
}

func TestGenerateContentFromChat_Errors(t *testing.T) {
	t.Parallel()

	t.Run("invalid message", func(t *testing.T) {
		t.Parallel()
		srvState, srv := newCaptureServer(t, http.StatusOK, `{}`)
		llm := newChatLLM(t, srv.URL)
		_, err := llm.GenerateContent(context.Background(), []llms.Message{
			{Role: "robot"},
		})
		require.EqualError(t, err, "role robot not supported")
		assert.Empty(t, srvState.requestPaths(), "nothing is sent for an invalid request")
	})

	t.Run("upstream status", func(t *testing.T) {
		t.Parallel()
		_, srv := newCaptureServer(t, http.StatusBadRequest, `{"error":{"message":"bad model"}}`)
		llm := newChatLLM(t, srv.URL)
		_, err := llm.GenerateContent(context.Background(), []llms.Message{
			llms.MessageFromTextParts(llms.RoleHuman, "hi"),
		})
		require.EqualError(t, err, "API returned unexpected status code: 400: bad model")
	})

	t.Run("no choices", func(t *testing.T) {
		t.Parallel()
		_, srv := newCaptureServer(t, http.StatusOK, `{"choices":[]}`)
		llm := newChatLLM(t, srv.URL)
		_, err := llm.GenerateContent(context.Background(), []llms.Message{
			llms.MessageFromTextParts(llms.RoleHuman, "hi"),
		})
		require.ErrorIs(t, err, openaiclient.ErrEmptyResponse)
	})
}

func TestBuildChatRequestBody_MessageErrors(t *testing.T) {
	t.Parallel()

	llm := newChatLLM(t, "http://example.test")
	tests := []struct {
		name    string
		msg     llms.Message
		wantErr string
	}{
		{
			name: "tool message with two parts",
			msg: llms.Message{
				Role: llms.RoleTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{ToolCallID: "a"},
					llms.ToolCallResponse{ToolCallID: "b"},
				},
			},
			wantErr: "expected exactly one part for role tool, got 2",
		},
		{
			name:    "tool message with text",
			msg:     llms.MessageFromTextParts(llms.RoleTool, "oops"),
			wantErr: "expected part of type ToolCallResponse for role tool, got llms.TextContent",
		},
		{
			name:    "unknown role",
			msg:     llms.Message{Role: "robot"},
			wantErr: "role robot not supported",
		},
		{
			// F-038: used to panic on the nil FunctionCall.
			name: "tool call without function call",
			msg: llms.Message{
				Role: llms.RoleAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "ws_1",
						Type: "web_search",
					},
				},
			},
			wantErr: `unsupported tool call in history: tool call "ws_1" of type "web_search": tool call has no function call`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := llm.buildChatRequestBody([]llms.Message{tc.msg})
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestBuildChatRequestBody_HistoryToolCall(t *testing.T) {
	t.Parallel()

	llm := newChatLLM(t, "http://example.test")
	req, err := llm.buildChatRequestBody([]llms.Message{
		llms.MessageFromToolCalls(llms.RoleAI, llms.ToolCall{
			ID: "call_1",
			// An empty Type still sends the function call.
			FunctionCall: &llms.FunctionCall{
				Name:      toolLookup,
				Arguments: `{"q":"x"}`,
			},
		}),
	})
	require.NoError(t, err)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, []openaiclient.ToolCall{
		{
			ID:   "call_1",
			Type: openaiclient.ToolTypeFunction,
			Function: openaiclient.ToolFunction{
				Name:      toolLookup,
				Arguments: `{"q":"x"}`,
			},
		},
	}, req.Messages[0].ToolCalls)
}

func TestBuildChatRequestBody_Tools(t *testing.T) {
	t.Parallel()

	webSearch := llms.Tool{
		Type: "web_search",
		WebSearchOptions: &llms.WebSearchOptions{
			AllowedDomains: []string{"example.test"},
		},
	}
	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	}

	llm := newChatLLM(t, "http://example.test")
	req, err := llm.buildChatRequestBody(messages, llms.WithTools([]llms.Tool{webSearch, lookupFunctionTool(true)}))
	require.NoError(t, err)
	require.Len(t, req.Tools, 2)
	assert.Equal(t, openaiclient.Tool{
		Type: openaiclient.ToolTypeWebSearch,
		WebSearchOptions: &openaiclient.WebSearchOptions{
			AllowedDomains: []string{"example.test"},
		},
	}, req.Tools[0])
	assert.Equal(t, openaiclient.ToolTypeFunction, req.Tools[1].Type)
	require.NotNil(t, req.Tools[1].Function)
	assert.Equal(t, toolLookup, req.Tools[1].Function.Name)
	assert.Equal(t, "look things up", req.Tools[1].Function.Description)
	assert.True(t, req.Tools[1].Function.Strict)

	_, err = llm.buildChatRequestBody(messages, llms.WithTools([]llms.Tool{{Type: "code_interpreter"}}))
	require.EqualError(t, err, "failed to convert llms tool to openai tool: tool type code_interpreter not supported")

	// A function without parameters leaves "parameters" out instead of
	// sending null.
	req, err = llm.buildChatRequestBody(messages, llms.WithTools([]llms.Tool{{
		Type: llms.ToolTypeFunction,
		Function: &llms.FunctionDefinition{
			Name: "now",
		},
	}}))
	require.NoError(t, err)
	require.Len(t, req.Tools, 1)
	b, err := json.Marshal(req.Tools[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"function","function":{"name":"now"}}`, string(b))

	// F-022: a function tool without a definition used to panic.
	_, err = llm.buildChatRequestBody(messages, llms.WithTools([]llms.Tool{{Type: llms.ToolTypeFunction}}))
	require.ErrorIs(t, err, llms.ErrMissingFunctionDefinition)
	require.EqualError(t, err, `failed to convert llms tool to openai tool: tool of type "function": tool has no function definition`)
}

func TestToolFromTool_BedrockDropsWebSearchFilters(t *testing.T) {
	t.Parallel()

	llm := &LLM{providerType: llms.ProviderOpenAIBedrock}
	tool, err := llm.toolFromTool(llms.Tool{
		Type: "web_search",
		WebSearchOptions: &llms.WebSearchOptions{
			AllowedDomains: []string{"example.test"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, openaiclient.Tool{Type: openaiclient.ToolTypeWebSearch}, tool)

	llm = &LLM{providerType: llms.ProviderOpenAI}
	tool, err = llm.toolFromTool(llms.Tool{
		Type:             "web_search",
		WebSearchOptions: &llms.WebSearchOptions{},
	})
	require.NoError(t, err)
	assert.Equal(t, openaiclient.Tool{Type: openaiclient.ToolTypeWebSearch}, tool, "no domains means no filters")
}

func TestBuildChatRequestBody_ClientResponseFormatWins(t *testing.T) {
	t.Parallel()

	clientFormat := &schema.ResponseFormat{Type: "json_object"}
	callFormat := &schema.ResponseFormat{Type: "text"}
	llm := newChatLLM(t, "http://example.test", WithResponseFormat(clientFormat))

	req, err := llm.buildChatRequestBody([]llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	}, llms.WithResponseFormat(callFormat))
	require.NoError(t, err)
	assert.Same(t, clientFormat, req.ResponseFormat)
}

func TestGenerateContentFromResponses_ToolCallIDFallback(t *testing.T) {
	t.Parallel()

	const reply = `{"id":"resp_1","object":"response","status":"completed","output":[` +
		`{"id":"fc_1","type":"function_call","status":"completed","call_id":"","name":"lookup","arguments":"{}"},` +
		`{"id":"fc_2","type":"function_call","status":"completed","call_id":"call_2","name":"other","arguments":"{\"a\":1}"}` +
		`],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6,"input_tokens_details":{"cached_tokens":1},"output_tokens_details":{"reasoning_tokens":0}}}`
	_, srv := newCaptureServer(t, http.StatusOK, reply)
	llm := newResponsesLLM(t, srv.URL)

	resp, err := llm.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	})
	require.NoError(t, err)
	first := &llms.FunctionCall{
		Name:      toolLookup,
		Arguments: "{}",
	}
	assert.Equal(t, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Usage: llms.Usage{
					OutputTokens:    2,
					InputTokens:     4,
					CacheReadTokens: 1,
					TotalTokens:     6,
				},
				FuncCall: first,
				ToolCalls: []llms.ToolCall{
					{
						ID:           "fc_1",
						Type:         "function",
						FunctionCall: first,
					},
					{
						ID:   "call_2",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "other",
							Arguments: `{"a":1}`,
						},
					},
				},
			},
		},
	}, resp)
}

func TestGenerateContentFromResponses_Errors(t *testing.T) {
	t.Parallel()

	t.Run("invalid message", func(t *testing.T) {
		t.Parallel()
		srvState, srv := newCaptureServer(t, http.StatusOK, `{}`)
		llm := newResponsesLLM(t, srv.URL)
		_, err := llm.GenerateContent(context.Background(), []llms.Message{
			{Role: "robot"},
		})
		require.EqualError(t, err, "role robot not supported")
		assert.Empty(t, srvState.requestPaths())
	})

	t.Run("empty output", func(t *testing.T) {
		t.Parallel()
		_, srv := newCaptureServer(t, http.StatusOK, `{"id":"resp_1","object":"response","status":"completed","output":[]}`)
		llm := newResponsesLLM(t, srv.URL)
		_, err := llm.GenerateContent(context.Background(), []llms.Message{
			llms.MessageFromTextParts(llms.RoleHuman, "hi"),
		})
		require.ErrorIs(t, err, ErrEmptyResponse)
		assert.EqualError(t, err, "empty response from chat: no response")
	})
}

func TestBuildResponsesRequestBody_Messages(t *testing.T) {
	t.Parallel()

	llm := newResponsesLLM(t, "http://example.test")
	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "be brief"),
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextPart("look"),
				llms.ImageURLWithDetailPart(imageURL, "low"),
				llms.BinaryPart("text/plain", []byte("hi")),
			},
		},
		{Role: llms.RoleHuman},
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				llms.TextPart("calling"),
				toolCallPart(toolCallID, toolLookup, `{"q":"y"}`),
				llms.ToolCall{ID: "ignored"},
				llms.ImageURLPart(imageURL),
			},
		},
		toolResponseMessage(toolCallID, "found y"),
		{Role: llms.RoleGeneric},
	}
	req, err := llm.buildResponsesRequestBody(messages,
		llms.WithModel("req-model"),
		llms.WithMaxTokens(99),
		llms.WithMetadata(map[string]any{"run": 7}),
		llms.WithToolChoice("required"),
		llms.WithTools([]llms.Tool{lookupFunctionTool(true)}),
	)
	require.NoError(t, err)

	raw, err := json.Marshal(req)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))

	assert.Equal(t, "req-model", body["model"])
	assert.Equal(t, float64(99), body["max_output_tokens"])
	assert.Equal(t, map[string]any{"run": "7"}, body["metadata"])
	assert.Equal(t, "required", body["tool_choice"])
	assert.Equal(t, []any{
		map[string]any{
			"type":        "function",
			"name":        toolLookup,
			"description": "look things up",
			"strict":      true,
			"parameters": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"q": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}, body["tools"])

	input, ok := body["input"].([]any)
	require.True(t, ok, "input is a list of items")
	require.Len(t, input, 7)
	assert.Equal(t, map[string]any{
		"role": "system",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": "be brief",
			},
		},
	}, input[0])
	assert.Equal(t, map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": "look",
			},
			map[string]any{
				"type":      "input_image",
				"image_url": imageURL,
				"detail":    "low",
			},
			map[string]any{
				"type":      "input_file",
				"file_data": "data:text/plain;base64,aGk=",
			},
		},
	}, input[1])
	assert.Equal(t, map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": "",
			},
		},
	}, input[2], "a message without parts sends one empty text part")
	assert.Equal(t, map[string]any{
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []any{
			map[string]any{
				"type": "output_text",
				"text": "calling",
			},
		},
	}, input[3])
	assert.Equal(t, map[string]any{
		"type":      "function_call",
		"call_id":   toolCallID,
		"name":      toolLookup,
		"arguments": `{"q":"y"}`,
	}, input[4])
	assert.Equal(t, map[string]any{
		"type":    "function_call_output",
		"call_id": toolCallID,
		"output":  "found y",
	}, input[5])
	assert.Equal(t, map[string]any{
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []any{
			map[string]any{
				"type": "output_text",
				"text": "",
			},
		},
	}, input[6], "an assistant message without text sends one empty text part")
}

func TestBuildResponsesRequestBody_Errors(t *testing.T) {
	t.Parallel()

	llm := newResponsesLLM(t, "http://example.test")
	tests := []struct {
		name    string
		msgs    []llms.Message
		opts    []llms.CallOption
		wantErr string
	}{
		{
			name: "unsupported human part",
			msgs: []llms.Message{
				{
					Role: llms.RoleHuman,
					Parts: []llms.ContentPart{
						toolCallPart(toolCallID, toolLookup, "{}"),
					},
				},
			},
			wantErr: "unsupported content part type llms.ToolCall",
		},
		{
			name: "tool message with two parts",
			msgs: []llms.Message{
				{
					Role: llms.RoleTool,
					Parts: []llms.ContentPart{
						llms.ToolCallResponse{ToolCallID: "a"},
						llms.ToolCallResponse{ToolCallID: "b"},
					},
				},
			},
			wantErr: "expected exactly one part for role tool, got 2",
		},
		{
			name:    "tool message with text",
			msgs:    []llms.Message{llms.MessageFromTextParts(llms.RoleTool, "oops")},
			wantErr: "expected ToolCallResponse for tool role, got llms.TextContent",
		},
		{
			name:    "unknown role",
			msgs:    []llms.Message{{Role: "robot"}},
			wantErr: "role robot not supported",
		},
		{
			name: "unsupported tool",
			msgs: []llms.Message{llms.MessageFromTextParts(llms.RoleHuman, "hi")},
			opts: []llms.CallOption{
				llms.WithTools([]llms.Tool{{Type: "code_interpreter"}}),
			},
			wantErr: "failed to convert llms tool to openai tool: tool type code_interpreter not supported",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := llm.buildResponsesRequestBody(tc.msgs, tc.opts...)
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestBuildResponsesRequestBody_Defaults(t *testing.T) {
	t.Parallel()

	llm := &LLM{
		client: &openaiclient.Client{
			Provider: openaiclient.ProviderPerplexity,
		},
	}
	req, err := llm.buildResponsesRequestBody([]llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	}, llms.WithToolChoice(map[string]any{"type": "function"}))
	require.NoError(t, err)
	assert.Equal(t, openaiclient.DefaultChatModel, req.Model)
	assert.Equal(t, int64(openaiclient.DefaultMaxTokens), req.MaxOutputTokens.Value)
	assert.Nil(t, req.Metadata)
	assert.False(t, req.ToolChoice.OfToolChoiceMode.Valid(), "a non-string tool choice is not sent")
	assert.False(t, req.PromptCacheKey.Valid(), "prompt caching is not applied for this provider")
}

func TestBuildResponsesRequestBody_ResponseFormat(t *testing.T) {
	t.Parallel()

	clientFormat := &schema.ResponseFormat{
		Type: "json_schema",
		JSONSchema: &schema.ResponseFormatJSONSchema{
			Name:   "client",
			Strict: true,
			Schema: &schema.ResponseFormatJSONSchemaProperty{
				Type: "object",
			},
		},
	}
	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	}

	llm := newResponsesLLM(t, "http://example.test", WithResponseFormat(clientFormat))
	req, err := llm.buildResponsesRequestBody(messages)
	require.NoError(t, err)
	require.NotNil(t, req.Text.Format.OfJSONSchema)
	assert.Equal(t, "client", req.Text.Format.OfJSONSchema.Name)
	assert.True(t, req.Text.Format.OfJSONSchema.Strict.Value)
	assert.Equal(t, map[string]any{"type": "object"}, req.Text.Format.OfJSONSchema.Schema)

	// A call format replaces the client format.
	req, err = llm.buildResponsesRequestBody(messages, llms.WithResponseFormat(&schema.ResponseFormat{Type: "json_object"}))
	require.NoError(t, err)
	assert.Nil(t, req.Text.Format.OfJSONSchema)
	assert.NotNil(t, req.Text.Format.OfText)
}

func TestResponsesToolFromTool(t *testing.T) {
	t.Parallel()

	webSearch := llms.Tool{
		Type: "web_search",
		WebSearchOptions: &llms.WebSearchOptions{
			AllowedDomains: []string{"example.test"},
		},
	}

	openAI := &LLM{providerType: llms.ProviderOpenAI}
	tool, err := openAI.responsesToolFromTool(webSearch)
	require.NoError(t, err)
	require.NotNil(t, tool.OfWebSearch)
	assert.Equal(t, []string{"example.test"}, tool.OfWebSearch.Filters.AllowedDomains)

	bedrock := &LLM{providerType: llms.ProviderOpenAIBedrock}
	tool, err = bedrock.responsesToolFromTool(webSearch)
	require.NoError(t, err)
	require.NotNil(t, tool.OfWebSearch)
	assert.Nil(t, tool.OfWebSearch.Filters.AllowedDomains, "bedrock does not support domain filters")

	tool, err = openAI.responsesToolFromTool(llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name: "noargs",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, tool.OfFunction)
	assert.Equal(t, "noargs", tool.OfFunction.Name)
	assert.Nil(t, tool.OfFunction.Parameters)
	assert.False(t, tool.OfFunction.Strict.Value)
}

func TestResponsesToolFromTool_Errors(t *testing.T) {
	t.Parallel()

	llm := &LLM{providerType: llms.ProviderOpenAI}
	tests := []struct {
		name       string
		tool       llms.Tool
		wantErr    string
		wantPrefix string
	}{
		{
			name:    "function without definition",
			tool:    llms.Tool{Type: "function"},
			wantErr: `tool of type "function": tool has no function definition`,
		},
		{
			name: "parameters that cannot be marshaled",
			tool: llms.Tool{
				Type: "function",
				Function: &llms.FunctionDefinition{
					Name: "bad",
					Parameters: &jsonschema.Schema{
						Type:    "object",
						Default: func() {},
					},
				},
			},
			wantPrefix: "marshal function parameters: ",
		},
		{
			name: "boolean parameters schema",
			tool: llms.Tool{
				Type: "function",
				Function: &llms.FunctionDefinition{
					Name:       "bool",
					Parameters: jsonschema.TrueSchema,
				},
			},
			wantPrefix: "unmarshal function parameters: ",
		},
		{
			name:    "unsupported type",
			tool:    llms.Tool{Type: "code_interpreter"},
			wantErr: "tool type code_interpreter not supported",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := llm.responsesToolFromTool(tc.tool)
			require.Error(t, err)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
			}
			if tc.wantPrefix != "" {
				assert.True(t, strings.HasPrefix(err.Error(), tc.wantPrefix), err.Error())
			}
		})
	}
}

func TestToResponsesText(t *testing.T) {
	t.Parallel()

	got := toResponsesText(nil)
	assert.NotNil(t, got.Format.OfText)
	assert.Nil(t, got.Format.OfJSONSchema)

	got = toResponsesText(&schema.ResponseFormat{Type: "json_schema"})
	require.NotNil(t, got.Format.OfJSONSchema)
	assert.Equal(t, "response", got.Format.OfJSONSchema.Name, "an unnamed schema gets a default name")
	assert.False(t, got.Format.OfJSONSchema.Strict.Value)
	assert.Nil(t, got.Format.OfJSONSchema.Schema)

	got = toResponsesText(&schema.ResponseFormat{
		Type: "json_schema",
		JSONSchema: &schema.ResponseFormatJSONSchema{
			Strict: true,
			Schema: &schema.ResponseFormatJSONSchemaProperty{
				Type: "string",
			},
		},
	})
	require.NotNil(t, got.Format.OfJSONSchema)
	assert.Equal(t, "response", got.Format.OfJSONSchema.Name)
	assert.True(t, got.Format.OfJSONSchema.Strict.Value)
	assert.Equal(t, map[string]any{"type": "string"}, got.Format.OfJSONSchema.Schema)

	for _, typ := range []string{"json_object", "text", "anything"} {
		got = toResponsesText(&schema.ResponseFormat{Type: typ})
		assert.NotNil(t, got.Format.OfText, typ)
		assert.Nil(t, got.Format.OfJSONSchema, typ)
	}
}

func TestEnforceStrictSchema(t *testing.T) {
	t.Parallel()

	enforceStrictSchema(nil)

	m := map[string]any{
		"properties": map[string]any{
			"name": map[string]any{
				"type": "string",
			},
			"tags": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"k": map[string]any{
							"type": "string",
						},
					},
				},
			},
			"nested": map[string]any{
				"type": "object",
			},
			"raw": true,
		},
	}
	enforceStrictSchema(m)
	assert.Equal(t, map[string]any{
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{
				"type": "string",
			},
			"tags": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"k": map[string]any{
							"type": "string",
						},
					},
				},
			},
			"nested": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			"raw": true,
		},
	}, m)
}

func TestConvertMetadata(t *testing.T) {
	t.Parallel()

	assert.Nil(t, convertMetadata(nil))
	assert.Nil(t, convertMetadata(map[string]any{}))
	assert.Equal(t, map[string]string{
		"n":    "7",
		"ok":   "true",
		"name": "x",
	}, map[string]string(convertMetadata(map[string]any{
		"n":    7,
		"ok":   true,
		"name": "x",
	})))
}

func TestExtractToolParts(t *testing.T) {
	t.Parallel()

	call := toolCallPart(toolCallID, toolLookup, "{}")
	msg := &ChatMessage{
		MultiContent: []llms.ContentPart{
			llms.TextPart("t"),
			llms.ImageURLPart(imageURL),
			llms.BinaryPart("text/plain", []byte("b")),
			call,
			llms.ToolCallResponse{ToolCallID: "dropped"},
		},
	}
	content, calls := ExtractToolParts(msg)
	assert.Equal(t, []llms.ContentPart{
		llms.TextPart("t"),
		llms.ImageURLPart(imageURL),
		llms.BinaryPart("text/plain", []byte("b")),
	}, content)
	assert.Equal(t, []llms.ToolCall{call}, calls)
}

func TestCreateEmbedding(t *testing.T) {
	t.Parallel()

	const reply = `{"object":"list","data":[{"object":"embedding","embedding":[0.5,1],"index":0},{"object":"embedding","embedding":[2],"index":1}]}`
	srvState, srv := newCaptureServer(t, http.StatusOK, reply)
	llm := newResponsesLLM(t, srv.URL, WithEmbeddingModel(llmTestEmbedding))

	got, err := llm.CreateEmbedding(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{0.5, 1}, {2}}, got)
	assert.Equal(t, []string{embeddingsPath}, srvState.requestPaths())
	assert.Equal(t, map[string]any{
		"model": llmTestEmbedding,
		"input": []any{"a", "b"},
	}, srvState.lastJSON())
}

func TestCreateEmbedding_Errors(t *testing.T) {
	t.Parallel()

	t.Run("upstream", func(t *testing.T) {
		t.Parallel()
		_, srv := newCaptureServer(t, http.StatusUnauthorized, `{"error":{"message":"bad key"}}`)
		llm := newResponsesLLM(t, srv.URL)
		_, err := llm.CreateEmbedding(context.Background(), []string{"a"})
		require.EqualError(t, err, "failed to create openai embeddings: API returned unexpected status code: 401: bad key")
	})

	t.Run("no embeddings", func(t *testing.T) {
		t.Parallel()
		_, srv := newCaptureServer(t, http.StatusOK, `{"data":[]}`)
		llm := newResponsesLLM(t, srv.URL)
		_, err := llm.CreateEmbedding(context.Background(), []string{"a"})
		require.ErrorIs(t, err, openaiclient.ErrEmptyResponse)
	})

	t.Run("length mismatch", func(t *testing.T) {
		t.Parallel()
		_, srv := newCaptureServer(t, http.StatusOK, `{"data":[{"embedding":[1]}]}`)
		llm := newResponsesLLM(t, srv.URL)
		got, err := llm.CreateEmbedding(context.Background(), []string{"a", "b"})
		require.True(t, errors.Is(err, ErrUnexpectedResponseLength))
		assert.Equal(t, [][]float32{{1}}, got, "the partial result is returned with the error")
	})
}
