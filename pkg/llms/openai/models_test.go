package openai_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openRouterBaseURL is the endpoint of the OpenRouter API.
const openRouterBaseURL = "https://openrouter.ai/api/v1"

// modelList serves one canned reply to every request and records the requests.
type modelList struct {
	status   int
	reply    string
	requests []*http.Request
}

func (l *modelList) client() *http.Client {
	return &http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			l.requests = append(l.requests, r)
			status := l.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(l.reply)),
				Request: r,
			}, nil
		}),
	}
}

// recorded returns a model list recorded from the provider's API.
func recorded(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(raw)
}

func unixUTC(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}

func dateUTC(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// The list recorded from api.openai.com: every model carries shutdown_date,
// null or a date.
func TestListModels_OpenAI(t *testing.T) {
	list := &modelList{reply: recorded(t, "models_openai.json")}
	model, err := openai.New(
		openai.WithModel("gpt-5.5"),
		openai.WithToken("fixture"),
		openai.WithHTTPClient(list.client()),
	)
	require.NoError(t, err)

	// The capability is advertised, so the model can be asked for its list.
	require.True(t, model.GetProviderType().Supports(llms.CapabilityModels))
	lister, ok := llms.Model(model).(llms.ModelLister)
	require.True(t, ok)

	models, err := lister.ListModels(context.Background())
	require.NoError(t, err)

	require.Len(t, list.requests, 1)
	req := list.requests[0]
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, "https://api.openai.com/v1/models", req.URL.String())
	assert.Equal(t, "Bearer fixture", req.Header.Get("Authorization"))

	want := []llms.ModelInfo{
		{
			ID:        "gpt-5.6-luna",
			Created:   unixUTC(1782228658),
			Lifecycle: llms.ModelLifecycleActive,
		},
		{
			ID:        "gpt-5.5-2026-04-23",
			Created:   unixUTC(1776839241),
			Lifecycle: llms.ModelLifecycleActive,
		},
		{
			ID:        "gpt-5.4-2026-03-05",
			Created:   unixUTC(1772654062),
			Lifecycle: llms.ModelLifecycleActive,
		},
		{
			ID:        "gpt-5-mini",
			Created:   unixUTC(1754425928),
			Lifecycle: llms.ModelLifecycleDeprecated,
			RetiresAt: dateUTC(2026, time.December, 11),
		},
		{
			ID:        "omni-moderation-latest",
			Created:   unixUTC(1731689265),
			Lifecycle: llms.ModelLifecycleActive,
		},
		{
			ID:        "tts-1",
			Created:   unixUTC(1681940951),
			Lifecycle: llms.ModelLifecycleDeprecated,
			RetiresAt: dateUTC(2027, time.January, 6),
		},
	}
	assert.Equal(t, want, models)
}

// The list recorded from openrouter.ai: display names, expiration dates and
// the reasoning levels of each model.
func TestListModels_OpenRouter(t *testing.T) {
	list := &modelList{reply: recorded(t, "models_openrouter.json")}
	lister, err := openai.NewModelLister(
		openai.WithProvider(llms.ProviderOpenRouter),
		openai.WithBaseURL(openRouterBaseURL),
		openai.WithToken("fixture"),
		openai.WithHTTPClient(list.client()),
	)
	require.NoError(t, err)

	models, err := lister.ListModels(context.Background())
	require.NoError(t, err)

	require.Len(t, list.requests, 1)
	req := list.requests[0]
	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, openRouterBaseURL+"/models", req.URL.String())
	assert.Equal(t, "Bearer fixture", req.Header.Get("Authorization"))

	noneToXHigh := llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
	)
	noneToMax := llms.NewEffortSet(
		llms.ReasoningEffortNone,
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
		llms.ReasoningEffortXHigh,
		llms.ReasoningEffortMax,
	)
	// "minimal" has no portable name and is left out.
	lowToHigh := llms.NewEffortSet(
		llms.ReasoningEffortLow,
		llms.ReasoningEffortMedium,
		llms.ReasoningEffortHigh,
	)
	want := []llms.ModelInfo{
		{
			ID:            "openai/gpt-5.5",
			DisplayName:   "OpenAI: GPT-5.5",
			Created:       unixUTC(1777051893),
			Lifecycle:     llms.ModelLifecycleActive,
			Efforts:       noneToXHigh,
			DefaultEffort: llms.ReasoningEffortMedium,
		},
		{
			ID:            "anthropic/claude-opus-4.8",
			DisplayName:   "Anthropic: Claude Opus 4.8",
			Created:       unixUTC(1779905091),
			Lifecycle:     llms.ModelLifecycleActive,
			Efforts:       noneToMax,
			DefaultEffort: llms.ReasoningEffortHigh,
		},
		{
			ID:            "openai/gpt-5",
			DisplayName:   "OpenAI: GPT-5",
			Created:       unixUTC(1754587413),
			Lifecycle:     llms.ModelLifecycleActive,
			Efforts:       lowToHigh,
			DefaultEffort: llms.ReasoningEffortMedium,
		},
		{
			// Reasoning without named levels.
			ID:          "google/gemini-2.5-pro",
			DisplayName: "Google: Gemini 2.5 Pro",
			Created:     unixUTC(1750169544),
			Lifecycle:   llms.ModelLifecycleActive,
		},
		{
			// No reasoning control.
			ID:          "openai/gpt-4o",
			DisplayName: "OpenAI: GPT-4o",
			Created:     unixUTC(1715558400),
			Lifecycle:   llms.ModelLifecycleActive,
		},
		{
			ID:          "openai/gpt-5-image-mini",
			DisplayName: "OpenAI: GPT-5 Image Mini",
			Created:     unixUTC(1760624583),
			Lifecycle:   llms.ModelLifecycleDeprecated,
			RetiresAt:   dateUTC(2026, time.December, 1),
		},
	}
	assert.Equal(t, want, models)
}

func TestListModels_Replies(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		reply   string
		want    []llms.ModelInfo
		wantErr string
	}{
		{
			name:  "empty list",
			reply: `{"object":"list","data":[]}`,
		},
		{
			name:  "a model without lifecycle fields has an unknown lifecycle",
			reply: `{"object":"list","data":[{"id":"local-model","object":"model"}]}`,
			want: []llms.ModelInfo{
				{
					ID: "local-model",
				},
			},
		},
		{
			name:  "a reasoning level without a portable name is left out",
			reply: `{"data":[{"id":"m","reasoning":{"supported_efforts":["ultra","high"]}}]}`,
			want: []llms.ModelInfo{
				{
					ID:      "m",
					Efforts: llms.NewEffortSet(llms.ReasoningEffortHigh),
				},
			},
		},
		{
			name:    "HTTP error",
			status:  http.StatusUnauthorized,
			reply:   `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`,
			wantErr: "failed to list models",
		},
		{
			name:    "a total above the returned count is a truncated list",
			reply:   `{"data":[{"id":"a"},{"id":"b"}],"total_count":5,"links":{"next":null}}`,
			wantErr: "model list is incomplete: got 2 of 5 models",
		},
		{
			name:    "a next page is a truncated list",
			reply:   `{"data":[{"id":"a"}],"links":{"next":"https://openrouter.ai/api/v1/models?offset=1"}}`,
			wantErr: "model list is incomplete: got 1 models and a next page",
		},
		{
			name:    "invalid expiration date",
			reply:   `{"data":[{"id":"a","expiration_date":"soon"}]}`,
			wantErr: "model a: invalid expiration_date",
		},
		{
			name:    "invalid reasoning field",
			reply:   `{"data":[{"id":"a","reasoning":"yes"}]}`,
			wantErr: "model a: invalid reasoning",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := &modelList{
				status: tt.status,
				reply:  tt.reply,
			}
			lister, err := openai.NewModelLister(
				openai.WithProvider(llms.ProviderOpenRouter),
				openai.WithBaseURL(openRouterBaseURL),
				openai.WithToken("fixture"),
				openai.WithHTTPClient(list.client()),
			)
			require.NoError(t, err)

			models, err := lister.ListModels(context.Background())
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.ErrorContains(t, err, "openai: provider OPENROUTER")
				assert.Nil(t, models)
				return
			}
			require.NoError(t, err)
			assert.Len(t, list.requests, 1)
			if tt.want == nil {
				assert.Empty(t, models)
				return
			}
			assert.Equal(t, tt.want, models)
		})
	}
}

// Provider types without the capability refuse before any request is made.
func TestListModels_NotSupported(t *testing.T) {
	tests := []struct {
		name string
		opts []openai.Option
	}{
		{
			name: "AZURE",
			opts: []openai.Option{
				openai.WithProvider(llms.ProviderAzure),
				openai.WithBaseURL("https://azure.test"),
				openai.WithModel("deployment"),
				openai.WithEmbeddingModel("embeddings"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := &modelList{reply: `{"object":"list","data":[{"id":"unreachable"}]}`}
			opts := append([]openai.Option{
				openai.WithToken("fixture"),
				openai.WithHTTPClient(list.client()),
			}, tt.opts...)

			model, err := openai.New(opts...)
			require.NoError(t, err)
			require.False(t, model.GetProviderType().Supports(llms.CapabilityModels))

			models, err := model.ListModels(context.Background())
			require.Error(t, err)
			assert.True(t, errors.Is(err, llms.ErrModelListNotSupported), "got %v", err)
			assert.Nil(t, models)

			lister, err := openai.NewModelLister(opts...)
			require.Error(t, err)
			assert.True(t, errors.Is(err, llms.ErrModelListNotSupported), "got %v", err)
			assert.Nil(t, lister)

			assert.Empty(t, list.requests)
		})
	}
}

func TestNewModelLister(t *testing.T) {
	t.Run("needs no model name", func(t *testing.T) {
		t.Setenv(openai.DefaultModelEnvVarName, "")
		list := &modelList{reply: `{"object":"list","data":[{"id":"gpt-5.5","created":1,"shutdown_date":null}]}`}
		lister, err := openai.NewModelLister(
			openai.WithToken("fixture"),
			openai.WithHTTPClient(list.client()),
		)
		require.NoError(t, err)
		models, err := lister.ListModels(context.Background())
		require.NoError(t, err)
		require.Len(t, models, 1)
		assert.Equal(t, "gpt-5.5", models[0].ID)
	})

	t.Run("an Azure lister is refused whatever else is missing", func(t *testing.T) {
		lister, err := openai.NewModelLister(
			openai.WithProvider(llms.ProviderAzure),
			openai.WithToken("fixture"),
		)
		require.Error(t, err)
		assert.True(t, errors.Is(err, llms.ErrModelListNotSupported), "got %v", err)
		assert.Nil(t, lister)
	})

	t.Run("missing token", func(t *testing.T) {
		t.Setenv(openai.DefaultTokenEnvVarName, "")
		lister, err := openai.NewModelLister()
		require.Error(t, err)
		assert.True(t, errors.Is(err, openai.ErrMissingToken), "got %v", err)
		assert.Nil(t, lister)
	})
}

// TestIntegrationListModels lists the models of the account behind
// OPENAI_API_KEY. Listing is free of charge.
func TestIntegrationListModels(t *testing.T) {
	apiKey := os.Getenv(openai.DefaultTokenEnvVarName)
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("OPENAI_API_KEY not set")
	}

	lister, err := openai.NewModelLister(openai.WithToken(apiKey))
	require.NoError(t, err)
	models, err := lister.ListModels(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, models)

	deprecated := 0
	for _, model := range models {
		assert.NotEmpty(t, model.ID)
		assert.NotEmpty(t, model.Lifecycle, "model %s", model.ID)
		if model.Lifecycle == llms.ModelLifecycleDeprecated {
			deprecated++
			assert.False(t, model.RetiresAt.IsZero(), "model %s", model.ID)
		}
	}
	t.Logf("%d models, %d with an announced shutdown date", len(models), deprecated)
}

// TestIntegrationListModelsOpenRouter lists the models of the account behind
// OPENROUTER_API_KEY. Listing is free of charge.
func TestIntegrationListModelsOpenRouter(t *testing.T) {
	apiKey := os.Getenv(openai.DefaultOpenRouterTokenEnvVarName)
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	lister, err := openai.NewModelLister(
		openai.WithProvider(llms.ProviderOpenRouter),
		openai.WithBaseURL(openRouterBaseURL),
		openai.WithToken(apiKey),
	)
	require.NoError(t, err)
	models, err := lister.ListModels(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, models)

	withEfforts := 0
	for _, model := range models {
		assert.NotEmpty(t, model.ID)
		assert.NotEmpty(t, model.DisplayName, "model %s", model.ID)
		assert.NotEmpty(t, model.Lifecycle, "model %s", model.ID)
		if !model.Efforts.Empty() {
			withEfforts++
		}
	}
	assert.Positive(t, withEfforts, "no model reports its effort levels")
	t.Logf("%d models, %d report their effort levels", len(models), withEfforts)
}
