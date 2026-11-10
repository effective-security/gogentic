package cloudflare

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptions(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{}
	var got options
	for _, opt := range []Option{
		WithModel(testModel),
		WithEmbeddingModel(testEmbeddingModel),
		WithSystemPrompt(testSystemPrompt),
		WithAccountID(testAccountID),
		WithServerURL(testServerURL),
		WithToken(testToken),
		WithHTTPClient(httpClient),
	} {
		opt(&got)
	}

	serverURL, err := url.Parse(testServerURL)
	require.NoError(t, err)
	assert.Equal(t, options{
		cloudflareAccountID: testAccountID,
		cloudflareServerURL: serverURL,
		cloudflareToken:     testToken,
		httpClient:          httpClient,
		model:               testModel,
		embeddingModel:      testEmbeddingModel,
		system:              testSystemPrompt,
	}, got)
}

func TestNew_DefaultHTTPClient(t *testing.T) {
	t.Parallel()

	llm, err := New(WithServerURL(testServerURL), WithModel(testModel))
	require.NoError(t, err)
	assert.Same(t, http.DefaultClient, llm.options.httpClient)
}
