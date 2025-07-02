package llmfactory

import (
	"slices"

	"github.com/effective-security/x/configloader"
)

type Config struct {
	// Providers specifies the list of providers to use
	Providers []*ProviderConfig `json:"providers" yaml:"providers"`
	// DefaultProvider specifies the default provider to use
	DefaultProvider string `json:"default_provider" yaml:"default_provider"`
	// ToolModels specifies the mapping of tools to providers.
	// key is the tool name, value is the model name.
	// Use `default: <model_name>` as the default model for tools.
	ToolModels map[string][]string `json:"tool_models" yaml:"tool_models"`
	// AssistantModels specifies the mapping of assistants to models.
	// key is the assistant name, value is the model name.
	// Use `default: <model_name>` as the default model for assistants.
	AssistantModels map[string][]string `json:"assistant_models" yaml:"assistant_models"`
}

// ProviderConfig for the OpenAI provider
type ProviderConfig struct {
	Name            string       `json:"name" yaml:"name"`
	Token           string       `json:"token,omitempty" yaml:"token,omitempty"`
	DefaultModel    string       `json:"default_model,omitempty" yaml:"default_model,omitempty"`
	AvailableModels []string     `json:"available_models,omitempty" yaml:"available_models,omitempty"`
	OpenAI          OpenAIConfig `json:"open_ai" yaml:"open_ai"`
}

// OpenAIConfig specifies options config
type OpenAIConfig struct {
	BaseURL    string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	APIVersion string `json:"api_version,omitempty" yaml:"api_version,omitempty"`
	// APIType specifies the type of API to use:
	// OPENAI|AZURE|AZURE_AD|CLOUDFLARE|ANTHROPIC|GOOGLEAI|BEDROCK|PERPLEXITY
	APIType string `json:"api_type,omitempty" yaml:"api_type,omitempty"`
	// OrgID specifies which organization's quota and billing should be used when making API requests.
	OrgID            string `json:"org_id,omitempty" yaml:"org_id,omitempty"`
	AssistantVersion string `json:"assistant_version,omitempty" yaml:"assistant_version,omitempty"`
}

func (c *ProviderConfig) FindModel(models ...string) string {
	for _, model := range models {
		if slices.Contains(c.AvailableModels, model) {
			return model
		}
	}
	return c.DefaultModel
}

// LoadConfig from file
func LoadConfig(file string) (*Config, error) {
	cfg := new(Config)
	if file == "" {
		return cfg, nil
	}

	err := configloader.UnmarshalAndExpand(file, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
