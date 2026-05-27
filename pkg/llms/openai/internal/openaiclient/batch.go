package openaiclient

import (
	"context"
	"io"
	"net/http"

	"github.com/cockroachdb/errors"
	openaisdk "github.com/openai/openai-go/v3"
)

// BatchEndpoint is the OpenAI URL path used by every request in a batch.
type BatchEndpoint string

const (
	// EndpointChatCompletions targets the /v1/chat/completions endpoint.
	EndpointChatCompletions BatchEndpoint = "/v1/chat/completions"
	// EndpointResponses targets the /v1/responses endpoint.
	EndpointResponses BatchEndpoint = "/v1/responses"
)

// BatchCreateParams is the input to CreateBatch.
type BatchCreateParams struct {
	// InputFileID is the file_id returned by UploadBatchFile.
	InputFileID string
	// Endpoint is the OpenAI URL path used by every request in the batch.
	Endpoint BatchEndpoint
	// CompletionWindow is the maximum time the batch may run. Currently only
	// "24h" is accepted by the API; empty defaults to "24h".
	CompletionWindow string
	// Metadata is round-tripped through the API on the batch object.
	Metadata map[string]string
}

// UploadBatchFile uploads a JSONL file with purpose=batch and returns the file
// ID that should be passed to CreateBatch. The caller is responsible for
// formatting body as a JSONL document where each line is a valid batch request.
func (c *Client) UploadBatchFile(ctx context.Context, name string, body io.Reader) (string, error) {
	if !c.supportsBatchAPI() {
		return "", errors.WithStack(errBatchUnsupported)
	}
	sdk := c.sdkClient()
	file, err := sdk.Files.New(ctx, openaisdk.FileNewParams{
		File:    namedReader{Reader: body, name: name},
		Purpose: openaisdk.FilePurposeBatch,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to upload batch input file")
	}
	return file.ID, nil
}

// CreateBatch creates a new batch from a previously uploaded input file.
func (c *Client) CreateBatch(ctx context.Context, params BatchCreateParams) (*openaisdk.Batch, error) {
	if !c.supportsBatchAPI() {
		return nil, errors.WithStack(errBatchUnsupported)
	}
	if params.InputFileID == "" {
		return nil, errors.New("invalid argument: InputFileID is required")
	}
	if params.Endpoint == "" {
		return nil, errors.New("invalid argument: Endpoint is required")
	}
	window := params.CompletionWindow
	if window == "" {
		window = string(openaisdk.BatchNewParamsCompletionWindow24h)
	}
	sdkParams := openaisdk.BatchNewParams{
		InputFileID:      params.InputFileID,
		Endpoint:         openaisdk.BatchNewParamsEndpoint(params.Endpoint),
		CompletionWindow: openaisdk.BatchNewParamsCompletionWindow(window),
	}
	if len(params.Metadata) > 0 {
		sdkParams.Metadata = openaisdk.Metadata(params.Metadata)
	}
	batch, err := c.sdkClient().Batches.New(ctx, sdkParams)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create batch")
	}
	return batch, nil
}

// GetBatch retrieves the current state of a submitted batch.
func (c *Client) GetBatch(ctx context.Context, batchID string) (*openaisdk.Batch, error) {
	if !c.supportsBatchAPI() {
		return nil, errors.WithStack(errBatchUnsupported)
	}
	if batchID == "" {
		return nil, errors.New("invalid argument: batchID is required")
	}
	batch, err := c.sdkClient().Batches.Get(ctx, batchID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get batch")
	}
	return batch, nil
}

// CancelBatch requests cancellation of an in-flight batch.
func (c *Client) CancelBatch(ctx context.Context, batchID string) (*openaisdk.Batch, error) {
	if !c.supportsBatchAPI() {
		return nil, errors.WithStack(errBatchUnsupported)
	}
	if batchID == "" {
		return nil, errors.New("invalid argument: batchID is required")
	}
	batch, err := c.sdkClient().Batches.Cancel(ctx, batchID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to cancel batch")
	}
	return batch, nil
}

// DownloadFile streams the raw contents of a file (typically a batch output or
// error file). The caller is responsible for closing the returned reader.
func (c *Client) DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if !c.supportsBatchAPI() {
		return nil, errors.WithStack(errBatchUnsupported)
	}
	if fileID == "" {
		return nil, errors.New("invalid argument: fileID is required")
	}
	resp, err := c.sdkClient().Files.Content(ctx, fileID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to download file content")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errors.Errorf("failed to download file content: unexpected status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// supportsBatchAPI reports whether the configured provider supports the OpenAI
// Batch API. Azure deployments use a different surface and are not currently
// supported by these wrappers.
func (c *Client) supportsBatchAPI() bool {
	return c.Provider == ProviderOpenAI || c.Provider == "OPEN_AI"
}

// errBatchUnsupported is returned when batching is invoked on a provider whose
// surface does not support the OpenAI Batch API (e.g. Azure deployments).
var errBatchUnsupported = errors.New("batch API is not supported for this provider")

// ErrBatchUnsupported is the package-level sentinel exposed for upstream
// callers that want to detect "provider does not support batching".
func ErrBatchUnsupported() error { return errBatchUnsupported }

// namedReader pairs an io.Reader with a file name so multipart uploads can
// populate the filename field. The SDK's apiform marshaller respects this
// interface when the reader is passed as a file field.
type namedReader struct {
	io.Reader
	name string
}

func (n namedReader) Name() string { return n.name }
