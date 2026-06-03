package llms

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
)

// BatchStatus describes the lifecycle state of a batch.
type BatchStatus string

const (
	// BatchStatusValidating indicates the batch input is being validated.
	BatchStatusValidating BatchStatus = "validating"
	// BatchStatusInProgress indicates the batch is being processed.
	BatchStatusInProgress BatchStatus = "in_progress"
	// BatchStatusFinalizing indicates the batch finished processing and results are being prepared.
	BatchStatusFinalizing BatchStatus = "finalizing"
	// BatchStatusCompleted indicates the batch is finished and results are available.
	BatchStatusCompleted BatchStatus = "completed"
	// BatchStatusFailed indicates the batch failed during validation or processing.
	BatchStatusFailed BatchStatus = "failed"
	// BatchStatusExpired indicates the batch was not completed within the provider's
	// completion window and any partial results have been released.
	BatchStatusExpired BatchStatus = "expired"
	// BatchStatusCancelling indicates a cancellation has been requested but not finalized.
	BatchStatusCancelling BatchStatus = "cancelling"
	// BatchStatusCancelled indicates the batch was cancelled by the caller.
	BatchStatusCancelled BatchStatus = "cancelled"
)

// Batcher is implemented by LLM providers that support asynchronous batch processing.
// Callers obtain a Batcher by type-asserting against a concrete provider's *LLM:
//
//	if b, ok := llm.(llms.Batcher); ok {
//	    handle, err := b.SubmitBatch(ctx, requests)
//	    // ... persist handle.ID, poll GetBatch from a later process, FetchBatchResults when terminal.
//	}
//
// Implementations accept the same Messages + CallOptions shape as
// Model.GenerateContent, so building batch inputs reuses existing call sites.
//
// The interface is intentionally async-only: callers own the polling loop. This
// keeps the contract small and lets callers persist BatchHandle.ID to track a
// batch across process boundaries.
type Batcher interface {
	// SubmitBatch enqueues a set of requests for asynchronous processing and
	// returns a handle that callers persist to track the batch across processes.
	SubmitBatch(ctx context.Context, requests []BatchRequest, options ...BatchSubmitOption) (*BatchHandle, error)
	// GetBatch fetches the current state of a submitted batch. Callers poll this
	// until Status.IsTerminal() returns true.
	GetBatch(ctx context.Context, batchID string) (*BatchHandle, error)
	// CancelBatch requests cancellation of an in-flight batch. The returned
	// handle reflects the updated status (typically Cancelling, then Cancelled).
	CancelBatch(ctx context.Context, batchID string) (*BatchHandle, error)
	// FetchBatchResults returns all results for a terminal batch. It returns
	// ErrBatchNotReady if the batch has not yet reached a terminal state.
	// Per-request failures are surfaced on BatchResult.Error rather than as a
	// returned error, so callers can process partial successes.
	FetchBatchResults(ctx context.Context, batchID string) ([]BatchResult, error)
}

// IsTerminal reports whether the batch has reached a state that will not transition
// further. Callers polling GetBatch should stop once IsTerminal returns true.
func (s BatchStatus) IsTerminal() bool {
	switch s {
	case BatchStatusCompleted, BatchStatusFailed, BatchStatusExpired, BatchStatusCancelled:
		return true
	}
	return false
}

// BatchRequest is a single request submitted as part of a batch. The Messages and
// Options fields use the same types as Model.GenerateContent so callers can build
// batch inputs from the same code paths they use for synchronous calls.
type BatchRequest struct {
	// CustomID is a caller-supplied identifier used to correlate this request to its
	// result. It must be unique within the batch and is echoed back on each BatchResult.
	CustomID string
	// Messages is the input message sequence, identical to Model.GenerateContent.
	Messages []Message
	// Options are the per-request CallOptions, identical to Model.GenerateContent.
	Options []CallOption
}

// BatchRequestCounts summarizes per-request progress within a batch.
type BatchRequestCounts struct {
	// Total is the number of requests in the batch.
	Total int
	// Completed is the number of requests that have finished successfully.
	Completed int
	// Failed is the number of requests that returned a per-request error.
	Failed int
}

// BatchHandle is a snapshot of a submitted batch returned by SubmitBatch, GetBatch,
// and CancelBatch. Callers persist ID across processes to reattach to a long-running
// batch with GetBatch.
type BatchHandle struct {
	// ID is the provider-assigned batch identifier.
	ID string
	// Provider is the LLM provider that owns this batch.
	Provider ProviderType
	// Status is the current lifecycle state.
	Status BatchStatus
	// RequestCounts summarizes per-request progress.
	RequestCounts BatchRequestCounts
	// CreatedAt is when the batch was submitted to the provider. It is nil if
	// the provider did not report a creation time.
	CreatedAt *time.Time
	// CompletedAt is when the batch reached a terminal state. It is nil while
	// the batch is still in a non-terminal state.
	CompletedAt *time.Time
	// ExpiresAt is the deadline by which the batch must complete. It is nil if
	// the provider did not report an expiration time.
	ExpiresAt *time.Time
	// Metadata mirrors any caller-supplied metadata round-tripped through the provider.
	Metadata map[string]string
	// ProviderMeta is the raw provider-specific batch object, kept as an opaque
	// value for callers that need fields this abstraction does not surface
	// (e.g. OpenAI's input_file_id / output_file_id / error_file_id).
	ProviderMeta any
}

// BatchResult is one entry from a completed batch. Exactly one of Response or Error
// is non-nil for a given result.
type BatchResult struct {
	// CustomID matches the CustomID supplied on the original BatchRequest.
	CustomID string
	// Response is the parsed model response. Nil when the per-request call errored.
	Response *ContentResponse
	// Error describes a per-request failure. Nil when the request succeeded.
	Error *BatchRequestError
}

// BatchRequestError describes a per-request error returned within a batch's results.
type BatchRequestError struct {
	// Code is the provider's machine-readable error code (e.g. "rate_limit_exceeded").
	Code string
	// Message is a human-readable error description.
	Message string
}

// Error implements the error interface so callers can return *BatchRequestError directly.
func (e *BatchRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// BatchSubmitOptions are the options accepted by Batcher.SubmitBatch.
type BatchSubmitOptions struct {
	// CompletionWindow is the maximum time the provider should hold the batch open.
	// Providers that only support a single window (e.g. OpenAI's "24h") ignore this
	// when it conflicts with their fixed value.
	CompletionWindow time.Duration
	// Metadata is round-tripped through the provider on the batch object.
	Metadata map[string]string
}

// BatchSubmitOption configures a single call to Batcher.SubmitBatch.
type BatchSubmitOption func(*BatchSubmitOptions)

// WithBatchCompletionWindow sets the maximum time the provider should hold the
// batch open. Providers that only support a fixed window ignore conflicting values.
func WithBatchCompletionWindow(d time.Duration) BatchSubmitOption {
	return func(o *BatchSubmitOptions) {
		o.CompletionWindow = d
	}
}

// WithBatchMetadata attaches caller-supplied metadata to the batch. The provider
// will round-trip these key/value pairs on the BatchHandle.
func WithBatchMetadata(m map[string]string) BatchSubmitOption {
	return func(o *BatchSubmitOptions) {
		o.Metadata = m
	}
}

// Errors returned by Batcher implementations.
var (
	// ErrBatchNotReady is returned by FetchBatchResults when the batch is not
	// yet in a terminal state.
	ErrBatchNotReady = errors.New("batch is not yet terminal")
	// ErrBatchNotSupported is returned when a provider or specific configuration
	// (e.g. an Azure deployment) does not support the Batch API.
	ErrBatchNotSupported = errors.New("provider does not support batching")
	// ErrBatchNotFound is returned by GetBatch / CancelBatch / FetchBatchResults
	// when the batch ID is unknown to the provider.
	ErrBatchNotFound = errors.New("batch not found")
)
