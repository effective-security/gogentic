package openaiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cockroachdb/errors"
)

const (
	defaultEmbeddingModel = "text-embedding-ada-002"
)

type embeddingPayload struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponsePayload struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// nolint:lll
func (c *Client) createEmbedding(ctx context.Context, payload *embeddingPayload) (*embeddingResponsePayload, error) {
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	if c.Model == "" {
		payload.Model = c.EmbeddingModel
	}
	if payload.Model == "" {
		payload.Model = defaultEmbeddingModel
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal payload")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL("/embeddings", c.EmbeddingModel), bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}
	c.setHeaders(req)

	r, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "send request")
	}
	defer func() {
		_ = r.Body.Close()
	}()

	if r.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("API returned unexpected status code: %d", r.StatusCode)

		// No need to check the error here: if it fails, we'll just return the
		// status code.
		var errResp errorMessage
		if err := json.NewDecoder(r.Body).Decode(&errResp); err != nil {
			return nil, errors.New(msg) // nolint:goerr113
		}

		return nil, errors.Errorf("%s: %s", msg, errResp.Error.Message) // nolint:goerr113
	}

	var response embeddingResponsePayload

	if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}

	return &response, nil
}
