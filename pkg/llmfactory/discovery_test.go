package llmfactory

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const discoveryFixture = "testdata/llm_discovery.yaml"

func TestLoadConfig_Discovery(t *testing.T) {
	cfg, err := LoadConfig(discoveryFixture)
	require.NoError(t, err)
	require.Len(t, cfg.Providers, 2)
	assert.Equal(t, &DiscoveryConfig{
		Enabled: true,
		Refresh: "6h",
	}, cfg.Providers[0].Discovery)
	assert.Nil(t, cfg.Providers[1].Discovery)

	// Only the provider with the section gets a catalog, and New reads
	// nothing.
	lister := &fakeLister{}
	stubModelLister(t, map[string]*fakeLister{
		"ANTHROPIC": lister,
	})
	f := New(cfg).(*factory)
	require.Len(t, f.policies, 2)
	require.NotNil(t, f.policies[0].catalog)
	assert.Equal(t, 6*time.Hour, f.policies[0].catalog.refresh)
	assert.Nil(t, f.policies[1].catalog)
	assert.Zero(t, lister.count())
}

func TestConfig_ValidateDiscovery(t *testing.T) {
	provider := func(apiType string, discovery *DiscoveryConfig) *ProviderConfig {
		return &ProviderConfig{
			Name:            "P",
			DefaultModel:    "model-1",
			AvailableModels: []string{"model-1"},
			Discovery:       discovery,
			OpenAI: OpenAIConfig{
				APIType: apiType,
			},
		}
	}
	enabled := func(refresh string) *DiscoveryConfig {
		return &DiscoveryConfig{
			Enabled: true,
			Refresh: refresh,
		}
	}

	tests := []struct {
		name        string
		provider    *ProviderConfig
		wantErr     string
		wantRefresh time.Duration
	}{
		{
			name:        "default refresh",
			provider:    provider("ANTHROPIC", enabled("")),
			wantRefresh: time.Hour,
		},
		{
			name:        "configured refresh",
			provider:    provider("OPENAI", enabled("90m")),
			wantRefresh: 90 * time.Minute,
		},
		{
			name:        "OPENROUTER",
			provider:    provider("OPENROUTER", enabled("1m")),
			wantRefresh: time.Minute,
		},
		{
			name:        "GOOGLEAI",
			provider:    provider("GOOGLEAI", enabled("24h")),
			wantRefresh: 24 * time.Hour,
		},
		{
			name:     "no section",
			provider: provider("ANTHROPIC", nil),
		},
		{
			name:     "a section that is not enabled is off",
			provider: provider("AZURE", &DiscoveryConfig{Refresh: "6h"}),
		},
		{
			name:     "a provider type without a model list",
			provider: provider("AZURE", enabled("")),
			wantErr:  `provider P: discovery is not available for api_type "AZURE"`,
		},
		{
			name:     "Bedrock has no model list either",
			provider: provider("ANTHROPIC_BEDROCK", enabled("")),
			wantErr:  `provider P: discovery is not available for api_type "ANTHROPIC_BEDROCK"`,
		},
		{
			name:     "a refresh that is not a duration",
			provider: provider("ANTHROPIC", enabled("hourly")),
			wantErr:  "provider P: discovery.refresh",
		},
		{
			name:     "a refresh that is not a duration is reported when off, too",
			provider: provider("ANTHROPIC", &DiscoveryConfig{Refresh: "hourly"}),
			wantErr:  "provider P: discovery.refresh",
		},
		{
			name:     "a refresh below the minimum",
			provider: provider("ANTHROPIC", enabled("30s")),
			wantErr:  "provider P: discovery.refresh 30s is below the minimum of 1m0s",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Providers: []*ProviderConfig{tt.provider},
			}
			err := cfg.Validate()
			policy, _ := newProviderPolicy(tt.provider, 0)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.False(t, policy.discovers())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRefresh, policy.refresh)
			assert.Equal(t, tt.wantRefresh > 0, policy.discovers())
		})
	}

	t.Run("a pattern default needs no listed model with discovery", func(t *testing.T) {
		p := provider("ANTHROPIC", enabled(""))
		p.DefaultModel = "claude-sonnet-*"
		p.AvailableModels = []string{"claude-*"}
		cfg := &Config{
			Providers: []*ProviderConfig{p},
		}
		require.NoError(t, cfg.Validate())

		p.Discovery = nil
		require.ErrorContains(t, cfg.Validate(), `default_model "claude-sonnet-*" matches no available_models entry`)
	})
}

func TestCreateModelLister(t *testing.T) {
	for _, apiType := range []string{"OPENAI", "OPEN_AI", "OPENROUTER", "ANTHROPIC", "GOOGLEAI"} {
		t.Run(apiType, func(t *testing.T) {
			cfg := &ProviderConfig{
				Name:  "P",
				Token: "fakekey",
				OpenAI: OpenAIConfig{
					APIType: apiType,
				},
			}
			lister, err := CreateModelLister(cfg, NewOptions())
			require.NoError(t, err)
			assert.NotNil(t, lister)
		})
	}

	for _, apiType := range []string{"AZURE", "AZURE_AD", "BEDROCK", "ANTHROPIC_BEDROCK", "OPENAI_BEDROCK", "", "unknown"} {
		t.Run("unsupported "+apiType, func(t *testing.T) {
			cfg := &ProviderConfig{
				Name:  "P",
				Token: "fakekey",
				OpenAI: OpenAIConfig{
					APIType: apiType,
				},
			}
			lister, err := CreateModelLister(cfg, NewOptions())
			require.Error(t, err)
			assert.True(t, errors.Is(err, llms.ErrModelListNotSupported), "got %v", err)
			assert.Nil(t, lister)
		})
	}
}

// recordedAPIs answers the model list requests of Anthropic and OpenAI with
// the lists recorded from their APIs, and every other request like
// wireClient.
type recordedAPIs struct {
	wireClient
	t     *testing.T
	lists []string
}

func (c *recordedAPIs) Do(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		return c.wireClient.Do(r)
	}
	var name string
	switch {
	case r.URL.Host == "api.anthropic.com" && r.URL.Path == "/v1/models" && !r.URL.Query().Has("after_id"):
		name = "anthropic/testdata/models_page1.json"
	case r.URL.Host == "api.anthropic.com" && r.URL.Path == "/v1/models":
		name = "anthropic/testdata/models_page2.json"
	case r.URL.Host == "api.openai.com" && r.URL.Path == "/v1/models":
		name = "openai/testdata/models_openai.json"
	default:
		require.FailNow(c.t, "unexpected request", r.URL.String())
	}
	c.lists = append(c.lists, name)
	raw, err := os.ReadFile(filepath.Join("..", "llms", filepath.FromSlash(name)))
	require.NoError(c.t, err)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(string(raw))),
		Request: r,
	}, nil
}

// Discovery end to end, with the real listers and provider clients and the
// model lists recorded from the Anthropic and OpenAI APIs: the model a
// reference resolves to is the one named in the provider request.
func TestFactory_DiscoveryReachesTheWire(t *testing.T) {
	ctx := context.Background()
	client := &recordedAPIs{t: t}
	discovery := &DiscoveryConfig{
		Enabled: true,
	}
	cfg := &Config{
		Providers: []*ProviderConfig{
			{
				Name:  "ANTHROPIC",
				Token: "fixture",
				AvailableModels: []string{
					"claude-sonnet-*,low",
					"claude-opus-*",
					"claude-haiku-*",
				},
				Discovery: discovery,
				OpenAI: OpenAIConfig{
					APIType: "ANTHROPIC",
				},
			},
			{
				Name:  "OPENAI",
				Token: "fixture",
				AvailableModels: []string{
					"gpt-5*",
				},
				Discovery: discovery,
				OpenAI: OpenAIConfig{
					APIType: "OPENAI",
				},
			},
		},
		AssistantModels: map[string][]string{
			// Retired in the recorded list: the newest Sonnet takes over.
			"retired": {"ANTHROPIC/claude-sonnet-4-20250514"},
			// Listed only as claude-haiku-4-5-20251001.
			"floating": {"ANTHROPIC/claude-haiku-4-5"},
			"newest":   {"ANTHROPIC/claude-opus-*,max"},
			// Opus 4.6 has no xhigh level, as the list says.
			"reported": {"ANTHROPIC/claude-opus-4-6,xhigh"},
			// Not in the recorded OpenAI list.
			"gone": {"OPENAI/gpt-5.1"},
			// Listed only as gpt-5.5-2026-04-23.
			"alias": {"OPENAI/gpt-5.5"},
		},
	}
	require.NoError(t, cfg.Validate())
	f := New(cfg, WithHTTPClient(client))

	generate := func(t *testing.T, assistant string) (llms.Model, map[string]any) {
		t.Helper()
		model, err := f.GetModel(ctx, ModelOptions{AssistantName: assistant})
		require.NoError(t, err)
		messages := []llms.Message{{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "hi"},
			},
		}}
		_, err = model.GenerateContent(ctx, messages)
		require.NoError(t, err)
		return model, client.last(t)
	}

	t.Run("anthropic: a retired model is replaced", func(t *testing.T) {
		model, body := generate(t, "retired")
		assert.Equal(t, "claude-sonnet-5-5", model.GetName())
		assert.Equal(t, "claude-sonnet-5-5", body["model"])
		// The effort of the entry that allows the substitute applies.
		assert.Equal(t, map[string]any{"type": "adaptive"}, body["thinking"])
		assert.Equal(t, map[string]any{"effort": "low"}, body["output_config"])
	})

	t.Run("anthropic: a floating ID listed as a snapshot is kept", func(t *testing.T) {
		model, body := generate(t, "floating")
		assert.Equal(t, "claude-haiku-4-5", model.GetName())
		assert.Equal(t, "claude-haiku-4-5", body["model"])
	})

	t.Run("anthropic: a pattern selects the newest listed model", func(t *testing.T) {
		model, body := generate(t, "newest")
		assert.Equal(t, "claude-opus-5-5", model.GetName())
		assert.Equal(t, "claude-opus-5-5", body["model"])
		assert.Equal(t, map[string]any{"effort": "max"}, body["output_config"])
	})

	t.Run("anthropic: the levels the list reports are used", func(t *testing.T) {
		model, body := generate(t, "reported")
		assert.Equal(t, "claude-opus-4-6", model.GetName())
		assert.Equal(t, map[string]any{"effort": "high"}, body["output_config"])
	})

	t.Run("openai: a model that left the list is replaced", func(t *testing.T) {
		model, body := generate(t, "gone")
		assert.Equal(t, "gpt-5.5-2026-04-23", model.GetName())
		assert.Equal(t, "gpt-5.5-2026-04-23", body["model"])
	})

	t.Run("openai: a floating ID listed as a snapshot is kept", func(t *testing.T) {
		model, body := generate(t, "alias")
		assert.Equal(t, "gpt-5.5", model.GetName())
		assert.Equal(t, "gpt-5.5", body["model"])
	})

	// Each list was read once: two pages for Anthropic, one for OpenAI.
	assert.Equal(t, []string{
		"anthropic/testdata/models_page1.json",
		"anthropic/testdata/models_page2.json",
		"openai/testdata/models_openai.json",
	}, client.lists)
}
