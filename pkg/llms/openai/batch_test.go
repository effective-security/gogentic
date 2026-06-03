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

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
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

func newTestLLM(t *testing.T, baseURL string, provider ProviderType) *LLM {
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

	llm := newTestLLM(t, srv.URL, ProviderOpenAI)

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
	llm := newTestLLM(t, srv.URL, ProviderOpenAI)

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
		WithProvider(ProviderAzure),
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
			llm := newTestLLM(t, srv.URL, ProviderOpenAI)

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
	llm := newTestLLM(t, srv.URL, ProviderOpenAI)

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
	llm := newTestLLM(t, srv.URL, ProviderOpenAI)

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
	llm := newTestLLM(t, srv.URL, ProviderOpenAI)

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
		WithProvider(ProviderOpenAI),
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
