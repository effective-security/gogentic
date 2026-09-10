package llmfactory

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// Options customize model construction for providers that need extra clients
// or environment hooks, and allow installing per‑org model filters.
type Options struct {
	// HTTPClient is used to create a new HTTP client.
	HTTPClient HTTPClient
	// AwsConfigFactory is used to create a new AWS config.
	AwsConfigFactory func() (*aws.Config, error)
	// ModelFilter reports whether a model may be used for an org,
	// e.g. to enforce per-org / per-model quota.
	ModelFilter ModelFilterFunc
}

// ModelFilterFunc reports whether the given model may be used for the org.
// Provide this to enforce per-org / per-assistant / per-model quota: return false when the
// model must not be used for the org (e.g. quota exceeded), true otherwise.
// The orgID can be empty, in which case the check applies globally.
// The assistantName can be empty, in which case the check applies globally.
// The modelName can be in the format of <provider_name>/<model_name>.
type ModelFilterFunc func(ctx context.Context, orgID string, assistantName string, modelName string) bool

// WithModelFilter sets a predicate used to restrict which models an org may use,
// for example to enforce per-org / per-assistant / per-model quota.
func WithModelFilter(filter ModelFilterFunc) Option {
	return func(opts *Options) {
		opts.ModelFilter = filter
	}
}

// Option configures Options.
type Option func(*Options)

// NewOptions returns Options with the given options applied.
func NewOptions(opts ...Option) *Options {
	o := Options{}
	for _, opt := range opts {
		opt(&o)
	}
	return &o
}

// WithAWSConfigFactory supplies the AWS configuration used by the Bedrock-based
// providers (BEDROCK, ANTHROPIC_BEDROCK, OPENAI_BEDROCK). It is called once per
// model construction.
func WithAWSConfigFactory(factory func() (*aws.Config, error)) Option {
	return func(opts *Options) {
		opts.AwsConfigFactory = factory
	}
}

// WithHTTPClient allows setting a custom HTTP client. If not set, the default value
// is http.DefaultClient.
func WithHTTPClient(client HTTPClient) Option {
	return func(opts *Options) {
		opts.HTTPClient = client
	}
}

// HTTPClient is primarily used to describe an [*http.Client], but also
// supports custom implementations.
//
// For bespoke implementations, prefer using an [*http.Client] with a
// custom transport. See [http.RoundTripper] for further information.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}
