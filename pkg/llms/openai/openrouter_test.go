package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterRequest(t *testing.T) {
	t.Parallel()

	var (
		gotHeaders http.Header
		gotPath    string
		gotModel   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotPath = r.URL.Path

		var body struct {
			Model string `json:"model"`
		}
		err := json.NewDecoder(r.Body).Decode(&body)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotModel = body.Model

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write([]byte(`{"error":{"message":"test request complete","type":"invalid_request"}}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	headers := map[string]string{
		"HTTP-Referer":       "https://secdi.example",
		"X-OpenRouter-Title": "Secdi",
		"Authorization":      "Bearer must-not-win",
		"Content-Type":       "text/plain",
	}
	model, err := New(
		WithProvider(llms.ProviderOpenRouter),
		WithToken("openrouter-token"),
		WithModel("anthropic/claude-sonnet-4"),
		WithBaseURL(server.URL),
		WithHeaders(headers),
	)
	require.NoError(t, err)
	assert.Equal(t, llms.ProviderOpenRouter, model.GetProviderType())
	assert.Equal(t, "anthropic/claude-sonnet-4", model.GetName())
	assert.True(t, model.client.SupportsResponsesAPI())

	headers["X-OpenRouter-Title"] = "mutated"
	_, err = model.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hello"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test request complete")

	assert.Equal(t, "/responses", gotPath)
	assert.Equal(t, "anthropic/claude-sonnet-4", gotModel)
	assert.Equal(t, "Bearer openrouter-token", gotHeaders.Get("Authorization"))
	assert.Equal(t, "application/json", gotHeaders.Get("Content-Type"))
	assert.Equal(t, "https://secdi.example", gotHeaders.Get("HTTP-Referer"))
	assert.Equal(t, "Secdi", gotHeaders.Get("X-OpenRouter-Title"))
}

func TestOpenRouterMissingToken(t *testing.T) {
	t.Setenv(DefaultOpenRouterTokenEnvVarName, "")

	_, err := New(
		WithProvider(llms.ProviderOpenRouter),
		WithModel("vendor/model"),
	)
	require.ErrorIs(t, err, ErrMissingOpenRouterToken)
}

func TestOpenRouterResponsesCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		responseBody  string
		wantContent   string
		wantToolName  string
		wantInput     uint64
		wantOutput    uint64
		wantReasoning uint64
	}{
		{
			name:          "text and usage",
			responseBody:  `{"id":"resp_1","object":"response","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello from OpenRouter"}]}],"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}}}`,
			wantContent:   "hello from OpenRouter",
			wantInput:     7,
			wantOutput:    3,
			wantReasoning: 1,
		},
		{
			name:         "function tool call",
			responseBody: `{"id":"resp_2","object":"response","status":"completed","output":[{"id":"fc_1","type":"function_call","status":"completed","call_id":"call_1","name":"lookup_asset","arguments":"{\"id\":42}"}],"usage":{"input_tokens":5,"output_tokens":4,"total_tokens":9,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`,
			wantToolName: "lookup_asset",
			wantInput:    5,
			wantOutput:   4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(tt.responseBody))
				assert.NoError(t, err)
			}))
			defer server.Close()

			model, err := New(
				WithProvider(llms.ProviderOpenRouter),
				WithToken("openrouter-token"),
				WithModel("vendor/model"),
				WithBaseURL(server.URL),
			)
			require.NoError(t, err)

			response, err := model.GenerateContent(context.Background(), []llms.Message{
				llms.MessageFromTextParts(llms.RoleHuman, "hello"),
			})
			require.NoError(t, err)
			require.Len(t, response.Choices, 1)
			choice := response.Choices[0]
			assert.Equal(t, tt.wantContent, choice.Content)
			assert.Equal(t, tt.wantInput, choice.Usage.InputTokens)
			assert.Equal(t, tt.wantOutput, choice.Usage.OutputTokens)
			assert.Equal(t, tt.wantReasoning, choice.Usage.ReasoningTokens)
			if tt.wantToolName != "" {
				require.Len(t, choice.ToolCalls, 1)
				assert.Equal(t, tt.wantToolName, choice.ToolCalls[0].FunctionCall.Name)
			}
		})
	}
}

func TestOpenRouterResponseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       int
		headers      map[string]string
		responseBody string
		stream       bool
		wantErrors   []string
	}{
		{
			name:         "HTTP rate limit includes retry guidance",
			status:       http.StatusTooManyRequests,
			headers:      map[string]string{"Retry-After": "60"},
			responseBody: `{"error":{"code":429,"message":"rate limit exceeded","metadata":{"error_type":"rate_limit_exceeded"}}}`,
			wantErrors:   []string{"429", "rate limit exceeded", "retry after: 60"},
		},
		{
			name:         "HTTP 200 failed response is an error",
			status:       http.StatusOK,
			responseBody: `{"id":"resp_failed","object":"response","status":"failed","error":{"code":"server_error","message":"provider unavailable"},"output":[]}`,
			wantErrors:   []string{"server_error", "provider unavailable"},
		},
		{
			name:   "nested streaming response failure preserves message",
			status: http.StatusOK,
			responseBody: "event: response.failed\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failed\",\"object\":\"response\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"upstream disconnected\"},\"output\":[]}}\n\n",
			stream:     true,
			wantErrors: []string{"server_error", "upstream disconnected"},
		},
		{
			name:   "nested streaming error event preserves message",
			status: http.StatusOK,
			responseBody: "event: response.error\n" +
				"data: {\"type\":\"response.error\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"slow down\"}}\n\n",
			stream:     true,
			wantErrors: []string{"rate_limit_exceeded", "slow down"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.headers {
					w.Header().Set(key, value)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, err := w.Write([]byte(tt.responseBody))
				assert.NoError(t, err)
			}))
			defer server.Close()

			model, err := New(
				WithProvider(llms.ProviderOpenRouter),
				WithToken("openrouter-token"),
				WithModel("vendor/model"),
				WithBaseURL(server.URL),
			)
			require.NoError(t, err)

			var opts []llms.CallOption
			if tt.stream {
				opts = append(opts, llms.WithStreamingFunc(func(context.Context, []byte) error { return nil }))
			}
			_, err = model.GenerateContent(context.Background(), []llms.Message{
				llms.MessageFromTextParts(llms.RoleHuman, "hello"),
			}, opts...)
			require.Error(t, err)
			for _, want := range tt.wantErrors {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}
