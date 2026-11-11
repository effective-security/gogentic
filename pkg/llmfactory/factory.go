package llmfactory

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/llms/bedrock"
	"github.com/effective-security/gogentic/pkg/llms/googleai"
	"github.com/effective-security/gogentic/pkg/llms/openai"
	"github.com/effective-security/gogentic/skills"
	"github.com/effective-security/x/values"
	"github.com/effective-security/xlog"
)

//go:generate mockgen -source=factory.go -destination=../../mocks/mockllmfactory/llmfactory_mock.gen.go  -package mockllmfactory

const (
	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

	// instanceCachePrefix separates the clients built for resolved models
	// from the other entries that share the cache.
	instanceCachePrefix = "model"
	// cacheKeySeparator joins the parts of a cache key. It cannot occur in a
	// model ID.
	cacheKeySeparator = "\x00"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "llmfactory")

// NewLLM is a wrapper for CreateLLM to allow for overriding the default implementation.
var NewLLM = CreateLLM

// NewModelLister is a wrapper for CreateModelLister to allow for overriding
// the default implementation. The factory calls it once per provider with
// discovery, on the first fetch of its model list.
var NewModelLister = CreateModelLister

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
	//     allowed model wins. An entry may be a pattern and may carry a
	//     default reasoning effort, as in "ANTHROPIC/claude-opus-*,high".
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
	// PreferredModels specifies the preferred models to use, in order. Each
	// entry is a model reference, [PROVIDER/]MODEL[,EFFORT]: the model may be
	// a pattern, and the effort is the default for calls that set none.
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

	defaultProvider *ProviderConfig
	// policies holds the compiled model policy of each provider, in the
	// order of cfg.Providers. It is immutable after New, and shared with the
	// factories derived by WithModelFilter, so they read one model list per
	// provider.
	policies           []*providerPolicy
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
		f.assistantModels[k] = validReferences(configScopeGlobal, k, v)
	}

	for orgID, orgCfg := range cfg.Orgs {
		if orgCfg == nil {
			continue
		}
		for assistantName, models := range orgCfg.AssistantModels {
			if f.orgAssistantModels[orgID] == nil {
				f.orgAssistantModels[orgID] = make(map[string][]string)
			}
			f.orgAssistantModels[orgID][assistantName] = validReferences(configScopeOrg+orgID, assistantName, models)
		}
	}

	for _, provider := range cfg.Providers {
		if strings.EqualFold(provider.OpenAI.APIType, "OPEN_AI") {
			provider.OpenAI.APIType = string(llms.ProviderOpenAI)
		}
	}

	// New cannot fail, so entries that do not parse are dropped and logged.
	// Configurations read with LoadConfig were already validated.
	for index, provider := range cfg.Providers {
		policy, err := newProviderPolicy(provider, index)
		if err != nil {
			logger.KV(xlog.ERROR,
				"reason", "invalid_model_policy",
				"provider", provider.Name,
				"err", err.Error(),
			)
		}
		if policy.discovers() {
			policy.catalog = newModelCatalog(provider.Name, policy.refresh, func() (llms.ModelLister, error) {
				return NewModelLister(provider, f.options)
			})
		}
		f.policies = append(f.policies, policy)
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
		policies:           f.policies,
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
		return newOpenAI(cfg, preferredModels, opts)
	case string(llms.ProviderOpenRouter):
		return newOpenRouter(cfg, preferredModels, opts)
	case string(llms.ProviderOpenAIBedrock):
		return newOpenAIBedrock(cfg, preferredModels, opts)
	case string(llms.ProviderAzure), string(llms.ProviderAzureAD):
		return newAzure(cfg, preferredModels, opts)
	case string(llms.ProviderAnthropic):
		return newAnthropic(cfg, preferredModels, opts)
	case string(llms.ProviderGoogleAI):
		return newGoogleAI(cfg, preferredModels, opts)
	case string(llms.ProviderBedrock):
		return newBedrock(cfg, preferredModels, opts)
	case string(llms.ProviderAnthropicBedrock):
		return newAnthropicBedrock(cfg, preferredModels, opts)
	}
	return nil, errors.Errorf("unsupported provider type: %s", provType)
}

func newOpenAI(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []openai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, openai.WithModel(model))
	opts = append(opts, openAIEffort(options)...)
	inferenceAPI, err := inferenceAPIOption(cfg)
	if err != nil {
		return nil, err
	}
	opts = append(opts, inferenceAPI)
	opts = append(opts, openAIConnection(cfg, options)...)
	return openai.New(opts...)
}

func newOpenRouter(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}

	opts := []openai.Option{
		openai.WithModel(model),
	}
	opts = append(opts, openAIEffort(options)...)
	opts = append(opts, openRouterConnection(cfg, options)...)
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
	opts = append(opts, openAIEffort(options)...)
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

func newAzure(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []openai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, openai.WithAPIVersion(cfg.OpenAI.APIVersion), openai.WithModel(model))
	opts = append(opts, openAIEffort(options)...)

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
	opts = append(opts, anthropicEffort(options)...)
	opts = append(opts, anthropicConnection(cfg, options)...)
	return anthropic.New(opts...)
}

func newGoogleAI(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []googleai.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, googleai.WithDefaultModel(model))
	if options != nil && !options.Effort.IsZero() {
		opts = append(opts, googleai.WithEffortPolicy(options.Effort))
	}
	opts = append(opts, googleAIConnection(cfg, options)...)
	return googleai.New(context.Background(), opts...)
}

func newBedrock(cfg *ProviderConfig, preferredModels []string, options *Options) (llms.Model, error) {
	var opts []bedrock.Option
	model, err := cfg.FindModel(preferredModels...)
	if err != nil {
		return nil, err
	}
	opts = append(opts, bedrock.WithModel(model))
	if cfg.OpenAI.Converse {
		opts = append(opts, bedrock.WithConverse())
	}
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
	opts = append(opts, anthropicEffort(options)...)
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

// openAIEffort returns the option that hands the model's effort settings to
// an OpenAI-family client, or nothing when there are none.
func openAIEffort(options *Options) []openai.Option {
	if options == nil || options.Effort.IsZero() {
		return nil
	}
	return []openai.Option{
		openai.WithEffortPolicy(options.Effort),
	}
}

// anthropicEffort returns the option that hands the model's effort settings
// to an Anthropic client, or nothing when there are none.
func anthropicEffort(options *Options) []anthropic.Option {
	if options == nil || options.Effort.IsZero() {
		return nil
	}
	return []anthropic.Option{
		anthropic.WithEffortPolicy(options.Effort),
	}
}

// validReferences returns the model references of one assistant that parse.
// The others are logged and left out, so resolution never sees them.
func validReferences(scope, assistant string, refs []string) []string {
	valid := make([]string, 0, len(refs))
	for _, ref := range refs {
		if _, err := parseRefSpec(ref); err != nil {
			logger.KV(xlog.ERROR,
				"reason", "invalid_model_reference",
				"scope", scope,
				"assistant", assistant,
				"err", err.Error(),
			)
			continue
		}
		valid = append(valid, ref)
	}
	return valid
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

// optionsFor returns the construction options for a model with the given
// effort settings. Without any, the factory options are used as they are.
func (f *factory) optionsFor(effort llms.EffortPolicy) *Options {
	if effort.IsZero() {
		return f.options
	}
	options := *f.options
	options.Effort = effort
	return &options
}

// policyFor returns the compiled model policy of a provider configuration.
func (f *factory) policyFor(cfg *ProviderConfig) *providerPolicy {
	for _, policy := range f.policies {
		if policy.cfg == cfg {
			return policy
		}
	}
	// Every configured provider has a policy; this is reached only for a
	// configuration that is not part of the factory.
	policy, _ := newProviderPolicy(cfg, len(f.policies))
	return policy
}

// instanceKey identifies a client in the cache by provider, model and effort
// settings, including what the provider reports for the model.
func instanceKey(provider int, model string, effort llms.EffortPolicy) string {
	return strings.Join([]string{
		instanceCachePrefix,
		strconv.Itoa(provider),
		model,
		effortKey(effort),
	}, cacheKeySeparator)
}

// effortKey is the part of a cache key that tells clients of one model
// apart by their effort settings. A client is built with them, so a change of
// what the provider reports for the model needs another client.
func effortKey(effort llms.EffortPolicy) string {
	return strings.Join([]string{
		effort.Default.String(),
		effort.Max.String(),
		strconv.Itoa(int(effort.Supported)),
		effort.Implicit.String(),
	}, cacheKeySeparator)
}

func (f *factory) getOrgAssistants(orgID string) map[string][]string {
	if am, ok := f.orgAssistantModels[orgID]; ok {
		return am
	}
	return f.assistantModels
}

// providerScope says which providers a model reference may address.
type providerScope struct {
	// prefix is the provider part of the reference as written, or empty for
	// a reference without one.
	prefix string
	// name is the provider name the reference names, when it names one.
	name string
	// typ is the provider type the reference names, when it names one.
	typ llms.ProviderType
}

// includes reports whether the reference may be resolved on the provider.
func (s providerScope) includes(policy *providerPolicy) bool {
	switch {
	case s.name != "":
		return policy.cfg.Name == s.name
	case s.typ != "":
		return policy.typ == s.typ
	}
	return true
}

// qualify returns the model ID with the provider prefix the reference used.
func (s providerScope) qualify(model string) string {
	if s.prefix == "" {
		return model
	}
	return s.prefix + providerSeparator + model
}

// splitProvider splits a provider-qualified model reference while preserving
// slash-delimited model IDs. A prefix is a provider when it exactly matches a
// configured provider name, or when it is an upper-case provider type or alias
// of a configured provider. Anything else is part of the model ID, so vendor
// prefixes such as "openai/" in an OpenRouter ID are left alone.
func (f *factory) splitProvider(path string) (providerScope, string) {
	for _, cfg := range f.cfg.Providers {
		prefix := cfg.Name + providerSeparator
		if strings.HasPrefix(path, prefix) {
			scope := providerScope{
				prefix: cfg.Name,
				name:   cfg.Name,
			}
			return scope, strings.TrimPrefix(path, prefix)
		}
	}

	head, rest, found := strings.Cut(path, providerSeparator)
	if !found || head == "" || head != strings.ToUpper(head) {
		return providerScope{}, path
	}
	typ, err := llms.ParseProviderType(head)
	if err != nil {
		return providerScope{}, path
	}
	for _, policy := range f.policies {
		if policy.typ == typ {
			scope := providerScope{
				prefix: head,
				typ:    typ,
			}
			return scope, rest
		}
	}
	return providerScope{}, path
}

// Skills returns all loaded skills for the given agent sorted alphabetically by name.
func (f *factory) Skills(agent string, tags ...string) skills.Skills {
	if f.skillsLoader == nil {
		return nil
	}
	return f.skillsLoader.Skills(agent, tags...)
}

// inferenceAPIOption validates the configured portable inference API selection.
func inferenceAPIOption(cfg *ProviderConfig) (openai.Option, error) {
	api := openai.InferenceAPI(strings.ToLower(cfg.OpenAI.InferenceAPI))
	switch api {
	case openai.InferenceAPIDefault, openai.InferenceAPIResponses, openai.InferenceAPIChat:
		return openai.WithInferenceAPI(api), nil
	}
	return nil, errors.Errorf("provider %s: unsupported inference_api %q", cfg.Name, cfg.OpenAI.InferenceAPI)
}

// factoryTransport adapts the factory Doer to SDKs requiring an http.Client.
type factoryTransport struct{ client HTTPClient }

func (t factoryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.client.Do(r)
	if err != nil {
		return nil, errors.WithMessage(err, "perform provider HTTP request")
	}
	return res, nil
}
