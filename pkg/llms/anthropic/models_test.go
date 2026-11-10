package anthropic_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Query parameters of the model list request.
	queryAfterID   = "after_id"
	queryLimit     = "limit"
	queryLifecycle = "lifecycle[]"

	// lastIDOfPage1 is the cursor the first recorded page ends with.
	lastIDOfPage1 = "claude-fable-5"

	emptyList = `{"data":[],"has_more":false,"first_id":null,"last_id":null}`
	authError = `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`
)

// listReply is one canned reply of the model list.
type listReply struct {
	status int
	body   string
}

// modelList serves the model list page by page and records the requests. The
// page is chosen by the after_id cursor of the request.
type modelList struct {
	// pages maps a cursor to its reply; the empty cursor is the first page.
	pages    map[string]listReply
	requests []*http.Request
}

func (l *modelList) client(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			l.requests = append(l.requests, r)
			cursor := r.URL.Query().Get(queryAfterID)
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

// recorded returns a page of the model list recorded from the Anthropic API.
func recorded(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(raw)
}

// recordedList serves the two recorded pages. The API returned no retired
// model when they were recorded, so the last entry of the second page,
// claude-sonnet-4-20250514, was written by hand in the shape of the others.
func recordedList(t *testing.T) *modelList {
	t.Helper()
	return &modelList{
		pages: map[string]listReply{
			"": {
				body: recorded(t, "models_page1.json"),
			},
			lastIDOfPage1: {
				body: recorded(t, "models_page2.json"),
			},
		},
	}
}

func TestListModels_Pages(t *testing.T) {
	list := recordedList(t)
	model, err := anthropic.New(
		anthropic.WithModel("claude-opus-4-8"),
		anthropic.WithToken("fixture"),
		anthropic.WithHTTPClient(list.client(t)),
	)
	require.NoError(t, err)

	// The capability is advertised, so the model can be asked for its list.
	require.True(t, model.GetProviderType().Supports(llms.CapabilityModels))
	lister, ok := llms.Model(model).(llms.ModelLister)
	require.True(t, ok)

	models, err := lister.ListModels(context.Background())
	require.NoError(t, err)

	// Both pages are read, each asking for every lifecycle stage.
	wantLifecycles := []string{"active", "deprecated", "retired"}
	require.Len(t, list.requests, 2)
	for i, req := range list.requests {
		query := req.URL.Query()
		assert.Equal(t, http.MethodGet, req.Method, "request %d", i)
		assert.Equal(t, "/v1/models", req.URL.Path, "request %d", i)
		assert.Equal(t, "fixture", req.Header.Get("X-Api-Key"), "request %d", i)
		assert.Equal(t, "100", query.Get(queryLimit), "request %d", i)
		assert.Equal(t, wantLifecycles, query[queryLifecycle], "request %d", i)
	}
	assert.False(t, list.requests[0].URL.Query().Has(queryAfterID))
	assert.Equal(t, lastIDOfPage1, list.requests[1].URL.Query().Get(queryAfterID))

	lowToMax := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
	lowToMaxNoXHigh := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortMax,
	)
	lowToHigh := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	noEffort := llms.NewEffortSet()

	const (
		active     = llms.ModelLifecycleActive
		deprecated = llms.ModelLifecycleDeprecated
		retired    = llms.ModelLifecycleRetired
	)
	want := []struct {
		id        string
		family    string
		lifecycle llms.ModelLifecycle
		efforts   llms.EffortSet
	}{
		{"claude-fable-5-2", "fable", active, lowToMax},
		{"claude-haiku-5-5", "haiku", active, lowToMax},
		{"claude-sonnet-5-5", "sonnet", active, lowToMax},
		{"claude-opus-5-5", "opus", active, lowToMax},
		{"claude-fable-5-1", "fable", active, lowToMax},
		{"claude-opus-5", "opus", active, lowToMax},
		{"claude-sonnet-5", "sonnet", active, lowToMax},
		{"claude-fable-5", "fable", active, lowToMax},
		{"claude-opus-4-8", "opus", active, lowToMax},
		{"claude-opus-4-7", "opus", active, lowToMax},
		{"claude-sonnet-4-6", "sonnet", active, lowToMaxNoXHigh},
		{"claude-opus-4-6", "opus", active, lowToMaxNoXHigh},
		{"claude-opus-4-5-20251101", "opus", active, lowToHigh},
		{"claude-haiku-4-5-20251001", "haiku", active, noEffort},
		{"claude-sonnet-4-5-20250929", "sonnet", deprecated, noEffort},
		{"claude-sonnet-4-20250514", "sonnet", retired, noEffort},
	}
	require.Len(t, models, len(want))
	for i, w := range want {
		got := models[i]
		assert.Equal(t, w.id, got.ID, "model %d", i)
		assert.Equal(t, w.family, got.Family, "model %s", w.id)
		assert.Equal(t, w.lifecycle, got.Lifecycle, "model %s", w.id)
		assert.Equal(t, w.efforts.Levels(), got.Efforts.Levels(), "model %s", w.id)
		assert.NotEmpty(t, got.DisplayName, "model %s", w.id)
		assert.False(t, got.Created.IsZero(), "model %s", w.id)
		// Only a model on its way out has a retirement date.
		assert.Equal(t, w.lifecycle == active, got.RetiresAt.IsZero(), "model %s", w.id)
	}

	// One entry of each lifecycle stage, in full.
	assert.Equal(t, llms.ModelInfo{
		ID:          "claude-fable-5-2",
		DisplayName: "Claude Fable 5.2",
		Family:      "fable",
		Created:     time.Date(2026, time.November, 9, 18, 30, 0, 0, time.UTC),
		Lifecycle:   active,
		Efforts:     lowToMax,
	}, models[0])
	assert.Equal(t, llms.ModelInfo{
		ID:          "claude-sonnet-4-5-20250929",
		DisplayName: "Claude Sonnet 4.5",
		Family:      "sonnet",
		Created:     time.Date(2025, time.September, 29, 0, 0, 0, 0, time.UTC),
		Lifecycle:   deprecated,
		RetiresAt:   time.Date(2026, time.November, 30, 17, 0, 0, 0, time.UTC),
	}, models[14])
	assert.Equal(t, llms.ModelInfo{
		ID:          "claude-sonnet-4-20250514",
		DisplayName: "Claude Sonnet 4",
		Family:      "sonnet",
		Created:     time.Date(2025, time.May, 14, 0, 0, 0, 0, time.UTC),
		Lifecycle:   retired,
		RetiresAt:   time.Date(2026, time.June, 15, 16, 0, 0, 0, time.UTC),
	}, models[15])
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
					body: emptyList,
				},
			},
			wantRequests: 1,
		},
		{
			name: "HTTP error",
			pages: map[string]listReply{
				"": {
					status: http.StatusUnauthorized,
					body:   authError,
				},
			},
			wantRequests: 1,
			wantErr:      "anthropic: failed to list models",
		},
		{
			name: "an error on a later page returns no partial list",
			pages: map[string]listReply{
				"": {
					body: recorded(t, "models_page1.json"),
				},
				lastIDOfPage1: {
					status: http.StatusUnauthorized,
					body:   authError,
				},
			},
			wantRequests: 2,
			wantErr:      "anthropic: failed to list models",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := &modelList{pages: tt.pages}
			lister, err := anthropic.NewModelLister(
				anthropic.WithToken("fixture"),
				anthropic.WithHTTPClient(list.client(t)),
			)
			require.NoError(t, err)

			models, err := lister.ListModels(context.Background())
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

// The Bedrock endpoint has no model list: nothing is requested.
func TestListModels_Bedrock(t *testing.T) {
	list := &modelList{}
	opts := []anthropic.Option{
		anthropic.WithModel("us.anthropic.claude-opus-4-8-v1:0"),
		anthropic.WithHTTPClient(list.client(t)),
		anthropic.WithAWSConfig(&aws.Config{
			Region: "us-east-1",
		}),
	}

	model, err := anthropic.NewBedrock(opts...)
	require.NoError(t, err)
	models, err := model.ListModels(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, llms.ErrModelListNotSupported), "got %v", err)
	assert.Nil(t, models)

	lister, err := anthropic.NewModelLister(opts...)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llms.ErrModelListNotSupported), "got %v", err)
	assert.Nil(t, lister)

	assert.Empty(t, list.requests)
}

func TestNewModelLister(t *testing.T) {
	t.Run("needs no model name", func(t *testing.T) {
		list := recordedList(t)
		lister, err := anthropic.NewModelLister(
			anthropic.WithToken("fixture"),
			anthropic.WithHTTPClient(list.client(t)),
		)
		require.NoError(t, err)
		models, err := lister.ListModels(context.Background())
		require.NoError(t, err)
		assert.Len(t, models, 16)
	})

	t.Run("missing token", func(t *testing.T) {
		t.Setenv(anthropic.TokenEnvVarName, "")
		lister, err := anthropic.NewModelLister()
		require.Error(t, err)
		assert.True(t, errors.Is(err, anthropic.ErrMissingToken), "got %v", err)
		assert.Nil(t, lister)
	})
}

// TestIntegrationListModels lists the models of the account behind
// ANTHROPIC_API_KEY. Listing is free of charge.
func TestIntegrationListModels(t *testing.T) {
	apiKey := os.Getenv(anthropic.TokenEnvVarName)
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	lister, err := anthropic.NewModelLister(anthropic.WithToken(apiKey))
	require.NoError(t, err)
	models, err := lister.ListModels(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, models)

	active := 0
	for _, model := range models {
		t.Logf("%-32s %-10s family=%-7s efforts=%v retires=%s",
			model.ID, model.Lifecycle, model.Family, model.Efforts.Levels(), retirement(model))
		assert.NotEmpty(t, model.ID)
		assert.NotEmpty(t, model.DisplayName, "model %s", model.ID)
		assert.NotEmpty(t, model.Lifecycle, "model %s", model.ID)
		assert.False(t, model.Created.IsZero(), "model %s", model.ID)
		if model.Lifecycle == llms.ModelLifecycleActive {
			active++
			assert.True(t, model.RetiresAt.IsZero(), "model %s", model.ID)
		}
	}
	assert.Positive(t, active, "no active model listed")
}

// retirement formats the retirement date of a model for the test log.
func retirement(model llms.ModelInfo) string {
	if model.RetiresAt.IsZero() {
		return "-"
	}
	return model.RetiresAt.Format(time.DateOnly)
}
