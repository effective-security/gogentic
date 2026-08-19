package llmfactory_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlcfg "go.uber.org/config"
)

func Test_Factory(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "fakekey")
	t.Setenv("TAVILY_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	t.Setenv("PERPLEXITY_TOKEN", "fakekey")
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")
	t.Setenv("AZURE_OPENAI_API_KEY", "fakekey")
	t.Setenv("AZURE_OPENAI_URL", "https://azure.example")

	ctx := context.Background()
	cfg, err := llmfactory.LoadConfig("testdata/llm.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Providers)

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)
	model, err := f.GetModel(ctx, llmfactory.ModelOptions{})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm := model.(*fakeLLM)
	assert.Equal(t, "gpt-5", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	// Test ModelByName with single model
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"gpt-5"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"AZURE/gpt-5.1"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.1", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"OPENAI/gpt-5.1"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.1", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	// Test ModelByName with multiple preferred models
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"gpt-5.1-unknown", "gpt-5.1-mini"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.1-mini", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	// Test ModelByName with non-existent models (should fallback to default)
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"non-existent-model"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderAzure})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.1", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderOpenAI})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderOpenAIBedrock})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "openai.gpt-5.6-luna", fm.model)
	assert.Equal(t, "OPENAI_BEDROCK", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderAnthropic})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "claude-sonnet-4-8", fm.model)
	assert.Equal(t, "ANTHROPIC", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderBedrock})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "us.anthropic.claude-opus-4-20250514-v1:0", fm.model)
	assert.Equal(t, "BEDROCK", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderPerplexity})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "sonar", fm.model)
	assert.Equal(t, "PERPLEXITY", fm.provider)

	// Test AssistantModel with specific assistant
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "orchestrator"})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "claude-opus-4-8", fm.model)
	assert.Equal(t, "ANTHROPIC", fm.provider)

	// Test AssistantModel with preferred models
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "orchestrator", PreferredModels: []string{"gpt-5.5"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.5", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	// Test AssistantModel with path
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "azure_tool", PreferredModels: []string{"gpt-5.1"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.1", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "azure_tool"})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.1", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "azure_tool", PreferredModels: []string{"AZURE/gpt-5.1"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.1", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	// Test AssistantModel with non-existent assistant (should use default)
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "non-existent-assistant"})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5.5", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)

	// Test error cases
	// Test with unsupported provider type
	_, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderType("UNSUPPORTED")})
	assert.EqualError(t, err, "provider not found for type: UNSUPPORTED")

	// Test with empty providers list
	emptyCfg := &llmfactory.Config{}
	emptyFactory := llmfactory.New(emptyCfg)
	_, err = emptyFactory.GetModel(ctx, llmfactory.ModelOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")

	// Test with invalid default provider
	invalidCfg := &llmfactory.Config{
		DefaultProvider: "non-existent",
		Providers:       cfg.Providers,
	}
	invalidFactory := llmfactory.New(invalidCfg)
	model, err = invalidFactory.GetModel(ctx, llmfactory.ModelOptions{})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-5", fm.model)
	assert.Equal(t, "OPENAI", fm.provider)
}

func Test_ModelNamePathResolution(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name         string
		providers    []*llmfactory.ProviderConfig
		requested    string
		wantProvider string
		wantModel    string
	}{
		{
			name: "provider-qualified model preserves slash-delimited model ID",
			providers: []*llmfactory.ProviderConfig{
				{
					Name:            "openrouter",
					AvailableModels: []string{"anthropic/claude-sonnet-4"},
				},
			},
			requested:    "openrouter/anthropic/claude-sonnet-4",
			wantProvider: "openrouter",
			wantModel:    "anthropic/claude-sonnet-4",
		},
		{
			name: "unqualified slash-delimited model ID remains intact",
			providers: []*llmfactory.ProviderConfig{
				{
					Name:            "openrouter",
					AvailableModels: []string{"anthropic/claude-sonnet-4"},
					DefaultModel:    "fallback/model",
				},
			},
			requested:    "anthropic/claude-sonnet-4",
			wantProvider: "openrouter",
			wantModel:    "anthropic/claude-sonnet-4",
		},
		{
			name: "legacy provider-qualified model remains supported",
			providers: []*llmfactory.ProviderConfig{
				{
					Name:            "openai",
					AvailableModels: []string{"gpt-5"},
					DefaultModel:    "gpt-5",
				},
			},
			requested:    "openai/gpt-5",
			wantProvider: "openai",
			wantModel:    "gpt-5",
		},
		{
			name: "unknown prefix is part of the raw model ID",
			providers: []*llmfactory.ProviderConfig{
				{
					Name:            "openrouter",
					AvailableModels: []string{"vendor/family/model"},
					DefaultModel:    "fallback/model",
				},
			},
			requested:    "vendor/family/model",
			wantProvider: "openrouter",
			wantModel:    "vendor/family/model",
		},
	}

	originalNewLLM := llmfactory.NewLLM
	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, _ *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	t.Cleanup(func() {
		llmfactory.NewLLM = originalNewLLM
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := llmfactory.New(&llmfactory.Config{
				DefaultProvider: tt.providers[0].Name,
				Providers:       tt.providers,
			})

			model, err := factory.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{tt.requested}})
			require.NoError(t, err)
			require.NotNil(t, model)
			selected := model.(*fakeLLM)
			assert.Equal(t, tt.wantProvider, selected.provider)
			assert.Equal(t, tt.wantModel, selected.model)
		})
	}
}
func Test_Load(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "fakekey")
	t.Setenv("TAVILY_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	t.Setenv("PERPLEXITY_TOKEN", "fakekey")
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")
	t.Setenv("AZURE_OPENAI_API_KEY", "fakekey")
	t.Setenv("AZURE_OPENAI_URL", "https://azure.example")

	// Test successful load
	f, err := llmfactory.Load("testdata/llm.yaml")
	require.NoError(t, err)
	require.NotNil(t, f)

	skills := f.Skills("agent-foo")
	assert.Equal(t, 2, len(skills))

	// Test load with non-existent file
	_, err = llmfactory.Load("testdata/non-existent.yaml")
	require.Error(t, err)
}

func Test_CreateLLM(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "fakekey")
	t.Setenv("TAVILY_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	t.Setenv("PERPLEXITY_TOKEN", "fakekey")
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")
	t.Setenv("AZURE_OPENAI_API_KEY", "fakekey")
	t.Setenv("AZURE_OPENAI_URL", "https://azure.example")

	cfg := &llmfactory.ProviderConfig{
		Name: "test-provider",
		OpenAI: llmfactory.OpenAIConfig{
			APIType:    "OPEN_AI",
			APIVersion: "2024-02-15-preview",
		},
		AvailableModels: []string{"gpt-4"},
		DefaultModel:    "gpt-4",
		Token:           "fakekey",
	}

	// Test OpenAI provider
	cfg.OpenAI.APIType = string(llms.ProviderOpenAI)
	model, err := llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Azure provider
	cfg.OpenAI.APIType = string(llms.ProviderAzure)
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Azure AD provider
	cfg.OpenAI.APIType = string(llms.ProviderAzureAD)
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Anthropic provider
	cfg.OpenAI.APIType = string(llms.ProviderAnthropic)
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Bedrock provider
	cfg.OpenAI.APIType = string(llms.ProviderBedrock)
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Perplexity provider
	cfg.OpenAI.APIType = string(llms.ProviderPerplexity)
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test GoogleAI provider
	cfg.OpenAI.APIType = string(llms.ProviderGoogleAI)
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test unsupported provider
	cfg.OpenAI.APIType = "UNSUPPORTED"
	_, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider type")
}

func Test_CreateOpenRouterLLM(t *testing.T) {
	cfg := &llmfactory.ProviderConfig{
		Name:            "openrouter",
		Token:           "test-token",
		AvailableModels: []string{"anthropic/claude-sonnet-4"},
		Headers: map[string]string{
			"HTTP-Referer":       "https://secdi.example",
			"X-OpenRouter-Title": "Secdi",
		},
		OpenAI: llmfactory.OpenAIConfig{
			APIType: string(llms.ProviderOpenRouter),
			BaseURL: "https://openrouter.ai/api/v1",
		},
	}

	model, err := llmfactory.CreateLLM(cfg, []string{"anthropic/claude-sonnet-4"}, nil)
	require.NoError(t, err)
	assert.Equal(t, llms.ProviderOpenRouter, model.GetProviderType())
	assert.Equal(t, "anthropic/claude-sonnet-4", model.GetName())
	assert.True(t, llms.ProviderOpenRouter.Supports(llms.CapabilityText))
	assert.True(t, llms.ProviderOpenRouter.Supports(llms.CapabilityFunctionCalling))
	assert.True(t, llms.ProviderOpenRouter.Supports(llms.CapabilityJSONSchema))
	assert.False(t, llms.ProviderOpenRouter.Supports(llms.CapabilityBatch))
}

func Test_LoadConfig(t *testing.T) {
	// Test loading non-existent file
	_, err := llmfactory.LoadConfig("testdata/non-existent.yaml")
	require.Error(t, err)

	// Test loading invalid YAML
	_, err = llmfactory.LoadConfig("testdata/invalid.yaml")
	require.Error(t, err)
}

// Test_GoogleAIProvider tests GoogleAI provider with proper error handling
func Test_GoogleAIProvider(t *testing.T) {
	t.Skip("GoogleAI provider is not supported yet")
	// Test with valid API key
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")
	t.Setenv("AZURE_OPENAI_API_KEY", "fakekey")
	t.Setenv("AZURE_OPENAI_URL", "https://azure.example")

	cfg := &llmfactory.ProviderConfig{
		Name:  "google-test",
		Token: "fakekey",
		OpenAI: llmfactory.OpenAIConfig{
			APIType: "GOOGLEAI",
		},
		AvailableModels: []string{"gemini-2.5-flash-preview-05-20"},
		DefaultModel:    "gemini-2.5-flash-preview-05-20",
	}
	model, err := llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test with missing API key
	t.Setenv("GOOGLEAI_TOKEN", "")
	cfg.Token = ""

	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	// GoogleAI might fail due to missing API key, but we should handle it gracefully
	if err != nil {
		// If it fails, it should be due to missing API key or auth
		assert.True(t,
			containsAny(err.Error(), []string{"API key", "auth", "GEMINI_API_KEY", "You need an auth option"}),
			"Expected error to contain auth-related message, got: %s", err.Error())
	} else {
		require.NotNil(t, model)
	}
}

func Test_LoadOpenRouterConfig(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "openrouter-token")
	t.Setenv("OPENROUTER_HTTP_REFERER", "https://secdi.example")

	cfg, err := llmfactory.LoadConfig("testdata/openrouter.yaml")
	require.NoError(t, err)
	require.Len(t, cfg.Providers, 1)
	provider := cfg.Providers[0]
	assert.Equal(t, "openrouter-token", provider.Token)
	assert.Equal(t, "https://secdi.example", provider.Headers["HTTP-Referer"])
	assert.Equal(t, "Secdi", provider.Headers["X-OpenRouter-Title"])

	factory := llmfactory.New(cfg)
	tests := []struct {
		name      string
		orgID     string
		wantModel string
	}{
		{
			name:      "global assistant mapping",
			wantModel: "anthropic/claude-sonnet-4",
		},
		{
			name:      "organization assistant override",
			orgID:     "12345",
			wantModel: "openai/gpt-5-mini",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := factory.GetModel(context.Background(), llmfactory.ModelOptions{
				AssistantName: "triage_factor_extractor",
				OrgID:         tt.orgID,
			})
			require.NoError(t, err)
			assert.Equal(t, llms.ProviderOpenRouter, model.GetProviderType())
			assert.Equal(t, tt.wantModel, model.GetName())
		})
	}
}

// Test_ProviderConfigEdgeCases tests edge cases in provider configuration
func Test_ProviderConfigEdgeCases(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")

	// Test provider with empty available models
	cfg := &llmfactory.ProviderConfig{
		Name: "empty-models",
		OpenAI: llmfactory.OpenAIConfig{
			APIType: "OPEN_AI",
		},
		AvailableModels: []string{},
		DefaultModel:    "gpt-4",
	}

	model, err := llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test provider with nil available models
	cfg.AvailableModels = nil
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test provider with empty default model
	cfg.DefaultModel = ""
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	assert.EqualError(t, err, "no LLM model found")
	assert.Nil(t, model)
}

// Test_ModelCaching tests that models are properly cached
func Test_ModelCaching(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")

	// Create a config manually instead of loading from YAML to avoid env var dependencies
	cfg := &llmfactory.Config{
		Providers: []*llmfactory.ProviderConfig{
			{
				Name: "OPEN_AI",
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "OPEN_AI",
				},
				AvailableModels: []string{"gpt-4o", "gpt-4-mini"},
				DefaultModel:    "gpt-4o",
			},
		},
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)
	ctx := context.Background()
	// First call should create the model
	model1, err := f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderOpenAI})
	require.NoError(t, err)
	require.NotNil(t, model1)

	// Second call should return cached model
	model2, err := f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderOpenAI})
	require.NoError(t, err)
	require.NotNil(t, model2)

	// Should be the same instance
	assert.Equal(t, model1, model2)

	// Test name caching
	model3, err := f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"gpt-4-mini"}})
	require.NoError(t, err)
	require.NotNil(t, model3)

	model4, err := f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"gpt-4-mini"}})
	require.NoError(t, err)
	require.NotNil(t, model4)

	assert.Equal(t, model3, model4)
}

// Test_AssistantModelFallback tests assistant model fallback scenarios
func Test_AssistantModelFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")

	cfg := &llmfactory.Config{
		Providers: []*llmfactory.ProviderConfig{
			{
				Name: "OPEN_AI",
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "OPEN_AI",
				},
				AvailableModels: []string{"gpt-4", "gpt-4-mini"},
				DefaultModel:    "gpt-4",
			},
		},
		AssistantModels: map[string][]string{
			"default":      {"gpt-4-mini"},
			"orchestrator": {"gpt-4-mini"},
		},
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)
	ctx := context.Background()
	// Test assistant with specific mapping
	model, err := f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "orchestrator"})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm := model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)

	// Test assistant with default mapping
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "unknown_assistant"})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)

	// Test assistant with preferred models
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "unknown_assistant", PreferredModels: []string{"gpt-4"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4", fm.model) // Should still use default mapping
}

// Test_ConcurrentAccess tests concurrent access to factory methods
func Test_ConcurrentAccess(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	ctx := context.Background()
	// Create a config manually instead of loading from YAML to avoid env var dependencies
	cfg := &llmfactory.Config{
		Providers: []*llmfactory.ProviderConfig{
			{
				Name: "OPEN_AI",
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "OPEN_AI",
				},
				AvailableModels: []string{"gpt-4o", "gpt-4-mini"},
				DefaultModel:    "gpt-4o",
			},
		},
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)

	// Test concurrent access to ModelByType
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			model, err := f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderOpenAI})
			assert.NoError(t, err)
			assert.NotNil(t, model)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Test concurrent access to ModelByName
	for i := 0; i < 10; i++ {
		go func() {
			model, err := f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"gpt-4-mini"}})
			assert.NoError(t, err)
			assert.NotNil(t, model)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// Test_ProviderConfigFindModel tests the FindModel method
func Test_ProviderConfigFindModel(t *testing.T) {
	cfg := &llmfactory.ProviderConfig{
		AvailableModels: []string{"gpt-4", "gpt-4-mini", "gpt-3.5-turbo"},
		DefaultModel:    "gpt-4",
	}

	// Test finding existing model
	model, err := cfg.FindModel("gpt-4-mini")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4-mini", model)

	// Test finding first model in preferred list
	model, err = cfg.FindModel("gpt-4-mini", "gpt-3.5-turbo")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4-mini", model)

	// Test fallback to default when model not found
	model, err = cfg.FindModel("non-existent-model")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", model)

	// Test with empty preferred models
	model, err = cfg.FindModel()
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", model)

	// Test with nil available models
	cfg.AvailableModels = nil
	model, err = cfg.FindModel("gpt-4-mini")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", model)

	// Test with empty available models
	cfg.AvailableModels = []string{}
	model, err = cfg.FindModel("gpt-4-mini")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", model)
}

// Test_EmptyConfig tests factory behavior with empty configuration
func Test_EmptyConfig(t *testing.T) {
	// Test with completely empty config
	emptyCfg := &llmfactory.Config{}
	f := llmfactory.New(emptyCfg)
	ctx := context.Background()
	_, err := f.GetModel(ctx, llmfactory.ModelOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")

	_, err = f.GetModel(ctx, llmfactory.ModelOptions{ProviderType: llms.ProviderOpenAI})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider not found for type: OPENAI")

	_, err = f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"gpt-4"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")

	_, err = f.GetModel(ctx, llmfactory.ModelOptions{AssistantName: "orchestrator"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")
}

// Test_ProviderConfigWithBaseURL tests providers with custom base URLs
func Test_ProviderConfigWithBaseURL(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")

	cfg := &llmfactory.ProviderConfig{
		Name:  "custom-openai",
		Token: "fakekey",
		OpenAI: llmfactory.OpenAIConfig{
			APIType: "OPEN_AI",
			BaseURL: "https://custom.openai.com",
		},
		AvailableModels: []string{"gpt-4"},
		DefaultModel:    "gpt-4",
	}

	model, err := llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Azure with base URL
	cfg.OpenAI.APIType = "AZURE"
	cfg.OpenAI.BaseURL = "https://azure-test.openai.azure.com"
	cfg.OpenAI.APIVersion = "2024-02-15-preview"

	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)
}

// Test_ModelByNameWithFallback tests ModelByName fallback behavior
func Test_ModelByNameWithFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")

	cfg := &llmfactory.Config{
		Providers: []*llmfactory.ProviderConfig{
			{
				Name: "OPEN_AI",
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "OPEN_AI",
				},
				AvailableModels: []string{"gpt-4"},
				DefaultModel:    "gpt-4",
			},
			{
				Name: "AZURE",
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "AZURE",
				},
				AvailableModels: []string{"gpt-41-mini"},
				DefaultModel:    "gpt-41-mini",
			},
		},
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)
	ctx := context.Background()
	// Test fallback when first model not found but second is
	model, err := f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"non-existent", "gpt-41-mini"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm := model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	// Test fallback to default when no models found
	model, err = f.GetModel(ctx, llmfactory.ModelOptions{PreferredModels: []string{"non-existent-1", "non-existent-2"}})
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)
}

// Test_ProviderConfigWithTokens tests providers with different token configurations
func Test_ProviderConfigWithTokens(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	t.Setenv("PERPLEXITY_TOKEN", "fakekey")
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")
	t.Setenv("AZURE_OPENAI_API_KEY", "fakekey")
	t.Setenv("AZURE_OPENAI_URL", "https://azure.example")

	// Test OpenAI with token
	cfg := &llmfactory.ProviderConfig{
		Name:  "openai-with-token",
		Token: "fakekey",
		OpenAI: llmfactory.OpenAIConfig{
			APIType: "OPEN_AI",
		},
		AvailableModels: []string{"gpt-4"},
		DefaultModel:    "gpt-4",
	}

	model, err := llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test OpenAI without token (should still work as it uses env var)
	cfg.Token = ""
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Anthropic with token
	cfg.OpenAI.APIType = "ANTHROPIC"
	cfg.Token = "fakekey"
	model, err = llmfactory.CreateLLM(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, model)
}

func Test_YamlConfigOverride(t *testing.T) {
	cfg := &llmfactory.Config{
		AssistantModels: map[string][]string{
			"default": {"gpt-4"},
			"foo":     {"gpt-4-mini", "gpt-4", "gpt-3.5-turbo"},
			"bar":     {"gpt-5.1-mini", "gpt-5.1"},
			"sample":  {"s1", "s2"},
		},
		Orgs: map[string]*llmfactory.OrgConfig{
			"1000": {
				AssistantModels: map[string][]string{
					"foo":   {"gpt-5.1-mini", "gpt-5.1"},
					"bar":   {"claude-opus-4-8", "claude-sonnet-4-8"},
					"other": {"o1", "o2"},
				},
			},
		},
	}

	gs := yamlcfg.Static(cfg.AssistantModels)

	for orgID, orgCfg := range cfg.Orgs {
		ops := []yamlcfg.YAMLOption{gs, yamlcfg.Static(orgCfg.AssistantModels)}

		cprovider, err := yamlcfg.NewYAML(ops...)
		require.NoError(t, err)

		var res map[string][]string
		err = cprovider.Get(yamlcfg.Root).Populate(&res)
		require.NoError(t, err)

		cfg.Orgs[orgID].AssistantModels = res
	}

	orgCfg := cfg.Orgs["1000"]
	require.Equal(t, 5, len(orgCfg.AssistantModels))
	assert.Equal(t, []string{"gpt-4"}, orgCfg.AssistantModels["default"])
	assert.Equal(t, []string{"claude-opus-4-8", "claude-sonnet-4-8"}, orgCfg.AssistantModels["bar"])
	assert.Equal(t, []string{"gpt-5.1-mini", "gpt-5.1"}, orgCfg.AssistantModels["foo"])
	assert.Equal(t, []string{"o1", "o2"}, orgCfg.AssistantModels["other"])
	assert.Equal(t, []string{"s1", "s2"}, orgCfg.AssistantModels["sample"])
}

// Test_ModelFilter tests the per-org model filter (e.g. quota enforcement).
func Test_ModelFilter(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	ctx := context.Background()
	newConfig := func() *llmfactory.Config {
		return &llmfactory.Config{
			DefaultProvider: "OPENAI",
			Providers: []*llmfactory.ProviderConfig{
				{
					Name:            "OPENAI",
					OpenAI:          llmfactory.OpenAIConfig{APIType: "OPEN_AI"},
					AvailableModels: []string{"gpt-5", "gpt-5-mini"},
					DefaultModel:    "gpt-5",
				},
				{
					Name:            "ANTHROPIC",
					OpenAI:          llmfactory.OpenAIConfig{APIType: "ANTHROPIC"},
					AvailableModels: []string{"claude-opus-4-8"},
					DefaultModel:    "claude-opus-4-8",
				},
			},
			AssistantModels: map[string][]string{
				"orchestrator": {"gpt-5-mini", "gpt-5"},
			},
		}
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	t.Run("nil filter allows all", func(t *testing.T) {
		f := llmfactory.New(newConfig())

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1"})
		require.NoError(t, err)
		assert.Equal(t, "gpt-5", model.(*fakeLLM).model)
	})

	t.Run("filter receives orgID and model", func(t *testing.T) {
		type call struct {
			orgID string
			model string
		}
		var calls []call
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			calls = append(calls, call{orgID: orgID, model: modelName})
			return true
		}))

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org-42"})
		require.NoError(t, err)
		require.NotNil(t, model)
		require.Equal(t, []call{{orgID: "org-42", model: "gpt-5"}}, calls)
	})

	t.Run("deny default model returns error", func(t *testing.T) {
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return false
		}))

		_, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1"})
		require.Error(t, err)
		assert.EqualError(t, err, "model not available for org: gpt-5")
	})

	t.Run("skip denied preferred model and select allowed one", func(t *testing.T) {
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return modelName != "gpt-5-mini"
		}))

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5-mini", "gpt-5"}})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)
	})

	t.Run("all preferred denied falls back to allowed default", func(t *testing.T) {
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return modelName != "gpt-5-mini"
		}))

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5-mini"}})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)
	})

	t.Run("all denied including default returns error", func(t *testing.T) {
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return false
		}))

		_, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5-mini"}})
		require.Error(t, err)
		assert.EqualError(t, err, "model not available for org: gpt-5")
	})

	t.Run("filter is called once per selection", func(t *testing.T) {
		counts := map[string]int{}
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			counts[modelName]++
			return true
		}))

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5"}})
		require.NoError(t, err)
		require.NotNil(t, model)
		assert.Equal(t, 1, counts["gpt-5"], "resolver must not be invoked more than once per model")
	})

	t.Run("filter is called twice per selection", func(t *testing.T) {
		counts := map[string]int{}
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			counts[modelName]++
			return true
		}))

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"OPENAI/gpt-5"}})
		require.NoError(t, err)
		require.NotNil(t, model)
		assert.Equal(t, 1, counts["gpt-5"], "resolver must not be invoked more than once per model")
		assert.Equal(t, 1, counts["OPENAI/gpt-5"], "resolver must not be invoked more than once per model")
	})

	t.Run("filter gates cached model", func(t *testing.T) {
		deny := false
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return !deny
		}))

		// First call populates the cache while allowed.
		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5"}})
		require.NoError(t, err)
		assert.Equal(t, "gpt-5", model.(*fakeLLM).model)

		// Once denied, the cached model must not be returned; falls through to
		// the (also denied) default model.
		deny = true
		_, err = f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5"}})
		require.Error(t, err)
		assert.EqualError(t, err, "model not available for org: gpt-5")
	})

	t.Run("filter applies through AssistantModelForOrg", func(t *testing.T) {
		f := llmfactory.New(newConfig(), llmfactory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			if assistantName == "orchestrator" {
				return modelName == "gpt-5-mini"
			}
			return true
		}))

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1"})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)

		model, err = f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", AssistantName: "orchestrator"})
		require.NoError(t, err)
		fm = model.(*fakeLLM)
		assert.Equal(t, "gpt-5-mini", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)

	})

}

// Test_ModelFilter tests the per-org model filter (e.g. quota enforcement).
func Test_ModelFilterOnFactory(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	ctx := context.Background()
	newConfig := func() *llmfactory.Config {
		return &llmfactory.Config{
			DefaultProvider: "OPENAI",
			Providers: []*llmfactory.ProviderConfig{
				{
					Name:            "OPENAI",
					OpenAI:          llmfactory.OpenAIConfig{APIType: "OPEN_AI"},
					AvailableModels: []string{"gpt-5", "gpt-5-mini"},
					DefaultModel:    "gpt-5",
				},
				{
					Name:            "ANTHROPIC",
					OpenAI:          llmfactory.OpenAIConfig{APIType: "ANTHROPIC"},
					AvailableModels: []string{"claude-opus-4-8"},
					DefaultModel:    "claude-opus-4-8",
				},
			},
			AssistantModels: map[string][]string{
				"orchestrator": {"gpt-5-mini", "gpt-5"},
			},
		}
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	factory := llmfactory.New(newConfig())

	t.Run("nil filter allows all", func(t *testing.T) {
		model, err := factory.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1"})
		require.NoError(t, err)
		assert.Equal(t, "gpt-5", model.(*fakeLLM).model)
	})

	t.Run("filter receives orgID and model", func(t *testing.T) {
		type call struct {
			orgID         string
			assistantName string
			model         string
		}
		var calls []call
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			calls = append(calls, call{orgID: orgID, assistantName: assistantName, model: modelName})
			return true
		})

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org-42"})
		require.NoError(t, err)
		require.NotNil(t, model)
		require.Equal(t, []call{{orgID: "org-42", assistantName: "default", model: "gpt-5"}}, calls)
	})

	t.Run("deny default model returns error", func(t *testing.T) {
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return false
		})

		_, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1"})
		require.Error(t, err)
		assert.EqualError(t, err, "model not available for org: gpt-5")
	})

	t.Run("skip denied preferred model and select allowed one", func(t *testing.T) {
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return modelName != "gpt-5-mini"
		})

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5-mini", "gpt-5"}})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)
	})

	t.Run("all preferred denied falls back to allowed default", func(t *testing.T) {
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return modelName != "gpt-5-mini"
		})

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5-mini"}})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)
	})

	t.Run("all denied including default returns error", func(t *testing.T) {
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return false
		})

		_, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5-mini"}})
		require.Error(t, err)
		assert.EqualError(t, err, "model not available for org: gpt-5")
	})

	t.Run("filter is called once per selection", func(t *testing.T) {
		counts := map[string]int{}
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			counts[modelName]++
			return true
		})

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5"}})
		require.NoError(t, err)
		require.NotNil(t, model)
		assert.Equal(t, 1, counts["gpt-5"], "resolver must not be invoked more than once per model")
	})

	t.Run("filter is called twice per selection", func(t *testing.T) {
		counts := map[string]int{}
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			counts[modelName]++
			return true
		})

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"OPENAI/gpt-5"}})
		require.NoError(t, err)
		require.NotNil(t, model)
		assert.Equal(t, 1, counts["gpt-5"], "resolver must not be invoked more than once per model")
		assert.Equal(t, 1, counts["OPENAI/gpt-5"], "resolver must not be invoked more than once per model")
	})

	t.Run("filter gates cached model", func(t *testing.T) {
		deny := false
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return !deny
		})

		// First call populates the cache while allowed.
		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5"}})
		require.NoError(t, err)
		assert.Equal(t, "gpt-5", model.(*fakeLLM).model)

		// Once denied, the cached model must not be returned; falls through to
		// the (also denied) default model.
		deny = true
		_, err = f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", PreferredModels: []string{"gpt-5"}})
		require.Error(t, err)
		assert.EqualError(t, err, "model not available for org: gpt-5")
	})

	t.Run("filter applies through AssistantModelForOrg", func(t *testing.T) {
		f := factory.WithModelFilter(func(ctx context.Context, orgID, assistantName, modelName string) bool {
			return modelName != "gpt-5-mini"
		})

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{OrgID: "org1", AssistantName: "orchestrator"})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)
	})
}

// Test_GetModel_RequiredCapabilities tests that GetModel restricts candidates
// to providers whose type supports ALL of the requested capabilities.
func Test_GetModel_RequiredCapabilities(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	ctx := context.Background()
	newConfig := func() *llmfactory.Config {
		return &llmfactory.Config{
			DefaultProvider: "OPENAI",
			Providers: []*llmfactory.ProviderConfig{
				{
					Name:            "OPENAI",
					OpenAI:          llmfactory.OpenAIConfig{APIType: string(llms.ProviderOpenAI)},
					AvailableModels: []string{"gpt-5", "gpt-5-mini"},
					DefaultModel:    "gpt-5",
				},
				{
					Name:            "ANTHROPIC",
					OpenAI:          llmfactory.OpenAIConfig{APIType: string(llms.ProviderAnthropic)},
					AvailableModels: []string{"claude-opus-4-8"},
					DefaultModel:    "claude-opus-4-8",
				},
			},
		}
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels []string, opts *llmfactory.Options) (llms.Model, error) {
		model, err := cfg.FindModel(preferredModels...)
		if err != nil {
			return nil, err
		}
		return &fakeLLM{provider: cfg.Name, model: model}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	// Sanity check: only OpenAI advertises the Batch capability, Anthropic does not.
	require.True(t, llms.ProviderOpenAI.Supports(llms.CapabilityBatch))
	require.False(t, llms.ProviderAnthropic.Supports(llms.CapabilityBatch))

	t.Run("preferred model on unsupported provider is skipped, falls back to default", func(t *testing.T) {
		f := llmfactory.New(newConfig())

		// claude lives on ANTHROPIC, which lacks Batch, so it is skipped and the
		// default OpenAI model is returned instead.
		model, err := f.GetModel(ctx, llmfactory.ModelOptions{
			PreferredModels:      []string{"claude-opus-4-8"},
			RequiredCapabilities: llms.CapabilityBatch,
		})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)
	})

	t.Run("preferred model on supported provider is selected", func(t *testing.T) {
		f := llmfactory.New(newConfig())

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{
			PreferredModels:      []string{"gpt-5-mini"},
			RequiredCapabilities: llms.CapabilityBatch,
		})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "gpt-5-mini", fm.model)
		assert.Equal(t, "OPENAI", fm.provider)
	})

	t.Run("provider type not supporting capability returns error", func(t *testing.T) {
		f := llmfactory.New(newConfig())

		_, err := f.GetModel(ctx, llmfactory.ModelOptions{
			ProviderType:         "ANTHROPIC",
			RequiredCapabilities: llms.CapabilityBatch,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support required capabilities")
	})

	t.Run("default provider not supporting capability returns error", func(t *testing.T) {
		cfg := newConfig()
		cfg.DefaultProvider = "ANTHROPIC"
		f := llmfactory.New(cfg)

		_, err := f.GetModel(ctx, llmfactory.ModelOptions{
			RequiredCapabilities: llms.CapabilityBatch,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support required capabilities")
	})

	t.Run("zero capabilities matches any provider", func(t *testing.T) {
		f := llmfactory.New(newConfig())

		model, err := f.GetModel(ctx, llmfactory.ModelOptions{
			PreferredModels: []string{"claude-opus-4-8"},
		})
		require.NoError(t, err)
		fm := model.(*fakeLLM)
		assert.Equal(t, "claude-opus-4-8", fm.model)
		assert.Equal(t, "ANTHROPIC", fm.provider)
	})
}

// Helper function to check if error message contains any of the expected strings
func containsAny(errMsg string, expectedStrings []string) bool {
	for _, expected := range expectedStrings {
		if contains(errMsg, expected) {
			return true
		}
	}
	return false
}

// Helper function to check if string contains substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (contains(s[:len(s)-1], substr) ||
			contains(s[1:], substr))))
}

type fakeLLM struct {
	provider string
	model    string
}

func (f *fakeLLM) Name() string {
	return f.model
}

func (f *fakeLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", nil
}
func (f *fakeLLM) GenerateContent(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, nil
}

func (f *fakeLLM) GetProviderType() llms.ProviderType {
	return llms.ProviderOpenAI
}

func (f *fakeLLM) GetName() string {
	return f.model
}
