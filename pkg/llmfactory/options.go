package llmfactory

import (
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type Options struct {
	// HTTPClient is used to create a new HTTP client.
	HTTPClient HTTPClient
	// AwsConfigFactory is used to create a new AWS config.
	AwsConfigFactory func() (*aws.Config, error)
}

type Option func(*Options)

func NewOptions(opts ...Option) Options {
	o := Options{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

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
