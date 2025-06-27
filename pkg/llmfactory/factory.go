package llmfactory

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/llms/bedrock"
	"github.com/effective-security/gogentic/pkg/llms/googleai"
	"github.com/effective-security/gogentic/pkg/llms/openai"
	"github.com/effective-security/xlog"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "llmfactory")

// NewLLM is a wrapper for CreateLLM to allow for overriding the default implementation.
var NewLLM = CreateLLM

// Factory is the interface for creating and managing LLM models.
type Factory interface {
	// DefaultModel returns the default LLM model.
	DefaultModel() (llms.Model, error)
	// ModelByType returns an LLM model by its type, e.g.
	// OPEN_AI, AZURE, AZURE_AD, CLOUDFLARE, ANTHROPIC, GOOGLEAI, BEDROCK, PERPLEXITY
	ModelByType(providerType string) (llms.Model, error)
	// ModelByName returns an LLM model by its name,
	// if the model is not found, it will return the default model.
	ModelByName(preferredModels ...string) (llms.Model, error)
	// ToolModel returns a tool model by its name.
	ToolModel(toolName string, preferredModels ...string) (llms.Model, error)
	// AssistantModel returns an assistant model by its name.
	AssistantModel(assistantName string, preferredModels ...string) (llms.Model, error)
}

// Load returns OpenAI factory
func Load(location string) (Factory, error) {
	cfg, err := LoadConfig(location)
	if err != nil {
		return nil, err
	}
	return New(cfg), nil
}

type factory struct {
	cfg *Config

	defaultProvider *ProviderConfig
	toolModels      map[string][]string
	assistantModels map[string][]string
	byType          map[string]llms.Model
	byName          map[string]llms.Model
	lock            sync.Mutex
}

// New creates a new LLM factory
func New(cfg *Config) Factory {
	f := &factory{
		cfg:             cfg,
		byType:          make(map[string]llms.Model),
		byName:          make(map[string]llms.Model),
		toolModels:      make(map[string][]string),
		assistantModels: make(map[string][]string),
	}

	for k, v := range cfg.ToolModels {
		f.toolModels[k] = slices.Clone(v)
	}
	for k, v := range cfg.AssistantModels {
		f.assistantModels[k] = slices.Clone(v)
	}

	if cfg.DefaultProvider != "" {
		for _, provider := range cfg.Providers {
			if provider.Name == cfg.DefaultProvider {
				f.defaultProvider = provider
				break
			}
		}
	}

	if f.defaultProvider == nil && len(f.cfg.Providers) > 0 {
		f.defaultProvider = f.cfg.Providers[0]
	}

	return f
}

func CreateLLM(cfg *ProviderConfig, preferredModels ...string) (llms.Model, error) {
	provType := strings.ToUpper(cfg.OpenAI.APIType)
	switch provType {
	case "OPENAI", "OPEN_AI":
		return newOpenAI(cfg, preferredModels...)
	case "PERPLEXITY":
		return newPerplexity(cfg, preferredModels...)
	case "AZURE", "AZURE_AD":
		return newAzure(cfg, preferredModels...)
	case "ANTHROPIC":
		return newAnthropic(cfg, preferredModels...)
	case "GOOGLEAI":
		return newGoogleAI(cfg, preferredModels...)
	case "BEDROCK":
		return newBedrock(cfg, preferredModels...)
	}
	return nil, errors.Errorf("unsupported provider type: %s", provType)
}

func newOpenAI(cfg *ProviderConfig, preferredModels ...string) (llms.Model, error) {
	var opts []openai.Option
	model := cfg.FindModel(preferredModels...)
	opts = append(opts, openai.WithAPIType(openai.APITypeOpenAI), openai.WithModel(model))

	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	return openai.New(opts...)
}

func newPerplexity(cfg *ProviderConfig, preferredModels ...string) (llms.Model, error) {
	var opts []openai.Option
	model := cfg.FindModel(preferredModels...)
	opts = append(opts, openai.WithModel(model))

	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	return openai.New(opts...)
}

func newAzure(cfg *ProviderConfig, preferredModels ...string) (llms.Model, error) {
	var opts []openai.Option
	model := cfg.FindModel(preferredModels...)
	opts = append(opts, openai.WithAPIVersion(cfg.OpenAI.APIVersion), openai.WithModel(model))

	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if strings.EqualFold(cfg.OpenAI.APIType, "AZURE_AD") {
		opts = append(opts, openai.WithAPIType(openai.APITypeAzureAD))
	} else {
		opts = append(opts, openai.WithAPIType(openai.APITypeAzure))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	return openai.New(opts...)
}

func newAnthropic(cfg *ProviderConfig, preferredModels ...string) (llms.Model, error) {
	var opts []anthropic.Option
	model := cfg.FindModel(preferredModels...)
	opts = append(opts, anthropic.WithModel(model))
	if cfg.Token != "" {
		opts = append(opts, anthropic.WithToken(cfg.Token))
	}
	return anthropic.New(opts...)
}

func newGoogleAI(cfg *ProviderConfig, preferredModels ...string) (llms.Model, error) {
	var opts []googleai.Option
	model := cfg.FindModel(preferredModels...)
	opts = append(opts, googleai.WithDefaultModel(model))
	if cfg.Token != "" {
		opts = append(opts, googleai.WithAPIKey(cfg.Token))
	}
	return googleai.New(context.Background(), opts...)
}

func newBedrock(cfg *ProviderConfig, preferredModels ...string) (llms.Model, error) {
	var opts []bedrock.Option
	model := cfg.FindModel(preferredModels...)
	opts = append(opts, bedrock.WithModel(model))
	return bedrock.New(opts...)
}

// Default returns the default OpenAI client
func (f *factory) DefaultModel() (llms.Model, error) {
	if len(f.cfg.Providers) == 0 || f.defaultProvider == nil {
		return nil, errors.New("no providers configured")
	}

	return NewLLM(f.defaultProvider, f.defaultProvider.DefaultModel)
}

func (f *factory) ModelByType(providerType string) (llms.Model, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if client, ok := f.byType[providerType]; ok {
		return client, nil
	}

	for _, cfg := range f.cfg.Providers {
		if cfg.OpenAI.APIType == providerType {
			model, err := NewLLM(cfg)
			if err != nil {
				return nil, err
			}

			logger.KV(xlog.DEBUG,
				"status", "created_llm",
				"type", cfg.OpenAI.APIType,
				"version", cfg.OpenAI.APIVersion,
				"name", cfg.Name)

			f.byType[providerType] = model
			return model, nil
		}
	}
	return nil, errors.Errorf("provider not found for type: %s", providerType)
}

func (f *factory) ModelByName(modelNames ...string) (llms.Model, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, modelName := range modelNames {
		if client, ok := f.byName[modelName]; ok {
			return client, nil
		}

		for _, cfg := range f.cfg.Providers {
			if slices.Contains(cfg.AvailableModels, modelName) {
				model, err := NewLLM(cfg, modelNames...)
				if err != nil {
					logger.KV(xlog.ERROR,
						"reason", "NewLLM",
						"type", cfg.OpenAI.APIType,
						"version", cfg.OpenAI.APIVersion,
						"models", modelNames,
					)
					continue
				}

				logger.KV(xlog.DEBUG,
					"status", "created_llm",
					"type", cfg.OpenAI.APIType,
					"version", cfg.OpenAI.APIVersion,
					"name", cfg.Name)

				f.byName[modelName] = model
				return model, nil
			}
		}
	}
	return f.DefaultModel()
}

// ToolModel returns a tool model by its name.
func (f *factory) ToolModel(toolName string, preferredModels ...string) (llms.Model, error) {
	// Check if we have a specific model mapping for this tool
	if modelNames, ok := f.toolModels[toolName]; ok {
		return f.ModelByName(modelNames...)
	}

	// Check for default model mapping
	if modelNames, ok := f.toolModels["default"]; ok {
		return f.ModelByName(modelNames...)
	}

	// Fallback to default provider
	return f.ModelByName(preferredModels...)
}

// AssistantModel returns an assistant model by its name.
func (f *factory) AssistantModel(assistantName string, preferredModels ...string) (llms.Model, error) {
	// Check if we have a specific model mapping for this assistant
	if modelNames, ok := f.assistantModels[assistantName]; ok {
		return f.ModelByName(modelNames...)
	}

	// Check for default model mapping
	if modelNames, ok := f.assistantModels["default"]; ok {
		return f.ModelByName(modelNames...)
	}

	// Fallback to default provider
	return f.ModelByName(preferredModels...)
}
