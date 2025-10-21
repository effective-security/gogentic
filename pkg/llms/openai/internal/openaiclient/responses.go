package openaiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/openai/openai-go/v3/responses"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "openai")

// createResponse sends the request to /responses and parses streaming or non-streaming reply.
func (c *Client) createResponse(ctx context.Context, payload *responses.ResponseNewParams) (*responses.Response, error) { //nolint:lll,cyclop
	// if payload.StreamingFunc != nil || payload.StreamingReasoningFunc != nil {
	// 	payload.Stream = true
	// }

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal payload")
	}

	u := c.buildURL("/responses", payload.Model)
	logger.ContextKV(ctx, xlog.DEBUG, "url", u)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}
	c.setHeaders(req)

	r, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "send request")
	}
	defer func() { _ = r.Body.Close() }()

	if r.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("API returned unexpected status code: %d", r.StatusCode)
		if r.StatusCode == http.StatusNotFound {
			msg += ": url: " + u
		}
		var errResp errorMessage
		if err := json.NewDecoder(r.Body).Decode(&errResp); err != nil {
			return nil, errors.New(msg)
		}
		return nil, errors.Errorf("%s: %s", msg, errResp.Error.Message)
	}

	// TODO
	// if payload.Stream {
	// 	return parseStreamingResponses(ctx, r, payload)
	// }

	// read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read body")
	}

	var resp responses.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}
	return &resp, nil
}
