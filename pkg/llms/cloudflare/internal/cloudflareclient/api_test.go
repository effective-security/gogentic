package cloudflareclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHTTPClient struct {
	response *http.Response
	err      error
}

func (m *mockHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	return m.response, m.err
}

func TestClient_GenerateContent(t *testing.T) { // nolint:funlen
	t.Parallel()

	type fields struct {
		httpClient         httpClient
		accountID          string
		token              string
		baseURL            string
		modelName          string
		embeddingModelName string
		endpointURL        string
		bearerToken        string
	}
	type args struct {
		ctx     context.Context // nolint:containedctx
		request *GenerateContentRequest
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *GenerateContentResponse
		wantErr bool
	}{
		{
			name: "test generate content success",
			fields: fields{
				httpClient: &mockHTTPClient{
					response: &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"result": {"response": "response"}}`)),
					},
				},
				accountID:          "accountID",
				token:              "token",
				baseURL:            "baseURL",
				modelName:          "modelName",
				embeddingModelName: "embeddingModelName",
			},
			args: args{
				ctx: context.Background(),
				request: &GenerateContentRequest{
					Messages: []Message{
						{Role: "system", Content: "systemPrompt"},
						{Role: "user", Content: "userPrompt"},
					},
				},
			},
			want: &GenerateContentResponse{
				Result: struct {
					Response string `json:"response"`
				}{
					Response: "response",
				},
			},
		},
		{
			name: "test generate content stream success",
			fields: fields{
				httpClient: &mockHTTPClient{
					response: &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"result": {"response": "response"}}`)),
					},
				},
				accountID:          "accountID",
				token:              "token",
				baseURL:            "baseURL",
				modelName:          "modelName",
				embeddingModelName: "embeddingModelName",
			},
			args: args{
				ctx: context.Background(),
				request: &GenerateContentRequest{
					Messages: []Message{
						{Role: "system", Content: "systemPrompt"},
						{Role: "user", Content: "userPrompt"},
					},
					Stream: true,
					StreamingFunc: func(_ context.Context, chunk []byte) error {
						if string(chunk) != `{"result": {"response": "response"}}` {
							return io.EOF
						}
						return nil
					},
				},
			},
			want: &GenerateContentResponse{
				Result: struct {
					Response string `json:"response"`
				}{
					Response: "",
				},
			},
		},
	}
	for i := range tests {
		tt := tests[i]

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := Client{
				httpClient:         tt.fields.httpClient,
				accountID:          tt.fields.accountID,
				token:              tt.fields.token,
				baseURL:            tt.fields.baseURL,
				modelName:          tt.fields.modelName,
				embeddingModelName: tt.fields.embeddingModelName,
				endpointURL:        tt.fields.endpointURL,
				bearerToken:        tt.fields.bearerToken,
			}

			got, err := c.GenerateContent(context.Background(), tt.args.request)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateContent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GenerateContent() got = %v, want %v", got, tt.want)
			}
		})
	}
}

const (
	testAccountID      = "acct-1"
	testToken          = "tok-1"
	testBaseURL        = "https://workers.example.test/client/v4/accounts"
	testModel          = "@cf/meta/llama-3.1-8b-instruct"
	testEmbeddingModel = "@cf/baai/bge-small-en-v1.5"

	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
	contentTypeSSE    = "text/event-stream"

	// badBaseURL holds a control character, so no request can be built from it.
	badBaseURL = "http://bad\x7fhost"

	userHiBody = `{"messages":[{"role":"user","content":"hi"}],"stream":true}`
)

var (
	errTransport = errors.New("transport down")
	errRead      = errors.New("body read failed")
	errStop      = errors.New("stop streaming")
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

// newWorkersAI starts a fake Workers AI server and returns a client pointed at
// it. The client uses testModel for both chat and embeddings.
func newWorkersAI(t *testing.T, status int, contentType, reply string) (*workersAI, *Client) {
	t.Helper()
	w := &workersAI{
		status:      status,
		contentType: contentType,
		reply:       reply,
	}
	srv := httptest.NewServer(w)
	t.Cleanup(srv.Close)
	return w, NewClient(srv.Client(), testAccountID, srv.URL, testToken, testModel, testModel)
}

// expectedRequest is the request every client method sends with body.
func expectedRequest(body string) capturedRequest {
	return capturedRequest{
		Method:        http.MethodPost,
		Path:          "/" + testAccountID + "/ai/run/" + testModel,
		Authorization: "Bearer " + testToken,
		ContentType:   contentTypeJSON,
		Body:          body,
	}
}

// chunkRecorder collects the chunks a streaming request delivers.
type chunkRecorder struct {
	chunks []string
	failOn int
}

func (c *chunkRecorder) record(_ context.Context, chunk []byte) error {
	c.chunks = append(c.chunks, string(chunk))
	if c.failOn > 0 && len(c.chunks) == c.failOn {
		return errStop
	}
	return nil
}

func userHi(stream bool, fn func(context.Context, []byte) error) *GenerateContentRequest {
	return &GenerateContentRequest{
		Messages: []Message{
			{
				Role:    RoleTypeUser,
				Content: "hi",
			},
		},
		Stream:        stream,
		StreamingFunc: fn,
	}
}

func TestClient_GenerateContent_Server(t *testing.T) {
	t.Parallel()

	const reply = `{"errors":[],"messages":["note"],"result":{"response":"hello"},"success":true}`

	t.Run("not streaming", func(t *testing.T) {
		t.Parallel()

		w, c := newWorkersAI(t, http.StatusOK, contentTypeJSON, reply)
		got, err := c.GenerateContent(context.Background(), &GenerateContentRequest{
			Messages: []Message{
				{
					Role:    RoleSystem,
					Content: "be brief",
				},
				{
					Role:    RoleTypeUser,
					Content: "hi",
				},
			},
		})
		require.NoError(t, err)

		want := &GenerateContentResponse{
			Errors:   []APIError{},
			Messages: []string{"note"},
			Success:  true,
		}
		want.Result.Response = "hello"
		assert.Equal(t, want, got)
		assert.Equal(t, []capturedRequest{
			expectedRequest(`{"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}],"stream":false}`),
		}, w.received())
	})

	t.Run("stream flag without streaming func reads one reply", func(t *testing.T) {
		t.Parallel()

		w, c := newWorkersAI(t, http.StatusOK, contentTypeJSON, reply)
		got, err := c.GenerateContent(context.Background(), userHi(true, nil))
		require.NoError(t, err)
		assert.Equal(t, "hello", got.Result.Response)
		assert.Equal(t, []capturedRequest{expectedRequest(userHiBody)}, w.received())
	})
}

func TestClient_GenerateContent_Stream(t *testing.T) {
	t.Parallel()

	t.Run("delivers chunks until done", func(t *testing.T) {
		t.Parallel()

		// The line after [DONE] is not JSON: reading it would fail the call.
		reply := "data: {\"response\":\"Hel\",\"p\":\"abc\"}\n\n" +
			"data: {\"response\":\"lo\"}\n\n" +
			"data: [DONE]\n\n" +
			"data: not-json\n\n"
		w, c := newWorkersAI(t, http.StatusOK, contentTypeSSE, reply)
		rec := &chunkRecorder{}

		got, err := c.GenerateContent(context.Background(), userHi(true, rec.record))
		require.NoError(t, err)
		assert.Equal(t, &GenerateContentResponse{}, got)
		assert.Equal(t, []string{`{"response":"Hel","p":"abc"}`, `{"response":"lo"}`}, rec.chunks)
		assert.Equal(t, []capturedRequest{expectedRequest(userHiBody)}, w.received())
	})

	t.Run("invalid chunk", func(t *testing.T) {
		t.Parallel()

		_, c := newWorkersAI(t, http.StatusOK, contentTypeSSE, "data: {broken\n\n")
		rec := &chunkRecorder{}

		got, err := c.GenerateContent(context.Background(), userHi(true, rec.record))
		var syntaxErr *json.SyntaxError
		require.ErrorAs(t, err, &syntaxErr)
		assert.Nil(t, got)
		assert.Empty(t, rec.chunks)
	})

	t.Run("streaming func stops the stream", func(t *testing.T) {
		t.Parallel()

		reply := "data: {\"response\":\"a\"}\n\ndata: {\"response\":\"b\"}\n\n"
		_, c := newWorkersAI(t, http.StatusOK, contentTypeSSE, reply)
		rec := &chunkRecorder{
			failOn: 1,
		}

		got, err := c.GenerateContent(context.Background(), userHi(true, rec.record))
		require.ErrorIs(t, err, errStop)
		assert.Nil(t, got)
		assert.Equal(t, []string{`{"response":"a"}`}, rec.chunks)
	})

	t.Run("error status returns the rest of the body", func(t *testing.T) {
		t.Parallel()

		const upstream = `{"errors":[{"message":"rate limited"}]}`
		// One byte per read, so the scanner holds nothing past the first line.
		body := iotest.OneByteReader(strings.NewReader("data: {\"response\":\"\"}\n" + upstream))
		c := NewClient(&mockHTTPClient{
			response: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(body),
			},
		}, testAccountID, testBaseURL, testToken, testModel, testEmbeddingModel)
		rec := &chunkRecorder{}

		got, err := c.GenerateContent(context.Background(), userHi(true, rec.record))
		require.NoError(t, err)
		assert.Equal(t, &GenerateContentResponse{
			Errors: []APIError{
				{
					Message: upstream,
				},
			},
		}, got)
		assert.Empty(t, rec.chunks)
	})

	t.Run("error status with unreadable body", func(t *testing.T) {
		t.Parallel()

		body := iotest.OneByteReader(io.MultiReader(strings.NewReader("data: {}\n"), iotest.ErrReader(errRead)))
		c := NewClient(&mockHTTPClient{
			response: &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(body),
			},
		}, testAccountID, testBaseURL, testToken, testModel, testEmbeddingModel)
		rec := &chunkRecorder{}

		got, err := c.GenerateContent(context.Background(), userHi(true, rec.record))
		require.ErrorIs(t, err, errRead)
		assert.Nil(t, got)
		assert.Empty(t, rec.chunks)
	})
}

func TestClient_CreateEmbedding(t *testing.T) {
	t.Parallel()

	w, c := newWorkersAI(t, http.StatusOK, contentTypeJSON, `{"result":{"shape":[2,3],"data":[[0.1,0.2,0.3],[0.4,0.5,0.6]]}}`)
	got, err := c.CreateEmbedding(context.Background(), &CreateEmbeddingRequest{
		Text: []string{"a", "b"},
	})
	require.NoError(t, err)

	want := &CreateEmbeddingResponse{}
	want.Result.Shape = []int{2, 3}
	want.Result.Data = [][]float32{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	}
	assert.Equal(t, want, got)
	assert.Equal(t, []capturedRequest{expectedRequest(`{"text":["a","b"]}`)}, w.received())
}

func TestClient_Summarize(t *testing.T) {
	t.Parallel()

	w, c := newWorkersAI(t, http.StatusOK, contentTypeJSON, `{"result":{"summary":"short"},"success":true}`)
	got, err := c.Summarize(context.Background(), "a long text", 12)
	require.NoError(t, err)

	want := &SummarizeResponse{
		Success: true,
	}
	want.Result.Summary = "short"
	assert.Equal(t, want, got)
	assert.Equal(t, []capturedRequest{expectedRequest(`{"input_text":"a long text","max_length":12}`)}, w.received())
}

// clientCall runs one client method and returns only its error.
type clientCall struct {
	name string
	call func(context.Context, *Client) error
}

var clientCalls = []clientCall{
	{
		name: "GenerateContent",
		call: func(ctx context.Context, c *Client) error {
			got, err := c.GenerateContent(ctx, userHi(false, nil))
			if got != nil {
				return errors.New("unexpected response")
			}
			return err
		},
	},
	{
		name: "CreateEmbedding",
		call: func(ctx context.Context, c *Client) error {
			got, err := c.CreateEmbedding(ctx, &CreateEmbeddingRequest{
				Text: []string{"a"},
			})
			if got != nil {
				return errors.New("unexpected response")
			}
			return err
		},
	},
	{
		name: "Summarize",
		call: func(ctx context.Context, c *Client) error {
			got, err := c.Summarize(ctx, "text", 1)
			if got != nil {
				return errors.New("unexpected response")
			}
			return err
		},
	},
}

func TestClient_Errors(t *testing.T) {
	t.Parallel()

	reply := func(status int, body io.Reader) *mockHTTPClient {
		return &mockHTTPClient{
			response: &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(body),
			},
		}
	}

	for _, cc := range clientCalls {
		t.Run(cc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			t.Run("invalid endpoint", func(t *testing.T) {
				t.Parallel()
				c := NewClient(&mockHTTPClient{err: errTransport}, testAccountID, badBaseURL, testToken, testModel, testEmbeddingModel)
				err := cc.call(ctx, c)
				var urlErr *url.Error
				require.ErrorAs(t, err, &urlErr)
				assert.Equal(t, "parse", urlErr.Op)
				assert.NotErrorIs(t, err, errTransport, "no request is sent")
			})

			t.Run("transport error", func(t *testing.T) {
				t.Parallel()
				c := NewClient(&mockHTTPClient{err: errTransport}, testAccountID, testBaseURL, testToken, testModel, testEmbeddingModel)
				require.ErrorIs(t, cc.call(ctx, c), errTransport)
			})

			t.Run("unreadable body", func(t *testing.T) {
				t.Parallel()
				c := NewClient(reply(http.StatusOK, iotest.ErrReader(errRead)), testAccountID, testBaseURL, testToken, testModel, testEmbeddingModel)
				require.ErrorIs(t, cc.call(ctx, c), errRead)
			})

			t.Run("error status", func(t *testing.T) {
				t.Parallel()
				c := NewClient(reply(http.StatusServiceUnavailable, strings.NewReader("upstream down")), testAccountID, testBaseURL, testToken, testModel, testEmbeddingModel)
				require.EqualError(t, cc.call(ctx, c), "error: upstream down")
			})

			t.Run("invalid JSON", func(t *testing.T) {
				t.Parallel()
				c := NewClient(reply(http.StatusOK, strings.NewReader("not-json")), testAccountID, testBaseURL, testToken, testModel, testEmbeddingModel)
				var syntaxErr *json.SyntaxError
				require.ErrorAs(t, cc.call(ctx, c), &syntaxErr)
			})
		})
	}
}
