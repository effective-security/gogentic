package llmfactory

import (
	"context"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/llms/googleai"
	"github.com/effective-security/gogentic/pkg/llms/openai"
)

// CreateModelLister builds the client that lists the models of a provider,
// from the same configuration and options as its model clients. The provider
// family is selected by cfg.OpenAI.APIType; a type that does not advertise
// llms.CapabilityModels returns llms.ErrModelListNotSupported.
//
// Assign to the NewModelLister variable to substitute this in tests.
func CreateModelLister(cfg *ProviderConfig, opts *Options) (llms.ModelLister, error) {
	switch configProviderType(cfg) {
	case llms.ProviderOpenAI:
		return openai.NewModelLister(openAIConnection(cfg, opts)...)
	case llms.ProviderOpenRouter:
		return openai.NewModelLister(openRouterConnection(cfg, opts)...)
	case llms.ProviderAnthropic:
		return anthropic.NewModelLister(anthropicConnection(cfg, opts)...)
	case llms.ProviderGoogleAI:
		return googleai.NewModelLister(context.Background(), googleAIConnection(cfg, opts)...)
	}
	return nil, errors.Wrapf(llms.ErrModelListNotSupported, "provider %s: api_type %q", cfg.Name, cfg.OpenAI.APIType)
}

// openAIConnection returns the options that connect a client to the OpenAI
// API of a provider: what a model client and a model lister have in common.
func openAIConnection(cfg *ProviderConfig, options *Options) []openai.Option {
	opts := []openai.Option{
		openai.WithProvider(llms.ProviderOpenAI),
	}
	if options != nil && options.HTTPClient != nil {
		opts = append(opts, openai.WithHTTPClient(options.HTTPClient))
	}
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
	return opts
}

// openRouterConnection returns the options that connect a client to the
// OpenRouter API of a provider.
func openRouterConnection(cfg *ProviderConfig, options *Options) []openai.Option {
	baseURL := cfg.OpenAI.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenRouterBaseURL
	}
	opts := []openai.Option{
		openai.WithProvider(llms.ProviderOpenRouter),
		openai.WithBaseURL(baseURL),
		openai.WithHeaders(cfg.Headers),
	}
	if cfg.Token != "" {
		opts = append(opts, openai.WithToken(cfg.Token))
	}
	if options != nil && options.HTTPClient != nil {
		opts = append(opts, openai.WithHTTPClient(options.HTTPClient))
	}
	return opts
}

// anthropicConnection returns the options that connect a client to the
// Anthropic API of a provider.
func anthropicConnection(cfg *ProviderConfig, options *Options) []anthropic.Option {
	var opts []anthropic.Option
	if cfg.Token != "" {
		opts = append(opts, anthropic.WithToken(cfg.Token))
	}
	if options != nil && options.HTTPClient != nil {
		opts = append(opts, anthropic.WithHTTPClient(options.HTTPClient))
	}
	return opts
}

// googleAIConnection returns the options that connect a client to the Google
// AI API of a provider.
func googleAIConnection(cfg *ProviderConfig, options *Options) []googleai.Option {
	var opts []googleai.Option
	if options != nil && options.HTTPClient != nil {
		transport := factoryTransport{
			client: options.HTTPClient,
		}
		opts = append(opts, googleai.WithHTTPClient(&http.Client{Transport: transport}))
	}
	if cfg.Token != "" {
		opts = append(opts, googleai.WithAPIKey(cfg.Token))
	}
	return opts
}
