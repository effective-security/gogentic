package openaiclient

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetCompletionDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clientModel string
		payload     CompletionRequest
		want        CompletionRequest
	}{
		{
			name:        "payload values win",
			clientModel: testModel,
			payload: CompletionRequest{
				Model:               "payload-model",
				MaxCompletionTokens: 10,
				StopWords:           []string{"END"},
			},
			want: CompletionRequest{
				Model:               "payload-model",
				MaxCompletionTokens: 10,
				StopWords:           []string{"END"},
			},
		},
		{
			name:        "client model and default limit",
			clientModel: testModel,
			payload: CompletionRequest{
				StopWords: []string{},
			},
			want: CompletionRequest{
				Model:               testModel,
				MaxCompletionTokens: 2048,
			},
		},
		{
			name: "default model",
			want: CompletionRequest{
				Model:               defaultChatModel,
				MaxCompletionTokens: 2048,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{Model: tc.clientModel}
			payload := tc.payload
			c.setCompletionDefaults(&payload)
			// assert.Equal tells a nil slice from an empty one, so this also
			// checks that empty stop words are dropped.
			assert.Equal(t, tc.want, payload)
		})
	}
}

func TestCreateCompletion_Streaming(t *testing.T) {
	t.Parallel()

	const stream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	stub, srv := newStubServer(t, http.StatusOK, stream)
	c := newTestClient(t, ProviderOpenAI, "", srv.URL, http.DefaultClient)

	var chunks []string
	got, err := c.CreateCompletion(context.Background(), &CompletionRequest{
		Prompt: "say hello",
		StreamingFunc: func(_ context.Context, chunk []byte) error {
			chunks = append(chunks, string(chunk))
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, &Completion{Text: "hello"}, got)
	assert.Equal(t, []string{"hel", "lo"}, chunks)

	body := stub.lastJSON()
	assert.Equal(t, defaultChatModel, body["model"])
	assert.Equal(t, true, body["stream"])
	assert.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
}
