package tavily_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/cockroachdb/errors"
	tavilyModels "github.com/diverged/tavily-go/models"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/mcp"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Tool(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "testkey")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req tavilyModels.SearchRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)

		assert.Equal(t, "What is capital of France", req.Query)

		resp := tavily.SearchResult{
			Results: []tavilyModels.SearchResult{
				{Title: "Test Result", URL: "https://example.com", Content: "Test content", Score: 0.9},
			},
		}
		if req.IncludeAnswer {
			resp.Answer = "Paris"
		}

		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ctx := context.Background()

	tool, err := tavily.New()
	require.NoError(t, err)
	tool.WithBaseURL(server.URL).WithHTTPClient(server.Client())

	assert.Equal(t, tavily.ToolName, tool.Name())
	assert.Contains(t, tool.Description(), `web search`)

	params := llmutils.ToJSONIndent(tool.Parameters())
	expParams := `{
  "properties": {
    "Query": {
      "type": "string",
      "title": "Search Query",
      "description": "The query to search web."
    }
  },
  "type": "object",
  "required": [
    "Query"
  ]
}`

	assert.Equal(t, expParams, string(params))

	_, err = tool.Call(ctx, "plain string")
	assert.True(t, errors.Is(err, chatmodel.ErrFailedUnmarshalInput))
	assert.EqualError(t, err, "failed to unmarshal input: check the schema and try again")

	input := &tavily.SearchRequest{
		Query: "What is capital of France",
	}

	resp, err := tool.Run(ctx, input)
	require.NoError(t, err)
	exp := `ANSWER: Paris
- URL: https://example.com
  TITLE: Test Result
  SCORE: 0.900000
  CONTENT: Test content
`
	assert.Equal(t, exp, resp.String())

	resp2, err := tool.Call(ctx, llmutils.ToJSON(input))
	require.NoError(t, err)
	exp = `{"results":[{"title":"Test Result","url":"https://example.com","content":"Test content","score":0.9}],"answer":"Paris"}`
	assert.Equal(t, exp, resp2)
}

func Test_Tool_Real(t *testing.T) {
	// uncomment to run Real Tests
	t.Skip("skipping real test")

	apikey := os.Getenv("TAVILY_API_KEY")
	if apikey == "" {
		t.Skip("TAVILY_API_KEY is not set")
	}

	ctx := context.Background()

	tool, err := tavily.New()
	require.NoError(t, err)

	input := &tavily.SearchRequest{
		Query: "What is capital of France",
	}

	resp, err := tool.Call(ctx, llmutils.ToJSON(input))
	require.NoError(t, err)
	assert.Contains(t, resp, "Paris")
}

const (
	testAPIKey      = "testkey"
	testQuery       = "What is capital of France"
	testName        = "custom_search"
	testDescription = "Custom web search."
	errEmptyQuery   = "invalid request: empty query"
)

// fakeRegistrator captures what a tool registers with an MCP server.
type fakeRegistrator struct {
	name        string
	description string
	handler     any
}

func (r *fakeRegistrator) RegisterTool(name, description string, handler any) error {
	r.name = name
	r.description = description
	r.handler = handler
	return nil
}

// newSearchServer serves Tavily search requests, recording the last request.
func newSearchServer(t *testing.T, status int, last *tavilyModels.SearchRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if last != nil {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(last))
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("boom"))
			return
		}
		_ = json.NewEncoder(w).Encode(tavily.SearchResult{
			Results: []tavilyModels.SearchResult{
				{
					Title: "Paris",
					URL:   "https://example.com/paris",
				},
			},
			Answer: "Paris",
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func Test_New_MissingAPIKey(t *testing.T) {
	t.Setenv(tavily.DefaultAPIKeyEnvName, "")
	tool, err := tavily.New()
	assert.Nil(t, tool)
	assert.EqualError(t, err, "TAVILY_API_KEY is not set")
}

func Test_Tool_BuildersAndSearchOpts(t *testing.T) {
	var got tavilyModels.SearchRequest
	server := newSearchServer(t, http.StatusOK, &got)

	tool, err := tavily.NewWithAPIKey(testAPIKey)
	require.NoError(t, err)
	opts := tavily.SearchOpts{
		SearchDepth:    "advanced",
		Topic:          "news",
		MaxResults:     2,
		IncludeDomains: []string{"example.com"},
		ExcludeDomains: []string{"spam.com"},
	}
	tool.WithName(testName).
		WithDescription(testDescription).
		WithSearchOpts(opts).
		WithBaseURL(server.URL).
		WithHTTPClient(server.Client())

	assert.Equal(t, testName, tool.Name())
	assert.Equal(t, testDescription, tool.Description())

	_, err = tool.Run(context.Background(), &tavily.SearchRequest{Query: testQuery})
	require.NoError(t, err)

	// The configured options, not the defaults, reach the API.
	assert.Equal(t, tavilyModels.SearchRequest{
		Query:          testQuery,
		SearchDepth:    "advanced",
		Topic:          "news",
		MaxResults:     2,
		IncludeDomains: []string{"example.com"},
		ExcludeDomains: []string{"spam.com"},
		APIKey:         testAPIKey,
	}, got)
}

func Test_Tool_RegisterAndRunMCP(t *testing.T) {
	server := newSearchServer(t, http.StatusOK, nil)

	tool, err := tavily.NewWithAPIKey(testAPIKey)
	require.NoError(t, err)
	tool.WithBaseURL(server.URL).WithHTTPClient(server.Client())

	reg := &fakeRegistrator{}
	require.NoError(t, tool.RegisterMCP(reg))
	assert.Equal(t, tavily.ToolName, reg.name)
	assert.Equal(t, tool.Description(), reg.description)

	handler, ok := reg.handler.(func(context.Context, *tavily.SearchRequest) (*mcp.ToolResponse, error))
	require.True(t, ok, "handler type %T", reg.handler)

	resp, err := handler(context.Background(), &tavily.SearchRequest{Query: testQuery})
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	require.NotNil(t, resp.Content[0].TextContent)
	assert.Equal(t,
		`{"results":[{"title":"Paris","url":"https://example.com/paris","content":"","score":0}],"answer":"Paris"}`,
		resp.Content[0].TextContent.Text)

	resp, err = tool.RunMCP(context.Background(), &tavily.SearchRequest{})
	assert.Nil(t, resp)
	assert.EqualError(t, err, errEmptyQuery)
}

func Test_Tool_Errors(t *testing.T) {
	server := newSearchServer(t, http.StatusInternalServerError, nil)

	tool, err := tavily.NewWithAPIKey(testAPIKey)
	require.NoError(t, err)
	tool.WithBaseURL(server.URL).WithHTTPClient(server.Client())

	ctx := context.Background()

	res, err := tool.Run(ctx, &tavily.SearchRequest{Query: testQuery})
	assert.Nil(t, res)
	assert.EqualError(t, err, "failed to perform search: API request failed with status code: 500, response: boom")

	out, err := tool.Call(ctx, `{"Query":""}`)
	assert.Empty(t, out)
	assert.EqualError(t, err, errEmptyQuery)
	assert.False(t, errors.Is(err, chatmodel.ErrFailedUnmarshalInput), "a valid but empty query is not an unmarshal error")
}
