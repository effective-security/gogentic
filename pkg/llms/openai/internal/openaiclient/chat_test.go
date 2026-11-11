package openaiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/invopop/jsonschema"
	orderedmap "github.com/pb33f/ordered-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStreamingChatResponse_FinishReason(t *testing.T) {
	t.Parallel()
	mockBody := `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`
	r := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(mockBody)),
	}

	req := &ChatRequest{
		StreamingFunc: func(_ context.Context, _ []byte) error {
			return nil
		},
	}

	resp, err := parseStreamingChatResponse(context.Background(), r, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, FinishReason("stop"), resp.Choices[0].FinishReason)
}

func TestParseStreamingChatResponse_ReasoningContent(t *testing.T) {
	t.Parallel()
	mockBody := `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"final answer","reasoning_content":"step-by-step reasoning"},"finish_reason":"stop"}]}`
	r := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(mockBody)),
	}

	req := &ChatRequest{
		StreamingFunc: func(_ context.Context, _ []byte) error {
			return nil
		},
	}

	resp, err := parseStreamingChatResponse(context.Background(), r, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "final answer", resp.Choices[0].Message.Content)
	assert.Equal(t, "step-by-step reasoning", resp.Choices[0].Message.ReasoningContent)
	assert.Equal(t, FinishReason("stop"), resp.Choices[0].FinishReason)
}

func TestParseStreamingChatResponse_ReasoningFunc(t *testing.T) {
	t.Parallel()
	mockBody := `
data: {"id":"fa7e4fc5-a05d-4e7b-9a66-a2dd89e91a4e","object":"chat.completion.chunk","created":1738492867,"model":"deepseek-reasoner","system_fingerprint":"fp_7e73fd9a08","choices":[{"index":0,"delta":{"content":null,"reasoning_content":"Okay"},"logprobs":null,"finish_reason":null}]}
`
	r := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(mockBody)),
	}

	req := &ChatRequest{
		StreamingReasoningFunc: func(_ context.Context, reasoningChunk, chunk []byte) error {
			t.Logf("reasoningChunk: %s", string(reasoningChunk))
			t.Logf("chunk: %s", string(chunk))
			return nil
		},
	}

	resp, err := parseStreamingChatResponse(context.Background(), r, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "", resp.Choices[0].Message.Content)
	assert.Equal(t, "Okay", resp.Choices[0].Message.ReasoningContent)
	assert.Equal(t, FinishReason(""), resp.Choices[0].FinishReason)
}

func TestChatMessage_MarshalUnmarshal(t *testing.T) {
	t.Parallel()
	msg := ChatMessage{
		Role:    "assistant",
		Content: "hello",
		ToolCalls: []ToolCall{
			{
				ID:   "1",
				Type: "function",
				Function: ToolFunction{
					Name:      "test",
					Arguments: "func",
				},
			},
		},
	}
	text, err := json.Marshal(msg)
	require.NoError(t, err)
	require.Equal(t, `{"role":"assistant","content":"hello","tool_calls":[{"id":"1","type":"function","function":{"name":"test","arguments":"func"}}]}`, string(text)) // nolint: lll

	var msg2 ChatMessage
	err = json.Unmarshal(text, &msg2)
	require.NoError(t, err)
	require.Equal(t, msg, msg2)
}

func TestChatMessage_MarshalUnmarshal_WithReasoning(t *testing.T) {
	t.Parallel()
	msg := ChatMessage{
		Role:             "assistant",
		Content:          "final answer",
		ReasoningContent: "step-by-step reasoning",
	}
	text, err := json.Marshal(msg)
	require.NoError(t, err)
	require.Equal(t, `{"role":"assistant","content":"final answer","reasoning_content":"step-by-step reasoning"}`, string(text))

	var msg2 ChatMessage
	err = json.Unmarshal(text, &msg2)
	require.NoError(t, err)
	require.Equal(t, msg, msg2)
}

func TestChatRequest_Marshal_WithPromptCacheFields(t *testing.T) {
	t.Parallel()

	req := ChatRequest{
		Model:                "gpt-5-mini",
		PromptCacheKey:       "cache-key",
		PromptCacheRetention: "in_memory",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	assert.Contains(t, string(data), `"prompt_cache_key":"cache-key"`)
	assert.Contains(t, string(data), `"prompt_cache_retention":"in_memory"`)
}

func TestTool_MarshalUnmarshal(t *testing.T) {
	t.Parallel()
	tool1 := Tool{
		Type: ToolTypeWebSearch,
		WebSearchOptions: &WebSearchOptions{
			AllowedDomains: []string{"example.com"},
		},
	}
	text, err := json.Marshal(tool1)
	require.NoError(t, err)
	require.Equal(t, `{"filters":{"allowed_domains":["example.com"]},"type":"web_search"}`, string(text))

	tool2 := Tool{
		Type: ToolTypeFunction,
		Function: &FunctionDefinition{
			Name:        "test",
			Description: "test",
		},
	}
	text, err = json.Marshal(tool2)
	require.NoError(t, err)
	// No parameters: the key is left out rather than sent as null.
	require.Equal(t, `{"type":"function","function":{"name":"test","description":"test"}}`, string(text))

	tool3 := Tool{
		Type: ToolTypeFunction,
		Function: &FunctionDefinition{
			Name:        "test",
			Description: "test",
			Parameters: &jsonschema.Schema{
				Type: "object",
				Properties: orderedmap.New[string, *jsonschema.Schema](
					orderedmap.WithInitialData(
						orderedmap.Pair[string, *jsonschema.Schema]{
							Key: "name",
							Value: &jsonschema.Schema{
								Type: "string",
							},
						},
					),
				),
				Required: []string{"name"},
			},
		},
	}
	text, err = json.Marshal(tool3)
	require.NoError(t, err)
	require.Equal(t, `{"type":"function","function":{"name":"test","description":"test","parameters":{"properties":{"name":{"type":"string"}},"type":"object","required":["name"]}}}`, string(text))
}

// sseChat joins chat completion stream chunks into an SSE body.
func sseChat(chunks ...string) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString("data: ")
		sb.WriteString(c)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func TestCreateChat_UpstreamStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		reply   string
		wantErr string
	}{
		{
			name:    "error message",
			status:  http.StatusBadRequest,
			reply:   `{"error":{"message":"bad request","type":"invalid_request_error"}}`,
			wantErr: "API returned unexpected status code: 400: bad request",
		},
		{
			name:    "body is not JSON",
			status:  http.StatusInternalServerError,
			reply:   `oops`,
			wantErr: "API returned unexpected status code: 500",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, tc.status, tc.reply)
			c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
			_, err := c.CreateChat(context.Background(), &ChatRequest{})
			require.EqualError(t, err, tc.wantErr)
			assert.Equal(t, 1, stub.count(), "the hand-rolled chat client does not retry")
		})
	}
}

func TestCreateChat_RequestErrors(t *testing.T) {
	t.Parallel()

	t.Run("message with both content kinds", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
		_, err := c.CreateChat(context.Background(), &ChatRequest{
			Messages: []*ChatMessage{
				{
					Role:         "user",
					Content:      "text",
					MultiContent: []llms.ContentPart{llms.TextPart("part")},
				},
			},
		})
		require.ErrorIs(t, err, ErrContentExclusive)
	})

	t.Run("invalid base URL", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, invalidBaseURL, http.DefaultClient)
		_, err := c.CreateChat(context.Background(), &ChatRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid character \" \" in host name")
	})

	t.Run("undecodable body", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, `{"choices":`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateChat(context.Background(), &ChatRequest{})
		require.EqualError(t, err, "unexpected EOF")
	})
}

func TestCreateChat_AzureURL(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, chatHelloReply)
	c, err := New(ProviderAzure, testModel, testToken, srv.URL, "", "", testAzureOldAPI, http.DefaultClient, testEmbeddingModel, nil, nil)
	require.NoError(t, err)
	require.False(t, c.SupportsResponsesAPI())

	_, err = c.CreateChat(context.Background(), &ChatRequest{})
	require.NoError(t, err)
	got := stub.last()
	assert.Equal(t, "/openai/deployments/"+testModel+chatCompletionsURLPath, got.path)
	assert.Equal(t, "api-version="+testAzureOldAPI, got.query)
	assert.Equal(t, bearerTestToken, got.header.Get(headerAuthorization))
}

func TestCreateChat_Streaming(t *testing.T) {
	t.Parallel()

	stream := sseChat(
		`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel","reasoning_content":"think "}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"lo","reasoning_content":"more"}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12,"completion_tokens_details":{"reasoning_tokens":2}}}`,
		`[DONE]`,
	)
	stub, srv := newStubServer(t, http.StatusOK, stream)
	c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

	var (
		chunks          []string
		reasoningChunks []string
		reasoningBodies []string
	)
	resp, err := c.CreateChat(context.Background(), &ChatRequest{
		StreamingFunc: func(_ context.Context, chunk []byte) error {
			chunks = append(chunks, string(chunk))
			return nil
		},
		StreamingReasoningFunc: func(_ context.Context, reasoningChunk, chunk []byte) error {
			reasoningChunks = append(reasoningChunks, string(reasoningChunk))
			reasoningBodies = append(reasoningBodies, string(chunk))
			return nil
		},
	})
	require.NoError(t, err)

	body := stub.lastJSON()
	assert.Equal(t, true, body["stream"])
	assert.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])

	require.Len(t, resp.Choices, 1)
	choice := resp.Choices[0]
	assert.Equal(t, "Hello", choice.Message.Content)
	assert.Equal(t, "think more", choice.Message.ReasoningContent)
	assert.Equal(t, FinishReasonToolCalls, choice.FinishReason)
	assert.Equal(t, []ToolCall{
		{
			ID:   "call_1",
			Type: ToolTypeFunction,
			Function: ToolFunction{
				Name:      "lookup",
				Arguments: `{"q":"x"}`,
			},
		},
	}, choice.Message.ToolCalls)
	assert.Equal(t, 5, resp.Usage.PromptTokens)
	assert.Equal(t, 7, resp.Usage.CompletionTokens)
	assert.Equal(t, 12, resp.Usage.TotalTokens)
	assert.Equal(t, 2, resp.Usage.CompletionTokensDetails.ReasoningTokens)

	wantChunks := []string{
		"Hel",
		"lo",
		`[{"type":"function","function":{"name":"lookup","arguments":"{\"q\":"},"id":"call_1"}]`,
		`[{"type":"","function":{"name":"","arguments":"\"x\"}"}}]`,
	}
	require.Len(t, chunks, len(wantChunks))
	for i := range wantChunks {
		assert.JSONEq(t, jsonOrString(wantChunks[i]), jsonOrString(chunks[i]), "chunk %d", i)
	}
	assert.Equal(t, chunks, reasoningBodies)
	assert.Equal(t, []string{"think ", "more", "", ""}, reasoningChunks)
}

// jsonOrString returns s when it is JSON, and s quoted as a JSON string
// otherwise, so plain text and JSON chunks compare with assert.JSONEq.
func jsonOrString(s string) string {
	if json.Valid([]byte(s)) {
		return s
	}
	quoted, _ := json.Marshal(s)
	return string(quoted)
}

func TestCreateChat_StreamingCallbackErrors(t *testing.T) {
	t.Parallel()

	errStop := errors.New("stop now")
	stream := sseChat(`{"choices":[{"index":0,"delta":{"content":"a"}}]}`)

	t.Run("streaming func", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, stream)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateChat(context.Background(), &ChatRequest{
			StreamingFunc: func(context.Context, []byte) error { return errStop },
		})
		require.ErrorIs(t, err, errStop)
		assert.EqualError(t, err, "streaming func returned an error: stop now")
	})

	t.Run("streaming reasoning func", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, stream)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateChat(context.Background(), &ChatRequest{
			StreamingReasoningFunc: func(context.Context, []byte, []byte) error { return errStop },
		})
		require.ErrorIs(t, err, errStop)
		assert.EqualError(t, err, "streaming reasoning func returned an error: stop now")
	})
}

func TestParseStreamingChatResponse_Errors(t *testing.T) {
	t.Parallel()

	noop := &ChatRequest{
		StreamingFunc: func(context.Context, []byte) error { return nil },
	}

	t.Run("undecodable chunk", func(t *testing.T) {
		t.Parallel()
		r := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("data: {bad json\n")),
		}
		_, err := parseStreamingChatResponse(context.Background(), r, noop)
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "error decoding streaming response: "), err.Error())
	})

	t.Run("body read failure", func(t *testing.T) {
		t.Parallel()
		r := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(errReader{}),
		}
		_, err := parseStreamingChatResponse(context.Background(), r, noop)
		require.ErrorIs(t, err, errTransport)
		assert.EqualError(t, err, "error reading streaming response: "+errTransport.Error())
	})
}

func TestUpdateToolCalls(t *testing.T) {
	t.Parallel()

	impls := map[string]func([]ToolCall, []*ToolCall) ([]byte, []ToolCall){
		"updateToolCalls":            updateToolCalls,
		"StreamingChatResponseTools": StreamingChatResponseTools,
	}
	for name, update := range impls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			existing := []ToolCall{
				{
					ID:   "call_1",
					Type: ToolTypeFunction,
					Function: ToolFunction{
						Name:      "lookup",
						Arguments: "{",
					},
				},
			}

			chunk, got := update(existing, nil)
			assert.Equal(t, []byte{}, chunk)
			assert.Equal(t, existing, got)

			// An argument fragment with no tool call to extend is dropped.
			orphan := []*ToolCall{
				{Function: ToolFunction{Arguments: "lost"}},
			}
			chunk, got = update(nil, orphan)
			assert.Empty(t, got)
			assert.JSONEq(t, `[{"type":"","function":{"name":"","arguments":"lost"}}]`, string(chunk))

			delta := []*ToolCall{
				{Function: ToolFunction{Arguments: "}"}},
				{
					ID:   "call_2",
					Type: ToolTypeFunction,
					Function: ToolFunction{
						Name: "second",
					},
				},
			}
			chunk, got = update(existing, delta)
			assert.Equal(t, []ToolCall{
				{
					ID:   "call_1",
					Type: ToolTypeFunction,
					Function: ToolFunction{
						Name:      "lookup",
						Arguments: "{}",
					},
				},
				{
					ID:   "call_2",
					Type: ToolTypeFunction,
					Function: ToolFunction{
						Name: "second",
					},
				},
			}, got)
			want, err := json.Marshal(delta)
			require.NoError(t, err)
			assert.Equal(t, want, chunk)
		})
	}
}

func TestFinishReason_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   FinishReason
		want string
	}{
		{
			in:   "",
			want: "null",
		},
		{
			in:   FinishReasonNull,
			want: "null",
		},
		{
			in:   FinishReasonStop,
			want: `"stop"`,
		},
		{
			in:   FinishReasonContentFilter,
			want: `"content_filter"`,
		},
	}
	for _, tc := range tests {
		got, err := json.Marshal(tc.in)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(got), "finish reason %q", tc.in)
	}
}

func TestChatMessage_MarshalMultiContent(t *testing.T) {
	t.Parallel()

	single := ChatMessage{
		Role:         "user",
		MultiContent: []llms.ContentPart{llms.TextPart("only")},
	}
	text, err := json.Marshal(single)
	require.NoError(t, err)
	assert.JSONEq(t, `{"role":"user","content":"only"}`, string(text), "a single text part is sent as plain content")

	multi := ChatMessage{
		Role: "user",
		Name: "alice",
		MultiContent: []llms.ContentPart{
			llms.TextPart("look"),
			llms.ImageURLPart("https://example.test/a.png"),
		},
	}
	text, err = json.Marshal(multi)
	require.NoError(t, err)
	assert.JSONEq(t, `{"role":"user","name":"alice","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}`, string(text))
}

func TestChatMessage_UnmarshalInvalid(t *testing.T) {
	t.Parallel()

	var msg ChatMessage
	err := json.Unmarshal([]byte(`{"role":7}`), &msg)
	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "role", typeErr.Field)
}

func TestTool_MarshalWebSearchWithoutOptions(t *testing.T) {
	t.Parallel()

	text, err := json.Marshal(Tool{Type: ToolTypeWebSearch})
	require.NoError(t, err)
	assert.Equal(t, `{"type":"web_search"}`, string(text))

	text, err = json.Marshal(Tool{
		Type:             ToolTypeWebSearch,
		WebSearchOptions: &WebSearchOptions{},
	})
	require.NoError(t, err)
	assert.Equal(t, `{"type":"web_search"}`, string(text), "empty allowed domains add no filters")
}
