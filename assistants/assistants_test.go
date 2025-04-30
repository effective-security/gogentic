package assistants_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tavilyModels "github.com/diverged/tavily-go/models"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/mocks/mocktools"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/effective-security/gogentic/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	"go.uber.org/mock/gomock"
)

func Test_Assistant(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSONSchema),
		assistants.WithJSONMode(true),
	}

	t.Setenv("TAVILY_API_KEY", "test-key")
	tavilyTool, err := tavily.New()
	require.NoError(t, err)

	mockTool := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
	mockTool.EXPECT().Name().Return(tavilyTool.Name()).AnyTimes()
	mockTool.EXPECT().Description().Return(tavilyTool.Description()).AnyTimes()
	mockTool.EXPECT().Parameters().Return(tavilyTool.Parameters()).AnyTimes()
	mockTool.EXPECT().Call(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, input string) (string, error) {
		if input == "" {
			return "", fmt.Errorf("empty query")
		}
		if strings.Contains(input, "error") {
			return "", fmt.Errorf("error")
		}
		if strings.Contains(input, "weather") {
			return utils.ToJSON(tavily.SearchResult{
				Results: []tavilyModels.SearchResult{
					{
						Title: "Weather in Europe",
						URL:   "https://weather.com/europe",
					},
					{
						Title: "Weather in France",
						URL:   "https://weather.com/france",
					},
				},
				Answer: "The weather in Europe is generally mild.",
			}), nil
		}
		if strings.Contains(input, "capital") {
			return utils.ToJSON(tavily.SearchResult{
				Results: []tavilyModels.SearchResult{
					{
						Title: "Capital of France",
						URL:   "https://france.com/capital",
					},
					{
						Title: "Capital of Germany",
						URL:   "https://germany.com/capital",
					},
				},
				Answer: "The capital of France is Paris.",
			}), nil
		}
		return utils.ToJSON(tavily.SearchResult{
			Results: []tavilyModels.SearchResult{
				{
					Title: "Search result 1",
					URL:   "https://example.com/1",
				},
				{
					Title: "Search result 2",
					URL:   "https://example.com/2",
				},
			},
			Answer: "This is a test answer.",
		}), nil
	}).AnyTimes()

	searchCalled := false
	// Create a mock LLM
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
			input := lastUserQuestion(messages)
			if strings.Contains(input, "error") {
				return nil, fmt.Errorf("error")
			}
			if !searchCalled && strings.Contains(input, "search") {
				searchCalled = true
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							ToolCalls: []llms.ToolCall{
								{
									ID:   tavilyTool.Name(),
									Type: "function",
									FunctionCall: &llms.FunctionCall{
										Name:      tavilyTool.Name(),
										Arguments: `{"Query":"Search for weather in Europe"}`,
									},
								},
							},
						},
					},
				}, nil
			}
			if strings.Contains(input, "weather") {
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							Content: `{"Content":"The weather in Europe is generally mild."}`,
						},
					},
				}, nil
			}
			if strings.Contains(input, "capital") {
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							Content: `{"Content":"The capital of France is Paris."}`,
						},
					},
				}, nil
			}
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: `{"Content":"This is a test answer."}`,
					},
				},
			}, nil
		}).AnyTimes()

	var buf strings.Builder
	ag := assistants.NewAssistant[chatmodel.Output](mockLLM, systemPrompt, acfg...).
		WithCallback(assistants.NewPrinterCallback(&buf)).
		WithTools(mockTool)

	ctx := chatmodel.WithChatContext(context.Background(), chatmodel.NewChatContext(chatmodel.NewChatID(), nil))

	var output chatmodel.Output
	apiResp, err := ag.Run(ctx, "What is a capital of largest country in Europe?", nil, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	apiResp, err = ag.Run(ctx, "Search for weather there.", nil, &output)
	require.NoError(t, err)

	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	history := ag.MessageHistory(ctx)
	assert.NotEmpty(t, history)
	fmt.Println(llms.GetBufferString(history, "Human", "AI"))
}

func lastUserQuestion(messages []llms.MessageContent) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == llms.ChatMessageTypeHuman {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					return strings.ToLower(textPart.Text)
				}
			}
		}
	}
	return ""
}
