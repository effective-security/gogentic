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
