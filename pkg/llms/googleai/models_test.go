package googleai

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// googleTokenEnvVarName is the variable the integration test reads its
	// key from. The Makefile sets it to a fake value, so make test stays
	// offline.
	googleTokenEnvVarName = "GOOGLEAI_TOKEN" //nolint:gosec

	// queryPageToken is the cursor parameter of the model list request.
	queryPageToken = "pageToken"
	// tokenOfPage1 is the cursor the first recorded page ends with.
	tokenOfPage1 = "Chltb2RlbHMvZ2VtbWEtNC0yNmItYTRiLWl0"
)

// listReply is one canned reply of the model list.
type listReply struct {
	status int
	body   string
}

// modelList serves the model list page by page and records the requests. The
// page is chosen by the pageToken cursor of the request.
type modelList struct {
	// pages maps a cursor to its reply; the empty cursor is the first page.
	pages    map[string]listReply
	requests []*http.Request
}

func (l *modelList) client(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			l.requests = append(l.requests, r)
			cursor := r.URL.Query().Get(queryPageToken)
			reply, ok := l.pages[cursor]
			require.True(t, ok, "unexpected cursor %q", cursor)
			status := reply.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(reply.body)),
				Request: r,
			}, nil
		}),
	}
}

// recorded returns a page of the model list recorded from the Gemini API.
func recorded(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(raw)
}

// The two recorded pages hold four models that generate content, an embedding
// model and a live-only model.
func TestListModels_Pages(t *testing.T) {
	ctx := context.Background()
	list := &modelList{
		pages: map[string]listReply{
			"": {
				body: recorded(t, "models_page1.json"),
			},
			tokenOfPage1: {
				body: recorded(t, "models_page2.json"),
			},
		},
	}
	model, err := New(ctx,
		WithDefaultModel("gemini-3.8-flash"),
		WithAPIKey("fixture"),
		WithHTTPClient(list.client(t)),
	)
	require.NoError(t, err)

	// The capability is advertised, so the model can be asked for its list.
	require.True(t, model.GetProviderType().Supports(llms.CapabilityModels))
	lister, ok := llms.Model(model).(llms.ModelLister)
	require.True(t, ok)

	models, err := lister.ListModels(ctx)
	require.NoError(t, err)

	require.Len(t, list.requests, 2)
	for i, req := range list.requests {
		assert.Equal(t, http.MethodGet, req.Method, "request %d", i)
		assert.Equal(t, "generativelanguage.googleapis.com", req.URL.Host, "request %d", i)
		assert.Equal(t, "/v1beta/models", req.URL.Path, "request %d", i)
		assert.Equal(t, "fixture", req.Header.Get("X-Goog-Api-Key"), "request %d", i)
	}
	assert.False(t, list.requests[0].URL.Query().Has(queryPageToken))
	assert.Equal(t, tokenOfPage1, list.requests[1].URL.Query().Get(queryPageToken))

	want := []llms.ModelInfo{
		{
			ID:          "gemini-2.5-flash",
			DisplayName: "Gemini 2.5 Flash",
		},
		{
			ID:          "gemini-2.5-pro",
			DisplayName: "Gemini 2.5 Pro",
		},
		{
			ID:          "gemini-3.8-flash",
			DisplayName: "Gemini 3.8 Flash",
		},
		{
			ID:          "gemma-4-26b-a4b-it",
			DisplayName: "Gemma 4 26B A4B IT",
		},
	}
	assert.Equal(t, want, models)
}

func TestListModels_Replies(t *testing.T) {
	tests := []struct {
		name         string
		pages        map[string]listReply
		wantRequests int
		wantErr      string
	}{
		{
			name: "empty list",
			pages: map[string]listReply{
				"": {
					body: `{}`,
				},
			},
			wantRequests: 1,
		},
		{
			name: "no model generates content",
			pages: map[string]listReply{
				"": {
					body: `{"models":[{"name":"models/gemini-embedding-001","supportedGenerationMethods":["embedContent"]}]}`,
				},
			},
			wantRequests: 1,
		},
		{
			name: "HTTP error",
			pages: map[string]listReply{
				"": {
					status: http.StatusForbidden,
					body:   `{"error":{"code":403,"message":"API key not valid","status":"PERMISSION_DENIED"}}`,
				},
			},
			wantRequests: 1,
			wantErr:      "googleai: failed to list models",
		},
		{
			name: "an error on a later page returns no partial list",
			pages: map[string]listReply{
				"": {
					body: recorded(t, "models_page1.json"),
				},
				tokenOfPage1: {
					status: http.StatusForbidden,
					body:   `{"error":{"code":403,"message":"API key not valid","status":"PERMISSION_DENIED"}}`,
				},
			},
			wantRequests: 2,
			wantErr:      "googleai: failed to list models",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			list := &modelList{pages: tt.pages}
			lister, err := NewModelLister(ctx,
				WithAPIKey("fixture"),
				WithHTTPClient(list.client(t)),
			)
			require.NoError(t, err)

			models, err := lister.ListModels(ctx)
			assert.Len(t, list.requests, tt.wantRequests)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, models)
				return
			}
			require.NoError(t, err)
			assert.Empty(t, models)
		})
	}
}

// TestIntegrationListModels lists the models of the account behind
// GOOGLEAI_TOKEN. Listing is free of charge.
func TestIntegrationListModels(t *testing.T) {
	apiKey := os.Getenv(googleTokenEnvVarName)
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("GOOGLEAI_TOKEN not set")
	}

	ctx := context.Background()
	lister, err := NewModelLister(ctx, WithAPIKey(apiKey))
	require.NoError(t, err)
	models, err := lister.ListModels(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, models)

	for _, model := range models {
		assert.NotEmpty(t, model.ID)
		assert.False(t, strings.HasPrefix(model.ID, modelNamePrefix), "model %s", model.ID)
	}
	t.Logf("%d models that generate content", len(models))
}
