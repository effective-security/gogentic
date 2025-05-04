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

	tavilygo "github.com/diverged/tavily-go"
	tavilyModels "github.com/diverged/tavily-go/models"
	"github.com/effective-security/gogentic/schema"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/gogentic/utils"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/pkg/errors"
	"github.com/tmc/langchaingo/llms"
)

const ToolName = "WebSearch"

var DefaultAPIKeyEnvName = "TAVILY_API_KEY"

// SearchRequest represents the tool input.
type SearchRequest struct {
	Query string `json:"Query" yaml:"Query" jsonschema:"title=Search Query,description=The query to search web."`
}

// SearchResult represents the structure for a search response
type SearchResult struct {
	Results []tavilyModels.SearchResult `json:"results" yaml:"Results" jsonschema:"title=Search Results,description=The results from a web search."`
	Answer  string                      `json:"answer,omitempty" yaml:"Results" jsonschema:"title=Final Answer,description=The aggregated answer from a web search."`
}

func (i *SearchResult) GetType() llms.ChatMessageType {
	return llms.ChatMessageTypeAI
}

func (i *SearchResult) GetContent() string {
	return utils.ToJSON(i)
}

// Tool is a tool that provides a web search functionality
type Tool struct {
	name        string
	description string
	funcParams  any
	apikey      string
	baseURL     string
	httpClient  *http.Client
}

// ensure WebSearchTool implements the llm.Function interface
var _ tools.Tool[SearchRequest, SearchResult] = (*Tool)(nil)
var _ tools.MCPTool[SearchRequest] = (*Tool)(nil)

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
	}
	return tool, nil
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

func (t *Tool) Parameters() any {
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
		Query:         req.Query,
		SearchDepth:   "basic",
		IncludeAnswer: true,
		// TODO: Option for
		//IncludeDomains:
		//ExcludeDomains: ,
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
	if err := json.Unmarshal(utils.CleanJSON([]byte(input)), &req); err != nil {
		return "", errors.Wrap(err, "failed to unmarshal input")
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
