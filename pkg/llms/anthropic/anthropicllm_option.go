package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/effective-security/gogentic/pkg/llms"
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

	// AWSCfg is used with Bedrock
	AWSCfg *aws.Config

	// Effort is the reasoning effort default and ceiling for every call.
	Effort llms.EffortPolicy
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

// WithAWSConfig passes the AWS config to the client.
func WithAWSConfig(cfg *aws.Config) Option {
	return func(opts *Options) {
		opts.AWSCfg = cfg
	}
}

// WithEffortPolicy sets the reasoning effort used when a call requests none,
// and the highest effort any call may use. Each call still sends the nearest
// level its model accepts. Legacy GenerateContent only; the portable Infer
// path is unaffected.
func WithEffortPolicy(policy llms.EffortPolicy) Option {
	return func(opts *Options) {
		opts.Effort = policy
	}
}
