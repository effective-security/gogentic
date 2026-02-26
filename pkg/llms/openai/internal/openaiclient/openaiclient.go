package openaiclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

const (
	DefaultBaseURL              = "https://api.openai.com/v1"
	DefaultFunctionCallBehavior = "auto"
	DefaultChatModel            = "gpt-5-mini"
	DefaultMaxTokens            = 2 * 16384
)

// ErrEmptyResponse is returned when the OpenAI API returns an empty response.
var ErrEmptyResponse = errors.New("empty response")

type ProviderType string

const (
	ProviderOpenAI     ProviderType = "OPENAI"
	ProviderAzure      ProviderType = "AZURE"
	ProviderAzureAD    ProviderType = "AZURE_AD"
	ProviderPerplexity ProviderType = "PERPLEXITY"
)

// ToolType is the type of a tool.
type ToolType string

const (
	ToolTypeFunction  ToolType = "function"
	ToolTypeWebSearch ToolType = "web_search"
)

// Client is a client for the OpenAI API.
type Client struct {
	Model    string
	Provider ProviderType

	token        string
	baseURL      string
	organization string
	httpClient   Doer

	EmbeddingModel string
	// required when APIType is APITypeAzure or APITypeAzureAD
	apiVersion           string
	supportsResponsesAPI bool

	ResponseFormat *schema.ResponseFormat
}

// Option is an option for the OpenAI client.
type Option func(*Client) error

// Doer performs a HTTP request.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// New returns a new OpenAI client.
func New(provider ProviderType, model string, token string, baseURL string, organization string,
	apiVersion string, httpClient Doer, embeddingModel string,
	responseFormat *schema.ResponseFormat,
	opts ...Option,
) (*Client, error) {
	c := &Client{
		Model:                model,
		token:                token,
		EmbeddingModel:       embeddingModel,
		baseURL:              strings.TrimSuffix(baseURL, "/"),
		organization:         organization,
		Provider:             provider,
		apiVersion:           apiVersion,
		httpClient:           httpClient,
		ResponseFormat:       responseFormat,
		supportsResponsesAPI: isResponsesAPI(provider, apiVersion),
	}
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func isResponsesAPI(provider ProviderType, apiVersion string) bool {
	if provider == ProviderAzure || provider == ProviderAzureAD {
		// Azure API versions are dates like YYYY-MM-DD, optionally with a "-preview" suffix.
		// Perform a proper date comparison instead of lexicographical string comparison.
		if idx := strings.Index(apiVersion, "-preview"); idx != -1 {
			apiVersion = apiVersion[:idx]
		}
		apiVersion = strings.TrimSpace(apiVersion)
		versionDate, err := time.Parse("2006-01-02", apiVersion)
		if err != nil {
			return false
		}
		thresholdDate := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
		return !versionDate.Before(thresholdDate)
	}
	return provider == ProviderOpenAI || provider == "OPEN_AI"
}

func (c *Client) SupportsResponsesAPI() bool {
	return c.supportsResponsesAPI
}

// Completion is a completion.
type Completion struct {
	Text string `json:"text"`
}

// CreateCompletion creates a completion.
func (c *Client) CreateCompletion(ctx context.Context, r *CompletionRequest) (*Completion, error) {
	resp, err := c.createCompletion(ctx, r)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}
	return &Completion{
		Text: resp.Choices[0].Message.Content,
	}, nil
}

// EmbeddingRequest is a request to create an embedding.
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// CreateEmbedding creates embeddings.
func (c *Client) CreateEmbedding(ctx context.Context, r *EmbeddingRequest) ([][]float32, error) {
	if r.Model == "" {
		r.Model = defaultEmbeddingModel
	}

	resp, err := c.createEmbedding(ctx, &embeddingPayload{
		Model: r.Model,
		Input: r.Input,
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, ErrEmptyResponse
	}

	embeddings := make([][]float32, 0)
	for i := 0; i < len(resp.Data); i++ {
		embeddings = append(embeddings, resp.Data[i].Embedding)
	}

	return embeddings, nil
}

// CreateChat creates chat request.
func (c *Client) CreateChat(ctx context.Context, r *ChatRequest) (*ChatCompletionResponse, error) {
	if r.Model == "" {
		if c.Model == "" {
			r.Model = defaultChatModel
		} else {
			r.Model = c.Model
		}
	}
	resp, err := c.createChat(ctx, r)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}
	return resp, nil
}

// CreateResponse creates a response using the Responses API.
func (c *Client) CreateResponse(ctx context.Context, r *responses.ResponseNewParams) (*responses.Response, error) {
	if r.Model == "" {
		if c.Model == "" {
			r.Model = DefaultChatModel
		} else {
			r.Model = c.Model
		}
	}
	if !r.MaxOutputTokens.Valid() {
		r.MaxOutputTokens = param.NewOpt(int64(DefaultMaxTokens))
	}
	return c.createResponse(ctx, r)
}

// CreateStreamingResponse creates a response using the Responses API with SSE streaming.
// streamFunc is called for each text delta chunk. The full Response (with usage stats) is returned
// once the response.completed event is received.
func (c *Client) CreateStreamingResponse(
	ctx context.Context,
	r *responses.ResponseNewParams,
	streamFunc func(ctx context.Context, chunk []byte) error,
) (*responses.Response, error) {
	if r.Model == "" {
		if c.Model == "" {
			r.Model = DefaultChatModel
		} else {
			r.Model = c.Model
		}
	}
	if !r.MaxOutputTokens.Valid() {
		r.MaxOutputTokens = param.NewOpt(int64(DefaultMaxTokens))
	}
	return c.createStreamingResponse(ctx, r, streamFunc)
}

func IsAzure(apiType ProviderType) bool {
	return apiType == ProviderAzure || apiType == ProviderAzureAD
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.Provider == ProviderOpenAI || c.Provider == ProviderAzure || c.Provider == ProviderAzureAD || c.Provider == "OPEN_AI" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else {
		req.Header.Set("api-key", c.token)
	}
	if c.organization != "" {
		req.Header.Set("OpenAI-Organization", c.organization)
	}
}

func (c *Client) buildURL(suffix string, model string) string {
	if IsAzure(c.Provider) {
		return c.buildAzureURL(suffix, model)
	}

	// open ai implement:
	return fmt.Sprintf("%s%s", c.baseURL, suffix)
}

func (c *Client) buildAzureURL(suffix string, model string) string {
	baseURL := c.baseURL
	baseURL = strings.TrimRight(baseURL, "/")

	if suffix == "/responses" {
		// for the new /responses API, Azure no longer nests it under /deployments/{deployment}.
		// Instead, you call the global /openai/responses endpoint and specify the model (deployment name) in the request body.
		return fmt.Sprintf("%s/openai/responses?api-version=%s",
			baseURL, c.apiVersion,
		)
	}

	// azure example url:
	// /openai/deployments/{model}/chat/completions?api-version={api_version}
	return fmt.Sprintf("%s/openai/deployments/%s%s?api-version=%s",
		baseURL, model, suffix, c.apiVersion,
	)
}

type errorMessage struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}
