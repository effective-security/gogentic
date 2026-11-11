package cloudflare

import (
	"net/http"
)

// DefaultServerURL is the Cloudflare API base URL that New uses when
// WithServerURL is not given. Requests go to
// <server URL>/<account ID>/ai/run/<model>.
const DefaultServerURL = "https://api.cloudflare.com/client/v4/accounts"

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type options struct {
	cloudflareAccountID string
	cloudflareServerURL string
	cloudflareToken     string
	httpClient          HTTPClient
	model               string
	embeddingModel      string
	system              string
}

type Option func(*options)

// WithModel Set the model to use.
func WithModel(model string) Option {
	return func(opts *options) {
		opts.model = model
	}
}

// WithSystemPrompt Set the system prompt. This is only valid if
// WithCustomTemplate is not set and the cloudflare model use
// .System in its model template OR if WithCustomTemplate
// is set using {{.System}}.
func WithSystemPrompt(p string) Option {
	return func(opts *options) {
		opts.system = p
	}
}

// WithAccountID Set the Account Id of the cloudflare account to use.
func WithAccountID(accountID string) Option {
	return func(opts *options) {
		opts.cloudflareAccountID = accountID
	}
}

// WithServerURL sets the base URL of the Cloudflare API: the part of the
// endpoint before /<account ID>/ai/run/<model>, such as DefaultServerURL.
// New returns an error when the URL is not absolute.
func WithServerURL(rawURL string) Option {
	return func(opts *options) {
		opts.cloudflareServerURL = rawURL
	}
}

// WithToken Set the token to use.
func WithToken(token string) Option {
	return func(opts *options) {
		opts.cloudflareToken = token
	}
}

func WithEmbeddingModel(model string) Option {
	return func(opts *options) {
		opts.embeddingModel = model
	}
}

// WithHTTPClient Set custom http client.
func WithHTTPClient(client HTTPClient) Option {
	return func(opts *options) {
		opts.httpClient = client
	}
}
