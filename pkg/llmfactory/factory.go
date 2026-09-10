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
	"github.com/effective-security/gogentic/pkg/llms/cloudflare"
	"github.com/effective-security/gogentic/pkg/llms/googleai"
	"github.com/effective-security/gogentic/pkg/llms/openai"
	"github.com/effective-security/gogentic/skills"
	"github.com/effective-security/x/values"
	"github.com/effective-security/xlog"
)

//go:generate mockgen -source=factory.go -destination=../../mocks/mockllmfactory/llmfactory_mock.gen.go  -package mockllmfactory

const defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "llmfactory")

// NewLLM is a wrapper for CreateLLM to allow for overriding the default implementation.
var NewLLM = CreateLLM

// Factory is the interface for creating and managing LLM models.
// In multi-tenant environments, the OrgID is used to determine the LLM model to use for the organization.
// The factory can also be provided with a ModelFilterFunc to restrict which models an organization may use.
type Factory interface {
	// WithModelFilter sets a predicate used to restrict which models an org may use.
	// Returns a new Factory with the filter applied.
	WithModelFilter(filter ModelFilterFunc) Factory

	// GetModel returns an LLM model that matches the given options.
	//
	// Resolution rules:
	//   - When ProviderType is set, a provider of that type is selected.
	//   - Otherwise, when AssistantName is set, the configured assistant model
	//     mapping (optionally per-org) is expanded into the preferred models.
	//   - The preferred models are tried in order; the first available and
	//     allowed model wins.
	//   - If no preferred model matches, the default model is returned.
	//
	// RequiredCapabilities, when non-zero, restricts the candidates to
	// providers whose type supports ALL of the requested capabilities.
	GetModel(ctx context.Context, opts ModelOptions) (llms.Model, error)

	// Skills returns all loaded skills for the given agent sorted alphabetically by name.
	// Use tags to filter skills by tags. The Skill must have all the tags provided.
	Skills(agent string, tags ...string) skills.Skills
}

// ModelOptions selects which model GetModel should return. See Factory.GetModel
// for the resolution rules.
type ModelOptions struct {
	// ProviderType specifies the provider type to use.
	// If not specified, a matching provider will be used.
	ProviderType llms.ProviderType
	// OrgID specifies the organization ID to use,
	// if configuration provides Org overrides.
	OrgID string
	// AssistantName specifies the assistant name to use.
	AssistantName string
	// PreferredModels specifies the preferred models to use.
	PreferredModels []string
	// RequiredCapabilities specifies the required capabilities the model must support.
	// When non-zero, only providers whose type supports ALL of the requested
	// capabilities are considered.
	RequiredCapabilities llms.Capability
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

	defaultProvider    *ProviderConfig
	assistantModels    map[string][]string
	orgAssistantModels map[string]map[string][]string
	byType             map[llms.ProviderType]llms.Model
	byName             map[string]llms.Model
	skillsLoader       skills.Loader
	lock               sync.Mutex

	options *Options
}

// New creates a new LLM factory
func New(cfg *Config, opts ...Option) Factory {
	f := &factory{
		cfg:                cfg,
		byType:             make(map[llms.ProviderType]llms.Model),
		byName:             make(map[string]llms.Model),
		assistantModels:    make(map[string][]string),
		orgAssistantModels: make(map[string]map[string][]string),
		options:            NewOptions(opts...),
	}

	for k, v := range cfg.AssistantModels {
		f.assistantModels[k] = slices.Clone(v)
	}

	for orgID, orgCfg := range cfg.Orgs {
		for assistantName, models := range orgCfg.AssistantModels {
			if f.orgAssistantModels[orgID] == nil {
				f.orgAssistantModels[orgID] = make(map[string][]string)
			}
			f.orgAssistantModels[orgID][assistantName] = slices.Clone(models)
		}
	}

	for _, provider := range cfg.Providers {
		if strings.EqualFold(provider.OpenAI.APIType, "OPEN_AI") {
			provider.OpenAI.APIType = string(llms.ProviderOpenAI)
		}
	}

	if cfg.DefaultProvider != "" {
		for _, provider := range cfg.Providers {
			if provider.Name == cfg.DefaultProvider {
				f.defaultProvider = provider
				break
			}
		}
	}

	// the first provider is the default one if default_provider is not set
	if f.defaultProvider == nil && len(f.cfg.Providers) > 0 {
		f.defaultProvider = f.cfg.Providers[0]
	}

	if f.cfg.Skills != nil {
		loader, err := skills.NewLoader(f.cfg.Skills, "")
		if err != nil {
			logger.KV(xlog.ERROR,
				"reason", "skills_loader",
				"err", err.Error(),
			)
		} else {
			f.skillsLoader = loader
			logger.KV(xlog.INFO,
				"status", "skills_loaded",
				"agents", loader.Agents(),
			)
		}
	}

	return f
}

func (f *factory) WithModelFilter(filter ModelFilterFunc) Factory {
	f.lock.Lock()
	defer f.lock.Unlock()

	newf := &factory{
		cfg:                f.cfg,
		defaultProvider:    f.defaultProvider,
		byType:             make(map[llms.ProviderType]llms.Model, len(f.byType)),
		byName:             make(map[string]llms.Model, len(f.byName)),
		assistantModels:    make(map[string][]string, len(f.assistantModels)),
		orgAssistantModels: make(map[string]map[string][]string, len(f.orgAssistantModels)),
		skillsLoader:       f.skillsLoader,
	}
	ops := *f.options
	ops.ModelFilter = filter
	newf.options = &ops

	for k, v := range f.byType {
		newf.byType[k] = v
	}
	for k, v := range f.byName {
		newf.byName[k] = v
	}
	for k, v := range f.assistantModels {
		newf.assistantModels[k] = v
	}
	for k, v := range f.orgAssistantModels {
		newf.orgAssistantModels[k] = v
	}
	return newf
}

// CreateLLM builds a provider client from its configuration, choosing the first
// of preferredModels that the provider offers and falling back to its default
// model. The provider family is selected by cfg.OpenAI.APIType; an unknown type
// returns an error.
//
// Assign to the NewLLM variable to substitute this in tests.
func CreateLLM(cfg *ProviderConfig, preferredModels []string, opts *Options) (llms.Model, error) {
	provType := strings.ToUpper(cfg.OpenAI.APIType)
	switch provType {
	case string(llms.ProviderOpenAI), "OPEN_AI":
		return newOpenAI(cfg, preferredModels)
	case string(llms.ProviderOpenRouter):
		return newOpenRouter(cfg, preferredModels, opts)
	case string(llms.ProviderOpenAIBedrock):
		return newOpenAIBedrock(cfg, preferredModels, opts)
	case string(llms.ProviderPerplexity):
		return newPerplexity(cfg, preferredModels)
	case string(llms.ProviderAzure), string(llms.ProviderAzureAD):
		return newAzure(cfg, preferredModels)
	case string(llms.ProviderAnthropic):
		return newAnthropic(cfg, preferredModels, opts)
	case string(llms.ProviderGoogleAI):
		return newGoogleAI(cfg, preferredModels)
	case string(llms.ProviderBedrock):
		return newBedrock(cfg, preferredModels, opts)
	case string(llms.ProviderAnthropicBedrock):
		return newAnthropicBedrock(cfg, preferredModels, opts)
	case string(llms.ProviderCloudflare):
		return newCloudflare(cfg, preferredModels, opts)
	}
	return nil, errors.Errorf("unsupported provider type: %s", provType)
}

func newOpenAI(cfg *ProviderConfig, preferredModels []string) (llms.Model, error) {
	var opts []openai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, openai.WithProvider(llms.ProviderOpenAI), openai.WithModel(model))

	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	if cfg.OpenAI.Organization != "" {
		opts = append(opts, openai.WithOrganization(cfg.OpenAI.Organization))
	}
	if cfg.OpenAI.Project != "" {
		opts = append(opts, openai.WithProject(cfg.OpenAI.Project))
	}
	return openai.New(opts...)
}

func newOpenRouter(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}

	baseURL := cfg.OpenAI.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenRouterBaseURL
	}
	opts := []openai.Option{
		openai.WithProvider(llms.ProviderOpenRouter),
		openai.WithModel(model),
		openai.WithBaseURL(baseURL),
		openai.WithHeaders(cfg.Headers),
	}
	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if options != nil && options.HTTPClient != nil {
		opts = append(opts, openai.WithHTTPClient(options.HTTPClient))
	}
	return openai.New(opts...)
}

func newOpenAIBedrock(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []openai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts,
		openai.WithProvider(llms.ProviderOpenAIBedrock),
		openai.WithModel(model),
	)
	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	if cfg.OpenAI.Organization != "" {
		opts = append(opts, openai.WithOrganization(cfg.OpenAI.Organization))
	}
	if cfg.OpenAI.Project != "" {
		opts = append(opts, openai.WithProject(cfg.OpenAI.Project))
	}
	if options != nil && options.AwsConfigFactory != nil {
		cfg, err := options.AwsConfigFactory()
		if err != nil {
			return nil, err
		}
		if options.HTTPClient != nil {
			cfg.HTTPClient = options.HTTPClient
		}
		opts = append(opts, openai.WithAWSConfig(cfg))
	}
	return openai.New(opts...)
}

func newPerplexity(cfg *ProviderConfig, preferredModels []string) (llms.Model, error) {
	var opts []openai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, openai.WithProvider(llms.ProviderPerplexity), openai.WithModel(model))

	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	return openai.New(opts...)
}

func newAzure(cfg *ProviderConfig, preferredModels []string) (llms.Model, error) {
	var opts []openai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, openai.WithAPIVersion(cfg.OpenAI.APIVersion), openai.WithModel(model))

	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if strings.EqualFold(cfg.OpenAI.APIType, "AZURE_AD") {
		opts = append(opts, openai.WithProvider(llms.ProviderAzureAD))
	} else {
		opts = append(opts, openai.WithProvider(llms.ProviderAzure))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(cfg.OpenAI.BaseURL))
	}
	return openai.New(opts...)
}

func newAnthropic(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []anthropic.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, anthropic.WithModel(model))
	if cfg.Token != "" {
		opts = append(opts, anthropic.WithToken(cfg.Token))
	}
	if options != nil && options.HTTPClient != nil {
		opts = append(opts, anthropic.WithHTTPClient(options.HTTPClient))
	}
	return anthropic.New(opts...)
}

func newGoogleAI(cfg *ProviderConfig, preferredModels []string) (llms.Model, error) {
	var opts []googleai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, googleai.WithDefaultModel(model))
	if cfg.Token != "" {
		opts = append(opts, googleai.WithAPIKey(cfg.Token))
	}
	return googleai.New(context.Background(), opts...)
}

func newBedrock(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []bedrock.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, bedrock.WithModel(model))
	if options != nil && options.AwsConfigFactory != nil {
		cfg, err := options.AwsConfigFactory()
		if err != nil {
			return nil, err
		}
		if options.HTTPClient != nil {
			cfg.HTTPClient = options.HTTPClient
		}
		opts = append(opts, bedrock.WithConfig(cfg))
	}

	return bedrock.New(opts...)
}

func newAnthropicBedrock(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []anthropic.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, anthropic.WithModel(model))
	if options != nil && options.AwsConfigFactory != nil {
		cfg, err := options.AwsConfigFactory()
		if err != nil {
			return nil, err
		}
		if options.HTTPClient != nil {
			cfg.HTTPClient = options.HTTPClient
		}
		opts = append(opts, anthropic.WithAWSConfig(cfg))
	}
	return anthropic.NewBedrock(opts...)
}

func newCloudflare(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []cloudflare.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, cloudflare.WithModel(model))
	if cfg.Token != "" {
		opts = append(opts, cloudflare.WithToken(cfg.Token))
	}
	if cfg.OpenAI.BaseURL != "" {
		opts = append(opts, cloudflare.WithServerURL(cfg.OpenAI.BaseURL))
	}
	if options != nil && options.HTTPClient != nil {
		opts = append(opts, cloudflare.WithHTTPClient(options.HTTPClient))
	}
	return cloudflare.New(opts...)
}

// GetModel returns an LLM model that matches the given options.
func (f *factory) GetModel(ctx context.Context, opts ModelOptions) (llms.Model, error) {
	// A specific provider type takes precedence over name-based resolution.
	if opts.ProviderType != "" {
		return f.getModelByType(opts)
	}

	// Expand the assistant model mapping (optionally per-org) into the
	// preferred models, keeping caller-provided models at the front.
	preferred := opts.PreferredModels
	if opts.AssistantName != "" {
		assistantModels := f.getOrgAssistants(opts.OrgID)
		if modelNames, ok := assistantModels[opts.AssistantName]; ok {
			preferred = append(slices.Clone(opts.PreferredModels), modelNames...)
		} else if modelNames, ok := assistantModels["default"]; ok {
			preferred = append(slices.Clone(opts.PreferredModels), modelNames...)
		}
	}

	return f.getModelByName(ctx, opts, preferred)
}

// configProviderType returns the normalized provider type for a provider
// config, so it can be matched against the capability table.
func configProviderType(cfg *ProviderConfig) llms.ProviderType {
	pt := llms.ProviderType(strings.ToUpper(cfg.OpenAI.APIType))
	if pt == "OPEN_AI" {
		return llms.ProviderOpenAI
	}
	return pt
}

// supportsCapabilities reports whether the provider type supports ALL of the
// required capabilities. A zero required mask always matches.
func supportsCapabilities(pt llms.ProviderType, required llms.Capability) bool {
	if required == 0 {
		return true
	}
	return llms.ProviderCapabilities(pt)&required == required
}

// isModelAllowed reports whether the model may be used for the org, assistant, and model.
// When no ModelFilter is configured, all models are allowed.
func (f *factory) isModelAllowed(ctx context.Context, orgID, assistantName, modelName string) bool {
	assistantName = values.StringsCoalesce(assistantName, "default")
	return f.options.ModelFilter == nil || f.options.ModelFilter(ctx, orgID, assistantName, modelName)
}

// resolveDefault returns the default model, honoring capability and org
// filters. It must be called with f.lock held.
func (f *factory) resolveDefault(ctx context.Context, opts ModelOptions) (llms.Model, error) {
	if len(f.cfg.Providers) == 0 || f.defaultProvider == nil {
		return nil, errors.New("no providers configured")
	}

	if f.defaultProvider.DefaultModel == "" {
		return nil, errors.New("no default model configured")
	}

	if !supportsCapabilities(configProviderType(f.defaultProvider), opts.RequiredCapabilities) {
		return nil, errors.Errorf("default provider %s does not support required capabilities", f.defaultProvider.Name)
	}

	if !f.isModelAllowed(ctx, opts.OrgID, opts.AssistantName, f.defaultProvider.DefaultModel) {
		return nil, errors.Errorf("model not available for org: %s", f.defaultProvider.DefaultModel)
	}

	return NewLLM(f.defaultProvider, []string{f.defaultProvider.DefaultModel}, f.options)
}

// getModelByType returns a model for the requested provider type.
func (f *factory) getModelByType(opts ModelOptions) (llms.Model, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	providerType := opts.ProviderType
	for _, cfg := range f.cfg.Providers {
		if cfg.OpenAI.APIType != string(providerType) {
			continue
		}
		if !supportsCapabilities(configProviderType(cfg), opts.RequiredCapabilities) {
			return nil, errors.Errorf("provider %s does not support required capabilities", cfg.Name)
		}

		if client, ok := f.byType[providerType]; ok {
			return client, nil
		}

		model, err := NewLLM(cfg, nil, f.options)
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
	return nil, errors.Errorf("provider not found for type: %s", providerType)
}

// getModelByName returns a model by resolving the preferred model names in
// order, falling back to the default model when none match.
func (f *factory) getModelByName(ctx context.Context, opts ModelOptions, modelNames []string) (llms.Model, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, modelNamePath := range modelNames {
		if !f.isModelAllowed(ctx, opts.OrgID, opts.AssistantName, modelNamePath) {
			continue
		}

		if client, ok := f.byName[modelNamePath]; ok {
			if supportsCapabilities(client.GetProviderType(), opts.RequiredCapabilities) {
				return client, nil
			}
			continue
		}

		providerName, modelName := f.resolveModelNamePath(modelNamePath)

		for _, cfg := range f.cfg.Providers {
			if providerName != "" && providerName != cfg.Name {
				continue
			}
			if !supportsCapabilities(configProviderType(cfg), opts.RequiredCapabilities) {
				continue
			}
			if slices.Contains(cfg.AvailableModels, modelName) {
				if modelName != modelNamePath && !f.isModelAllowed(ctx, opts.OrgID, opts.AssistantName, modelName) {
					continue
				}
				model, err := NewLLM(cfg, []string{modelName}, f.options)
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
					"provider", cfg.Name,
					"model", modelName,
					"path", modelNamePath,
				)

				f.byName[modelNamePath] = model
				return model, nil
			}
		}
	}

	return f.resolveDefault(ctx, opts)
}

func (f *factory) getOrgAssistants(orgID string) map[string][]string {
	if am, ok := f.orgAssistantModels[orgID]; ok {
		return am
	}
	return f.assistantModels
}

// resolveModelNamePath splits a provider-qualified model reference while preserving
// slash-delimited model IDs. A prefix is treated as a provider only when it
// exactly matches a configured provider name.
func (f *factory) resolveModelNamePath(modelNamePath string) (string, string) {
	for _, cfg := range f.cfg.Providers {
		prefix := cfg.Name + "/"
		if strings.HasPrefix(modelNamePath, prefix) {
			return cfg.Name, strings.TrimPrefix(modelNamePath, prefix)
		}
	}
	return "", modelNamePath
}

// Skills returns all loaded skills for the given agent sorted alphabetically by name.
func (f *factory) Skills(agent string, tags ...string) skills.Skills {
	if f.skillsLoader == nil {
		return nil
	}
	return f.skillsLoader.Skills(agent, tags...)
}
