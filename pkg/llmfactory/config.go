package llmfactory

import (
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/skills"
	"github.com/effective-security/x/configloader"
	yamlcfg "go.uber.org/config"
)

// Names of the assistant→model mappings in validation errors.
const (
	configScopeGlobal = "assistant_models"
	configScopeOrg    = "orgs_override."
)

// Config is the top-level factory configuration for providers, defaults,
// assistant→model mappings and optional per‑org overrides and skills.
type Config struct {
	// Providers specifies the list of providers to use
	Providers []*ProviderConfig `json:"providers" yaml:"providers"`
	// DefaultProvider specifies the default provider to use
	DefaultProvider string `json:"default_provider" yaml:"default_provider"`
	// AssistantModels specifies the mapping of assistants to models.
	// key is the assistant name, value is the list of model references to
	// try in order. A reference is [<provider>/]<model>[,<effort>]: the
	// provider is a provider name or an upper-case provider type, the model
	// may be a pattern with "*" and "?", and the effort is the default
	// reasoning effort for that assistant.
	// Use `default: <model_name>` as the default model for assistants.
	AssistantModels map[string][]string `json:"assistant_models" yaml:"assistant_models"`
	// Orgs specifies the organizations configuration to override the global configuration.
	// An organization listed without overrides (a null entry) uses the
	// global AssistantModels.
	Orgs map[string]*OrgConfig `json:"orgs_override" yaml:"orgs_override"`
	// Skills specifies the skills configuration.
	Skills *skills.Config `json:"skills,omitempty" yaml:"skills,omitempty"`
}

// OrgConfig defines assistant→model mappings that override global mappings for
// a given organization.
type OrgConfig struct {
	// AssistantModels specifies the mapping of assistants to models.
	// key is the assistant name, value is the list of model references to
	// try in order. A reference is [<provider>/]<model>[,<effort>]: the
	// provider is a provider name or an upper-case provider type, the model
	// may be a pattern with "*" and "?", and the effort is the default
	// reasoning effort for that assistant.
	// Use `default: <model_name>` as the default model for assistants.
	AssistantModels map[string][]string `json:"assistant_models" yaml:"assistant_models"`
}

// ProviderConfig defines a single provider instance and its available models.
// The OpenAI field conveys the API style for both OpenAI proper and
// OpenAI‑compatible APIs (Azure, Perplexity, Cloudflare, etc.).
//
// Model names in DefaultModel and AvailableModels may carry a default
// reasoning effort after a comma ("gpt-5.1,medium") and may be patterns in
// which "*" matches any run of characters and "?" exactly one
// ("claude-opus-*").
type ProviderConfig struct {
	Name  string `json:"name" yaml:"name"`
	Token string `json:"token,omitempty" yaml:"token,omitempty"`
	// DefaultModel is the model used when no preferred model matches. It does
	// not need to be listed in AvailableModels.
	DefaultModel string `json:"default_model,omitempty" yaml:"default_model,omitempty"`
	// AvailableModels is the allow-list of model IDs and patterns. A model
	// can be selected by name only when an entry matches it.
	AvailableModels []string `json:"available_models,omitempty" yaml:"available_models,omitempty"`
	// DisabledModels is the deny-list of model IDs and patterns. It wins over
	// AvailableModels and DefaultModel.
	DisabledModels []string `json:"disabled_models,omitempty" yaml:"disabled_models,omitempty"`
	// MaxEffort is the highest reasoning effort a call to this provider may
	// use: none, low, medium, high, xhigh or max. Empty means no ceiling.
	MaxEffort string `json:"max_effort,omitempty" yaml:"max_effort,omitempty"`
	// Discovery turns on reading the model list from the provider, so that
	// patterns see models nobody listed and a retired model gives way to a
	// newer one of its family. Nil means off.
	Discovery *DiscoveryConfig  `json:"discovery,omitempty" yaml:"discovery,omitempty"`
	Headers   map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	OpenAI    OpenAIConfig      `json:"open_ai" yaml:"open_ai"`
}

// DiscoveryConfig controls reading the model list from a provider. It is
// accepted for the provider types that advertise llms.CapabilityModels:
// OPENAI, OPENROUTER, ANTHROPIC and GOOGLEAI.
type DiscoveryConfig struct {
	// Enabled turns discovery on.
	Enabled bool `json:"enabled" yaml:"enabled"`
	// Refresh is how long a fetched list is trusted, as a duration string
	// such as "6h". Empty means one hour; the minimum is one minute.
	Refresh string `json:"refresh,omitempty" yaml:"refresh,omitempty"`
}

// OpenAIConfig specifies API parameters for OpenAI‑style providers. APIType
// selects the provider family: OPENAI|AZURE|AZURE_AD|CLOUDFLARE|ANTHROPIC|GOOGLEAI|BEDROCK|OPENROUTER|PERPLEXITY.
type OpenAIConfig struct {
	// Converse enables the optional Bedrock portable inference connector.
	Converse bool `json:"converse,omitempty" yaml:"converse,omitempty"`
	// InferenceAPI selects the upstream API for the OpenAI portable inference
	// connector: "responses", "chat", or empty for the provider default.
	InferenceAPI string `json:"inference_api,omitempty" yaml:"inference_api,omitempty"`
	BaseURL      string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	APIVersion   string `json:"api_version,omitempty" yaml:"api_version,omitempty"`
	// APIType specifies the type of API to use:
	// OPENAI|AZURE|AZURE_AD|CLOUDFLARE|ANTHROPIC|GOOGLEAI|BEDROCK|OPENROUTER|PERPLEXITY
	APIType string `json:"api_type,omitempty" yaml:"api_type,omitempty"`
	// Organization is the organization ID for the OpenAI API.
	Organization string `json:"organization,omitempty" yaml:"organization,omitempty"`
	// Project is the project ID for the OpenAI API.
	Project string `json:"project,omitempty" yaml:"project,omitempty"`
}

// FindModel selects the first name from models that AvailableModels allows
// and DisabledModels does not deny. If none match, the default model is
// returned when one is set and usable. Returns an error when no model can be
// selected.
//
// The names in models are concrete model IDs. Entries of the configuration
// that do not parse are ignored; [Config.Validate] reports them.
func (c *ProviderConfig) FindModel(models ...string) (string, error) {
	// Invalid entries are left out of the policy and reported by Validate.
	policy, _ := newProviderPolicy(c, 0)
	for _, model := range models {
		if policy.allowed(model) {
			return model, nil
		}
	}
	if model, _, ok := policy.defaultModel(); ok {
		return model, nil
	}
	return "", errors.New("no LLM model found")
}

// Validate checks the model references, effort levels and model policy of the
// configuration, and returns every problem found. [LoadConfig] calls it, so a
// configuration file with a mistyped effort or an unusable default model
// fails at load time instead of being skipped silently during resolution.
func (c *Config) Validate() error {
	var errs []error
	for index, provider := range c.Providers {
		policy, err := newProviderPolicy(provider, index)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := policy.validate(); err != nil {
			errs = append(errs, err)
		}
	}
	errs = append(errs, validateAssistantModels(configScopeGlobal, c.AssistantModels)...)
	for orgID, org := range c.Orgs {
		if org == nil {
			continue
		}
		errs = append(errs, validateAssistantModels(configScopeOrg+orgID, org.AssistantModels)...)
	}
	return errors.Join(errs...)
}

// validateAssistantModels checks that every reference of an assistant→model
// mapping parses. scope names the mapping in error messages.
func validateAssistantModels(scope string, assistants map[string][]string) []error {
	var errs []error
	for assistant, refs := range assistants {
		for _, ref := range refs {
			if _, err := parseRefSpec(ref); err != nil {
				errs = append(errs, errors.WithMessagef(err, "%s: assistant %s", scope, assistant))
			}
		}
	}
	return errs
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
			if orgCfg == nil {
				// "orgs_override: {acme: }" overrides nothing.
				orgCfg = new(OrgConfig)
				cfg.Orgs[orgID] = orgCfg
			}
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

			orgCfg.AssistantModels = res
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, errors.WithMessagef(err, "invalid llm config %s", file)
	}
	return cfg, nil
}
