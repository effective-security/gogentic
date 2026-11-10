package openaiclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateEmbedding_Errors(t *testing.T) {
	t.Parallel()

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, `{"object":"list","data":[]}`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{Input: []string{"a"}})
		require.ErrorIs(t, err, ErrEmptyResponse)
	})

	t.Run("upstream status with message", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusUnauthorized, `{"error":{"message":"bad key","type":"auth"}}`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{Input: []string{"a"}})
		require.EqualError(t, err, "API returned unexpected status code: 401: bad key")
	})

	t.Run("upstream status without JSON", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusBadGateway, `upstream down`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{Input: []string{"a"}})
		require.EqualError(t, err, "API returned unexpected status code: 502")
	})

	t.Run("undecodable body", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, `{"data":`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{Input: []string{"a"}})
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "decode response: "), err.Error())
	})

	t.Run("transport", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
		_, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{Input: []string{"a"}})
		require.ErrorIs(t, err, errTransport)
		assert.EqualError(t, err, "send request: "+errTransport.Error())
	})

	t.Run("invalid base URL", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, invalidBaseURL, http.DefaultClient)
		_, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{Input: []string{"a"}})
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "create request: "), err.Error())
	})
}

func TestCreateEmbedding_EmptyBaseURLUsesDefault(t *testing.T) {
	t.Parallel()

	// A Client literal skips New's base URL default; createEmbedding restores
	// it. The Doer captures the request so nothing leaves the process.
	var gotURL string
	c := &Client{
		Model:    testModel,
		Provider: ProviderOpenAI,
		token:    testToken,
		httpClient: doerFunc(func(r *http.Request) (*http.Response, error) {
			gotURL = r.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"embedding":[1]}]}`)),
				Request:    r,
			}, nil
		}),
	}
	got, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{Input: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1}}, got)
	assert.Equal(t, DefaultBaseURL+embeddingsURLPath, gotURL)
	assert.Equal(t, DefaultBaseURL, c.baseURL)
}

func TestCreateEmbedding_AzureDeploymentURL(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, `{"data":[{"embedding":[1]}]}`)
	c, err := New(ProviderAzure, testModel, testToken, srv.URL, "", "", testAzureOldAPI, http.DefaultClient, testEmbeddingModel, nil, nil)
	require.NoError(t, err)

	_, err = c.CreateEmbedding(context.Background(), &EmbeddingRequest{
		Model: testEmbeddingModel,
		Input: []string{"a"},
	})
	require.NoError(t, err)
	got := stub.last()
	assert.Equal(t, "/openai/deployments/"+testEmbeddingModel+embeddingsURLPath, got.path)
	assert.Equal(t, "api-version="+testAzureOldAPI, got.query)
}
