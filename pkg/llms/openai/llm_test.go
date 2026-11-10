package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bedrockRegion   = "us-west-2"
	bedrockResponse = `{"id":"resp_1","object":"response","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}]}`
)

// urlRecorder is a Doer that records request URLs and headers and answers
// every request itself, so nothing leaves the process.
type urlRecorder struct {
	urls    []string
	headers []http.Header
	reply   string
}

func (r *urlRecorder) Do(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	r.headers = append(r.headers, req.Header.Clone())
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(r.reply)),
		Request:    req,
	}, nil
}

func TestNew_ConfigErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    []Option
		wantIs  error
		wantErr string
	}{
		{
			name: "azure without model",
			opts: []Option{
				WithProvider(llms.ProviderAzure),
				WithToken(llmTestToken),
				WithModel(""),
			},
			wantIs: ErrMissingAzureModel,
		},
		{
			name: "azure ad without embedding model",
			opts: []Option{
				WithProvider(llms.ProviderAzureAD),
				WithToken(llmTestToken),
				WithModel(llmTestModel),
			},
			wantIs: ErrMissingAzureEmbeddingModel,
		},
		{
			name: "bedrock without AWS config",
			opts: []Option{
				WithProvider(llms.ProviderOpenAIBedrock),
				WithToken(llmTestToken),
			},
			wantErr: "bedrock openai: AWS config is required",
		},
		{
			name: "bedrock without region",
			opts: []Option{
				WithProvider(llms.ProviderOpenAIBedrock),
				WithToken(llmTestToken),
				WithAWSConfig(&aws.Config{}),
			},
			wantErr: "bedrock openai: AWS region is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			llm, err := New(tc.opts...)
			require.Error(t, err)
			assert.Nil(t, llm)
			if tc.wantIs != nil {
				assert.True(t, errors.Is(err, tc.wantIs), err.Error())
			}
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
			}
		})
	}
}

func TestNew_MissingToken(t *testing.T) {
	t.Setenv(DefaultTokenEnvVarName, "")

	_, err := New(WithModel(llmTestModel))
	require.ErrorIs(t, err, ErrMissingToken)
}

func TestNew_BedrockDefaultBaseURL(t *testing.T) {
	t.Parallel()

	rec := &urlRecorder{reply: bedrockResponse}
	llm, err := New(
		WithProvider(llms.ProviderOpenAIBedrock),
		WithToken(llmTestToken),
		WithModel(llmTestModel),
		WithAWSConfig(&aws.Config{Region: bedrockRegion}),
		WithHTTPClient(rec),
	)
	require.NoError(t, err)

	resp, err := llm.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	})
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "hello", resp.Choices[0].Content)
	assert.Equal(t, []string{"https://bedrock-mantle." + bedrockRegion + ".api.aws/openai/v1/responses"}, rec.urls)
	assert.Equal(t, "Bearer "+llmTestToken, rec.headers[0].Get("Authorization"))
}

func TestOrganizationAndProjectOptions(t *testing.T) {
	t.Parallel()

	rec := &urlRecorder{reply: bedrockResponse}
	llm, err := New(
		WithToken(llmTestToken),
		WithModel(llmTestModel),
		WithBaseURL("https://api.example.test/v1"),
		WithOrganization("org-1"),
		WithProject("proj-1"),
		WithHTTPClient(rec),
	)
	require.NoError(t, err)

	_, err = llm.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	})
	require.NoError(t, err)
	require.Len(t, rec.headers, 1)
	assert.Equal(t, []string{"https://api.example.test/v1/responses"}, rec.urls)
	assert.Equal(t, "org-1", rec.headers[0].Get("OpenAI-Organization"))
	assert.Equal(t, "proj-1", rec.headers[0].Get("OpenAI-Project"))
}
