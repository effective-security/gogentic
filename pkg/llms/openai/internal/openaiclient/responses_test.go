package openaiclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateResponse_OpenRouterHTTP200FailedStatus(t *testing.T) {
	t.Parallel()

	// OpenRouter may return HTTP 200 after inference starts but mark the
	// Responses payload itself as failed. This must surface the provider error,
	// not fall through as an "empty response" success.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"id":"resp_failed",
			"object":"response",
			"status":"failed",
			"error":{"code":"server_error","message":"provider unavailable"},
			"output":[]
		}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	client, err := New(
		ProviderOpenRouter,
		"vendor/model",
		"openrouter-token",
		server.URL,
		"",
		"",
		"",
		http.DefaultClient,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)

	_, err = client.CreateResponse(context.Background(), &responses.ResponseNewParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server_error")
	assert.Contains(t, err.Error(), "provider unavailable")
}

func TestParseStreamingResponses_OpenRouterNestedFailedEvent(t *testing.T) {
	t.Parallel()

	// A response.failed event nests the error under response.error. Looking only
	// for a top-level error loses the actionable provider code and message.
	stream := strings.NewReader("event: response.failed\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failed\",\"object\":\"response\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"upstream disconnected\"},\"output\":[]}}\n\n")

	_, err := parseStreamingResponses(context.Background(), stream, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server_error")
	assert.Contains(t, err.Error(), "upstream disconnected")
}

func TestParseStreamingResponses_OpenRouterNestedErrorEvent(t *testing.T) {
	t.Parallel()

	// OpenRouter also emits response.error with a nested error object. The
	// Responses parser accepts this in addition to OpenAI's direct error event.
	stream := strings.NewReader("event: response.error\n" +
		"data: {\"type\":\"response.error\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"slow down\"}}\n\n")

	_, err := parseStreamingResponses(context.Background(), stream, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit_exceeded")
	assert.Contains(t, err.Error(), "slow down")
}

func TestUnexpectedResponsesStatusError_OpenRouterRetryAfter(t *testing.T) {
	t.Parallel()

	// OpenRouter documents Retry-After on 429 and 503 responses. Preserve it in
	// the error so callers and logs retain the server-provided retry guidance.
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"60"}},
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"code":429,"message":"rate limit exceeded"}}`,
		)),
	}

	err := unexpectedResponsesStatusError(resp, "https://openrouter.ai/api/v1/responses")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Contains(t, err.Error(), "retry after: 60")
}
