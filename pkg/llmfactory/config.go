package llmfactory

import (
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/skills"
	"github.com/effective-security/x/configloader"
	yamlcfg "go.uber.org/config"
)

// Config is the top-level factory configuration for providers, defaults,
// assistant→model mappings and optional per‑org overrides and skills.
type Config struct {
	// Providers specifies the list of providers to use
	Providers []*ProviderConfig `json:"providers" yaml:"providers"`
	// DefaultProvider specifies the default provider to use
	DefaultProvider string `json:"default_provider" yaml:"default_provider"`
	// AssistantModels specifies the mapping of assistants to models.
	// key is the assistant name, value is the model name.
	// The model name can be in the format of <provider_name>/<model_name>.
	// Use `default: <model_name>` as the default model for assistants.
	AssistantModels map[string][]string `json:"assistant_models" yaml:"assistant_models"`
	// Orgs specifies the organizations configuration to override the global configuration.
	Orgs map[string]*OrgConfig `json:"orgs_override" yaml:"orgs_override"`
	// Skills specifies the skills configuration.
	Skills *skills.Config `json:"skills,omitempty" yaml:"skills,omitempty"`
}

// OrgConfig defines assistant→model mappings that override global mappings for
// a given organization.
type OrgConfig struct {
	// AssistantModels specifies the mapping of assistants to models.
	// key is the assistant name, value is the model name.
	// The model name can be in the format of <provider_name>/<model_name>.
	// Use `default: <model_name>` as the default model for assistants.
	AssistantModels map[string][]string `json:"assistant_models" yaml:"assistant_models"`
}

// ProviderConfig defines a single provider instance and its available models.
// The OpenAI field conveys the API style for both OpenAI proper and
// OpenAI‑compatible APIs (Azure, Perplexity, Cloudflare, etc.).
type ProviderConfig struct {
	Name            string       `json:"name" yaml:"name"`
	Token           string       `json:"token,omitempty" yaml:"token,omitempty"`
	DefaultModel    string       `json:"default_model,omitempty" yaml:"default_model,omitempty"`
	AvailableModels []string     `json:"available_models,omitempty" yaml:"available_models,omitempty"`
	OpenAI          OpenAIConfig `json:"open_ai" yaml:"open_ai"`
}

// OpenAIConfig specifies API parameters for OpenAI‑style providers. APIType
// selects the provider family: OPENAI|AZURE|AZURE_AD|CLOUDFLARE|ANTHROPIC|GOOGLEAI|BEDROCK|PERPLEXITY.
type OpenAIConfig struct {
	BaseURL    string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	APIVersion string `json:"api_version,omitempty" yaml:"api_version,omitempty"`
	// APIType specifies the type of API to use:
	// OPENAI|AZURE|AZURE_AD|CLOUDFLARE|ANTHROPIC|GOOGLEAI|BEDROCK|PERPLEXITY
	APIType string `json:"api_type,omitempty" yaml:"api_type,omitempty"`
	// Organization is the organization ID for the OpenAI API.
	Organization string `json:"organization,omitempty" yaml:"organization,omitempty"`
	// Project is the project ID for the OpenAI API.
	Project string `json:"project,omitempty" yaml:"project,omitempty"`
}

// FindModel selects the first name from models that is present in
// AvailableModels. If none match, DefaultModel is returned when set.
// Returns an error when no model can be selected.
func (c *ProviderConfig) FindModel(models ...string) (string, error) {
	for _, model := range models {
		if slices.Contains(c.AvailableModels, model) {
			return model, nil
		}
	}
	if c.DefaultModel != "" {
		return c.DefaultModel, nil
	}
	return "", errors.New("no LLM model found")
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
	if len(cfg.Orgs) > 0 {
		// merge Org config overrides
		gs := yamlcfg.Static(cfg.AssistantModels)
		for orgID, orgCfg := range cfg.Orgs {
			ops := []yamlcfg.YAMLOption{gs, yamlcfg.Static(orgCfg.AssistantModels)}

			cprovider, err := yamlcfg.NewYAML(ops...)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to override yaml config for org %s", orgID)
			}

			var res map[string][]string
			err = cprovider.Get(yamlcfg.Root).Populate(&res)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to override yaml config for org %s", orgID)
			}

			cfg.Orgs[orgID].AssistantModels = res
		}
	}
	return cfg, nil
}
