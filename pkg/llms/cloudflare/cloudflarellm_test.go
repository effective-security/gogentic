package cloudflare

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/cloudflare/internal/cloudflareclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAccountID      = "acct-1"
	testToken          = "tok-1"
	testModel          = "@cf/meta/llama-3.1-8b-instruct"
	testEmbeddingModel = "@cf/baai/bge-small-en-v1.5"
	testSystemPrompt   = "You are terse."
	testServerURL      = "https://api.cloudflare.example/client/v4/accounts"

	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
	contentTypeSSE    = "text/event-stream"

	answerReply = `{"result":{"response":"answer"},"success":true}`
)

// capturedRequest is what the fake Workers AI server received.
type capturedRequest struct {
	Method        string
	Path          string
	Authorization string
	ContentType   string
	Body          string
}

// workersAI is an httptest handler playing the Cloudflare Workers AI run
// endpoint: it records every request and serves one canned reply.
type workersAI struct {
	status      int
	contentType string
	reply       string

	mu       sync.Mutex
	requests []capturedRequest
}

func (w *workersAI) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	w.mu.Lock()
	w.requests = append(w.requests, capturedRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		Authorization: r.Header.Get("Authorization"),
		ContentType:   r.Header.Get(headerContentType),
		Body:          string(body),
	})
	w.mu.Unlock()
	rw.Header().Set(headerContentType, w.contentType)
	rw.WriteHeader(w.status)
	_, _ = io.WriteString(rw, w.reply)
}

// received returns a copy of the requests recorded so far.
func (w *workersAI) received() []capturedRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.requests)
}

// newTestLLM starts a fake Workers AI server and returns an LLM pointed at it.
func newTestLLM(t *testing.T, status int, contentType, reply string, opts ...Option) (*workersAI, *LLM) {
	t.Helper()
	w := &workersAI{
		status:      status,
		contentType: contentType,
		reply:       reply,
	}
	srv := httptest.NewServer(w)
	t.Cleanup(srv.Close)

	opts = append([]Option{
		WithServerURL(srv.URL),
		WithAccountID(testAccountID),
		WithToken(testToken),
		WithModel(testModel),
		WithHTTPClient(srv.Client()),
	}, opts...)
	llm, err := New(opts...)
	require.NoError(t, err)
	return w, llm
}

// expectedRequest is the request the LLM sends to the testModel endpoint.
func expectedRequest(body string) capturedRequest {
	return capturedRequest{
		Method:        http.MethodPost,
		Path:          "/" + testAccountID + "/ai/run/" + testModel,
		Authorization: "Bearer " + testToken,
		ContentType:   contentTypeJSON,
		Body:          body,
	}
}

func human(text string) []llms.Message {
	return []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, text),
	}
}

func TestLLM_Identity(t *testing.T) {
	t.Parallel()

	_, llm := newTestLLM(t, http.StatusOK, contentTypeJSON, answerReply)
	assert.Equal(t, testModel, llm.GetName())
	assert.Equal(t, llms.ProviderCloudflare, llm.GetProviderType())
}

func TestLLM_GenerateContent(t *testing.T) {
	t.Parallel()

	w, llm := newTestLLM(t, http.StatusOK, contentTypeJSON, answerReply, WithSystemPrompt(testSystemPrompt))
	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
		llms.MessageFromTextParts(llms.RoleAI, "hello"),
		llms.MessageFromTextParts(llms.RoleGeneric, "generic"),
		llms.MessageFromTextParts(llms.RoleTool, "tool output"),
		llms.MessageFromTextParts(llms.RoleSystem, "inline system"),
	}

	resp, err := llm.GenerateContent(context.Background(), messages)
	require.NoError(t, err)
	assert.Equal(t, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "answer",
			},
		},
	}, resp)

	wantBody := `{"messages":[` +
		`{"role":"system","content":"You are terse."},` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":"hello"},` +
		`{"role":"user","content":"generic"},` +
		`{"role":"user","content":"tool output"},` +
		`{"role":"system","content":"inline system"}` +
		`],"stream":false}`
	assert.Equal(t, []capturedRequest{expectedRequest(wantBody)}, w.received())
}

func TestLLM_GenerateContent_Streaming(t *testing.T) {
	t.Parallel()

	reply := "data: {\"response\":\"Hel\"}\n\ndata: {\"response\":\"lo\"}\n\ndata: [DONE]\n\n"
	w, llm := newTestLLM(t, http.StatusOK, contentTypeSSE, reply)
	var chunks []string

	resp, err := llm.GenerateContent(context.Background(), human("hi"), llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
		chunks = append(chunks, string(chunk))
		return nil
	}))
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, []string{`{"response":"Hel"}`, `{"response":"lo"}`}, chunks)
	assert.Equal(t, []capturedRequest{
		expectedRequest(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, w.received())
}

func TestLLM_GenerateContent_UnsupportedParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		parts   []llms.ContentPart
		wantErr string
	}{
		{
			name: "two text parts",
			parts: []llms.ContentPart{
				llms.TextPart("one"),
				llms.TextPart("two"),
			},
			wantErr: "expecting a single Text content",
		},
		{
			name: "binary part",
			parts: []llms.ContentPart{
				llms.BinaryPart("image/png", []byte("png")),
			},
			wantErr: "only supports Text right now",
		},
		{
			name: "tool call part",
			parts: []llms.ContentPart{
				llms.ToolCall{
					ID: "call_1",
				},
			},
			wantErr: "only supports Text right now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, llm := newTestLLM(t, http.StatusOK, contentTypeJSON, answerReply)
			resp, err := llm.GenerateContent(context.Background(), []llms.Message{
				{
					Role:  llms.RoleHuman,
					Parts: tt.parts,
				},
			})
			require.EqualError(t, err, tt.wantErr)
			assert.Nil(t, resp)
			assert.Empty(t, w.received(), "nothing is sent")
		})
	}
}

func TestLLM_GenerateContent_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		reply   string
		wantErr string
	}{
		{
			name:    "error status",
			status:  http.StatusInternalServerError,
			reply:   `{"success":false}`,
			wantErr: `error: {"success":false}`,
		},
		{
			name:    "errors in reply",
			status:  http.StatusOK,
			reply:   `{"errors":[{"message":"model not found"},{"message":"second"}],"success":false}`,
			wantErr: "model not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, llm := newTestLLM(t, tt.status, contentTypeJSON, tt.reply)
			resp, err := llm.GenerateContent(context.Background(), human("hi"))
			require.EqualError(t, err, tt.wantErr)
			assert.Nil(t, resp)
			assert.Len(t, w.received(), 1)
		})
	}
}

func TestLLM_CreateEmbedding(t *testing.T) {
	t.Parallel()

	inputs := []string{"a", "b"}
	tests := []struct {
		name    string
		status  int
		reply   string
		want    [][]float32
		wantErr error
		wantMsg string
	}{
		{
			name:   "embeds every input",
			status: http.StatusOK,
			reply:  `{"result":{"shape":[2,2],"data":[[0.5,1],[2,-1]]}}`,
			want: [][]float32{
				{0.5, 1},
				{2, -1},
			},
		},
		{
			name:    "no data",
			status:  http.StatusOK,
			reply:   `{"result":{"shape":[0],"data":[]}}`,
			wantErr: ErrEmptyResponse,
		},
		{
			name:   "fewer vectors than inputs",
			status: http.StatusOK,
			reply:  `{"result":{"shape":[1,2],"data":[[0.5,1]]}}`,
			want: [][]float32{
				{0.5, 1},
			},
			wantErr: ErrIncompleteEmbedding,
		},
		{
			name:    "error status",
			status:  http.StatusBadRequest,
			reply:   "bad input",
			wantMsg: "error: bad input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The embedding model is also the chat model, so the request goes
			// to the same endpoint whichever of the two the client uses.
			w, llm := newTestLLM(t, tt.status, contentTypeJSON, tt.reply, WithEmbeddingModel(testModel))
			got, err := llm.CreateEmbedding(context.Background(), inputs)
			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantMsg != "":
				require.EqualError(t, err, tt.wantMsg)
			default:
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
			assert.Equal(t, []capturedRequest{expectedRequest(`{"text":["a","b"]}`)}, w.received())
		})
	}
}

func TestTypeToRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role llms.Role
		want cloudflareclient.Role
	}{
		{
			role: llms.RoleSystem,
			want: cloudflareclient.RoleSystem,
		},
		{
			role: llms.RoleAI,
			want: cloudflareclient.RoleAssistant,
		},
		{
			role: llms.RoleHuman,
			want: cloudflareclient.RoleTypeUser,
		},
		{
			role: llms.RoleGeneric,
			want: cloudflareclient.RoleTypeUser,
		},
		{
			role: llms.RoleTool,
			want: cloudflareclient.RoleTypeUser,
		},
		{
			role: llms.Role("unknown"),
			want: "",
		},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, typeToRole(tt.role), "role %q", tt.role)
	}
}
