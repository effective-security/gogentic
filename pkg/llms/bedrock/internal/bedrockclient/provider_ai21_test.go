package bedrockclient

import (
	"context"
	"fmt"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ai21ModelID = "ai21.j2-mid-v1"

func TestCreateAi21Completion(t *testing.T) {
	tests := []struct {
		name    string
		options llms.CallOptions
		request string
	}{
		{
			name:    "defaults",
			options: llms.CallOptions{},
			request: `{"prompt":"\nhuman: hi\nAI: ","maxTokens":2048,"countPenalty":{"scale":0},"presencePenalty":{"scale":0},"frequencyPenalty":{"scale":0}}`,
		},
		{
			name: "sampling options",
			options: llms.CallOptions{
				MaxTokens:         16,
				Temperature:       0.7,
				TopP:              0.9,
				StopWords:         []string{"##"},
				RepetitionPenalty: 0.3,
				CandidateCount:    2,
			},
			request: `{"prompt":"\nhuman: hi\nAI: ","temperature":0.7,"topP":0.9,"maxTokens":16,"stopSequences":["##"],"countPenalty":{"scale":0.3},"presencePenalty":{"scale":0},"frequencyPenalty":{"scale":0},"numResults":2}`,
		},
	}
	const response = `{
		"id": 1234,
		"prompt": {"tokens": [{}, {}, {}]},
		"completions": [
			{"data": {"text": "first", "tokens": [{}, {}]}, "finishReason": {"reason": "endoftext"}},
			{"data": {"text": "second", "tokens": [{}]}, "finishReason": {"reason": "length"}}
		]
	}`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, response)
			res, err := s.client().CreateCompletion(context.Background(), ai21ModelID, []Message{
				textMessage(llms.RoleHuman, "hi"),
			}, tt.options)
			require.NoError(t, err)
			assert.Equal(t, []*llms.ContentChoice{
				{
					Content:    "first",
					StopReason: Ai21CompletionReasonEndOfText,
					Usage: llms.Usage{
						InputTokens:  3,
						OutputTokens: 2,
						TotalTokens:  5,
					},
					GenerationInfo: map[string]any{
						"ID": float64(1234),
					},
				},
				{
					Content:    "second",
					StopReason: Ai21CompletionReasonLength,
					Usage: llms.Usage{
						InputTokens:  3,
						OutputTokens: 1,
						TotalTokens:  4,
					},
					GenerationInfo: map[string]any{
						"ID": float64(1234),
					},
				},
			}, res.Choices)

			calls := s.requests()
			require.Len(t, calls, 1)
			assert.Equal(t, fmt.Sprintf(invokePathFmt, ai21ModelID), calls[0].path)
			assert.Equal(t, acceptAny, calls[0].accept)
			assert.JSONEq(t, tt.request, calls[0].body)
		})
	}
}

func TestCreateAi21CompletionErrors(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "hi"),
	}

	s := newStub(t).fail()
	res, err := s.client().CreateCompletion(context.Background(), ai21ModelID, msgs, llms.CallOptions{})
	requireValidationError(t, err)
	assert.Nil(t, res)

	s = newStub(t, invalidJSONBody)
	res, err = s.client().CreateCompletion(context.Background(), ai21ModelID, msgs, llms.CallOptions{})
	require.ErrorContains(t, err, "invalid character 'n' looking for beginning of object key string")
	assert.Nil(t, res)
}
