package llmfactory

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderPolicy(t *testing.T) {
	cfg := &ProviderConfig{
		Name:         "ANTHROPIC",
		DefaultModel: "claude-sonnet-*,medium",
		AvailableModels: []string{
			"claude-fable-5-2,high",
			"claude-opus-4-8",
			"claude-opus-4-6",
			"claude-sonnet-5-5",
			"claude-sonnet-4-6",
			"claude-sonnet-6-preview",
			"claude-opus-*,low",
		},
		DisabledModels: []string{
			"*-preview",
			"claude-opus-4-6",
		},
		MaxEffort: "xhigh",
		OpenAI: OpenAIConfig{
			APIType: "ANTHROPIC",
		},
	}
	policy, err := newProviderPolicy(cfg, 3)
	require.NoError(t, err)
	require.NoError(t, policy.validate())
	assert.Equal(t, 3, policy.index)
	assert.Equal(t, llms.ProviderAnthropic, policy.typ)
	assert.Equal(t, llms.ReasoningEffortXHigh, policy.maxEffort)

	t.Run("allow and deny", func(t *testing.T) {
		assert.True(t, policy.allowed("claude-fable-5-2"))
		assert.True(t, policy.allowed("claude-opus-4-8"))
		// A pattern entry admits IDs that are not listed by name.
		assert.True(t, policy.allowed("claude-opus-5-5"))
		// The deny-list wins over an exact entry and over a pattern entry.
		assert.False(t, policy.allowed("claude-opus-4-6"))
		assert.False(t, policy.allowed("claude-opus-5-preview"))
		assert.False(t, policy.allowed("claude-sonnet-6-preview"))
		// Nothing admits these.
		assert.False(t, policy.allowed("claude-haiku-5-5"))
		assert.False(t, policy.allowed("claude-fable-5-1"))
	})

	t.Run("known models are the exact entries that are not denied", func(t *testing.T) {
		assert.Equal(t, []string{
			"claude-fable-5-2",
			"claude-opus-4-8",
			"claude-sonnet-5-5",
			"claude-sonnet-4-6",
		}, policy.literals())
	})

	t.Run("candidates", func(t *testing.T) {
		// An exact ID selects itself when allowed, listed or not.
		assert.Equal(t, []string{"claude-opus-4-8"}, policy.candidates(newModelMatcher("claude-opus-4-8")))
		assert.Equal(t, []string{"claude-opus-5-5"}, policy.candidates(newModelMatcher("claude-opus-5-5")))
		assert.Empty(t, policy.candidates(newModelMatcher("claude-opus-4-6")))
		assert.Empty(t, policy.candidates(newModelMatcher("claude-haiku-5-5")))
		// A pattern selects among the known models, best first.
		assert.Equal(t,
			[]string{"claude-sonnet-5-5", "claude-sonnet-4-6"},
			policy.candidates(newModelMatcher("claude-sonnet-*")))
		assert.Equal(t, []string{"claude-opus-4-8"}, policy.candidates(newModelMatcher("claude-opus-*")))
		assert.Empty(t, policy.candidates(newModelMatcher("claude-haiku-*")))
		assert.Equal(t,
			[]string{"claude-sonnet-5-5", "claude-fable-5-2", "claude-opus-4-8", "claude-sonnet-4-6"},
			policy.candidates(newModelMatcher("*")),
			"a pattern across families orders by version alone")
	})

	t.Run("default model", func(t *testing.T) {
		model, effort, ok := policy.defaultModel()
		require.True(t, ok)
		assert.Equal(t, "claude-sonnet-5-5", model)
		assert.Equal(t, llms.ReasoningEffortMedium, effort)
	})

	t.Run("effort", func(t *testing.T) {
		ceiling := llms.ReasoningEffortXHigh
		// The level on the selecting reference wins.
		assert.Equal(t,
			llms.EffortPolicy{Default: llms.ReasoningEffortLow, Max: ceiling},
			policy.effortPolicy(llms.ReasoningEffortLow, "claude-fable-5-2"))
		// Otherwise the first matching available_models entry decides.
		assert.Equal(t,
			llms.EffortPolicy{Default: llms.ReasoningEffortHigh, Max: ceiling},
			policy.effortPolicy(llms.ReasoningEffortDefault, "claude-fable-5-2"))
		// The exact entry without a level comes before the pattern entry.
		assert.Equal(t,
			llms.EffortPolicy{Max: ceiling},
			policy.effortPolicy(llms.ReasoningEffortDefault, "claude-opus-4-8"))
		assert.Equal(t,
			llms.EffortPolicy{Default: llms.ReasoningEffortLow, Max: ceiling},
			policy.effortPolicy(llms.ReasoningEffortDefault, "claude-opus-5-5"))
		// A model no entry matches gets the ceiling only.
		assert.Equal(t,
			llms.EffortPolicy{Max: ceiling},
			policy.effortPolicy(llms.ReasoningEffortDefault, "claude-haiku-5-5"))
	})
}

func TestProviderPolicy_ExactDefault(t *testing.T) {
	policy, err := newProviderPolicy(&ProviderConfig{
		Name:            "OPENAI",
		DefaultModel:    "gpt-5,low",
		AvailableModels: []string{"gpt-5.1"},
	}, 0)
	require.NoError(t, err)
	require.NoError(t, policy.validate())

	// An exact default does not need to be listed in available_models.
	model, effort, ok := policy.defaultModel()
	require.True(t, ok)
	assert.Equal(t, "gpt-5", model)
	assert.Equal(t, llms.ReasoningEffortLow, effort)
	assert.False(t, policy.allowed("gpt-5"))

	// No default configured.
	policy, err = newProviderPolicy(&ProviderConfig{Name: "OPENAI"}, 0)
	require.NoError(t, err)
	require.NoError(t, policy.validate())
	_, _, ok = policy.defaultModel()
	assert.False(t, ok)
	assert.Equal(t, llms.EffortPolicy{}, policy.effortPolicy(llms.ReasoningEffortDefault, "any"))
}

func TestConfigValidate(t *testing.T) {
	provider := func(mutate func(*ProviderConfig)) *Config {
		cfg := &ProviderConfig{
			Name:            "OPENAI",
			DefaultModel:    "gpt-5.1",
			AvailableModels: []string{"gpt-5.1", "gpt-5.5"},
		}
		mutate(cfg)
		return &Config{
			Providers: []*ProviderConfig{cfg},
		}
	}

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "valid legacy config",
			cfg:  provider(func(*ProviderConfig) {}),
		},
		{
			name: "empty config",
			cfg:  &Config{},
		},
		{
			name: "valid new syntax",
			cfg: provider(func(c *ProviderConfig) {
				c.DefaultModel = "gpt-5*,low"
				c.AvailableModels = []string{"gpt-5.1,medium", "gpt-5.5", "gpt-*-mini"}
				c.DisabledModels = []string{"*-preview"}
				c.MaxEffort = "High"
			}),
		},
		{
			name: "unknown effort in available_models",
			cfg: provider(func(c *ProviderConfig) {
				c.AvailableModels = []string{"gpt-5.1,hihg"}
			}),
			wantErr: `provider OPENAI: available_models: model reference "gpt-5.1,hihg": invalid reasoning effort: "hihg"`,
		},
		{
			name: "bare effort level left by a YAML flow sequence",
			cfg: provider(func(c *ProviderConfig) {
				c.AvailableModels = []string{"gpt-5.1", "medium"}
			}),
			wantErr: `provider OPENAI: available_models: model reference "medium" is only an effort level`,
		},
		{
			name: "effort on a disabled_models entry",
			cfg: provider(func(c *ProviderConfig) {
				c.DisabledModels = []string{"gpt-5.5,low"}
			}),
			wantErr: `provider OPENAI: disabled_models entry "gpt-5.5,low" must not carry an effort`,
		},
		{
			name: "unknown max_effort",
			cfg: provider(func(c *ProviderConfig) {
				c.MaxEffort = "ultra"
			}),
			wantErr: `provider OPENAI: max_effort: invalid reasoning effort: "ultra"`,
		},
		{
			name: "unknown effort in default_model",
			cfg: provider(func(c *ProviderConfig) {
				c.DefaultModel = "gpt-5.1,default"
			}),
			wantErr: `provider OPENAI: default_model: model reference "gpt-5.1,default": invalid reasoning effort: "default"`,
		},
		{
			name: "default model is disabled",
			cfg: provider(func(c *ProviderConfig) {
				c.DisabledModels = []string{"gpt-5.1"}
			}),
			wantErr: `provider OPENAI: default_model "gpt-5.1" is listed in disabled_models`,
		},
		{
			name: "default pattern selects nothing",
			cfg: provider(func(c *ProviderConfig) {
				c.DefaultModel = "gpt-6*"
			}),
			wantErr: `provider OPENAI: default_model "gpt-6*" matches no available_models entry`,
		},
		{
			name: "invalid assistant reference",
			cfg: &Config{
				AssistantModels: map[string][]string{
					"planner": {"OPENAI/gpt-5.1,turbo"},
				},
			},
			wantErr: `assistant_models: assistant planner: model reference "OPENAI/gpt-5.1,turbo": invalid reasoning effort: "turbo"`,
		},
		{
			name: "invalid org assistant reference",
			cfg: &Config{
				Orgs: map[string]*OrgConfig{
					"1000": {
						AssistantModels: map[string][]string{
							"planner": {"high"},
						},
					},
				},
			},
			wantErr: `orgs_override.1000: assistant planner: model reference "high" is only an effort level`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("every problem is reported", func(t *testing.T) {
		cfg := provider(func(c *ProviderConfig) {
			c.AvailableModels = []string{"gpt-5.1,hihg"}
			c.MaxEffort = "ultra"
		})
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `invalid reasoning effort: "hihg"`)
		assert.Contains(t, err.Error(), `invalid reasoning effort: "ultra"`)
	})
}

func TestLoadConfig_Validates(t *testing.T) {
	cfg, err := LoadConfig("testdata/llm_effort.yaml")
	require.NoError(t, err)
	require.Len(t, cfg.Providers, 2)
	assert.Equal(t, "gpt-5.1,low", cfg.Providers[0].DefaultModel)
	assert.Equal(t, []string{"*-sol"}, cfg.Providers[0].DisabledModels)
	assert.Equal(t, "high", cfg.Providers[0].MaxEffort)
	assert.Empty(t, cfg.Providers[1].DisabledModels)

	// The existing fixtures use none of the new syntax and still load.
	for _, name := range []string{
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"AWS_BEARER_TOKEN_BEDROCK",
		"AZURE_OPENAI_API_KEY",
		"GOOGLEAI_TOKEN",
		"PERPLEXITY_TOKEN",
		"OPENROUTER_API_KEY",
	} {
		t.Setenv(name, "fakekey")
	}
	t.Setenv("AZURE_OPENAI_URL", "https://azure.example")
	t.Setenv("OPENROUTER_HTTP_REFERER", "https://referer.example")
	for _, file := range []string{"testdata/llm.yaml", "testdata/openrouter.yaml"} {
		_, err := LoadConfig(file)
		require.NoError(t, err, file)
	}

	// A flow sequence splits "OPENAI/gpt-5.1,medium" into two entries; the
	// second one is caught instead of being skipped during resolution.
	_, err = LoadConfig("testdata/llm_flow_effort.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid llm config testdata/llm_flow_effort.yaml")
	assert.Contains(t, err.Error(), `model reference "medium" is only an effort level`)
}

func TestFindModel_Policy(t *testing.T) {
	cfg := &ProviderConfig{
		Name:            "OPENAI",
		DefaultModel:    "gpt-5,low",
		AvailableModels: []string{"gpt-5.1,medium", "gpt-5.5", "gpt-*-mini"},
		DisabledModels:  []string{"gpt-5.5"},
	}

	tests := []struct {
		name   string
		models []string
		want   string
	}{
		{"exact entry", []string{"gpt-5.1"}, "gpt-5.1"},
		{"first allowed name wins", []string{"gpt-4", "gpt-5.1"}, "gpt-5.1"},
		{"pattern entry admits a concrete name", []string{"gpt-5.1-mini"}, "gpt-5.1-mini"},
		{"denied name falls back to the default", []string{"gpt-5.5"}, "gpt-5"},
		{"unknown name falls back to the default without its effort", []string{"gpt-4"}, "gpt-5"},
		{"no names", nil, "gpt-5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cfg.FindModel(tt.models...)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("no default", func(t *testing.T) {
		noDefault := &ProviderConfig{
			AvailableModels: []string{"gpt-5.1"},
		}
		_, err := noDefault.FindModel("gpt-4")
		require.Error(t, err)
		assert.Equal(t, "no LLM model found", err.Error())
	})

	t.Run("denied default", func(t *testing.T) {
		denied := &ProviderConfig{
			DefaultModel:   "gpt-5",
			DisabledModels: []string{"gpt-5"},
		}
		_, err := denied.FindModel()
		require.Error(t, err)
		assert.Equal(t, "no LLM model found", err.Error())
	})
}
