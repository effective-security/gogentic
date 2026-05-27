package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

var _ llms.Batcher = (*LLM)(nil)

// SubmitBatch implements [llms.Batcher]. It marshals each BatchRequest into a
// JSONL line, uploads the document as a file with purpose=batch, and creates a
// batch targeting either /v1/chat/completions or /v1/responses depending on
// whether the underlying provider configuration uses the Responses API.
func (o *LLM) SubmitBatch(ctx context.Context, requests []llms.BatchRequest, options ...llms.BatchSubmitOption) (*llms.BatchHandle, error) {
	if len(requests) == 0 {
		return nil, errors.New("invalid argument: requests must not be empty")
	}
	if err := validateCustomIDs(requests); err != nil {
		return nil, err
	}

	opts := llms.BatchSubmitOptions{}
	for _, opt := range options {
		opt(&opts)
	}

	endpoint := openaiclient.EndpointChatCompletions
	if o.client.SupportsResponsesAPI() {
		endpoint = openaiclient.EndpointResponses
	}

	var buf bytes.Buffer
	for _, r := range requests {
		line, err := o.buildBatchLine(r, endpoint)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to build batch line for request: %s", r.CustomID)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	fileID, err := o.client.UploadBatchFile(ctx, "requests.jsonl", &buf)
	if err != nil {
		return nil, batchError(err)
	}

	batch, err := o.client.CreateBatch(ctx, openaiclient.BatchCreateParams{
		InputFileID:      fileID,
		Endpoint:         endpoint,
		CompletionWindow: completionWindowString(opts.CompletionWindow),
		Metadata:         opts.Metadata,
	})
	if err != nil {
		return nil, batchError(err)
	}
	return batchHandleFromSDK(batch), nil
}

// GetBatch implements [llms.Batcher].
func (o *LLM) GetBatch(ctx context.Context, batchID string) (*llms.BatchHandle, error) {
	batch, err := o.client.GetBatch(ctx, batchID)
	if err != nil {
		return nil, batchError(err)
	}
	return batchHandleFromSDK(batch), nil
}

// CancelBatch implements [llms.Batcher].
func (o *LLM) CancelBatch(ctx context.Context, batchID string) (*llms.BatchHandle, error) {
	batch, err := o.client.CancelBatch(ctx, batchID)
	if err != nil {
		return nil, batchError(err)
	}
	return batchHandleFromSDK(batch), nil
}

// FetchBatchResults implements [llms.Batcher]. It refuses to read partial
// output: callers must wait until BatchHandle.Status.IsTerminal() returns true
// (typically by polling GetBatch).
func (o *LLM) FetchBatchResults(ctx context.Context, batchID string) ([]llms.BatchResult, error) {
	batch, err := o.client.GetBatch(ctx, batchID)
	if err != nil {
		return nil, batchError(err)
	}
	handle := batchHandleFromSDK(batch)
	if !handle.Status.IsTerminal() {
		return nil, errors.WithStack(llms.ErrBatchNotReady)
	}

	endpoint := openaiclient.BatchEndpoint(batch.Endpoint)
	var results []llms.BatchResult

	if batch.OutputFileID != "" {
		out, err := o.readBatchFile(ctx, batch.OutputFileID, endpoint)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read batch output file %s", batch.OutputFileID)
		}
		results = append(results, out...)
	}
	if batch.ErrorFileID != "" {
		errs, err := o.readBatchFile(ctx, batch.ErrorFileID, endpoint)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read batch error file %s", batch.ErrorFileID)
		}
		results = append(results, errs...)
	}
	return results, nil
}

// batchError translates an error returned by the openaiclient batch wrappers
// into the provider-neutral sentinels callers match with errors.Is. Errors that
// have no neutral equivalent are returned unchanged: the client already attaches
// a stack trace, so re-wrapping here would only stack a redundant one.
func batchError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, openaiclient.ErrBatchUnsupported()) {
		return errors.WithStack(llms.ErrBatchNotSupported)
	}
	var apiErr *openaisdk.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return errors.WithStack(llms.ErrBatchNotFound)
	}
	return err
}

// buildBatchLine marshals a single BatchRequest into the JSONL line format
// expected by the OpenAI Batch API.
func (o *LLM) buildBatchLine(r llms.BatchRequest, endpoint openaiclient.BatchEndpoint) ([]byte, error) {
	if r.CustomID == "" {
		return nil, errors.New("invalid argument: BatchRequest.CustomID is required")
	}

	var body json.RawMessage
	switch endpoint {
	case openaiclient.EndpointResponses:
		req, err := o.buildResponsesRequestBody(r.Messages, r.Options...)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(req)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal responses body")
		}
		body = raw
	case openaiclient.EndpointChatCompletions:
		req, err := o.buildChatRequestBody(r.Messages, r.Options...)
		if err != nil {
			return nil, err
		}
		if req.Model == "" {
			req.Model = o.client.Model
		}
		raw, err := json.Marshal(req)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal chat body")
		}
		body = raw
	default:
		return nil, errors.Errorf("unsupported batch endpoint %q", endpoint)
	}

	line := batchInputLine{
		CustomID: r.CustomID,
		Method:   "POST",
		URL:      string(endpoint),
		Body:     body,
	}
	return json.Marshal(line)
}

// readBatchFile downloads a JSONL file from the API and decodes each line into
// a BatchResult, dispatching on endpoint to interpret the inner response body.
func (o *LLM) readBatchFile(ctx context.Context, fileID string, endpoint openaiclient.BatchEndpoint) ([]llms.BatchResult, error) {
	r, err := o.client.DownloadFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	var results []llms.BatchResult
	scanner := bufio.NewScanner(r)
	// JSONL lines for chat/responses bodies can be large; let single lines
	// reach the per-request 200 MB API ceiling rather than failing at 64 KB.
	scanner.Buffer(make([]byte, 0, 1<<20), 200*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		result, err := parseBatchOutputLine(line, endpoint)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to scan batch file")
	}
	return results, nil
}

// batchInputLine is the JSONL line format the OpenAI Batch API accepts as input.
type batchInputLine struct {
	CustomID string          `json:"custom_id"`
	Method   string          `json:"method"`
	URL      string          `json:"url"`
	Body     json.RawMessage `json:"body"`
}

// batchOutputLine is the JSONL line format the OpenAI Batch API emits in
// output and error files.
type batchOutputLine struct {
	ID       string                   `json:"id"`
	CustomID string                   `json:"custom_id"`
	Response *batchOutputResponseBody `json:"response"`
	Error    *batchOutputError        `json:"error"`
}

type batchOutputResponseBody struct {
	StatusCode int             `json:"status_code"`
	RequestID  string          `json:"request_id"`
	Body       json.RawMessage `json:"body"`
}

type batchOutputError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// bodyErrorEnvelope captures the {"error":{...}} shape that OpenAI returns
// inside response.body when an individual request fails with a non-2xx status.
type bodyErrorEnvelope struct {
	Error *batchOutputError `json:"error"`
}

func parseBatchOutputLine(line []byte, endpoint openaiclient.BatchEndpoint) (llms.BatchResult, error) {
	var out batchOutputLine
	if err := json.Unmarshal(line, &out); err != nil {
		return llms.BatchResult{}, errors.Wrap(err, "failed to unmarshal batch output line")
	}
	result := llms.BatchResult{CustomID: out.CustomID}

	if out.Error != nil {
		result.Error = &llms.BatchRequestError{
			Code:    out.Error.Code,
			Message: out.Error.Message,
		}
		return result, nil
	}
	if out.Response == nil {
		result.Error = &llms.BatchRequestError{Message: "no response body and no error"}
		return result, nil
	}

	// Per-request HTTP failures land in response.body as {"error":{...}}.
	if out.Response.StatusCode != 0 && (out.Response.StatusCode < 200 || out.Response.StatusCode >= 300) {
		var env bodyErrorEnvelope
		if err := json.Unmarshal(out.Response.Body, &env); err == nil && env.Error != nil {
			result.Error = &llms.BatchRequestError{
				Code:    env.Error.Code,
				Message: env.Error.Message,
			}
			return result, nil
		}
		result.Error = &llms.BatchRequestError{
			Code:    fmt.Sprintf("http_%d", out.Response.StatusCode),
			Message: string(out.Response.Body),
		}
		return result, nil
	}

	resp, err := parseBatchResponseBody(out.Response.Body, endpoint)
	if err != nil {
		return llms.BatchResult{}, errors.Wrapf(err, "failed to parse response body for custom_id %q", out.CustomID)
	}
	result.Response = resp
	return result, nil
}

func parseBatchResponseBody(body json.RawMessage, endpoint openaiclient.BatchEndpoint) (*llms.ContentResponse, error) {
	switch endpoint {
	case openaiclient.EndpointResponses:
		var r responses.Response
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal responses body")
		}
		return contentResponseFromResponses(&r), nil
	case openaiclient.EndpointChatCompletions:
		var r openaiclient.ChatCompletionResponse
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal chat completion body")
		}
		return contentResponseFromChat(&r), nil
	default:
		return nil, errors.Errorf("unsupported batch endpoint %q", endpoint)
	}
}

func contentResponseFromChat(r *openaiclient.ChatCompletionResponse) *llms.ContentResponse {
	choices := make([]*llms.ContentChoice, len(r.Choices))
	for i, c := range r.Choices {
		choices[i] = &llms.ContentChoice{
			Content:    c.Message.Content,
			StopReason: fmt.Sprint(c.FinishReason),
			GenerationInfo: map[string]any{
				"OutputTokens":    r.Usage.CompletionTokens,
				"InputTokens":     r.Usage.PromptTokens,
				"TotalTokens":     r.Usage.TotalTokens,
				"ReasoningTokens": r.Usage.CompletionTokensDetails.ReasoningTokens,
			},
		}
		for _, tool := range c.Message.ToolCalls {
			choices[i].ToolCalls = append(choices[i].ToolCalls, llms.ToolCall{
				ID:   tool.ID,
				Type: string(tool.Type),
				FunctionCall: &llms.FunctionCall{
					Name:      tool.Function.Name,
					Arguments: tool.Function.Arguments,
				},
			})
		}
		if len(choices[i].ToolCalls) > 0 {
			choices[i].FuncCall = choices[i].ToolCalls[0].FunctionCall
		}
	}
	return &llms.ContentResponse{Choices: choices}
}

func contentResponseFromResponses(r *responses.Response) *llms.ContentResponse {
	choice := &llms.ContentChoice{
		Content: r.OutputText(),
		GenerationInfo: map[string]any{
			"OutputTokens":    r.Usage.OutputTokens,
			"InputTokens":     r.Usage.InputTokens,
			"CacheReadTokens": r.Usage.InputTokensDetails.CachedTokens,
			"TotalTokens":     r.Usage.TotalTokens,
			"ReasoningTokens": r.Usage.OutputTokensDetails.ReasoningTokens,
		},
	}
	for _, item := range r.Output {
		if item.Type == "function_call" {
			id := item.CallID
			if id == "" {
				id = item.ID
			}
			choice.ToolCalls = append(choice.ToolCalls, llms.ToolCall{
				ID:   id,
				Type: string(openaiclient.ToolTypeFunction),
				FunctionCall: &llms.FunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments.OfString,
				},
			})
		}
	}
	if len(choice.ToolCalls) > 0 {
		choice.FuncCall = choice.ToolCalls[0].FunctionCall
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{choice}}
}

func batchHandleFromSDK(b *openaisdk.Batch) *llms.BatchHandle {
	if b == nil {
		return nil
	}
	h := &llms.BatchHandle{
		ID:       b.ID,
		Provider: llms.ProviderOpenAI,
		Status:   batchStatusFromSDK(b.Status),
		RequestCounts: llms.BatchRequestCounts{
			Total:     int(b.RequestCounts.Total),
			Completed: int(b.RequestCounts.Completed),
			Failed:    int(b.RequestCounts.Failed),
		},
		ProviderMeta: b,
	}
	if b.CreatedAt > 0 {
		created := time.Unix(b.CreatedAt, 0).UTC()
		h.CreatedAt = &created
	}

	var terminalAt int64
	switch {
	case b.CompletedAt > 0:
		terminalAt = b.CompletedAt
	case b.FailedAt > 0:
		terminalAt = b.FailedAt
	case b.ExpiredAt > 0:
		terminalAt = b.ExpiredAt
	case b.CancelledAt > 0:
		terminalAt = b.CancelledAt
	}
	if terminalAt > 0 {
		completed := time.Unix(terminalAt, 0).UTC()
		h.CompletedAt = &completed
	}
	if b.ExpiresAt > 0 {
		expires := time.Unix(b.ExpiresAt, 0).UTC()
		h.ExpiresAt = &expires
	}
	if len(b.Metadata) > 0 {
		h.Metadata = make(map[string]string, len(b.Metadata))
		for k, v := range b.Metadata {
			h.Metadata[k] = v
		}
	}
	return h
}

func batchStatusFromSDK(s openaisdk.BatchStatus) llms.BatchStatus {
	switch s {
	case openaisdk.BatchStatusValidating:
		return llms.BatchStatusValidating
	case openaisdk.BatchStatusInProgress:
		return llms.BatchStatusInProgress
	case openaisdk.BatchStatusFinalizing:
		return llms.BatchStatusFinalizing
	case openaisdk.BatchStatusCompleted:
		return llms.BatchStatusCompleted
	case openaisdk.BatchStatusFailed:
		return llms.BatchStatusFailed
	case openaisdk.BatchStatusExpired:
		return llms.BatchStatusExpired
	case openaisdk.BatchStatusCancelling:
		return llms.BatchStatusCancelling
	case openaisdk.BatchStatusCancelled:
		return llms.BatchStatusCancelled
	}
	return llms.BatchStatus(s)
}

// completionWindowString maps a Go duration to the API's accepted string. The
// API expresses the window in whole hours (currently only "24h" is accepted),
// so we round up to the next hour to avoid emitting sub-hour units like
// minutes or seconds, and let the API reject anything unsupported.
func completionWindowString(d time.Duration) string {
	if d <= 0 {
		return "24h"
	}
	hours := d / time.Hour
	if d%time.Hour != 0 {
		hours++
	}
	return fmt.Sprintf("%dh", hours)
}

func validateCustomIDs(requests []llms.BatchRequest) error {
	seen := make(map[string]struct{}, len(requests))
	for i, r := range requests {
		if r.CustomID == "" {
			return errors.Errorf("requests[%d]: CustomID is required", i)
		}
		if _, dup := seen[r.CustomID]; dup {
			return errors.Errorf("requests[%d]: duplicate CustomID %q", i, r.CustomID)
		}
		seen[r.CustomID] = struct{}{}
	}
	return nil
}
