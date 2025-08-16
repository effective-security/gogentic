package googleai

import (
	"net/http"
	"os"

	"cloud.google.com/go/auth"
	"google.golang.org/genai"
)

// Options is a set of options for GoogleAI and Vertex clients.
type Options struct {
	CloudProject          string
	CloudLocation         string
	DefaultModel          string
	DefaultEmbeddingModel string
	DefaultCandidateCount int
	DefaultMaxTokens      int
	DefaultTemperature    float64
	DefaultTopK           int
	DefaultTopP           float64
	HarmThreshold         genai.HarmBlockThreshold
	APIKey                string
	Credentials           *auth.Credentials
	HTTPClient            *http.Client
}

func DefaultOptions() Options {
	return Options{
		CloudProject:          "",
		CloudLocation:         "",
		DefaultModel:          "gemini-2.5-pro",
		DefaultEmbeddingModel: "embedding-001",
		DefaultCandidateCount: 1,
		DefaultMaxTokens:      1048576,
		DefaultTemperature:    0.5,
		DefaultTopK:           3,
		DefaultTopP:           0.95,
		HarmThreshold:         genai.HarmBlockThresholdBlockOnlyHigh,
	}
}

// EnsureAuthPresent attempts to ensure that the client has authentication information. If it does not, it will attempt to use the GOOGLE_API_KEY environment variable.
func (o *Options) EnsureAuthPresent() {
	if o.Credentials == nil {
		if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			WithAPIKey(key)(o)
		}
	}
}

type Option func(*Options)

// WithAPIKey passes the API KEY (token) to the client. This is useful for
// googleai clients.
func WithAPIKey(apiKey string) Option {
	return func(opts *Options) {
		opts.APIKey = apiKey
	}
}

// WithCredentialsJSON append a ClientOption that authenticates
// API calls with the given service account or refresh token JSON
// credentials.
func WithCredentials(credentials *auth.Credentials) Option {
	return func(opts *Options) {
		if credentials == nil {
			return
		}
		opts.Credentials = credentials
	}
}

// WithHTTPClient append a ClientOption that uses the provided HTTP client to
// make requests.
// This is useful for vertex clients.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(opts *Options) {
		opts.HTTPClient = httpClient
	}
}

// WithCloudProject passes the GCP cloud project name to the client. This is
// useful for vertex clients.
func WithCloudProject(p string) Option {
	return func(opts *Options) {
		opts.CloudProject = p
	}
}

// WithCloudLocation passes the GCP cloud location (region) name to the client.
// This is useful for vertex clients.
func WithCloudLocation(l string) Option {
	return func(opts *Options) {
		opts.CloudLocation = l
	}
}

// WithDefaultModel passes a default content model name to the client. This
// model name is used if not explicitly provided in specific client invocations.
func WithDefaultModel(defaultModel string) Option {
	return func(opts *Options) {
		opts.DefaultModel = defaultModel
	}
}

// WithDefaultModel passes a default embedding model name to the client. This
// model name is used if not explicitly provided in specific client invocations.
func WithDefaultEmbeddingModel(defaultEmbeddingModel string) Option {
	return func(opts *Options) {
		opts.DefaultEmbeddingModel = defaultEmbeddingModel
	}
}

// WithDefaultCandidateCount sets the candidate count for the model.
func WithDefaultCandidateCount(defaultCandidateCount int) Option {
	return func(opts *Options) {
		opts.DefaultCandidateCount = defaultCandidateCount
	}
}

// WithDefaultMaxTokens sets the maximum token count for the model.
func WithDefaultMaxTokens(maxTokens int) Option {
	return func(opts *Options) {
		opts.DefaultMaxTokens = maxTokens
	}
}

// WithDefaultTemperature sets the maximum token count for the model.
func WithDefaultTemperature(defaultTemperature float64) Option {
	return func(opts *Options) {
		opts.DefaultTemperature = defaultTemperature
	}
}

// WithDefaultTopK sets the TopK for the model.
func WithDefaultTopK(defaultTopK int) Option {
	return func(opts *Options) {
		opts.DefaultTopK = defaultTopK
	}
}

// WithDefaultTopP sets the TopP for the model.
func WithDefaultTopP(defaultTopP float64) Option {
	return func(opts *Options) {
		opts.DefaultTopP = defaultTopP
	}
}

// WithHarmThreshold sets the safety/harm setting for the model, potentially
// limiting any harmful content it may generate.
func WithHarmThreshold(ht genai.HarmBlockThreshold) Option {
	return func(opts *Options) {
		opts.HarmThreshold = ht
	}
}
