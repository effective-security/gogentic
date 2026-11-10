package openaiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireInferenceError asserts err is an llms.InferenceError with the given
// status and message.
func requireInferenceError(t *testing.T, err error, status int, msg string) {
	t.Helper()
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, status, ie.Status)
	assert.EqualError(t, err, msg)
}

func TestInferenceStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, http.StatusBadGateway, inferenceStatus(errTransport))
	assert.Equal(t, http.StatusBadGateway, inferenceStatus(nil))
}

func TestInferChat_TransportFailure(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
	_, err := c.InferChat(context.Background(), map[string]any{
		"model": testModel,
	})
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)
	assert.ErrorIs(t, err, errTransport)
}

func TestInferChat_Decode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		reply string
		want  string
	}{
		{
			name:  "no choices",
			reply: `{"id":"c","choices":[]}`,
			want:  "decode openai chat inference: expected one choice, got 0",
		},
		{
			name:  "two choices",
			reply: `{"id":"c","choices":[{"finish_reason":"stop","message":{"content":"a"}},{"finish_reason":"stop","message":{"content":"b"}}]}`,
			want:  "decode openai chat inference: expected one choice, got 2",
		},
		{
			name:  "unknown finish reason",
			reply: `{"id":"c","choices":[{"finish_reason":"paused","message":{"content":"a"}}]}`,
			want:  `decode openai chat inference: unsupported finish reason "paused"`,
		},
		{
			name:  "custom tool call",
			reply: `{"id":"c","choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"t","type":"custom","function":{"name":"x","arguments":"{}"}}]}}]}`,
			want:  `decode openai chat inference: unexpected tool call type "custom"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, http.StatusOK, tc.reply)
			c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
			_, err := c.InferChat(context.Background(), map[string]any{
				"model": testModel,
			})
			requireInferenceError(t, err, http.StatusBadGateway, tc.want)
			assert.Equal(t, chatCompletionsURLPath, stub.last().path)
		})
	}
}

func TestInferChat_FunctionCallFinishAndNoUsage(t *testing.T) {
	t.Parallel()

	const reply = `{"id":"c","choices":[{"finish_reason":"function_call","message":{"content":"","refusal":"no","tool_calls":[{"id":"t1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`
	_, srv := newStubServer(t, http.StatusOK, reply)
	c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

	out, err := c.InferChat(context.Background(), map[string]any{
		"model": testModel,
	})
	require.NoError(t, err)
	assert.Equal(t, &llms.InferenceResponse{
		UpstreamID:   "c",
		FinishReason: llms.FinishToolCalls,
		Content: []llms.InferenceBlock{
			{
				Type: llms.BlockRefusal,
				Text: "no",
			},
			{
				Type:      llms.BlockFunctionCall,
				ID:        "t1",
				Name:      "lookup",
				Arguments: "{}",
			},
		},
	}, out)
}

func TestInferResponses_Decode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reply  string
		status int
		want   string
	}{
		{
			name:   "incomplete without details",
			reply:  `{"id":"r","status":"incomplete"}`,
			status: http.StatusBadGateway,
			want:   `decode openai responses inference: unsupported incomplete reason ""`,
		},
		{
			name:   "incomplete with unknown reason",
			reply:  `{"id":"r","status":"incomplete","incomplete_details":{"reason":"timeout"}}`,
			status: http.StatusBadGateway,
			want:   `decode openai responses inference: unsupported incomplete reason "timeout"`,
		},
		{
			name:   "failed without error",
			reply:  `{"id":"r","status":"failed"}`,
			status: http.StatusBadGateway,
			want:   "openai responses inference: response failed",
		},
		{
			name:   "failed with error",
			reply:  `{"id":"r","status":"failed","error":{"code":"server_error","message":"boom"}}`,
			status: http.StatusBadGateway,
			want:   "openai responses inference: server_error: boom",
		},
		{
			name:   "unknown status",
			reply:  `{"id":"r","status":"queued"}`,
			status: http.StatusBadGateway,
			want:   `decode openai responses inference: unsupported response status "queued"`,
		},
		{
			name:   "unknown output part",
			reply:  `{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"audio"}]}]}`,
			status: http.StatusBadGateway,
			want:   `decode openai responses inference: unsupported output part "audio"`,
		},
		{
			name:   "unknown output item",
			reply:  `{"id":"r","status":"completed","output":[{"type":"web_search_call"}]}`,
			status: http.StatusBadGateway,
			want:   `decode openai responses inference: unsupported output item "web_search_call"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, http.StatusOK, tc.reply)
			c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
			_, err := c.InferResponses(context.Background(), map[string]any{
				"model": testModel,
			})
			requireInferenceError(t, err, tc.status, tc.want)
			assert.Equal(t, responsesURLPath, stub.last().path)
		})
	}
}

func TestInferResponses_TransportFailure(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
	_, err := c.InferResponses(context.Background(), map[string]any{
		"model": testModel,
	})
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)
	assert.True(t, errors.Is(err, errTransport))
}

func TestInferResponses_UndecodableOutputItem(t *testing.T) {
	t.Parallel()

	_, srv := newStubServer(t, http.StatusOK, `{"id":"r","status":"completed","output":[1]}`)
	c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
	_, err := c.InferResponses(context.Background(), map[string]any{
		"model": testModel,
	})
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)
	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "number", typeErr.Value)
	assert.True(t, strings.HasPrefix(err.Error(), "decode openai responses output item: "), err.Error())
}

func TestInferResponses_IncompleteFinishReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason string
		want   llms.FinishReason
	}{
		{
			reason: "max_output_tokens",
			want:   llms.FinishLength,
		},
		{
			reason: "content_filter",
			want:   llms.FinishContentFilter,
		},
	}
	for _, tc := range tests {
		t.Run(tc.reason, func(t *testing.T) {
			t.Parallel()
			reply := `{"id":"r","status":"incomplete","incomplete_details":{"reason":"` + tc.reason + `"},"output":[]}`
			_, srv := newStubServer(t, http.StatusOK, reply)
			c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
			out, err := c.InferResponses(context.Background(), map[string]any{
				"model": testModel,
			})
			require.NoError(t, err)
			assert.Equal(t, &llms.InferenceResponse{
				UpstreamID:   "r",
				FinishReason: tc.want,
			}, out)
		})
	}
}
