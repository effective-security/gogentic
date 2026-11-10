package bedrockclient

import (
	"context"
	"fmt"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cohereModelID      = "cohere.command-text-v14"
	cohereEmbedModelID = "cohere.embed-english-v3"
)

func TestCreateCohereCompletion(t *testing.T) {
	tests := []struct {
		name    string
		options llms.CallOptions
		request string
	}{
		{
			name:    "defaults",
			options: llms.CallOptions{},
			request: `{"prompt":"\nhuman: hi\nAI: ","max_tokens":20}`,
		},
		{
			name: "sampling options",
			options: llms.CallOptions{
				MaxTokens:      100,
				Temperature:    0.4,
				TopP:           0.75,
				TopK:           5,
				StopWords:      []string{"END"},
				CandidateCount: 2,
			},
			request: `{"prompt":"\nhuman: hi\nAI: ","temperature":0.4,"p":0.75,"k":5,"max_tokens":100,"stop_sequences":["END"],"num_generations":2}`,
		},
	}
	const response = `{
		"id": "resp-1",
		"generations": [
			{"id": "gen-a", "index": 0, "finish_reason": "COMPLETE", "text": "alpha"},
			{"id": "gen-b", "index": 1, "finish_reason": "MAX_TOKENS", "text": "beta"}
		]
	}`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, response)
			res, err := s.client().CreateCompletion(context.Background(), cohereModelID, []Message{
				textMessage(llms.RoleHuman, "hi"),
			}, tt.options)
			require.NoError(t, err)
			assert.Equal(t, []*llms.ContentChoice{
				{
					Content:    "alpha",
					StopReason: CohereCompletionReasonComplete,
					GenerationInfo: map[string]any{
						"ID":    "gen-a",
						"Index": 0,
					},
				},
				{
					Content:    "beta",
					StopReason: CohereCompletionReasonMaxTokens,
					GenerationInfo: map[string]any{
						"ID":    "gen-b",
						"Index": 1,
					},
				},
			}, res.Choices)

			calls := s.requests()
			require.Len(t, calls, 1)
			assert.Equal(t, fmt.Sprintf(invokePathFmt, cohereModelID), calls[0].path)
			assert.Equal(t, acceptAny, calls[0].accept)
			assert.JSONEq(t, tt.request, calls[0].body)
		})
	}
}

func TestCreateCohereCompletionErrors(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "hi"),
	}

	s := newStub(t).fail()
	res, err := s.client().CreateCompletion(context.Background(), cohereModelID, msgs, llms.CallOptions{})
	requireValidationError(t, err)
	assert.Nil(t, res)

	s = newStub(t, invalidJSONBody)
	res, err = s.client().CreateCompletion(context.Background(), cohereModelID, msgs, llms.CallOptions{})
	require.ErrorContains(t, err, "invalid character 'n' looking for beginning of object key string")
	assert.Nil(t, res)
}

func TestCreateCohereEmbedding(t *testing.T) {
	s := newStub(t, `{"embeddings":[[0.5,-1],[2,0.25]]}`)
	res, err := s.client().CreateEmbedding(context.Background(), cohereEmbedModelID, []string{"first", "second"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{
		{0.5, -1},
		{2, 0.25},
	}, res)

	calls := s.requests()
	require.Len(t, calls, 1, "cohere embeds all texts in one request")
	assert.Equal(t, fmt.Sprintf(invokePathFmt, cohereEmbedModelID), calls[0].path)
	assert.Equal(t, contentTypeJSON, calls[0].accept)
	assert.Equal(t, contentTypeJSON, calls[0].contentType)
	assert.JSONEq(t, `{"texts":["first","second"],"input_type":"search_document"}`, calls[0].body)
}

func TestCreateCohereEmbeddingErrors(t *testing.T) {
	texts := []string{"first"}

	s := newStub(t).fail()
	res, err := s.client().CreateEmbedding(context.Background(), cohereEmbedModelID, texts)
	requireValidationError(t, err)
	assert.Nil(t, res)

	s = newStub(t, invalidJSONBody)
	res, err = s.client().CreateEmbedding(context.Background(), cohereEmbedModelID, texts)
	require.ErrorContains(t, err, "invalid character 'n' looking for beginning of object key string")
	assert.Nil(t, res)
}
