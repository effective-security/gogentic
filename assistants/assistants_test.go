package assistants_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tavilyModels "github.com/diverged/tavily-go/models"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockassitants"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/mocks/mocktools"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/store"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
	"go.uber.org/mock/gomock"
)

func Test_Assistant_Defined(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.\n", []string{})

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
			return llmutils.ToJSON(tavily.SearchResult{
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
			return llmutils.ToJSON(tavily.SearchResult{
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
		return llmutils.ToJSON(tavily.SearchResult{
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
			input := llmutils.FindLastUserQuestion(messages)
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

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSONSchema),
		assistants.WithJSONMode(true),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt, acfg...).
		WithTools(mockTool)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	sysPrompt, err := ag.GetSystemPrompt(ctx, "", nil)
	require.NoError(t, err)
	expPrompt := `You are helpful and friendly AI assistant.

# OUTPUT SCHEMA

Respond with JSON in the following JSON schema:` +
		"\n```json" + `
{
	"properties": {
		"content": {
			"type": "string",
			"title": "Response Content",
			"description": "The content returned by agent or tool."
		}
	},
	"type": "object",
	"required": [
		"content"
	]
}
` + "```" + `
Make sure to return an instance of the JSON, not the schema itself.
Use the exact field names as they are defined in the schema.`

	assert.Equal(t, expPrompt, sysPrompt)

	var output chatmodel.OutputResult
	apiResp, err := ag.Run(ctx, "What is a capital of largest country in Europe?", nil, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	apiResp, err = ag.Run(ctx, "Search for weather there.", nil, &output)
	require.NoError(t, err)

	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	exp := `Human: What is a capital of largest country in Europe?
AI: The capital of France is Paris.
Human: Search for weather there.
AI: The weather in Europe is generally mild.`
	chat, err := llms.GetBufferString(history, "Human", "AI")
	require.NoError(t, err)
	assert.Equal(t, exp, chat)
}

func Test_Assistant_Chat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

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
			return llmutils.ToJSON(tavily.SearchResult{
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
			return llmutils.ToJSON(tavily.SearchResult{
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
		return llmutils.ToJSON(tavily.SearchResult{
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
			input := llmutils.FindLastUserQuestion(messages)
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
							Content: `"The weather in Europe is generally mild."`,
						},
					},
				}, nil
			}
			if strings.Contains(input, "capital") {
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							Content: `"The capital of France is Paris."`,
						},
					},
				}, nil
			}
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: `"This is a test answer."`,
					},
				},
			}, nil
		}).AnyTimes()

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModePlainText),
		assistants.WithJSONMode(false),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	ag := assistants.NewAssistant[chatmodel.String](mockLLM, systemPrompt, acfg...).
		WithTools(mockTool)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	sysPrompt, err := ag.GetSystemPrompt(ctx, "", nil)
	require.NoError(t, err)
	expPrompt := `You are helpful and friendly AI assistant.`
	assert.Equal(t, expPrompt, sysPrompt)

	apiResp, err := ag.Call(ctx, "What is a capital of largest country in Europe?", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, apiResp.Choices)

	apiResp, err = ag.Call(ctx, "Search for weather there.", nil)
	require.NoError(t, err)

	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	exp := `Human: What is a capital of largest country in Europe?
AI: The capital of France is Paris.
Human: Search for weather there.
AI: The weather in Europe is generally mild.`
	chat, err := llms.GetBufferString(history, "Human", "AI")
	require.NoError(t, err)
	assert.Equal(t, exp, chat)
}

func Test_Assistant_FailtedParseToolInput(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	t.Setenv("TAVILY_API_KEY", "test-key")
	tavilyTool, err := tavily.New()
	require.NoError(t, err)

	// This will simulate a tool that fails to unmarshal input the first time, then succeeds
	callCount := 0
	mockTool := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
	mockTool.EXPECT().Name().Return(tavilyTool.Name()).AnyTimes()
	mockTool.EXPECT().Description().Return(tavilyTool.Description()).AnyTimes()
	mockTool.EXPECT().Parameters().Return(tavilyTool.Parameters()).AnyTimes()
	mockTool.EXPECT().Call(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, input string) (string, error) {
		callCount++
		if callCount == 1 {
			// Simulate failed unmarshal
			return "", chatmodel.ErrFailedUnmarshalInput
		}
		// On retry, succeed
		return llmutils.ToJSON(tavily.SearchResult{
			Results: []tavilyModels.SearchResult{
				{
					Title: "Weather in Europe",
					URL:   "https://weather.com/europe",
				},
			},
			Answer: "The weather in Europe is generally mild.",
		}), nil
	}).Times(2)

	// LLM mock: first returns a tool call with invalid input, then with valid input, then the final answer
	llmCall := 0
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
			llmCall++
			if llmCall == 1 {
				// First, LLM issues a tool call with invalid input
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							ToolCalls: []llms.ToolCall{
								{
									ID:   "tavily-search",
									Type: "function",
									FunctionCall: &llms.FunctionCall{
										Name:      tavilyTool.Name(),
										Arguments: `not a json`,
									},
								},
							},
						},
					},
				}, nil
			}
			if llmCall == 2 {
				// After error, LLM retries with valid JSON input
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							ToolCalls: []llms.ToolCall{
								{
									ID:   "tavily-search",
									Type: "function",
									FunctionCall: &llms.FunctionCall{
										Name:      tavilyTool.Name(),
										Arguments: `{"Query":"weather in Europe"}`,
									},
								},
							},
						},
					},
				}, nil
			}
			// Final, LLM returns the answer
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: `{"Content":"The weather in Europe is generally mild."}`,
					},
				},
			}, nil
		}).Times(3)

	memstore := store.NewMemoryStore()
	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSONSchema),
		assistants.WithJSONMode(true),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt, acfg...).
		WithTools(mockTool)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	var output chatmodel.OutputResult
	apiResp, err := ag.Run(ctx, "Search for weather in Europe", nil, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)
	assert.Contains(t, output.Content, "weather")

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	chat, err := llms.GetBufferString(history, "Human", "AI")
	require.NoError(t, err)
	// The final answer should be present
	assert.Contains(t, chat, "The weather in Europe is generally mild.")

	// // The error message should be present in the chat history
	// assert.Contains(t, chat, "Failed to unmarshal input, check the JSON schema and try again.")
}

func Test_Assistant_ParallelToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	// Create two mock tools
	mockTool1 := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
	mockTool1.EXPECT().Name().Return("search_tool").AnyTimes()
	mockTool1.EXPECT().Description().Return("Search tool for testing").AnyTimes()
	mockTool1.EXPECT().Parameters().Return(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type": "string",
			},
		},
	}).AnyTimes()
	mockTool1.EXPECT().Call(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, input string) (string, error) {
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		return `{"answer": "Result from tool 1"}`, nil
	}).AnyTimes()

	mockTool2 := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
	mockTool2.EXPECT().Name().Return("analyze_tool").AnyTimes()
	mockTool2.EXPECT().Description().Return("Analysis tool for testing").AnyTimes()
	mockTool2.EXPECT().Parameters().Return(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type": "string",
			},
		},
	}).AnyTimes()
	mockTool2.EXPECT().Call(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, input string) (string, error) {
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		return `{"answer": "Result from tool 2"}`, nil
	}).AnyTimes()

	// Create a mock LLM that returns multiple tool calls
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
			// First call: return multiple tool calls
			if len(messages) == 2 {
				toolCalls := make([]llms.ToolCall, 2)
				toolCalls[0] = llms.ToolCall{
					ID:   "search_tool",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "search_tool",
						Arguments: `{"query":"test query 1"}`,
					},
				}
				toolCalls[1] = llms.ToolCall{
					ID:   "analyze_tool",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "analyze_tool",
						Arguments: `{"query":"test query 2"}`,
					},
				}
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							ToolCalls: toolCalls,
						},
					},
				}, nil
			}
			// Second call: return final response
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: `{"Content":"Final response after parallel tool calls"}`,
					},
				},
			}, nil
		}).Times(2) // Expect exactly 2 calls: one for tool calls, one for final response

	// Create assistant with both tools
	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt).
		WithTools(mockTool1, mockTool2)

	// Create chat context
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	// Run the assistant
	var output chatmodel.OutputResult
	start := time.Now()
	apiResp, err := ag.Run(ctx, "Run parallel tools", nil, &output)
	duration := time.Since(start)

	// Verify results
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	// Verify that the execution took less than 300ms (indicating parallel execution)
	// If tools were executed sequentially, it would take at least 200ms (100ms per tool)
	assert.Less(t, duration, 300*time.Millisecond, "Tools should execute in parallel")

	// Verify the final response
	assert.Contains(t, output.Content, "Final response after parallel tool calls")
}

func Test_Assistant_MultipleParallelToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	// Create multiple mock tools with different processing times
	mockTools := make([]tools.ITool, 7)
	toolNames := []string{"search", "analyze", "summarize", "translate", "classify", "extract", "validate"}

	// Track tool call IDs and their results
	toolCallResults := make(map[string]string)
	processedToolCalls := make(map[string]bool)
	var mu sync.Mutex // Add mutex to protect map access

	for i := 0; i < 7; i++ {
		mockTool := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
		toolName := toolNames[i] + "_tool"
		mockTool.EXPECT().Name().Return(toolName).AnyTimes()
		mockTool.EXPECT().Description().Return(toolNames[i] + " tool for testing").AnyTimes()
		mockTool.EXPECT().Parameters().Return(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type": "string",
				},
			},
		}).AnyTimes()

		// Each tool has a different processing time to better demonstrate parallelism
		processingTime := time.Duration(50*(i+1)) * time.Millisecond
		mockTool.EXPECT().Call(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, input string) (string, error) {
			time.Sleep(processingTime)
			result := fmt.Sprintf(`{"answer": "Result from %s tool"}`, toolNames[i])
			// Store the result with the tool name for later verification
			mu.Lock()
			toolCallResults[strings.ToLower(toolName)] = result
			processedToolCalls[strings.ToLower(toolName)] = true
			mu.Unlock()
			return result, nil
		}).AnyTimes() // Allow any number of calls since we're testing parallel execution

		mockTools[i] = mockTool
	}

	// Create a mock LLM that returns multiple tool calls
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
			// First call: return multiple tool calls
			if len(messages) == 2 {
				toolCalls := make([]llms.ToolCall, 7)
				for i := 0; i < 7; i++ {
					toolName := toolNames[i] + "_tool"
					toolCalls[i] = llms.ToolCall{
						ID:   fmt.Sprintf("call_%d", i+1),
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      toolName,
							Arguments: fmt.Sprintf(`{"query":"test query %d"}`, i+1),
						},
					}
				}
				return &llms.ContentResponse{
					Choices: []*llms.ContentChoice{
						{
							ToolCalls: toolCalls,
						},
					},
				}, nil
			}
			// Second call: return final response
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: `{"Content":"Final response after multiple parallel tool calls"}`,
					},
				},
			}, nil
		}).AnyTimes() // Allow any number of calls since we're testing parallel execution

	// Create a mock callback to track tool calls
	mockCallback := mockassitants.NewMockCallback(ctrl)
	mockCallback.EXPECT().OnToolStart(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockCallback.EXPECT().OnToolEnd(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, tool tools.ITool, input string, output string) {
			mu.Lock()
			processedToolCalls[strings.ToLower(tool.Name())] = true
			toolCallResults[strings.ToLower(tool.Name())] = output
			mu.Unlock()
		},
	).AnyTimes()
	mockCallback.EXPECT().OnToolError(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockCallback.EXPECT().OnAssistantStart(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockCallback.EXPECT().OnAssistantEnd(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockCallback.EXPECT().OnAssistantError(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockCallback.EXPECT().OnAssistantLLMCall(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockCallback.EXPECT().OnAssistantLLMParseError(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockCallback.EXPECT().OnToolNotFound(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Create assistant with all tools and callback
	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt, assistants.WithCallback(mockCallback)).
		WithTools(mockTools...)

	// Create chat context
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	// Run the assistant
	var output chatmodel.OutputResult
	start := time.Now()
	apiResp, err := ag.Run(ctx, "Run multiple parallel tools", nil, &output)
	duration := time.Since(start)

	// Verify results
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	// Verify that the execution took less than the sum of all tool processing times
	// If tools were executed sequentially, it would take at least 350ms (50ms + 100ms + 150ms + 200ms + 250ms + 300ms + 350ms)
	// With parallel execution, it should take less than the longest tool's processing time plus some overhead
	assert.Less(t, duration, 400*time.Millisecond, "Tools should execute in parallel")

	// Verify the final response
	assert.Contains(t, output.Content, "Final response after multiple parallel tool calls")

	// Add debug logging to help diagnose the issue
	t.Logf("Processed tool calls: %v", processedToolCalls)
	t.Logf("Tool call results: %v", toolCallResults)

	// Verify that all tool calls were processed
	assert.Equal(t, 7, len(processedToolCalls), "All tool calls should have been processed")
	for _, toolName := range toolNames {
		expectedToolName := strings.ToLower(toolName + "_tool")
		assert.True(t, processedToolCalls[expectedToolName], "Tool %s should have been processed", expectedToolName)
	}

	// Verify that all tool call results were stored
	assert.Equal(t, 7, len(toolCallResults), "All tool call results should have been stored")
	for _, toolName := range toolNames {
		expectedToolName := strings.ToLower(toolName + "_tool")
		assert.Contains(t, toolCallResults, expectedToolName, "Tool %s result should have been stored", expectedToolName)
	}
}
