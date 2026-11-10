package openaiclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
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

// sseEvent formats one Responses API server-sent event.
func sseEvent(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

// unmarshalableParams returns request params whose JSON encoding fails: a
// function value cannot be marshaled.
func unmarshalableParams() *responses.ResponseNewParams {
	return &responses.ResponseNewParams{
		Model: testModel,
		Tools: []responses.ToolUnionParam{
			{
				OfFunction: &responses.FunctionToolParam{
					Name: "broken",
					Parameters: map[string]any{
						"bad": func() {},
					},
				},
			},
		},
	}
}

func TestUnexpectedResponsesStatusError(t *testing.T) {
	t.Parallel()

	const requestURL = "https://example.test/v1/responses"
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name:   "not found names the URL",
			status: http.StatusNotFound,
			body:   `{"error":{"message":"no such route"}}`,
			want:   "API returned unexpected status code: 404: url: " + requestURL + ": no such route",
		},
		{
			name:   "body is not JSON",
			status: http.StatusBadGateway,
			body:   `upstream down`,
			want:   "API returned unexpected status code: 502",
		},
		{
			name:   "error without message",
			status: http.StatusBadRequest,
			body:   `{"error":{"type":"invalid_request_error"}}`,
			want:   "API returned unexpected status code: 400",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := &http.Response{
				StatusCode: tc.status,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}
			assert.EqualError(t, unexpectedResponsesStatusError(resp, requestURL), tc.want)
		})
	}
}

func TestFailedResponseError(t *testing.T) {
	t.Parallel()

	failed := func(code, message string) *responses.Response {
		return &responses.Response{
			Status: responses.ResponseStatusFailed,
			Error: responses.ResponseError{
				Code:    responses.ResponseErrorCode(code),
				Message: message,
			},
		}
	}
	tests := []struct {
		name string
		resp *responses.Response
		want string
	}{
		{
			name: "code and message",
			resp: failed("server_error", "boom"),
			want: "responses API failed (server_error): boom",
		},
		{
			name: "message only",
			resp: failed("", "boom"),
			want: "responses API failed: boom",
		},
		{
			name: "code only",
			resp: failed("server_error", ""),
			want: "responses API failed (server_error)",
		},
		{
			name: "neither",
			resp: failed("", ""),
			want: "responses API failed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.EqualError(t, failedResponseError(tc.resp), tc.want)
		})
	}

	assert.NoError(t, failedResponseError(nil))
	assert.NoError(t, failedResponseError(&responses.Response{Status: responses.ResponseStatusCompleted}))
}

func TestCreateResponse_Errors(t *testing.T) {
	t.Parallel()

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
		_, err := c.CreateResponse(context.Background(), unmarshalableParams())
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "marshal payload: "), err.Error())
	})

	t.Run("invalid base URL", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, invalidBaseURL, http.DefaultClient)
		_, err := c.CreateResponse(context.Background(), &responses.ResponseNewParams{})
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "create request: "), err.Error())
	})

	t.Run("transport", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
		_, err := c.CreateResponse(context.Background(), &responses.ResponseNewParams{})
		require.ErrorIs(t, err, errTransport)
		assert.EqualError(t, err, "send request: "+errTransport.Error())
	})

	t.Run("read body", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", brokenBodyDoer())
		_, err := c.CreateResponse(context.Background(), &responses.ResponseNewParams{})
		require.ErrorIs(t, err, errTransport)
		assert.EqualError(t, err, "read body: "+errTransport.Error())
	})

	t.Run("undecodable body", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, `{"id":`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateResponse(context.Background(), &responses.ResponseNewParams{})
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "decode response: "), err.Error())
	})

	t.Run("upstream status", func(t *testing.T) {
		t.Parallel()
		stub, srv := newStubServer(t, http.StatusNotFound, `{"error":{"message":"missing"}}`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateResponse(context.Background(), &responses.ResponseNewParams{})
		require.EqualError(t, err, "API returned unexpected status code: 404: url: "+srv.URL+responsesURLPath+": missing")
		assert.Equal(t, 1, stub.count())
	})
}

func TestCreateResponse_AzureURL(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, responsesHelloReply)
	c, err := New(ProviderAzure, testModel, testToken, srv.URL, "", "", testAPIVersion, http.DefaultClient, testEmbeddingModel, nil, nil)
	require.NoError(t, err)
	require.True(t, c.SupportsResponsesAPI())

	_, err = c.CreateResponse(context.Background(), &responses.ResponseNewParams{})
	require.NoError(t, err)
	got := stub.last()
	assert.Equal(t, "/openai/responses", got.path)
	assert.Equal(t, "api-version="+testAPIVersion, got.query)
	assert.Equal(t, testModel, stub.lastJSON()["model"])
}

func TestCreateStreamingResponse_Errors(t *testing.T) {
	t.Parallel()

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
		_, err := c.CreateStreamingResponse(context.Background(), unmarshalableParams(), nil)
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "marshal payload: "), err.Error())
	})

	t.Run("invalid base URL", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, invalidBaseURL, http.DefaultClient)
		_, err := c.CreateStreamingResponse(context.Background(), &responses.ResponseNewParams{}, nil)
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "create request: "), err.Error())
	})

	t.Run("transport", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
		_, err := c.CreateStreamingResponse(context.Background(), &responses.ResponseNewParams{}, nil)
		require.ErrorIs(t, err, errTransport)
		assert.EqualError(t, err, "send request: "+errTransport.Error())
	})

	t.Run("upstream status", func(t *testing.T) {
		t.Parallel()
		stub, srv := newStubServer(t, http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)
		stub.setHeaders(map[string]string{"Retry-After": "5"})
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateStreamingResponse(context.Background(), &responses.ResponseNewParams{}, nil)
		require.EqualError(t, err, "API returned unexpected status code: 429; retry after: 5: slow down")
	})
}

func TestParseStreamingResponses_Deltas(t *testing.T) {
	t.Parallel()

	stream := sseEvent("response.created", `{"type":"response.created"}`) +
		sseEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hel"}`) +
		// A malformed delta is skipped, not fatal.
		sseEvent("response.output_text.delta", `{bad`) +
		// An empty delta is not forwarded.
		sseEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":""}`) +
		"event: response.output_text.delta\ndata:\n\n" +
		sseEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":"lo"}`) +
		sseEvent("response.completed", `{"type":"response.completed","response":`+responsesHelloReply+`}`) +
		"data: [DONE]\n\n"

	var chunks []string
	resp, err := parseStreamingResponses(context.Background(), strings.NewReader(stream), func(_ context.Context, chunk []byte) error {
		chunks = append(chunks, string(chunk))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Hel", "lo"}, chunks)
	assert.Equal(t, "resp_1", resp.ID)
	assert.Equal(t, "hello", resp.OutputText())

	// Without a callback the deltas are read and dropped.
	resp, err = parseStreamingResponses(context.Background(), strings.NewReader(stream), nil)
	require.NoError(t, err)
	assert.Equal(t, "resp_1", resp.ID)
}

func TestParseStreamingResponses_Errors(t *testing.T) {
	t.Parallel()

	errStop := errors.New("stop now")
	tests := []struct {
		name       string
		stream     string
		streamFunc func(context.Context, []byte) error
		wantErr    string
		wantPrefix string
		wantIs     error
	}{
		{
			name:       "streaming func",
			stream:     sseEvent("response.output_text.delta", `{"delta":"a"}`),
			streamFunc: func(context.Context, []byte) error { return errStop },
			wantErr:    "streaming func error: stop now",
			wantIs:     errStop,
		},
		{
			name:       "undecodable completed event",
			stream:     sseEvent("response.completed", `{bad`),
			wantPrefix: "unmarshal response.completed event: ",
		},
		{
			name:       "undecodable failed event",
			stream:     sseEvent("response.failed", `{bad`),
			wantPrefix: "unmarshal response.failed event: ",
		},
		{
			name:    "failed event without a failed status",
			stream:  sseEvent("response.failed", `{"type":"response.failed","response":{"id":"r","status":"in_progress"}}`),
			wantErr: "openai responses API streaming error: responses API failed",
		},
		{
			name:       "undecodable error event",
			stream:     sseEvent("error", `{bad`),
			wantPrefix: "unmarshal responses error event: ",
		},
		{
			name:    "top-level error code and message",
			stream:  sseEvent("error", `{"type":"error","code":"invalid_prompt","message":"nope"}`),
			wantErr: "openai responses API streaming error (invalid_prompt): nope",
		},
		{
			name:    "error without code",
			stream:  sseEvent("error", `{"type":"error","message":"nope"}`),
			wantErr: "openai responses API streaming error: nope",
		},
		{
			name:    "error without code or message",
			stream:  sseEvent("response.error", `{"type":"response.error"}`),
			wantErr: "openai responses API streaming error: streaming response failed",
		},
		{
			name:    "stream ends without completed event",
			stream:  sseEvent("response.output_text.delta", `{"delta":"a"}`),
			wantErr: "streaming response ended without response.completed event",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseStreamingResponses(context.Background(), strings.NewReader(tc.stream), tc.streamFunc)
			require.Error(t, err)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
			}
			if tc.wantPrefix != "" {
				assert.True(t, strings.HasPrefix(err.Error(), tc.wantPrefix), err.Error())
			}
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs)
			}
		})
	}
}

func TestParseStreamingResponses_ReadError(t *testing.T) {
	t.Parallel()

	_, err := parseStreamingResponses(context.Background(), errReader{}, nil)
	require.ErrorIs(t, err, errTransport)
	assert.EqualError(t, err, "read SSE stream: "+errTransport.Error())
}
