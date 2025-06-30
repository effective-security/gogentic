package openai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai/internal/openaiclient"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredOutputObjectSchema(t *testing.T) {
	t.Parallel()

	type Input struct {
		FinalAnswer string `json:"final_answer" description:"The final answer to the question"`
	}
	responseFormat, err := schema.NewResponseFormat(reflect.TypeOf(Input{}), true)
	require.NoError(t, err)

	llm := newTestClient(
		t,
		WithModel("gpt-4o-2024-08-06"),
		WithResponseFormat(responseFormat),
	)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextContent{Text: "You are a student taking a math exam."}},
		},
		{
			Role:  llms.ChatMessageTypeGeneric,
			Parts: []llms.ContentPart{llms.TextContent{Text: "Solve 2 + 2"}},
		},
	}

	rsp, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "\"final_answer\":", strings.ToLower(c1.Content))
}

func TestStructuredOutputObjectAndArraySchema(t *testing.T) {
	t.Parallel()

	type Input struct {
		Steps       []string `json:"steps" description:"The steps to solve the problem"`
		FinalAnswer string   `json:"final_answer" description:"The final answer to the question"`
	}
	responseFormat, err := schema.NewResponseFormat(reflect.TypeOf(Input{}), true)
	require.NoError(t, err)

	llm := newTestClient(
		t,
		WithModel("gpt-4o-2024-08-06"),
		WithResponseFormat(responseFormat),
	)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextContent{Text: "You are a student taking a math exam."}},
		},
		{
			Role:  llms.ChatMessageTypeGeneric,
			Parts: []llms.ContentPart{llms.TextContent{Text: "Solve 2 + 2"}},
		},
	}

	rsp, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "\"steps\":", strings.ToLower(c1.Content))
}

func TestStructuredOutputFunctionCalling(t *testing.T) {
	t.Parallel()
	llm := newTestClient(
		t,
		WithModel("gpt-4o-2024-08-06"),
	)

	type Search struct {
		SearchEngine string `json:"search_engine" enum:"google,duckduckgo,bing"`
		SearchQuery  string `json:"search_query"`
	}
	sc, err := schema.New(reflect.TypeOf(Search{}))
	require.NoError(t, err)

	toolList := []llms.Tool{
		{
			Type: string(openaiclient.ToolTypeFunction),
			Function: &llms.FunctionDefinition{
				Name:        "search",
				Description: "Search by the web search engine",
				Parameters:  sc.Parameters,
				Strict:      true,
			},
		},
	}

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextContent{Text: "You are a helpful assistant"}},
		},
		{
			Role:  llms.ChatMessageTypeGeneric,
			Parts: []llms.ContentPart{llms.TextContent{Text: "What is the age of Bob Odenkirk, a famous comedy screenwriter and an actor."}},
		},
	}

	rsp, err := llm.GenerateContent(
		context.Background(),
		content,
		llms.WithTools(toolList),
	)
	require.NoError(t, err)

	assert.NotEmpty(t, rsp.Choices)
	c1 := rsp.Choices[0]
	assert.Regexp(t, "\"search_engine\":", c1.ToolCalls[0].FunctionCall.Arguments)
	assert.Regexp(t, "\"search_query\":", c1.ToolCalls[0].FunctionCall.Arguments)
}
