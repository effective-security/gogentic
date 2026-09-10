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

func unexpectedResponsesStatusError(r *http.Response, requestURL string) error {
	msg := fmt.Sprintf("API returned unexpected status code: %d", r.StatusCode)
	if r.StatusCode == http.StatusNotFound {
		msg += ": url: " + requestURL
	}
	if retryAfter := r.Header.Get("Retry-After"); retryAfter != "" {
		msg += "; retry after: " + retryAfter
	}

	var errResp errorMessage
	if err := json.NewDecoder(r.Body).Decode(&errResp); err != nil || errResp.Error.Message == "" {
		return errors.New(msg)
	}
	return errors.Errorf("%s: %s", msg, errResp.Error.Message)
}

func failedResponseError(resp *responses.Response) error {
	if resp == nil || resp.Status != responses.ResponseStatusFailed {
		return nil
	}
	code := string(resp.Error.Code)
	message := resp.Error.Message
	switch {
	case code != "" && message != "":
		return errors.Errorf("responses API failed (%s): %s", code, message)
	case message != "":
		return errors.Errorf("responses API failed: %s", message)
	case code != "":
		return errors.Errorf("responses API failed (%s)", code)
	default:
		return errors.New("responses API failed")
	}
}

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
		return nil, unexpectedResponsesStatusError(r, u)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read body")
	}

	var resp responses.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}
	if err := failedResponseError(&resp); err != nil {
		return nil, err
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
		return nil, unexpectedResponsesStatusError(r, u)
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

			case "response.failed":
				var ev responses.ResponseFailedEvent
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					return nil, errors.Wrap(err, "unmarshal response.failed event")
				}
				failure := failedResponseError(&ev.Response)
				if failure == nil {
					failure = errors.New("responses API failed")
				}
				return nil, errors.WithMessage(failure, "openai responses API streaming error")

			case "response.error", "error":
				var ev struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Error   struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					return nil, errors.Wrap(err, "unmarshal responses error event")
				}
				code := ev.Code
				message := ev.Message
				if ev.Error.Code != "" {
					code = ev.Error.Code
				}
				if ev.Error.Message != "" {
					message = ev.Error.Message
				}
				if message == "" {
					message = "streaming response failed"
				}
				if code != "" {
					return nil, errors.Errorf("openai responses API streaming error (%s): %s", code, message)
				}
				return nil, errors.Errorf("openai responses API streaming error: %s", message)
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
