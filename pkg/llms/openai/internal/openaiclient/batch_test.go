package openaiclient

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBatchID    = "batch_1"
	testFileID     = "file_1"
	testBatchJSONL = `{"custom_id":"r-1"}` + "\n"
	batchReply     = `{"id":"batch_1","object":"batch","endpoint":"/v1/responses","input_file_id":"file_1","completion_window":"24h","status":"validating","created_at":1700000000,"request_counts":{"total":1,"completed":0,"failed":0}}`
	fileReply      = `{"id":"file_1","object":"file","bytes":20,"created_at":1700000000,"filename":"requests.jsonl","purpose":"batch","status":"processed"}`
	badRequestJSON = `{"error":{"message":"bad batch","type":"invalid_request_error","code":"invalid"}}`
)

func TestBatch_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, provider := range []ProviderType{ProviderAzure, ProviderAzureAD, ProviderOpenRouter, ProviderOpenAIBedrock} {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()
			c := newTestClient(t, provider, testModel, "", failingDoer())

			_, err := c.UploadBatchFile(ctx, "requests.jsonl", strings.NewReader(testBatchJSONL))
			assert.ErrorIs(t, err, ErrBatchUnsupported())
			_, err = c.CreateBatch(ctx, BatchCreateParams{
				InputFileID: testFileID,
				Endpoint:    EndpointResponses,
			})
			assert.ErrorIs(t, err, ErrBatchUnsupported())
			_, err = c.GetBatch(ctx, testBatchID)
			assert.ErrorIs(t, err, ErrBatchUnsupported())
			_, err = c.CancelBatch(ctx, testBatchID)
			assert.ErrorIs(t, err, ErrBatchUnsupported())
			_, err = c.DownloadFile(ctx, testFileID)
			assert.ErrorIs(t, err, ErrBatchUnsupported())
		})
	}
}

func TestBatch_InvalidArguments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())

	_, err := c.CreateBatch(ctx, BatchCreateParams{
		Endpoint: EndpointResponses,
	})
	assert.EqualError(t, err, "invalid argument: InputFileID is required")
	_, err = c.CreateBatch(ctx, BatchCreateParams{
		InputFileID: testFileID,
	})
	assert.EqualError(t, err, "invalid argument: Endpoint is required")
	_, err = c.GetBatch(ctx, "")
	assert.EqualError(t, err, "invalid argument: batchID is required")
	_, err = c.CancelBatch(ctx, "")
	assert.EqualError(t, err, "invalid argument: batchID is required")
	_, err = c.DownloadFile(ctx, "")
	assert.EqualError(t, err, "invalid argument: fileID is required")
}

func TestUploadBatchFile(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, fileReply)
	c := newTestClient(t, "OPEN_AI", testModel, srv.URL, http.DefaultClient)

	id, err := c.UploadBatchFile(context.Background(), "requests.jsonl", strings.NewReader(testBatchJSONL))
	require.NoError(t, err)
	assert.Equal(t, testFileID, id)

	got := stub.last()
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/files", got.path)
	assert.Equal(t, bearerTestToken, got.header.Get(headerAuthorization))

	mediaType, params, err := mime.ParseMediaType(got.header.Get(headerContentType))
	require.NoError(t, err)
	assert.Equal(t, "multipart/form-data", mediaType)
	form, err := multipart.NewReader(strings.NewReader(string(got.body)), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	assert.Equal(t, []string{"batch"}, form.Value["purpose"])
	require.Len(t, form.File["file"], 1)
	assert.Equal(t, "requests.jsonl", form.File["file"][0].Filename)
	f, err := form.File["file"][0].Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	content, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, testBatchJSONL, string(content))
}

func TestCreateBatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params BatchCreateParams
		want   map[string]any
	}{
		{
			name: "default window with metadata",
			params: BatchCreateParams{
				InputFileID: testFileID,
				Endpoint:    EndpointResponses,
				Metadata: map[string]string{
					"job": "nightly",
				},
			},
			want: map[string]any{
				"input_file_id":     testFileID,
				"endpoint":          string(EndpointResponses),
				"completion_window": "24h",
				"metadata": map[string]any{
					"job": "nightly",
				},
			},
		},
		{
			name: "explicit window without metadata",
			params: BatchCreateParams{
				InputFileID:      testFileID,
				Endpoint:         EndpointChatCompletions,
				CompletionWindow: "48h",
			},
			want: map[string]any{
				"input_file_id":     testFileID,
				"endpoint":          string(EndpointChatCompletions),
				"completion_window": "48h",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, http.StatusOK, batchReply)
			c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

			batch, err := c.CreateBatch(context.Background(), tc.params)
			require.NoError(t, err)
			assert.Equal(t, testBatchID, batch.ID)
			assert.Equal(t, openaisdk.BatchStatusValidating, batch.Status)

			got := stub.last()
			assert.Equal(t, http.MethodPost, got.method)
			assert.Equal(t, "/batches", got.path)
			assert.Equal(t, tc.want, stub.lastJSON())
		})
	}
}

func TestGetAndCancelBatch(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, batchReply)
	c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

	batch, err := c.GetBatch(context.Background(), testBatchID)
	require.NoError(t, err)
	assert.Equal(t, testBatchID, batch.ID)
	got := stub.last()
	assert.Equal(t, http.MethodGet, got.method)
	assert.Equal(t, "/batches/"+testBatchID, got.path)

	batch, err = c.CancelBatch(context.Background(), testBatchID)
	require.NoError(t, err)
	assert.Equal(t, testBatchID, batch.ID)
	got = stub.last()
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "/batches/"+testBatchID+"/cancel", got.path)
}

func TestDownloadFile(t *testing.T) {
	t.Parallel()

	t.Run("content", func(t *testing.T) {
		t.Parallel()
		stub, srv := newStubServer(t, http.StatusOK, testBatchJSONL)
		stub.setHeaders(map[string]string{headerContentType: "application/octet-stream"})
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

		rc, err := c.DownloadFile(context.Background(), testFileID)
		require.NoError(t, err)
		t.Cleanup(func() { _ = rc.Close() })
		content, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, testBatchJSONL, string(content))
		got := stub.last()
		assert.Equal(t, http.MethodGet, got.method)
		assert.Equal(t, "/files/"+testFileID+"/content", got.path)
	})

	t.Run("non-200 success status", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusAccepted, "")
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

		_, err := c.DownloadFile(context.Background(), testFileID)
		assert.EqualError(t, err, "failed to download file content: unexpected status 202")
	})
}

func TestBatch_UpstreamErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tests := []struct {
		name    string
		call    func(c *Client) error
		wantMsg string
	}{
		{
			name: "upload",
			call: func(c *Client) error {
				_, err := c.UploadBatchFile(ctx, "requests.jsonl", strings.NewReader(testBatchJSONL))
				return err
			},
			wantMsg: "failed to upload batch input file",
		},
		{
			name: "create",
			call: func(c *Client) error {
				_, err := c.CreateBatch(ctx, BatchCreateParams{
					InputFileID: testFileID,
					Endpoint:    EndpointResponses,
				})
				return err
			},
			wantMsg: "failed to create batch",
		},
		{
			name: "get",
			call: func(c *Client) error {
				_, err := c.GetBatch(ctx, testBatchID)
				return err
			},
			wantMsg: "failed to get batch",
		},
		{
			name: "cancel",
			call: func(c *Client) error {
				_, err := c.CancelBatch(ctx, testBatchID)
				return err
			},
			wantMsg: "failed to cancel batch",
		},
		{
			name: "download",
			call: func(c *Client) error {
				_, err := c.DownloadFile(ctx, testFileID)
				return err
			},
			wantMsg: "failed to download file content",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// 400 is not retried by the SDK, so exactly one request is sent.
			stub, srv := newStubServer(t, http.StatusBadRequest, badRequestJSON)
			c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

			err := tc.call(c)
			require.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), tc.wantMsg+": "), err.Error())
			var apiErr *openaisdk.Error
			require.True(t, errors.As(err, &apiErr), "the SDK error is preserved")
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Equal(t, "bad batch", apiErr.Message)
			assert.Equal(t, 1, stub.count())
		})
	}
}
