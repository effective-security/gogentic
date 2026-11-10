package bedrockclient

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const metaModelID = "us.meta.llama3-2-11b-instruct-v1:0"

func TestCreateMetaCompletion(t *testing.T) {
	tests := []struct {
		name    string
		options llms.CallOptions
		request string
	}{
		{
			name:    "defaults",
			options: llms.CallOptions{},
			request: `{"prompt":"\nhuman: hi\nAI: ","max_gen_len":512}`,
		},
		{
			name: "sampling options",
			options: llms.CallOptions{
				MaxTokens:   64,
				Temperature: 0.25,
				TopP:        0.5,
			},
			request: `{"prompt":"\nhuman: hi\nAI: ","temperature":0.25,"top_p":0.5,"max_gen_len":64}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStub(t, `{"generation":"hello","prompt_token_count":3,"generation_token_count":2,"stop_reason":"stop"}`)
			res, err := s.client().CreateCompletion(context.Background(), metaModelID, []Message{
				textMessage(llms.RoleHuman, "hi"),
			}, tt.options)
			require.NoError(t, err)
			require.Len(t, res.Choices, 1)
			assert.Equal(t, &llms.ContentChoice{
				Content:    "hello",
				StopReason: MetaCompletionReasonStop,
				Usage: llms.Usage{
					InputTokens:  3,
					OutputTokens: 2,
					TotalTokens:  5,
				},
			}, res.Choices[0])

			calls := s.requests()
			require.Len(t, calls, 1)
			assert.Equal(t, http.MethodPost, calls[0].method)
			assert.Equal(t, fmt.Sprintf(invokePathFmt, metaModelID), calls[0].path)
			assert.Equal(t, acceptAny, calls[0].accept)
			assert.Equal(t, contentTypeJSON, calls[0].contentType)
			assert.JSONEq(t, tt.request, calls[0].body)
		})
	}
}

func TestCreateMetaCompletionErrors(t *testing.T) {
	msgs := []Message{
		textMessage(llms.RoleHuman, "hi"),
	}

	s := newStub(t).fail()
	res, err := s.client().CreateCompletion(context.Background(), metaModelID, msgs, llms.CallOptions{})
	requireValidationError(t, err)
	assert.Nil(t, res)

	s = newStub(t, invalidJSONBody)
	res, err = s.client().CreateCompletion(context.Background(), metaModelID, msgs, llms.CallOptions{})
	require.ErrorContains(t, err, "invalid character 'n' looking for beginning of object key string")
	assert.Nil(t, res)
}
