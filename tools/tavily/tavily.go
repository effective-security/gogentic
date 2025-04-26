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
	"github.com/pkg/errors"
	"github.com/tmc/langchaingo/llms"
)

const ToolName = "WebSearch"

// SearchRequest represents the tool input.
type SearchRequest struct {
	Query string `json:"Query" yaml:"Query" jsonschema:"title=Query,description=The query to search web."`
}

// SearchResult represents the structure for a search response
type SearchResult struct {
	Results []tavilyModels.SearchResult `json:"results" yaml:"Results" jsonschema:"title=results,description=The results from a web search."`
	Answer  string                      `json:"answer,omitempty" yaml:"Results" jsonschema:"title=answer,description=The aggregated answer from a web search."`
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

	baseURL    string
	httpClient *http.Client
}

// ensure WebSearchTool implements the llm.Function interface
var _ tools.Tool[SearchRequest, SearchResult] = (*Tool)(nil)

func New() (*Tool, error) {
	apikey := os.Getenv("TAVILY_API_KEY")
	if apikey == "" {
		return nil, errors.Errorf("TAVILY_API_KEY is not set")
	}

	sc, err := schema.New(reflect.TypeOf(SearchResult{}))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create schema")
	}
	tool := &Tool{
		name:        ToolName,
		description: "A tool that provides a web search functionality.",
		//baseURL:     "https://api.tavily.com/search",
		httpClient: http.DefaultClient,
		funcParams: sc.Functions[0].Parameters,
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
	schema, _ := schema.New(reflect.TypeOf(SearchRequest{}))
	return schema.Functions[0].Parameters
}

func (t *Tool) Run(ctx context.Context, req *SearchRequest) (*SearchResult, error) {
	if req.Query == "" {
		return nil, errors.New("invalid request: empty query")
	}

	apikey := os.Getenv("TAVILY_API_KEY")
	if apikey == "" {
		return nil, errors.Errorf("TAVILY_API_KEY is not set")
	}

	// Create a new Tavily client
	client := tavilygo.NewClient(apikey)
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
	bs, err := json.Marshal(out)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal output")
	}
	return string(bs), nil
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
