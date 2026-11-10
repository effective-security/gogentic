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
	amazonModelID      = "amazon.titan-text-lite-v1"
	amazonEmbedModelID = "amazon.titan-embed-text-v1"
)

func TestCreateAmazonCompletion(t *testing.T) {
	tests := []struct {
		name    string
		options llms.CallOptions
		request string
	}{
		{
			name:    "defaults",
			options: llms.CallOptions{},
			request: `{"inputText":"\nhuman: hi\nAI: ","textGenerationConfig":{"maxTokenCount":512}}`,
		},
		{
			name: "sampling options",
			options: llms.CallOptions{
				MaxTokens:   128,
				Temperature: 0.2,
				TopP:        0.8,
				StopWords:   []string{"User:"},
			},
			request: `{"inputText":"\nhuman: hi\nAI: ","textGenerationConfig":{"maxTokenCount":128,"topP":0.8,"temperature":0.2,"stopSequences":["User:"]}}`,
		},
	}
	const response = `{
		"inputTextTokenCount": 4,
		"results": [
			{"tokenCount": 2, "outputText": "one", "completionReason": "FINISH"},
			{"tokenCount": 6, "outputText": "two", "completionReason": "LENGTH"}
		]
	}`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, response)
			res, err := s.client().CreateCompletion(context.Background(), amazonModelID, []Message{
				textMessage(llms.RoleHuman, "hi"),
			}, tt.options)
			require.NoError(t, err)
			assert.Equal(t, []*llms.ContentChoice{
				{
					Content:    "one",
					StopReason: AmazonCompletionReasonFinish,
					Usage: llms.Usage{
						InputTokens:  4,
						OutputTokens: 2,
						TotalTokens:  6,
					},
				},
				{
					Content:    "two",
					StopReason: AmazonCompletionReasonMaxTokens,
					Usage: llms.Usage{
						InputTokens:  4,
						OutputTokens: 6,
						TotalTokens:  10,
					},
				},
			}, res.Choices)

			calls := s.requests()
			require.Len(t, calls, 1)
			assert.Equal(t, fmt.Sprintf(invokePathFmt, amazonModelID), calls[0].path)
			assert.Equal(t, acceptAny, calls[0].accept)
			assert.JSONEq(t, tt.request, calls[0].body)
		})
	}
}

func TestCreateAmazonCompletionErrors(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "hi"),
	}

	s := newStub(t).fail()
	res, err := s.client().CreateCompletion(context.Background(), amazonModelID, msgs, llms.CallOptions{})
	requireValidationError(t, err)
	assert.Nil(t, res)

	s = newStub(t, invalidJSONBody)
	res, err = s.client().CreateCompletion(context.Background(), amazonModelID, msgs, llms.CallOptions{})
	require.ErrorContains(t, err, "invalid character 'n' looking for beginning of object key string")
	assert.Nil(t, res)

	s = newStub(t, `{"inputTextTokenCount":4,"results":[]}`)
	res, err = s.client().CreateCompletion(context.Background(), amazonModelID, msgs, llms.CallOptions{})
	require.EqualError(t, err, "no results")
	assert.Nil(t, res)
}

func TestCreateAmazonEmbedding(t *testing.T) {
	s := newStub(t,
		`{"embedding":[0.1,0.2],"inputTextTokenCount":1}`,
		`{"embedding":[0.3,0.4],"inputTextTokenCount":1}`,
	)
	res, err := s.client().CreateEmbedding(context.Background(), amazonEmbedModelID, []string{"first", "second"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{
		{0.1, 0.2},
		{0.3, 0.4},
	}, res)

	calls := s.requests()
	require.Len(t, calls, 2, "titan embeds one text per request")
	for i, text := range []string{"first", "second"} {
		assert.Equal(t, fmt.Sprintf(invokePathFmt, amazonEmbedModelID), calls[i].path)
		assert.Equal(t, contentTypeJSON, calls[i].accept)
		assert.JSONEq(t, `{"inputText":"`+text+`"}`, calls[i].body)
	}
}

func TestCreateAmazonEmbeddingEmptyInput(t *testing.T) {
	s := newStub(t)
	res, err := s.client().CreateEmbedding(context.Background(), amazonEmbedModelID, nil)
	require.NoError(t, err)
	assert.NotNil(t, res)
	assert.Empty(t, res)
	assert.Empty(t, s.requests())
}

func TestCreateAmazonEmbeddingErrors(t *testing.T) {
	texts := []string{"first"}

	s := newStub(t).fail()
	res, err := s.client().CreateEmbedding(context.Background(), amazonEmbedModelID, texts)
	requireValidationError(t, err)
	assert.Nil(t, res)

	s = newStub(t, invalidJSONBody)
	res, err = s.client().CreateEmbedding(context.Background(), amazonEmbedModelID, texts)
	require.ErrorContains(t, err, "invalid character 'n' looking for beginning of object key string")
	assert.Nil(t, res)
}
