package cloudflare

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptions(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{}
	var got options
	for _, opt := range []Option{
		WithModel(testModel),
		WithEmbeddingModel(testEmbeddingModel),
		WithSystemPrompt(testSystemPrompt),
		WithAccountID(testAccountID),
		WithServerURL(testServerURL),
		WithToken(testToken),
		WithHTTPClient(httpClient),
	} {
		opt(&got)
	}

	assert.Equal(t, options{
		cloudflareAccountID: testAccountID,
		cloudflareServerURL: testServerURL,
		cloudflareToken:     testToken,
		httpClient:          httpClient,
		model:               testModel,
		embeddingModel:      testEmbeddingModel,
		system:              testSystemPrompt,
	}, got)
}

func TestNew_DefaultHTTPClient(t *testing.T) {
	t.Parallel()

	llm, err := New(WithServerURL(testServerURL), WithModel(testModel))
	require.NoError(t, err)
	assert.Same(t, http.DefaultClient, llm.options.httpClient)
}

// urlRecorder is an HTTPClient that records the URL of every request and
// answers it with a canned Workers AI reply.
type urlRecorder struct {
	mu   sync.Mutex
	urls []string
}

func (r *urlRecorder) Do(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			headerContentType: []string{contentTypeJSON},
		},
		Body: io.NopCloser(strings.NewReader(answerReply)),
	}, nil
}

// TestNew_DefaultServerURL creates an LLM without WithServerURL, which used to
// panic (F-023), and checks that it calls the Cloudflare API.
func TestNew_DefaultServerURL(t *testing.T) {
	t.Parallel()

	rec := &urlRecorder{}
	llm, err := New(
		WithAccountID(testAccountID),
		WithModel(testModel),
		WithHTTPClient(rec),
	)
	require.NoError(t, err)

	resp, err := llm.GenerateContent(context.Background(), []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	})
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "answer", resp.Choices[0].Content)
	assert.Equal(t, []string{
		"https://api.cloudflare.com/client/v4/accounts/" + testAccountID + "/ai/run/" + testModel,
	}, rec.urls)
}

// TestNew_InvalidServerURL checks that a bad server URL is an error from New;
// WithServerURL used to exit the process on a URL that did not parse.
func TestNew_InvalidServerURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{
			name:    "does not parse",
			url:     "://api.cloudflare.com",
			wantErr: `cloudflare: invalid server URL: parse "://api.cloudflare.com": missing protocol scheme`,
		},
		{
			name:    "relative",
			url:     "client/v4/accounts",
			wantErr: `cloudflare: invalid server URL "client/v4/accounts": must be absolute`,
		},
		{
			name:    "no host",
			url:     "https:///client/v4/accounts",
			wantErr: `cloudflare: invalid server URL "https:///client/v4/accounts": must be absolute`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			llm, err := New(WithServerURL(tt.url), WithModel(testModel))
			assert.EqualError(t, err, tt.wantErr)
			assert.Nil(t, llm)
		})
	}
}
