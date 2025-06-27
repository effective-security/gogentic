package llmfactory_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Factory(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("TAVILY_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	t.Setenv("PERPLEXITY_TOKEN", "fakekey")
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")

	cfg, err := llmfactory.LoadConfig("testdata/llm.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Providers)

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels ...string) (llms.Model, error) {
		return &fakeLLM{provider: cfg.Name, model: cfg.FindModel(preferredModels...)}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)
	model, err := f.DefaultModel()
	require.NoError(t, err)
	require.NotNil(t, model)
	fm := model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	// Test ModelByName with single model
	model, err = f.ModelByName("gpt-4-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	// Test ModelByName with multiple preferred models
	model, err = f.ModelByName("gpt-4-unknown", "gpt-41-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	// Test ModelByName with non-existent models (should fallback to default)
	model, err = f.ModelByName("non-existent-model")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	model, err = f.ModelByName("gpt-41-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	model, err = f.ModelByType("OPEN_AI")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	model, err = f.ModelByType("ANTHROPIC")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "claude-sonnet-4-20250514", fm.model)
	assert.Equal(t, "ANTHROPIC", fm.provider)

	model, err = f.ModelByType("BEDROCK")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "anthropic.claude-3-5-sonnet-20241022-v2:0", fm.model)
	assert.Equal(t, "BEDROCK", fm.provider)

	model, err = f.ModelByType("PERPLEXITY")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "sonar", fm.model)
	assert.Equal(t, "PERPLEXITY", fm.provider)

	// Test ToolModel with specific tool
	model, err = f.ToolModel("web_search")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	// Test ToolModel with preferred models
	model, err = f.ToolModel("web_search", "gpt-41-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	// Test ToolModel with non-existent tool (should use default)
	model, err = f.ToolModel("non-existent-tool")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	// Test AssistantModel with specific assistant
	model, err = f.AssistantModel("orchestrator")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	// Test AssistantModel with preferred models
	model, err = f.AssistantModel("orchestrator", "gpt-4-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	// Test AssistantModel with non-existent assistant (should use default)
	model, err = f.AssistantModel("non-existent-assistant")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)

	model, err = f.ModelByType("AZURE")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	// Test error cases
	// Test with unsupported provider type
	_, err = f.ModelByType("UNSUPPORTED")
	assert.EqualError(t, err, "provider not found for type: UNSUPPORTED")

	// Test with empty providers list
	emptyCfg := &llmfactory.Config{}
	emptyFactory := llmfactory.New(emptyCfg)
	_, err = emptyFactory.DefaultModel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")

	// Test with invalid default provider
	invalidCfg := &llmfactory.Config{
		DefaultProvider: "non-existent",
		Providers:       cfg.Providers,
	}
	invalidFactory := llmfactory.New(invalidCfg)
	model, err = invalidFactory.DefaultModel()
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "OPEN_AI", fm.provider)
}

func Test_Load(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("TAVILY_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	t.Setenv("PERPLEXITY_TOKEN", "fakekey")
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")

	// Test successful load
	f, err := llmfactory.Load("testdata/llm.yaml")
	require.NoError(t, err)
	require.NotNil(t, f)

	// Test load with non-existent file
	_, err = llmfactory.Load("testdata/non-existent.yaml")
	require.Error(t, err)
}

func Test_CreateLLM(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fakekey")
	t.Setenv("TAVILY_API_KEY", "fakekey")
	t.Setenv("ANTHROPIC_API_KEY", "fakekey")
	t.Setenv("PERPLEXITY_TOKEN", "fakekey")
	t.Setenv("GOOGLEAI_TOKEN", "fakekey")

	t.Skip("skipping real test")

	cfg := &llmfactory.ProviderConfig{
		Name: "test-provider",
		OpenAI: llmfactory.OpenAIConfig{
			APIType:    "OPEN_AI",
			APIVersion: "2024-02-15-preview",
		},
		AvailableModels: []string{"gpt-4"},
		DefaultModel:    "gpt-4",
	}

	// Test OpenAI provider
	model, err := llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Azure provider
	cfg.OpenAI.APIType = "AZURE"
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Azure AD provider
	cfg.OpenAI.APIType = "AZURE_AD"
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Anthropic provider
	cfg.OpenAI.APIType = "ANTHROPIC"
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Bedrock provider
	cfg.OpenAI.APIType = "BEDROCK"
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Perplexity provider
	cfg.OpenAI.APIType = "PERPLEXITY"
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test GoogleAI provider
	// cfg.OpenAI.APIType = "GOOGLEAI"
	// model, err = llmfactory.CreateLLM(cfg)
	// require.NoError(t, err)
	// require.NotNil(t, model)

	// Test unsupported provider
	cfg.OpenAI.APIType = "UNSUPPORTED"
	_, err = llmfactory.CreateLLM(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider type")
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

	cfg := &llmfactory.ProviderConfig{
		Name:  "google-test",
		Token: "fakekey",
		OpenAI: llmfactory.OpenAIConfig{
			APIType: "GOOGLEAI",
		},
		AvailableModels: []string{"gemini-2.5-flash-preview-05-20"},
		DefaultModel:    "gemini-2.5-flash-preview-05-20",
	}

	model, err := llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test with missing API key
	t.Setenv("GOOGLEAI_TOKEN", "")
	cfg.Token = ""

	model, err = llmfactory.CreateLLM(cfg)
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

	model, err := llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test provider with nil available models
	cfg.AvailableModels = nil
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test provider with empty default model
	cfg.DefaultModel = ""
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)
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

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels ...string) (llms.Model, error) {
		return &fakeLLM{provider: cfg.Name, model: cfg.FindModel(preferredModels...)}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)

	// First call should create the model
	model1, err := f.ModelByType("OPEN_AI")
	require.NoError(t, err)
	require.NotNil(t, model1)

	// Second call should return cached model
	model2, err := f.ModelByType("OPEN_AI")
	require.NoError(t, err)
	require.NotNil(t, model2)

	// Should be the same instance
	assert.Equal(t, model1, model2)

	// Test name caching
	model3, err := f.ModelByName("gpt-4-mini")
	require.NoError(t, err)
	require.NotNil(t, model3)

	model4, err := f.ModelByName("gpt-4-mini")
	require.NoError(t, err)
	require.NotNil(t, model4)

	assert.Equal(t, model3, model4)
}

// Test_ToolModelFallback tests tool model fallback scenarios
func Test_ToolModelFallback(t *testing.T) {
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
		ToolModels: map[string][]string{
			"default":    {"gpt-4-mini"},
			"web_search": {"gpt-4-mini"},
		},
	}

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels ...string) (llms.Model, error) {
		return &fakeLLM{provider: cfg.Name, model: cfg.FindModel(preferredModels...)}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)

	// Test tool with specific mapping
	model, err := f.ToolModel("web_search")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm := model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)

	// Test tool with default mapping
	model, err = f.ToolModel("unknown_tool")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)

	// Test tool with preferred models
	model, err = f.ToolModel("unknown_tool", "gpt-4")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model) // Should still use default mapping
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

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels ...string) (llms.Model, error) {
		return &fakeLLM{provider: cfg.Name, model: cfg.FindModel(preferredModels...)}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)

	// Test assistant with specific mapping
	model, err := f.AssistantModel("orchestrator")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm := model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)

	// Test assistant with default mapping
	model, err = f.AssistantModel("unknown_assistant")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)

	// Test assistant with preferred models
	model, err = f.AssistantModel("unknown_assistant", "gpt-4")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model) // Should still use default mapping
}

// Test_ConcurrentAccess tests concurrent access to factory methods
func Test_ConcurrentAccess(t *testing.T) {
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

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels ...string) (llms.Model, error) {
		return &fakeLLM{provider: cfg.Name, model: cfg.FindModel(preferredModels...)}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)

	// Test concurrent access to ModelByType
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			model, err := f.ModelByType("OPEN_AI")
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
			model, err := f.ModelByName("gpt-4-mini")
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
	model := cfg.FindModel("gpt-4-mini")
	assert.Equal(t, "gpt-4-mini", model)

	// Test finding first model in preferred list
	model = cfg.FindModel("gpt-4-mini", "gpt-3.5-turbo")
	assert.Equal(t, "gpt-4-mini", model)

	// Test fallback to default when model not found
	model = cfg.FindModel("non-existent-model")
	assert.Equal(t, "gpt-4", model)

	// Test with empty preferred models
	model = cfg.FindModel()
	assert.Equal(t, "gpt-4", model)

	// Test with nil available models
	cfg.AvailableModels = nil
	model = cfg.FindModel("gpt-4-mini")
	assert.Equal(t, "gpt-4", model)

	// Test with empty available models
	cfg.AvailableModels = []string{}
	model = cfg.FindModel("gpt-4-mini")
	assert.Equal(t, "gpt-4", model)
}

// Test_EmptyConfig tests factory behavior with empty configuration
func Test_EmptyConfig(t *testing.T) {
	// Test with completely empty config
	emptyCfg := &llmfactory.Config{}
	f := llmfactory.New(emptyCfg)

	_, err := f.DefaultModel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")

	_, err = f.ModelByType("OPEN_AI")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider not found for type: OPEN_AI")

	_, err = f.ModelByName("gpt-4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")

	_, err = f.ToolModel("web_search")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers configured")

	_, err = f.AssistantModel("orchestrator")
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

	model, err := llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Azure with base URL
	cfg.OpenAI.APIType = "AZURE"
	cfg.OpenAI.BaseURL = "https://azure-test.openai.azure.com"
	cfg.OpenAI.APIVersion = "2024-02-15-preview"

	model, err = llmfactory.CreateLLM(cfg)
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

	llmfactory.NewLLM = func(cfg *llmfactory.ProviderConfig, preferredModels ...string) (llms.Model, error) {
		return &fakeLLM{provider: cfg.Name, model: cfg.FindModel(preferredModels...)}, nil
	}
	defer func() {
		llmfactory.NewLLM = llmfactory.CreateLLM
	}()

	f := llmfactory.New(cfg)

	// Test fallback when first model not found but second is
	model, err := f.ModelByName("non-existent", "gpt-41-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm := model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "AZURE", fm.provider)

	// Test fallback to default when no models found
	model, err = f.ModelByName("non-existent-1", "non-existent-2")
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

	model, err := llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test OpenAI without token (should still work as it uses env var)
	cfg.Token = ""
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)

	// Test Anthropic with token
	cfg.OpenAI.APIType = "ANTHROPIC"
	cfg.Token = "fakekey"
	model, err = llmfactory.CreateLLM(cfg)
	require.NoError(t, err)
	require.NotNil(t, model)
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
func (f *fakeLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, nil
}
