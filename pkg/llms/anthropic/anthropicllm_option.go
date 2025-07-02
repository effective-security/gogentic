package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	TokenEnvVarName = "ANTHROPIC_API_KEY" //nolint:gosec
)

type Options struct {
	Token      string
	Model      string
	BaseURL    string
	HttpClient option.HTTPClient

	// If supplied, the 'anthropic-beta' header will be added to the request with the given value.
	AnthropicBetaHeader string
}

type Option func(*Options)

// WithToken passes the Anthropic API token to the client. If not set, the token
// is read from the ANTHROPIC_API_KEY environment variable.
func WithToken(token string) Option {
	return func(opts *Options) {
		opts.Token = token
	}
}

// WithModel passes the Anthropic model to the client.
func WithModel(model string) Option {
	return func(opts *Options) {
		opts.Model = model
	}
}

// WithBaseUrl passes the Anthropic base URL to the client.
// If not set, the default base URL is used.
func WithBaseURL(baseURL string) Option {
	return func(opts *Options) {
		opts.BaseURL = baseURL
	}
}

// WithHTTPClient allows setting a custom HTTP client. If not set, the default value
// is http.DefaultClient.
func WithHTTPClient(client option.HTTPClient) Option {
	return func(opts *Options) {
		opts.HttpClient = client
	}
}

// WithAnthropicBetaHeader adds the Anthropic Beta header to support extended options.
func WithAnthropicBetaHeader(value string) Option {
	return func(opts *Options) {
		opts.AnthropicBetaHeader = value
	}
}
