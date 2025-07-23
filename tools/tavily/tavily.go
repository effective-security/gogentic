package tavily

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"

	"github.com/cockroachdb/errors"
	tavilygo "github.com/diverged/tavily-go"
	tavilyModels "github.com/diverged/tavily-go/models"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/mcp"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/gogentic/tools"
	"github.com/invopop/jsonschema"
)

const ToolName = "web_search"

var DefaultAPIKeyEnvName = "TAVILY_API_KEY"

// SearchRequest represents the tool input.
type SearchRequest struct {
	Query string `json:"Query" yaml:"Query" jsonschema:"title=Search Query,description=The query to search web."`
}

// SearchResult represents the structure for a search response
type SearchResult struct {
	Results []tavilyModels.SearchResult `json:"results" yaml:"Results" jsonschema:"title=Search Results,description=The results from a web pages."`
	Answer  string                      `json:"answer,omitempty" yaml:"Answer" jsonschema:"title=Final Answer,description=The aggregated answer from a web search."`
}

func (i *SearchResult) GetContent() string {
	return llmutils.ToJSON(i)
}

// Tool is a tool that provides a web search functionality
type Tool struct {
	name        string
	description string
	funcParams  *jsonschema.Schema
	apikey      string
	baseURL     string
	httpClient  *http.Client

	opts SearchOpts
}

// ensure WebSearchTool implements the llm.Function interface
var _ tools.Tool[SearchRequest, SearchResult] = (*Tool)(nil)
var _ tools.MCPTool[SearchRequest] = (*Tool)(nil)

// SearchOpts represents the options for a web search.
// See: https://docs.tavily.com/documentation/api-reference/endpoint/search
type SearchOpts struct {
	// Available options: basic, advanced
	// A basic search costs 1 API Credit, while an advanced search costs 2 API Credits.
	SearchDepth string `json:"search_depth,omitempty"`
	// Available options: general, news
	Topic string `json:"topic,omitempty"`
	// Include an LLM-generated answer to the provided query.
	// `basic` or `true` returns a quick answer.
	// `advanced` returns a more detailed answer.
	IncludeAnswer bool `json:"include_answer,omitempty"`
	// Required range: 0 <= x <= 20
	MaxResults int `json:"max_results,omitempty"`
	// A list of domains to specifically include in the search results.
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	UseCache       bool     `json:"use_cache,omitempty"`
	// IncludeImages  bool     `json:"include_images,omitempty"`
}

func New() (*Tool, error) {
	apikey := os.Getenv(DefaultAPIKeyEnvName)
	if apikey == "" {
		return nil, errors.Errorf("TAVILY_API_KEY is not set")
	}
	return NewWithAPIKey(apikey)
}

func NewWithAPIKey(apikey string) (*Tool, error) {
	sc, err := schema.New(reflect.TypeOf(SearchRequest{}))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create schema")
	}
	tool := &Tool{
		name:        ToolName,
		description: "A tool that provides a web search functionality.",
		apikey:      apikey,
		//baseURL:     "https://api.tavily.com/search",
		httpClient: http.DefaultClient,
		funcParams: sc.Parameters,
		opts: SearchOpts{
			SearchDepth:   "basic",
			IncludeAnswer: true,
			MaxResults:    5,
			UseCache:      true,
			Topic:         "general",
		},
	}
	return tool, nil
}

func (t *Tool) WithName(name string) *Tool {
	t.name = name
	return t
}

func (t *Tool) WithDescription(description string) *Tool {
	t.description = description
	return t
}

func (t *Tool) WithSearchOpts(opts SearchOpts) *Tool {
	t.opts = opts
	return t
}

func (t *Tool) WithBaseURL(baseURL string) *Tool {
	t.baseURL = baseURL
	return t
}

func (t *Tool) WithHTTPClient(client *http.Client) *Tool {
	t.httpClient = client
	return t
}

func (t *Tool) Name() string {
	return t.name
}

func (t *Tool) Description() string {
	return t.description
}

func (t *Tool) Parameters() *jsonschema.Schema {
	return t.funcParams
}

func (t *Tool) RegisterMCP(registrator tools.McpServerRegistrator) error {
	return registrator.RegisterTool(t.name, t.description, t.RunMCP)
}

func (t *Tool) RunMCP(ctx context.Context, req *SearchRequest) (*mcp.ToolResponse, error) {
	res, err := t.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResponse(mcp.NewTextContent(res.GetContent())), nil
}

func (t *Tool) Run(ctx context.Context, req *SearchRequest) (*SearchResult, error) {
	if req.Query == "" {
		return nil, errors.New("invalid request: empty query")
	}

	// Create a new Tavily client
	client := tavilygo.NewClient(t.apikey)
	if t.baseURL != "" {
		client.BaseURL = t.baseURL
	}
	// Set the HTTP client if provided
	if t.httpClient != nil {
		client.HTTPClient = t.httpClient
	}

	// Perform a search
	searchReq := tavilyModels.SearchRequest{
		Query:          req.Query,
		SearchDepth:    t.opts.SearchDepth,
		IncludeAnswer:  t.opts.IncludeAnswer,
		MaxResults:     t.opts.MaxResults,
		UseCache:       t.opts.UseCache,
		Topic:          t.opts.Topic,
		IncludeDomains: t.opts.IncludeDomains,
		ExcludeDomains: t.opts.ExcludeDomains,
	}

	searchResp, err := tavilygo.Search(client, searchReq)
	if err != nil {
		return nil, errors.Wrap(err, "failed to perform search")
	}

	res := &SearchResult{
		Results: searchResp.Results,
		Answer:  searchResp.Answer,
	}

	return res, nil
}

func (t *Tool) Call(ctx context.Context, input string) (string, error) {
	var req SearchRequest
	if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &req); err != nil {
		return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
	}
	out, err := t.Run(ctx, &req)
	if err != nil {
		return "", err
	}
	return out.GetContent(), nil
}

func (r *SearchResult) String() string {
	var buf bytes.Buffer
	if r.Answer != "" {
		fmt.Fprintf(&buf, "ANSWER: %s\n", r.Answer)
	}

	for _, result := range r.Results {
		fmt.Fprintf(&buf, "- URL: %s\n", result.URL)
		fmt.Fprintf(&buf, "  TITLE: %s\n", result.Title)
		fmt.Fprintf(&buf, "  SCORE: %f\n", result.Score)
		fmt.Fprintf(&buf, "  CONTENT: %s\n", result.Content)
	}

	return buf.String()
}
