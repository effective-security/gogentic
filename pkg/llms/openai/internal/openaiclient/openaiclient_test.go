package openaiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testToken          = "test-token"
	testModel          = "gpt-test"
	testEmbeddingModel = "embed-test"
	testOrganization   = "org-test"
	testProject        = "proj-test"
	testAPIVersion     = "2025-03-01-preview"
	testAzureOldAPI    = "2024-10-21"

	// invalidBaseURL makes http.NewRequestWithContext fail on URL parsing.
	invalidBaseURL = "http://invalid host"

	headerAuthorization = "Authorization"
	headerContentType   = "Content-Type"
	headerAPIKey        = "api-key"
	headerOrganization  = "OpenAI-Organization"
	headerProject       = "OpenAI-Project"
	headerCustom        = "X-Custom"
	customHeaderValue   = "custom-value"
	bearerTestToken     = "Bearer " + testToken
	contentTypeJSON     = "application/json"

	chatCompletionsURLPath = "/chat/completions"
	responsesURLPath       = "/responses"
	embeddingsURLPath      = "/embeddings"

	chatHelloReply      = `{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	chatNoChoicesReply  = `{"id":"chatcmpl-1","choices":[]}`
	responsesHelloReply = `{"id":"resp_1","object":"response","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}]}`
)

var errTransport = errors.New("transport down")

// recordedRequest is one request captured by stubServer.
type recordedRequest struct {
	method string
	path   string
	query  string
	header http.Header
	body   []byte
}

// stubServer is an httptest server that records every request and serves one
// canned reply.
type stubServer struct {
	t *testing.T

	mu       sync.Mutex
	requests []recordedRequest

	status  int
	headers map[string]string
	reply   string
}

func newStubServer(t *testing.T, status int, reply string) (*stubServer, *httptest.Server) {
	t.Helper()
	s := &stubServer{
		t:      t,
		status: status,
		reply:  reply,
	}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *stubServer) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	assert.NoError(s.t, err)
	s.mu.Lock()
	s.requests = append(s.requests, recordedRequest{
		method: r.Method,
		path:   r.URL.Path,
		query:  r.URL.RawQuery,
		header: r.Header.Clone(),
		body:   body,
	})
	headers := s.headers
	s.mu.Unlock()
	for key, value := range headers {
		w.Header().Set(key, value)
	}
	if w.Header().Get(headerContentType) == "" {
		w.Header().Set(headerContentType, contentTypeJSON)
	}
	w.WriteHeader(s.status)
	_, err = w.Write([]byte(s.reply))
	assert.NoError(s.t, err)
}

// setHeaders sets the response headers served with the reply.
func (s *stubServer) setHeaders(headers map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers = headers
}

// last returns the last recorded request; it fails the test when none arrived.
func (s *stubServer) last() recordedRequest {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(s.t, s.requests, "no request reached the stub server")
	return s.requests[len(s.requests)-1]
}

// count returns the number of requests the server has seen.
func (s *stubServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// lastJSON decodes the body of the last recorded request.
func (s *stubServer) lastJSON() map[string]any {
	s.t.Helper()
	var out map[string]any
	require.NoError(s.t, json.Unmarshal(s.last().body, &out))
	return out
}

// doerFunc adapts a function to the Doer interface.
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

// failingDoer always fails the HTTP round trip.
func failingDoer() Doer {
	return doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errTransport
	})
}

// errReader fails every read.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errTransport }

// brokenBodyDoer answers with HTTP 200 and a body that fails on read.
func brokenBodyDoer() Doer {
	return doerFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{headerContentType: []string{contentTypeJSON}},
			Body:       io.NopCloser(errReader{}),
			Request:    r,
		}, nil
	})
}

// newTestClient builds a client for the given provider and base URL.
func newTestClient(t *testing.T, provider ProviderType, model, baseURL string, httpClient Doer) *Client {
	t.Helper()
	c, err := New(provider, model, testToken, baseURL, "", "", "", httpClient, "", nil, nil)
	require.NoError(t, err)
	return c
}

func TestNew_Defaults(t *testing.T) {
	t.Parallel()

	headers := map[string]string{headerCustom: customHeaderValue}
	c, err := New(ProviderOpenAI, testModel, testToken, "", testOrganization, testProject, "", http.DefaultClient, testEmbeddingModel, nil, headers)
	require.NoError(t, err)
	assert.Equal(t, DefaultBaseURL, c.baseURL)
	assert.Equal(t, testModel, c.Model)
	assert.Equal(t, testEmbeddingModel, c.EmbeddingModel)
	assert.Equal(t, ProviderOpenAI, c.Provider)
	assert.True(t, c.SupportsResponsesAPI())

	// The client keeps its own copy of the headers.
	headers[headerCustom] = "mutated"
	assert.Equal(t, customHeaderValue, c.headers[headerCustom])

	c, err = New(ProviderOpenAI, testModel, testToken, "http://example.test/v1/", "", "", "", http.DefaultClient, "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "http://example.test/v1", c.baseURL, "a trailing slash is trimmed")
}

func TestNew_Options(t *testing.T) {
	t.Parallel()

	errOption := errors.New("option failed")
	_, err := New(ProviderOpenAI, testModel, testToken, "", "", "", "", http.DefaultClient, "", nil, nil,
		func(*Client) error { return errOption },
	)
	require.ErrorIs(t, err, errOption)

	c, err := New(ProviderOpenAI, testModel, testToken, "", "", "", "", http.DefaultClient, "", nil, nil,
		func(c *Client) error {
			c.Model = "from-option"
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "from-option", c.Model)
}

func TestIsResponsesAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		provider   ProviderType
		apiVersion string
		want       bool
	}{
		{
			name:     "openai",
			provider: ProviderOpenAI,
			want:     true,
		},
		{
			name:     "legacy openai name",
			provider: "OPEN_AI",
			want:     true,
		},
		{
			name:     "bedrock",
			provider: ProviderOpenAIBedrock,
			want:     true,
		},
		{
			name:     "openrouter",
			provider: ProviderOpenRouter,
			want:     true,
		},
		{
			name:     "perplexity",
			provider: ProviderPerplexity,
			want:     false,
		},
		{
			name:       "azure threshold date",
			provider:   ProviderAzure,
			apiVersion: "2025-03-01",
			want:       true,
		},
		{
			name:       "azure preview after threshold",
			provider:   ProviderAzureAD,
			apiVersion: testAPIVersion,
			want:       true,
		},
		{
			name:       "azure before threshold",
			provider:   ProviderAzure,
			apiVersion: testAzureOldAPI,
			want:       false,
		},
		{
			name:       "azure padded version",
			provider:   ProviderAzure,
			apiVersion: " 2025-04-01 ",
			want:       true,
		},
		{
			name:       "azure invalid version",
			provider:   ProviderAzure,
			apiVersion: "latest",
			want:       false,
		},
		{
			name:     "azure empty version",
			provider: ProviderAzureAD,
			want:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isResponsesAPI(tc.provider, tc.apiVersion))
		})
	}
}

func TestProviderPredicates(t *testing.T) {
	t.Parallel()

	assert.True(t, IsAzure(ProviderAzure))
	assert.True(t, IsAzure(ProviderAzureAD))
	assert.False(t, IsAzure(ProviderOpenAI))
	assert.True(t, IsBedrock(ProviderOpenAIBedrock))
	assert.False(t, IsBedrock(ProviderOpenAI))
}

func TestSetHeaders(t *testing.T) {
	t.Parallel()

	headers := map[string]string{
		headerCustom:        customHeaderValue,
		headerAuthorization: "Bearer must-not-win",
		"content-type":      "text/plain",
	}
	tests := []struct {
		name        string
		provider    ProviderType
		wantAuth    string
		wantAPIKey  string
		org         string
		project     string
		wantOrg     string
		wantProject string
	}{
		{
			name:        "openai bearer",
			provider:    ProviderOpenAI,
			wantAuth:    bearerTestToken,
			org:         testOrganization,
			project:     testProject,
			wantOrg:     testOrganization,
			wantProject: testProject,
		},
		{
			name:     "legacy openai bearer",
			provider: "OPEN_AI",
			wantAuth: bearerTestToken,
		},
		{
			name:     "azure bearer",
			provider: ProviderAzure,
			wantAuth: bearerTestToken,
		},
		{
			name:     "azure ad bearer",
			provider: ProviderAzureAD,
			wantAuth: bearerTestToken,
		},
		{
			name:     "bedrock bearer",
			provider: ProviderOpenAIBedrock,
			wantAuth: bearerTestToken,
		},
		{
			name:     "openrouter bearer",
			provider: ProviderOpenRouter,
			wantAuth: bearerTestToken,
		},
		{
			name:       "perplexity api key",
			provider:   ProviderPerplexity,
			wantAPIKey: testToken,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := New(tc.provider, testModel, testToken, "", tc.org, tc.project, "", http.DefaultClient, "", nil, headers)
			require.NoError(t, err)
			req, err := http.NewRequest(http.MethodPost, DefaultBaseURL, nil)
			require.NoError(t, err)

			c.setHeaders(req)
			assert.Equal(t, contentTypeJSON, req.Header.Get(headerContentType))
			assert.Equal(t, customHeaderValue, req.Header.Get(headerCustom))
			assert.Equal(t, tc.wantAuth, req.Header.Get(headerAuthorization))
			assert.Equal(t, tc.wantAPIKey, req.Header.Get(headerAPIKey))
			if tc.org == "" {
				assert.Empty(t, req.Header.Values(headerOrganization), "organization header must be absent")
				assert.Empty(t, req.Header.Values(headerProject), "project header must be absent")
			} else {
				assert.Equal(t, []string{tc.wantOrg}, req.Header.Values(headerOrganization))
				assert.Equal(t, []string{tc.wantProject}, req.Header.Values(headerProject))
			}
		})
	}
}

func TestBuildURL(t *testing.T) {
	t.Parallel()

	const base = "https://example.test"
	tests := []struct {
		name     string
		provider ProviderType
		suffix   string
		model    string
		want     string
	}{
		{
			name:     "openai",
			provider: ProviderOpenAI,
			suffix:   chatCompletionsURLPath,
			model:    testModel,
			want:     base + chatCompletionsURLPath,
		},
		{
			name:     "azure deployment",
			provider: ProviderAzure,
			suffix:   chatCompletionsURLPath,
			model:    testModel,
			want:     base + "/openai/deployments/" + testModel + chatCompletionsURLPath + "?api-version=" + testAPIVersion,
		},
		{
			name:     "azure ad embeddings",
			provider: ProviderAzureAD,
			suffix:   embeddingsURLPath,
			model:    testEmbeddingModel,
			want:     base + "/openai/deployments/" + testEmbeddingModel + embeddingsURLPath + "?api-version=" + testAPIVersion,
		},
		{
			name:     "azure responses",
			provider: ProviderAzure,
			suffix:   responsesURLPath,
			model:    testModel,
			want:     base + "/openai/responses?api-version=" + testAPIVersion,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := New(tc.provider, testModel, testToken, base+"/", "", "", testAPIVersion, http.DefaultClient, "", nil, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, c.buildURL(tc.suffix, tc.model))
		})
	}
}

func TestCreateChat_ModelDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		clientModel  string
		requestModel string
		want         string
	}{
		{
			name:         "request model wins",
			clientModel:  testModel,
			requestModel: "req-model",
			want:         "req-model",
		},
		{
			name:        "client model",
			clientModel: testModel,
			want:        testModel,
		},
		{
			name: "default model",
			want: defaultChatModel,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, http.StatusOK, chatHelloReply)
			c := newTestClient(t, ProviderOpenAI, tc.clientModel, srv.URL, http.DefaultClient)

			req := &ChatRequest{
				Model: tc.requestModel,
				Messages: []*ChatMessage{
					{
						Role:    "user",
						Content: "hi",
					},
				},
			}
			resp, err := c.CreateChat(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, resp.Choices, 1)
			assert.Equal(t, "hello", resp.Choices[0].Message.Content)
			assert.Equal(t, FinishReasonStop, resp.Choices[0].FinishReason)
			assert.Equal(t, 4, resp.Usage.TotalTokens)

			got := stub.last()
			assert.Equal(t, http.MethodPost, got.method)
			assert.Equal(t, chatCompletionsURLPath, got.path)
			assert.Equal(t, bearerTestToken, got.header.Get(headerAuthorization))
			assert.Equal(t, tc.want, stub.lastJSON()["model"])
		})
	}
}

func TestCreateChat_Errors(t *testing.T) {
	t.Parallel()

	t.Run("no choices", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, chatNoChoicesReply)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateChat(context.Background(), &ChatRequest{})
		require.ErrorIs(t, err, ErrEmptyResponse)
	})

	t.Run("transport", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, ProviderOpenAI, testModel, "", failingDoer())
		_, err := c.CreateChat(context.Background(), &ChatRequest{})
		require.ErrorIs(t, err, errTransport)
	})
}

func TestCreateResponse_ModelDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		clientModel  string
		requestModel string
		maxTokens    int64
		want         string
		wantMax      float64
	}{
		{
			name:         "request model and limit win",
			clientModel:  testModel,
			requestModel: "req-model",
			maxTokens:    10,
			want:         "req-model",
			wantMax:      10,
		},
		{
			name:        "client model",
			clientModel: testModel,
			want:        testModel,
			wantMax:     DefaultMaxTokens,
		},
		{
			name:    "default model",
			want:    DefaultChatModel,
			wantMax: DefaultMaxTokens,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, http.StatusOK, responsesHelloReply)
			c := newTestClient(t, ProviderOpenAI, tc.clientModel, srv.URL, http.DefaultClient)

			req := &responses.ResponseNewParams{
				Model: tc.requestModel,
			}
			if tc.maxTokens > 0 {
				req.MaxOutputTokens = param.NewOpt(tc.maxTokens)
			}
			resp, err := c.CreateResponse(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, "hello", resp.OutputText())

			body := stub.lastJSON()
			assert.Equal(t, tc.want, body["model"])
			assert.Equal(t, tc.wantMax, body["max_output_tokens"])
			assert.NotContains(t, body, "stream")
		})
	}
}

func TestCreateStreamingResponse_ModelDefaults(t *testing.T) {
	t.Parallel()

	const stream = "event: response.completed\n" +
		`data: {"type":"response.completed","response":` + responsesHelloReply + "}\n\n"

	tests := []struct {
		name         string
		clientModel  string
		requestModel string
		maxTokens    int64
		want         string
		wantMax      float64
	}{
		{
			name:         "request model and limit win",
			clientModel:  testModel,
			requestModel: "req-model",
			maxTokens:    10,
			want:         "req-model",
			wantMax:      10,
		},
		{
			name:        "client model",
			clientModel: testModel,
			want:        testModel,
			wantMax:     DefaultMaxTokens,
		},
		{
			name:    "default model",
			want:    DefaultChatModel,
			wantMax: DefaultMaxTokens,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, http.StatusOK, stream)
			stub.setHeaders(map[string]string{headerContentType: "text/event-stream"})
			c := newTestClient(t, ProviderOpenAI, tc.clientModel, srv.URL, http.DefaultClient)

			req := &responses.ResponseNewParams{
				Model: tc.requestModel,
			}
			if tc.maxTokens > 0 {
				req.MaxOutputTokens = param.NewOpt(tc.maxTokens)
			}
			resp, err := c.CreateStreamingResponse(context.Background(), req, nil)
			require.NoError(t, err)
			assert.Equal(t, "resp_1", resp.ID)
			assert.Equal(t, "hello", resp.OutputText())

			got := stub.last()
			assert.Equal(t, responsesURLPath, got.path)
			assert.Equal(t, "text/event-stream", got.header.Get("Accept"))
			body := stub.lastJSON()
			assert.Equal(t, true, body["stream"])
			assert.Equal(t, tc.want, body["model"])
			assert.Equal(t, tc.wantMax, body["max_output_tokens"])
		})
	}
}

func TestCreateCompletion(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, chatHelloReply)
	c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)

	got, err := c.CreateCompletion(context.Background(), &CompletionRequest{
		Prompt:      "say hello",
		Temperature: 0.5,
		TopP:        0.9,
		N:           2,
		Seed:        7,
		StopWords:   []string{"END"},
	})
	require.NoError(t, err)
	assert.Equal(t, &Completion{Text: "hello"}, got)

	assert.Equal(t, chatCompletionsURLPath, stub.last().path)
	body := stub.lastJSON()
	assert.Equal(t, testModel, body["model"])
	assert.Equal(t, 0.5, body["temperature"])
	assert.Equal(t, 0.9, body["top_p"])
	assert.Equal(t, float64(2), body["n"])
	assert.Equal(t, float64(7), body["seed"])
	assert.Equal(t, float64(2048), body["max_completion_tokens"])
	assert.Equal(t, []any{"END"}, body["stop"])
	assert.Equal(t, []any{
		map[string]any{
			"role":    "user",
			"content": "say hello",
		},
	}, body["messages"])
	assert.NotContains(t, body, "stream")
}

func TestCreateCompletion_Errors(t *testing.T) {
	t.Parallel()

	t.Run("no choices", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusOK, chatNoChoicesReply)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateCompletion(context.Background(), &CompletionRequest{Prompt: "x"})
		require.ErrorIs(t, err, ErrEmptyResponse)
	})

	t.Run("upstream status", func(t *testing.T) {
		t.Parallel()
		_, srv := newStubServer(t, http.StatusBadRequest, `{"error":{"message":"bad prompt","type":"invalid_request_error"}}`)
		c := newTestClient(t, ProviderOpenAI, testModel, srv.URL, http.DefaultClient)
		_, err := c.CreateCompletion(context.Background(), &CompletionRequest{Prompt: "x"})
		require.EqualError(t, err, "API returned unexpected status code: 400: bad prompt")
	})
}

func TestSDKClient_Headers(t *testing.T) {
	t.Parallel()

	stub, srv := newStubServer(t, http.StatusOK, `{"object":"list","data":[]}`)
	headers := map[string]string{
		headerCustom:        customHeaderValue,
		headerAuthorization: "Bearer must-not-win",
		headerContentType:   "text/plain",
	}
	c, err := New(ProviderOpenAI, testModel, testToken, srv.URL, testOrganization, testProject, "", http.DefaultClient, "", nil, headers)
	require.NoError(t, err)

	first := c.sdkClient()
	assert.Same(t, first, c.sdkClient(), "the SDK client is built once")

	models, err := c.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)

	got := stub.last()
	assert.Equal(t, http.MethodGet, got.method)
	assert.Equal(t, "/models", got.path)
	assert.Equal(t, bearerTestToken, got.header.Get(headerAuthorization))
	assert.Equal(t, customHeaderValue, got.header.Get(headerCustom))
	assert.Equal(t, testOrganization, got.header.Get(headerOrganization))
	assert.Equal(t, testProject, got.header.Get(headerProject))
	assert.NotEqual(t, "text/plain", got.header.Get(headerContentType))
}

func TestCreateEmbedding(t *testing.T) {
	t.Parallel()

	const reply = `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0},{"object":"embedding","embedding":[0.3],"index":1}],"model":"m","usage":{"prompt_tokens":2,"total_tokens":2}}`

	tests := []struct {
		name           string
		clientModel    string
		embeddingModel string
		requestModel   string
		want           string
	}{
		{
			name:         "request model kept when the client has a chat model",
			clientModel:  testModel,
			requestModel: "req-embed",
			want:         "req-embed",
		},
		{
			name:           "client embedding model overrides the request when the client has no chat model",
			embeddingModel: testEmbeddingModel,
			requestModel:   "req-embed",
			want:           testEmbeddingModel,
		},
		{
			name:        "default model",
			clientModel: testModel,
			want:        defaultEmbeddingModel,
		},
		{
			name: "default model without any configuration",
			want: defaultEmbeddingModel,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stub, srv := newStubServer(t, http.StatusOK, reply)
			c, err := New(ProviderOpenAI, tc.clientModel, testToken, srv.URL, "", "", "", http.DefaultClient, tc.embeddingModel, nil, nil)
			require.NoError(t, err)

			got, err := c.CreateEmbedding(context.Background(), &EmbeddingRequest{
				Model: tc.requestModel,
				Input: []string{"a", "b"},
			})
			require.NoError(t, err)
			assert.Equal(t, [][]float32{{0.1, 0.2}, {0.3}}, got)

			req := stub.last()
			assert.Equal(t, embeddingsURLPath, req.path)
			assert.Equal(t, bearerTestToken, req.header.Get(headerAuthorization))
			body := stub.lastJSON()
			assert.Equal(t, tc.want, body["model"])
			assert.Equal(t, []any{"a", "b"}, body["input"])
		})
	}
}
