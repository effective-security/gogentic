package llmfactory_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func Test_Factory(t *testing.T) {
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
	assert.Equal(t, "openai-dev", fm.provider)

	// Test ModelByName with single model
	model, err = f.ModelByName("gpt-4-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)
	assert.Equal(t, "openai-dev", fm.provider)

	// Test ModelByName with multiple preferred models
	model, err = f.ModelByName("gpt-4-unknown", "gpt-41-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "azure-test", fm.provider)

	// Test ModelByName with non-existent models (should fallback to default)
	model, err = f.ModelByName("non-existent-model")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "openai-dev", fm.provider)

	model, err = f.ModelByName("gpt-41-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "azure-test", fm.provider)

	model, err = f.ModelByType("OPEN_AI")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "openai-dev", fm.provider)

	// Test ToolModel with specific tool
	model, err = f.ToolModel("web_search")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)
	assert.Equal(t, "openai-dev", fm.provider)

	// Test ToolModel with preferred models
	model, err = f.ToolModel("web_search", "gpt-41-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4-mini", fm.model)
	assert.Equal(t, "openai-dev", fm.provider)

	// Test ToolModel with non-existent tool (should use default)
	model, err = f.ToolModel("non-existent-tool")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "openai-dev", fm.provider)

	// Test AssistantModel with specific assistant
	model, err = f.AssistantModel("orchestrator")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "azure-test", fm.provider)

	// Test AssistantModel with preferred models
	model, err = f.AssistantModel("orchestrator", "gpt-4-mini")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41-mini", fm.model)
	assert.Equal(t, "azure-test", fm.provider)

	// Test AssistantModel with non-existent assistant (should use default)
	model, err = f.AssistantModel("non-existent-assistant")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-4o", fm.model)
	assert.Equal(t, "openai-dev", fm.provider)

	model, err = f.ModelByType("AZURE")
	require.NoError(t, err)
	require.NotNil(t, model)
	fm = model.(*fakeLLM)
	assert.Equal(t, "gpt-41", fm.model)
	assert.Equal(t, "azure-test", fm.provider)

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
	assert.Equal(t, "openai-dev", fm.provider)
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
