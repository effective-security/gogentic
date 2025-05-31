package llmfactory

import (
	"github.com/effective-security/x/configloader"
)

type Config struct {
	Providers []*ProviderConfig `json:"providers" yaml:"providers"`
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
	// OPEN_AI|AZURE|AZURE_AD|CLOUDFLARE_AZURE
	APIType string `json:"api_type,omitempty" yaml:"api_type,omitempty"`
	// OrgID specifies which organization's quota and billing should be used when making API requests.
	OrgID            string `json:"org_id,omitempty" yaml:"org_id,omitempty"`
	AssistantVersion string `json:"assistant_version,omitempty" yaml:"assistant_version,omitempty"`
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
