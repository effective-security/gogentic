package openaiclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xlog"
	"github.com/openai/openai-go/v3/responses"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "openai")

// createResponse sends the request to /responses and parses a non-streaming reply.
func (c *Client) createResponse(ctx context.Context, payload *responses.ResponseNewParams) (*responses.Response, error) { //nolint:lll
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

// createStreamingResponse sends the request to /responses with stream:true, calls streamFunc for
// each text delta, and returns the full Response from the response.completed event.
func (c *Client) createStreamingResponse( //nolint:cyclop
	ctx context.Context,
	payload *responses.ResponseNewParams,
	streamFunc func(ctx context.Context, chunk []byte) error,
) (*responses.Response, error) {
	// Marshal payload then inject "stream": true.
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal payload")
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(rawPayload, &payloadMap); err != nil {
		return nil, errors.Wrap(err, "unmarshal payload for stream")
	}
	payloadMap["stream"] = true
	bodyBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, errors.Wrap(err, "re-marshal payload with stream")
	}

	u := c.buildURL("/responses", payload.Model)
	logger.ContextKV(ctx, xlog.DEBUG, "url", u, "stream", true)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}
	c.setHeaders(req)
	req.Header.Set("Accept", "text/event-stream")

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

	return parseStreamingResponses(ctx, r.Body, streamFunc)
}

// parseStreamingResponses reads an SSE stream from the Responses API.
// It calls streamFunc for each response.output_text.delta event and returns
// the full Response embedded in the response.completed event.
func parseStreamingResponses( //nolint:cyclop
	ctx context.Context,
	body io.Reader,
	streamFunc func(ctx context.Context, chunk []byte) error,
) (*responses.Response, error) {
	scanner := bufio.NewScanner(body)

	var (
		eventType string
		completed *responses.Response
	)

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))

		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}

			switch eventType {
			case "response.output_text.delta":
				var ev responses.ResponseTextDeltaEvent
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					logger.ContextKV(ctx, xlog.WARNING, "reason", "unmarshal text delta", "err", err)
					continue
				}
				if ev.Delta != "" && streamFunc != nil {
					if err := streamFunc(ctx, []byte(ev.Delta)); err != nil {
						return nil, errors.Wrap(err, "streaming func error")
					}
				}

			case "response.completed":
				var ev responses.ResponseCompletedEvent
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					return nil, errors.Wrap(err, "unmarshal response.completed event")
				}
				resp := ev.Response
				completed = &resp

			case "response.failed", "error":
				var ev struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				_ = json.Unmarshal([]byte(data), &ev)
				msg := ev.Error.Message
				if msg == "" {
					msg = "streaming response failed"
				}
				return nil, errors.Errorf("openai responses API streaming error: %s", msg)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "read SSE stream")
	}
	if completed == nil {
		return nil, errors.New("streaming response ended without response.completed event")
	}
	return completed, nil
}
