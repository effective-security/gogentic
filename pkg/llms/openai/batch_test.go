package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/invopop/jsonschema"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBatchServer is a minimal stand-in for the OpenAI Files + Batches API,
// sufficient to exercise the SubmitBatch / GetBatch / CancelBatch /
// FetchBatchResults lifecycle.
type fakeBatchServer struct {
	t  *testing.T
	mu sync.Mutex

	files     map[string][]byte
	batches   map[string]map[string]any
	nextFile  int
	nextBatch int

	uploadedJSONL []byte // last uploaded file content
}

func newFakeBatchServer(t *testing.T) *fakeBatchServer {
	return &fakeBatchServer{
		t:       t,
		files:   make(map[string][]byte),
		batches: make(map[string]map[string]any),
	}
}

func (s *fakeBatchServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/files", s.handleFilesNew)
	mux.HandleFunc("/v1/files/", s.handleFilesContent)
	mux.HandleFunc("/v1/batches", s.handleBatchesNew)
	mux.HandleFunc("/v1/batches/", s.handleBatchPath)
	return mux
}

func (s *fakeBatchServer) handleFilesNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = f.Close() }()
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.nextFile++
	id := fmt.Sprintf("file-%03d", s.nextFile)
	s.files[id] = body
	s.uploadedJSONL = body
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"object":     "file",
		"bytes":      len(body),
		"created_at": 1700000000,
		"filename":   "requests.jsonl",
		"purpose":    "batch",
		"status":     "processed",
	})
}

func (s *fakeBatchServer) handleFilesContent(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/content") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/files/"), "/content")
	s.mu.Lock()
	body, ok := s.files[id]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(body)
}

func (s *fakeBatchServer) handleBatchesNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		InputFileID      string            `json:"input_file_id"`
		Endpoint         string            `json:"endpoint"`
		CompletionWindow string            `json:"completion_window"`
		Metadata         map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.nextBatch++
	id := fmt.Sprintf("batch-%03d", s.nextBatch)
	obj := map[string]any{
		"id":                id,
		"object":            "batch",
		"endpoint":          body.Endpoint,
		"input_file_id":     body.InputFileID,
		"completion_window": body.CompletionWindow,
		"created_at":        1700000100,
		"status":            "validating",
		"request_counts": map[string]any{
			"total":     0,
			"completed": 0,
			"failed":    0,
		},
		"metadata": body.Metadata,
	}
	s.batches[id] = obj
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(obj)
}

func (s *fakeBatchServer) handleBatchPath(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/batches/")
	if strings.HasSuffix(id, "/cancel") {
		id = strings.TrimSuffix(id, "/cancel")
		s.mu.Lock()
		batch, ok := s.batches[id]
		if ok {
			batch["status"] = string(llms.BatchStatusCancelling)
		}
		s.mu.Unlock()
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(batch)
		return
	}
	s.mu.Lock()
	batch, ok := s.batches[id]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(batch)
}

// setBatchState mutates a batch's fields directly (for tests that need to
// simulate the API transitioning the batch to a particular state).
func (s *fakeBatchServer) setBatchState(id string, patch map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.batches[id]
	if !ok {
		s.t.Fatalf("setBatchState: batch %q not found", id)
	}
	for k, v := range patch {
		b[k] = v
	}
}

// addFile stores a synthetic file body the SDK can fetch via GET /v1/files/{id}/content.
func (s *fakeBatchServer) addFile(id string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[id] = body
}

func humanMsg(s string) llms.Message {
	return llms.Message{Role: llms.RoleHuman, Parts: []llms.ContentPart{llms.TextPart(s)}}
}

func newTestLLM(t *testing.T, baseURL string, provider llms.ProviderType) *LLM {
	t.Helper()
	llm, err := New(
		WithToken("test-token"),
		WithBaseURL(baseURL+"/v1"),
		WithModel("gpt-5-mini"),
		WithProvider(provider),
		WithHTTPClient(http.DefaultClient),
	)
	require.NoError(t, err)
	return llm
}

func TestSubmitBatch_HappyPath(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

	requests := []llms.BatchRequest{
		{
			CustomID: "r-1",
			Messages: []llms.Message{
				{Role: llms.RoleSystem, Parts: []llms.ContentPart{llms.TextPart("be brief")}},
				humanMsg("summarize hello"),
			},
		},
		{
			CustomID: "r-2",
			Messages: []llms.Message{
				humanMsg("summarize world"),
			},
		},
	}

	handle, err := llm.SubmitBatch(context.Background(), requests,
		llms.WithBatchMetadata(map[string]string{"job": "summaries"}))
	require.NoError(t, err)
	require.NotNil(t, handle)
	assert.NotEmpty(t, handle.ID)
	assert.Equal(t, llms.ProviderOpenAI, handle.Provider)
	assert.Equal(t, llms.BatchStatusValidating, handle.Status)
	assert.Equal(t, "summaries", handle.Metadata["job"])

	// The fake server captured the JSONL file we uploaded.
	require.NotEmpty(t, fs.uploadedJSONL)
	lines := strings.Split(strings.TrimRight(string(fs.uploadedJSONL), "\n"), "\n")
	require.Len(t, lines, 2)
	for i, line := range lines {
		var parsed struct {
			CustomID string          `json:"custom_id"`
			Method   string          `json:"method"`
			URL      string          `json:"url"`
			Body     json.RawMessage `json:"body"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &parsed))
		assert.Equal(t, requests[i].CustomID, parsed.CustomID)
		assert.Equal(t, "POST", parsed.Method)
		// OpenAI default routes to /v1/responses.
		assert.Equal(t, "/v1/responses", parsed.URL)
		assert.NotEmpty(t, parsed.Body, "request body must not be empty")
	}
}

func TestSubmitBatch_RejectsBadInput(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

	tests := []struct {
		name string
		reqs []llms.BatchRequest
		want string
	}{
		{name: "empty", reqs: nil, want: "requests must not be empty"},
		{
			name: "missing custom_id",
			reqs: []llms.BatchRequest{{Messages: []llms.Message{humanMsg("hi")}}},
			want: "CustomID is required",
		},
		{
			name: "duplicate custom_id",
			reqs: []llms.BatchRequest{
				{CustomID: "x", Messages: []llms.Message{humanMsg("a")}},
				{CustomID: "x", Messages: []llms.Message{humanMsg("b")}},
			},
			want: "duplicate CustomID",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := llm.SubmitBatch(context.Background(), tc.reqs)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSubmitBatch_AzureNotSupported(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	llm, err := New(
		WithToken("test-token"),
		WithBaseURL(srv.URL+"/v1"),
		WithModel("gpt-5-mini"),
		WithEmbeddingModel("text-embedding-3-small"),
		WithProvider(llms.ProviderAzure),
		WithAPIVersion("2024-12-01-preview"),
		WithHTTPClient(http.DefaultClient),
	)
	require.NoError(t, err)

	_, err = llm.SubmitBatch(context.Background(), []llms.BatchRequest{
		{CustomID: "r-1", Messages: []llms.Message{humanMsg("hi")}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, llms.ErrBatchNotSupported)
}

func TestGetBatch_StatusMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		apiStatus string
		want      llms.BatchStatus
	}{
		{"validating", llms.BatchStatusValidating},
		{"in_progress", llms.BatchStatusInProgress},
		{"finalizing", llms.BatchStatusFinalizing},
		{"completed", llms.BatchStatusCompleted},
		{"failed", llms.BatchStatusFailed},
		{"expired", llms.BatchStatusExpired},
		{"cancelling", llms.BatchStatusCancelling},
		{"cancelled", llms.BatchStatusCancelled},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.apiStatus, func(t *testing.T) {
			t.Parallel()
			fs := newFakeBatchServer(t)
			srv := httptest.NewServer(fs.handler())
			defer srv.Close()
			llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

			handle, err := llm.SubmitBatch(context.Background(), []llms.BatchRequest{
				{CustomID: "r-1", Messages: []llms.Message{humanMsg("hi")}},
			})
			require.NoError(t, err)
			fs.setBatchState(handle.ID, map[string]any{"status": tc.apiStatus})

			got, err := llm.GetBatch(context.Background(), handle.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Status)
		})
	}
}

func TestFetchBatchResults_NotReady(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

	handle, err := llm.SubmitBatch(context.Background(), []llms.BatchRequest{
		{CustomID: "r-1", Messages: []llms.Message{humanMsg("hi")}},
	})
	require.NoError(t, err)
	// status is "validating" — not terminal

	_, err = llm.FetchBatchResults(context.Background(), handle.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, llms.ErrBatchNotReady)
}

func TestFetchBatchResults_ResponsesEndpoint(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

	handle, err := llm.SubmitBatch(context.Background(), []llms.BatchRequest{
		{CustomID: "r-ok", Messages: []llms.Message{humanMsg("say hi")}},
		{CustomID: "r-err", Messages: []llms.Message{humanMsg("fail")}},
		{CustomID: "r-http", Messages: []llms.Message{humanMsg("rate")}},
	})
	require.NoError(t, err)

	// Build a synthetic batch output file with one success, one inline-error,
	// and one top-level error row.
	outputBody := strings.Join([]string{
		// Success — body is a Responses API object with an output_text item.
		`{"id":"row-1","custom_id":"r-ok","response":{"status_code":200,"request_id":"req-1","body":{"id":"resp_1","object":"response","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi there"}]}],"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}},"error":null}`,
		// Top-level error row.
		`{"id":"row-3","custom_id":"r-err","response":null,"error":{"code":"server_error","message":"unavailable"}}`,
		// HTTP-level error (non-2xx) carried inside response.body.
		`{"id":"row-4","custom_id":"r-http","response":{"status_code":429,"request_id":"req-4","body":{"error":{"code":"rate_limit_exceeded","message":"slow down"}}},"error":null}`,
	}, "\n")

	fs.addFile("file-output", []byte(outputBody))
	fs.setBatchState(handle.ID, map[string]any{
		"status":         "completed",
		"output_file_id": "file-output",
		"completed_at":   1700000200,
	})

	results, err := llm.FetchBatchResults(context.Background(), handle.ID)
	require.NoError(t, err)
	require.Len(t, results, 3)

	byID := map[string]llms.BatchResult{}
	for _, r := range results {
		byID[r.CustomID] = r
	}

	ok := byID["r-ok"]
	require.NotNil(t, ok.Response)
	require.Len(t, ok.Response.Choices, 1)
	assert.Equal(t, "hi there", ok.Response.Choices[0].Content)
	assert.Nil(t, ok.Error)

	errRow := byID["r-err"]
	require.NotNil(t, errRow.Error)
	assert.Equal(t, "server_error", errRow.Error.Code)
	assert.Equal(t, "unavailable", errRow.Error.Message)
	assert.Nil(t, errRow.Response)

	httpRow := byID["r-http"]
	require.NotNil(t, httpRow.Error)
	assert.Equal(t, "rate_limit_exceeded", httpRow.Error.Code)
	assert.Equal(t, "slow down", httpRow.Error.Message)
	assert.Nil(t, httpRow.Response)
}

func TestCancelBatch(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()
	llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

	handle, err := llm.SubmitBatch(context.Background(), []llms.BatchRequest{
		{CustomID: "r-1", Messages: []llms.Message{humanMsg("hi")}},
	})
	require.NoError(t, err)

	got, err := llm.CancelBatch(context.Background(), handle.ID)
	require.NoError(t, err)
	assert.Equal(t, llms.BatchStatusCancelling, got.Status)
}

func TestBuildBatchLine_ChatEndpoint(t *testing.T) {
	t.Parallel()

	llm, err := New(
		WithToken("test-token"),
		WithBaseURL("http://example.test/v1"),
		WithModel("gpt-4o-mini"),
		// Azure-with-old-apiVersion is the only path that yields chat-completions routing,
		// but the buildBatchLine method itself accepts the endpoint as input, so we can
		// test the chat path directly without depending on SupportsResponsesAPI.
		WithProvider(llms.ProviderOpenAI),
		WithHTTPClient(http.DefaultClient),
	)
	require.NoError(t, err)

	r := llms.BatchRequest{
		CustomID: "r-chat",
		Messages: []llms.Message{
			{Role: llms.RoleSystem, Parts: []llms.ContentPart{llms.TextPart("be brief")}},
			humanMsg("hello"),
		},
	}
	line, err := llm.buildBatchLine(r, openaiclient.EndpointChatCompletions)
	require.NoError(t, err)

	var parsed struct {
		CustomID string `json:"custom_id"`
		Method   string `json:"method"`
		URL      string `json:"url"`
		Body     struct {
			Model    string `json:"model"`
			Messages []any  `json:"messages"`
		} `json:"body"`
	}
	require.NoError(t, json.Unmarshal(line, &parsed))
	assert.Equal(t, "r-chat", parsed.CustomID)
	assert.Equal(t, "POST", parsed.Method)
	assert.Equal(t, "/v1/chat/completions", parsed.URL)
	assert.Equal(t, "gpt-4o-mini", parsed.Body.Model)
	assert.Len(t, parsed.Body.Messages, 2)
}

func TestCompletionWindowString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "zero defaults to 24h", in: 0, want: "24h"},
		{name: "negative defaults to 24h", in: -time.Hour, want: "24h"},
		{name: "exact hour", in: time.Hour, want: "1h"},
		{name: "exact 24h", in: 24 * time.Hour, want: "24h"},
		{name: "rounds sub-hour up", in: 30 * time.Minute, want: "1h"},
		{name: "rounds seconds up", in: time.Hour + time.Second, want: "2h"},
		{name: "rounds minutes up", in: 90 * time.Minute, want: "2h"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, completionWindowString(tc.in))
		})
	}
}

const (
	batchOutputFile = "file-output"
	batchErrorFile  = "file-errors"
	missingFile     = "file-missing"
	unknownBatchID  = "batch-unknown"
)

// newBatchFixture starts a fake batch server with one submitted batch and
// returns the server, the LLM pointed at it and the batch ID.
func newBatchFixture(t *testing.T, opts ...Option) (*fakeBatchServer, *LLM, string) {
	t.Helper()
	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	t.Cleanup(srv.Close)

	opts = append([]Option{
		WithToken("test-token"),
		WithBaseURL(srv.URL + "/v1"),
		WithModel("gpt-5-mini"),
		WithProvider(llms.ProviderOpenAI),
	}, opts...)
	llm, err := New(opts...)
	require.NoError(t, err)

	handle, err := llm.SubmitBatch(context.Background(), []llms.BatchRequest{
		{CustomID: "r-1", Messages: []llms.Message{humanMsg("hi")}},
	})
	require.NoError(t, err)
	return fs, llm, handle.ID
}

// badToolMessage is a tool message that neither request builder accepts.
func badToolMessage() llms.Message {
	return llms.MessageFromTextParts(llms.RoleTool, "not a tool response")
}

func TestSubmitBatch_BuildLineError(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	srv := httptest.NewServer(fs.handler())
	t.Cleanup(srv.Close)
	llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

	_, err := llm.SubmitBatch(context.Background(), []llms.BatchRequest{
		{CustomID: "r-1", Messages: []llms.Message{badToolMessage()}},
	})
	require.EqualError(t, err, "failed to build batch line for request: r-1: expected ToolCallResponse for tool role, got llms.TextContent")
	assert.Empty(t, fs.uploadedJSONL, "nothing is uploaded when a line cannot be built")
}

func TestSubmitBatch_CreateBatchError(t *testing.T) {
	t.Parallel()

	fs := newFakeBatchServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/files", fs.handleFilesNew)
	mux.HandleFunc("/v1/batches", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte(`{"error":{"message":"window not allowed","type":"invalid_request_error"}}`))
		assert.NoError(t, err)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	llm := newTestLLM(t, srv.URL, llms.ProviderOpenAI)

	_, err := llm.SubmitBatch(context.Background(), []llms.BatchRequest{
		{CustomID: "r-1", Messages: []llms.Message{humanMsg("hi")}},
	})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "failed to create batch: "), err.Error())
	var apiErr *openaisdk.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.NotEmpty(t, fs.uploadedJSONL, "the input file was uploaded before the batch failed")
}

func TestBatch_NotFound(t *testing.T) {
	t.Parallel()

	_, llm, _ := newBatchFixture(t)
	ctx := context.Background()

	_, err := llm.GetBatch(ctx, unknownBatchID)
	assert.ErrorIs(t, err, llms.ErrBatchNotFound)
	_, err = llm.CancelBatch(ctx, unknownBatchID)
	assert.ErrorIs(t, err, llms.ErrBatchNotFound)
	_, err = llm.FetchBatchResults(ctx, unknownBatchID)
	assert.ErrorIs(t, err, llms.ErrBatchNotFound)
}

func TestBatchError(t *testing.T) {
	t.Parallel()

	assert.NoError(t, batchError(nil))
	assert.ErrorIs(t, batchError(openaiclient.ErrBatchUnsupported()), llms.ErrBatchNotSupported)

	other := errors.New("other failure")
	assert.True(t, batchError(other) == other, "an error without a neutral equivalent is returned unchanged")

	badRequest := errors.Wrap(&openaisdk.Error{StatusCode: http.StatusBadRequest}, "failed to get batch")
	assert.True(t, batchError(badRequest) == badRequest, "a non-404 API error is returned unchanged")
}

func TestFetchBatchResults_ChatEndpointWithErrorFile(t *testing.T) {
	t.Parallel()

	fs, llm, id := newBatchFixture(t)

	output := strings.Join([]string{
		`{"id":"row-1","custom_id":"r-chat","response":{"status_code":200,"request_id":"req-1","body":{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"},{"index":1,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8,"completion_tokens_details":{"reasoning_tokens":1}}}},"error":null}`,
		"",
		"   ",
		`{"id":"row-2","custom_id":"r-500","response":{"status_code":500,"request_id":"req-2","body":"upstream exploded"},"error":null}`,
	}, "\n")
	errorsFile := `{"id":"row-3","custom_id":"r-empty","response":null,"error":null}` + "\n"
	fs.addFile(batchOutputFile, []byte(output))
	fs.addFile(batchErrorFile, []byte(errorsFile))
	fs.setBatchState(id, map[string]any{
		"status":         "completed",
		"endpoint":       string(openaiclient.EndpointChatCompletions),
		"output_file_id": batchOutputFile,
		"error_file_id":  batchErrorFile,
	})

	results, err := llm.FetchBatchResults(context.Background(), id)
	require.NoError(t, err)

	usage := llms.Usage{
		OutputTokens:    3,
		InputTokens:     5,
		TotalTokens:     8,
		ReasoningTokens: 1,
	}
	call := &llms.FunctionCall{
		Name:      "lookup",
		Arguments: "{}",
	}
	assert.Equal(t, []llms.BatchResult{
		{
			CustomID: "r-chat",
			Response: &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content:    "hi there",
						StopReason: "stop",
						Usage:      usage,
					},
					{
						StopReason: "tool_calls",
						Usage:      usage,
						FuncCall:   call,
						ToolCalls: []llms.ToolCall{
							{
								ID:           "call_1",
								Type:         "function",
								FunctionCall: call,
							},
						},
					},
				},
			},
		},
		{
			CustomID: "r-500",
			Error: &llms.BatchRequestError{
				Code:    "http_500",
				Message: `"upstream exploded"`,
			},
		},
		{
			CustomID: "r-empty",
			Error: &llms.BatchRequestError{
				Message: "no response body and no error",
			},
		},
	}, results)
}

func TestFetchBatchResults_FileErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		state      map[string]any
		files      map[string]string
		wantPrefix string
	}{
		{
			name: "missing output file",
			state: map[string]any{
				"status":         "completed",
				"output_file_id": missingFile,
			},
			wantPrefix: "failed to read batch output file " + missingFile + ": failed to download file content: ",
		},
		{
			name: "missing error file",
			state: map[string]any{
				"status":        "failed",
				"error_file_id": missingFile,
			},
			wantPrefix: "failed to read batch error file " + missingFile + ": failed to download file content: ",
		},
		{
			name: "malformed output line",
			state: map[string]any{
				"status":         "completed",
				"output_file_id": batchOutputFile,
			},
			files: map[string]string{
				batchOutputFile: "{not json\n",
			},
			wantPrefix: "failed to read batch output file " + batchOutputFile + ": failed to unmarshal batch output line: ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs, llm, id := newBatchFixture(t)
			for fileID, body := range tc.files {
				fs.addFile(fileID, []byte(body))
			}
			fs.setBatchState(id, tc.state)

			_, err := llm.FetchBatchResults(context.Background(), id)
			require.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), tc.wantPrefix), err.Error())
		})
	}
}

// errReadCloser fails every read.
type errReadCloser struct{}

var errBodyRead = errors.New("body read failed")

func (errReadCloser) Read([]byte) (int, error) { return 0, errBodyRead }
func (errReadCloser) Close() error             { return nil }

// brokenDownloadDoer forwards requests to the fake server but replaces the
// body of every file download with one that fails to read.
type brokenDownloadDoer struct{}

func (brokenDownloadDoer) Do(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultClient.Do(r)
	if err != nil || !strings.HasSuffix(r.URL.Path, "/content") {
		return resp, err
	}
	_ = resp.Body.Close()
	resp.Body = errReadCloser{}
	return resp, nil
}

func TestFetchBatchResults_ScanError(t *testing.T) {
	t.Parallel()

	fs, llm, id := newBatchFixture(t, WithHTTPClient(brokenDownloadDoer{}))
	fs.addFile(batchOutputFile, []byte(`{"custom_id":"r-1"}`))
	fs.setBatchState(id, map[string]any{
		"status":         "completed",
		"output_file_id": batchOutputFile,
	})

	_, err := llm.FetchBatchResults(context.Background(), id)
	require.ErrorIs(t, err, errBodyRead)
	assert.EqualError(t, err, "failed to read batch output file "+batchOutputFile+": failed to scan batch file: "+errBodyRead.Error())
}

func TestFetchBatchResults_TerminalWithoutFiles(t *testing.T) {
	t.Parallel()

	fs, llm, id := newBatchFixture(t)
	fs.setBatchState(id, map[string]any{
		"status":       "expired",
		"expired_at":   1700000300,
		"cancelled_at": 1700000400,
	})

	results, err := llm.FetchBatchResults(context.Background(), id)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestParseBatchOutputLine_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		line       string
		endpoint   openaiclient.BatchEndpoint
		wantPrefix string
	}{
		{
			name:       "chat body",
			line:       `{"custom_id":"c1","response":{"status_code":200,"body":{"choices":"nope"}}}`,
			endpoint:   openaiclient.EndpointChatCompletions,
			wantPrefix: `failed to parse response body for custom_id "c1": failed to unmarshal chat completion body: `,
		},
		{
			name:       "responses body",
			line:       `{"custom_id":"r1","response":{"status_code":200,"body":"nope"}}`,
			endpoint:   openaiclient.EndpointResponses,
			wantPrefix: `failed to parse response body for custom_id "r1": failed to unmarshal responses body: `,
		},
		{
			name:       "unsupported endpoint",
			line:       `{"custom_id":"e1","response":{"status_code":200,"body":{}}}`,
			endpoint:   "/v1/embeddings",
			wantPrefix: `failed to parse response body for custom_id "e1": unsupported batch endpoint "/v1/embeddings"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseBatchOutputLine([]byte(tc.line), tc.endpoint)
			require.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), tc.wantPrefix), err.Error())
			assert.Equal(t, llms.BatchResult{}, got)
		})
	}
}

func TestParseBatchOutputLine_ResponsesToolCalls(t *testing.T) {
	t.Parallel()

	const line = `{"custom_id":"r-tools","response":{"status_code":200,"body":{"id":"resp_1","object":"response","output":[` +
		`{"id":"fc_1","type":"function_call","call_id":"","name":"lookup","arguments":"{}"},` +
		`{"id":"fc_2","type":"function_call","call_id":"call_2","name":"other","arguments":"{\"a\":1}"}` +
		`],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6,"input_tokens_details":{"cached_tokens":1},"output_tokens_details":{"reasoning_tokens":1}}}}}`

	got, err := parseBatchOutputLine([]byte(line), openaiclient.EndpointResponses)
	require.NoError(t, err)
	first := &llms.FunctionCall{
		Name:      "lookup",
		Arguments: "{}",
	}
	assert.Equal(t, llms.BatchResult{
		CustomID: "r-tools",
		Response: &llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					Usage: llms.Usage{
						OutputTokens:    2,
						InputTokens:     4,
						CacheReadTokens: 1,
						TotalTokens:     6,
						ReasoningTokens: 1,
					},
					FuncCall: first,
					ToolCalls: []llms.ToolCall{
						{
							ID:           "fc_1",
							Type:         "function",
							FunctionCall: first,
						},
						{
							ID:   "call_2",
							Type: "function",
							FunctionCall: &llms.FunctionCall{
								Name:      "other",
								Arguments: `{"a":1}`,
							},
						},
					},
				},
			},
		},
	}, got)
}

func TestBuildBatchLine_Errors(t *testing.T) {
	t.Parallel()

	llm := newTestLLM(t, "http://example.test", llms.ProviderOpenAI)
	unmarshalableTool := llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name: "bad",
			Parameters: &jsonschema.Schema{
				Type:    "object",
				Default: func() {},
			},
		},
	}
	tests := []struct {
		name       string
		req        llms.BatchRequest
		endpoint   openaiclient.BatchEndpoint
		wantErr    string
		wantPrefix string
	}{
		{
			name: "missing custom id",
			req: llms.BatchRequest{
				Messages: []llms.Message{humanMsg("hi")},
			},
			endpoint: openaiclient.EndpointResponses,
			wantErr:  "invalid argument: BatchRequest.CustomID is required",
		},
		{
			name: "responses build",
			req: llms.BatchRequest{
				CustomID: "r",
				Messages: []llms.Message{badToolMessage()},
			},
			endpoint: openaiclient.EndpointResponses,
			wantErr:  "expected ToolCallResponse for tool role, got llms.TextContent",
		},
		{
			name: "chat build",
			req: llms.BatchRequest{
				CustomID: "c",
				Messages: []llms.Message{badToolMessage()},
			},
			endpoint: openaiclient.EndpointChatCompletions,
			wantErr:  "expected part of type ToolCallResponse for role tool, got llms.TextContent",
		},
		{
			name: "chat marshal",
			req: llms.BatchRequest{
				CustomID: "c",
				Messages: []llms.Message{humanMsg("hi")},
				Options: []llms.CallOption{
					llms.WithTools([]llms.Tool{unmarshalableTool}),
				},
			},
			endpoint:   openaiclient.EndpointChatCompletions,
			wantPrefix: "failed to marshal chat body: ",
		},
		{
			name: "unsupported endpoint",
			req: llms.BatchRequest{
				CustomID: "e",
				Messages: []llms.Message{humanMsg("hi")},
			},
			endpoint: "/v1/embeddings",
			wantErr:  `unsupported batch endpoint "/v1/embeddings"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line, err := llm.buildBatchLine(tc.req, tc.endpoint)
			require.Error(t, err)
			assert.Nil(t, line)
			if tc.wantErr != "" {
				assert.EqualError(t, err, tc.wantErr)
			}
			if tc.wantPrefix != "" {
				assert.True(t, strings.HasPrefix(err.Error(), tc.wantPrefix), err.Error())
			}
		})
	}
}

func TestBatchHandleFromSDK(t *testing.T) {
	t.Parallel()

	assert.Nil(t, batchHandleFromSDK(nil))

	at := func(sec int64) *time.Time {
		v := time.Unix(sec, 0).UTC()
		return &v
	}
	tests := []struct {
		name          string
		batch         openaisdk.Batch
		wantCompleted *time.Time
	}{
		{
			name: "failed",
			batch: openaisdk.Batch{
				FailedAt:    200,
				ExpiredAt:   300,
				CancelledAt: 400,
			},
			wantCompleted: at(200),
		},
		{
			name: "expired",
			batch: openaisdk.Batch{
				ExpiredAt:   300,
				CancelledAt: 400,
			},
			wantCompleted: at(300),
		},
		{
			name: "cancelled",
			batch: openaisdk.Batch{
				CancelledAt: 400,
			},
			wantCompleted: at(400),
		},
		{
			name: "still running",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := tc.batch
			h := batchHandleFromSDK(&b)
			require.NotNil(t, h)
			assert.Equal(t, tc.wantCompleted, h.CompletedAt)
			assert.Nil(t, h.CreatedAt)
			assert.Nil(t, h.ExpiresAt)
			assert.Nil(t, h.Metadata)
		})
	}

	b := &openaisdk.Batch{
		ID:          "batch-x",
		Status:      openaisdk.BatchStatusCompleted,
		CreatedAt:   100,
		CompletedAt: 150,
		FailedAt:    160,
		ExpiresAt:   500,
		Metadata: map[string]string{
			"job": "nightly",
		},
		RequestCounts: openaisdk.BatchRequestCounts{
			Total:     3,
			Completed: 2,
			Failed:    1,
		},
	}
	assert.Equal(t, &llms.BatchHandle{
		ID:       "batch-x",
		Provider: llms.ProviderOpenAI,
		Status:   llms.BatchStatusCompleted,
		RequestCounts: llms.BatchRequestCounts{
			Total:     3,
			Completed: 2,
			Failed:    1,
		},
		CreatedAt:   at(100),
		CompletedAt: at(150),
		ExpiresAt:   at(500),
		Metadata: map[string]string{
			"job": "nightly",
		},
		ProviderMeta: b,
	}, batchHandleFromSDK(b))
}

func TestBatchStatusFromSDK_Unknown(t *testing.T) {
	t.Parallel()

	assert.Equal(t, llms.BatchStatus("paused"), batchStatusFromSDK(openaisdk.BatchStatus("paused")))
}
