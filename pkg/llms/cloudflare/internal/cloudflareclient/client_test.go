package cloudflareclient

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{}
	c := NewClient(httpClient, testAccountID, testBaseURL, testToken, testModel, testEmbeddingModel)

	assert.Equal(t, &Client{
		httpClient:         httpClient,
		accountID:          testAccountID,
		token:              testToken,
		baseURL:            testBaseURL,
		modelName:          testModel,
		embeddingModelName: testEmbeddingModel,
		endpointURL:        testBaseURL + "/" + testAccountID + "/ai/run/" + testModel,
		bearerToken:        "Bearer " + testToken,
	}, c)
}
