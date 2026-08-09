package openai

import (
	"fmt"
	"net/http"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/effective-security/x/values"
)

var (
	ErrEmptyResponse              = errors.New("no response")
	ErrMissingToken               = errors.New("missing the OpenAI API key")
	ErrMissingAzureModel          = errors.New("model needs to be provided when using Azure API")
	ErrMissingAzureEmbeddingModel = errors.New("embeddings model needs to be provided when using Azure API")

	ErrUnexpectedResponseLength = errors.New("unexpected length of response")
)

// newClient creates an instance of the internal client.
func newClient(opts ...Option) (*options, *openaiclient.Client, error) {
	// default options
	options := &options{
		model:        os.Getenv(DefaultModelEnvVarName),
		baseURL:      os.Getenv(DefaultBaseURLEnvVarName),
		organization: os.Getenv(DefaultOrganizationEnvVarName),
		project:      os.Getenv(DefaultProjectEnvVarName),
		provider:     llms.ProviderOpenAI,
		httpClient:   http.DefaultClient,
	}

	for _, opt := range opts {
		opt(options)
	}

	typ := openaiclient.ProviderType(options.provider)
	tokenVarName := DefaultTokenEnvVarName
	if openaiclient.IsBedrock(typ) {
		tokenVarName = "AWS_BEARER_TOKEN_BEDROCK"
	}
	options.token = values.StringsCoalesce(options.token, os.Getenv(tokenVarName))
	if len(options.token) == 0 {
		return options, nil, errors.WithStack(ErrMissingToken)
	}

	// set of options needed for Azure client
	if openaiclient.IsAzure(typ) && options.apiVersion == "" {
		options.apiVersion = DefaultAPIVersion
		if options.model == "" {
			return options, nil, errors.WithStack(ErrMissingAzureModel)
		}
		if options.embeddingModel == "" {
			return options, nil, errors.WithStack(ErrMissingAzureEmbeddingModel)
		}
	} else if openaiclient.IsBedrock(typ) {
		if options.AWSCfg == nil {
			return options, nil, errors.New("bedrock openai: AWS config is required")
		}
		if options.AWSCfg.Region == "" {
			return options, nil, errors.New("bedrock openai: AWS region is required")
		}
		if options.baseURL == "" {
			options.baseURL = fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1", options.AWSCfg.Region)
		}
	}

	cli, err := openaiclient.New(
		typ,
		options.model,
		options.token,
		options.baseURL,
		options.organization,
		options.project,
		options.apiVersion,
		options.httpClient,
		options.embeddingModel,
		options.responseFormat,
	)
	return options, cli, err
}
