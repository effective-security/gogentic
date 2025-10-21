package openai

import (
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/effective-security/gogentic/pkg/schema"
)

const (
	tokenEnvVarName        = "OPENAI_API_KEY"      //nolint:gosec
	modelEnvVarName        = "OPENAI_MODEL"        //nolint:gosec
	baseURLEnvVarName      = "OPENAI_BASE_URL"     //nolint:gosec
	baseAPIBaseEnvVarName  = "OPENAI_API_BASE"     //nolint:gosec
	organizationEnvVarName = "OPENAI_ORGANIZATION" //nolint:gosec
)

type ProviderType string

const (
	ProviderOpenAI     ProviderType = "OPENAI"
	ProviderAzure      ProviderType = "AZURE"
	ProviderAzureAD    ProviderType = "AZURE_AD"
	ProviderPerplexity ProviderType = "PERPLEXITY"
)
const (
	DefaultAPIVersion = "2023-05-15"
)

type options struct {
	token        string
	model        string
	baseURL      string
	organization string
	provider     ProviderType
	httpClient   openaiclient.Doer

	responseFormat *schema.ResponseFormat

	// required when APIType is APITypeAzure or APITypeAzureAD
	apiVersion     string
	embeddingModel string
}

// Option is a functional option for the OpenAI client.
type Option func(*options)

// WithToken passes the OpenAI API token to the client. If not set, the token
// is read from the OPENAI_API_KEY environment variable.
func WithToken(token string) Option {
	return func(opts *options) {
		opts.token = token
	}
}

// WithModel passes the OpenAI model to the client. If not set, the model
// is read from the OPENAI_MODEL environment variable.
// Required when ApiType is Azure.
func WithModel(model string) Option {
	return func(opts *options) {
		opts.model = model
	}
}

// WithEmbeddingModel passes the OpenAI model to the client. Required when ApiType is Azure.
func WithEmbeddingModel(embeddingModel string) Option {
	return func(opts *options) {
		opts.embeddingModel = embeddingModel
	}
}

// WithBaseURL passes the OpenAI base url to the client. If not set, the base url
// is read from the OPENAI_BASE_URL environment variable. If still not set in ENV
// VAR OPENAI_BASE_URL, then the default value is https://api.openai.com/v1 is used.
func WithBaseURL(baseURL string) Option {
	return func(opts *options) {
		opts.baseURL = baseURL
	}
}

// WithOrganization passes the OpenAI organization to the client. If not set, the
// organization is read from the OPENAI_ORGANIZATION.
func WithOrganization(organization string) Option {
	return func(opts *options) {
		opts.organization = organization
	}
}

// WithProvider passes the api type to the client. If not set, the default value
// is ProviderOpenAI.
func WithProvider(apiType ProviderType) Option {
	return func(opts *options) {
		opts.provider = apiType
	}
}

// WithAPIVersion passes the api version to the client. If not set, the default value
// is DefaultAPIVersion.
func WithAPIVersion(apiVersion string) Option {
	return func(opts *options) {
		opts.apiVersion = apiVersion
	}
}

// WithHTTPClient allows setting a custom HTTP client. If not set, the default value
// is http.DefaultClient.
func WithHTTPClient(client openaiclient.Doer) Option {
	return func(opts *options) {
		opts.httpClient = client
	}
}

// WithResponseFormat allows setting a custom response format.
func WithResponseFormat(responseFormat *schema.ResponseFormat) Option {
	return func(opts *options) {
		opts.responseFormat = responseFormat
	}
}
