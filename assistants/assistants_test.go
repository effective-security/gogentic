package assistants_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	tavilyModels "github.com/diverged/tavily-go/models"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockassitants"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/mocks/mocktools"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/gogentic/store"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			return "", errors.New("empty query")
		}
		if strings.Contains(input, "error") {
			return "", errors.New("error")
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
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
			input := llmutils.FindLastUserQuestion(messages)
			if strings.Contains(input, "error") {
				return nil, errors.New("error")
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
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt, acfg...).
		WithTools(mockTool)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	sysPrompt, err := ag.GetSystemPrompt(ctx, "", nil)
	require.NoError(t, err)
	expPrompt := `You are helpful and friendly AI assistant.`

	assert.Equal(t, expPrompt, sysPrompt)

	var output chatmodel.OutputResult
	req := &assistants.CallInput{
		Input: "What is a capital of largest country in Europe?",
	}
	apiResp, err := ag.Run(ctx, req, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	req = &assistants.CallInput{
		Input: "Search for weather there.",
	}
	apiResp, err = ag.Run(ctx, req, &output)
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
			return "", errors.New("empty query")
		}
		if strings.Contains(input, "error") {
			return "", errors.New("error")
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
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
			input := llmutils.FindLastUserQuestion(messages)
			if strings.Contains(input, "error") {
				return nil, errors.New("error")
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

	req := &assistants.CallInput{
		Input: "What is a capital of largest country in Europe?",
	}
	apiResp, err := ag.Call(ctx, req)
	require.NoError(t, err)
	assert.NotEmpty(t, apiResp.Choices)

	req = &assistants.CallInput{
		Input: "Search for weather there.",
	}
	apiResp, err = ag.Call(ctx, req)
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
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
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
		assistants.WithMode(encoding.ModeJSONSchemaStrict),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt, acfg...).
		WithTools(mockTool)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	var output chatmodel.OutputResult
	req := &assistants.CallInput{
		Input: "Search for weather in Europe",
	}
	apiResp, err := ag.Run(ctx, req, &output)
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
	mockTool1.EXPECT().Parameters().Return(schema.MustFromAny(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type": "string",
			},
		},
	})).AnyTimes()
	mockTool1.EXPECT().Call(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, input string) (string, error) {
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		return `{"answer": "Result from tool 1"}`, nil
	}).AnyTimes()

	mockTool2 := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
	mockTool2.EXPECT().Name().Return("analyze_tool").AnyTimes()
	mockTool2.EXPECT().Description().Return("Analysis tool for testing").AnyTimes()
	mockTool2.EXPECT().Parameters().Return(schema.MustFromAny(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type": "string",
			},
		},
	})).AnyTimes()
	mockTool2.EXPECT().Call(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, input string) (string, error) {
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		return `{"answer": "Result from tool 2"}`, nil
	}).AnyTimes()

	// Create a mock LLM that returns multiple tool calls
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
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
	req := &assistants.CallInput{
		Input: "Run parallel tools",
	}
	apiResp, err := ag.Run(ctx, req, &output)
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
		mockTool.EXPECT().Parameters().Return(schema.MustFromAny(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type": "string",
				},
			},
		})).AnyTimes()

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
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
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
	req := &assistants.CallInput{
		Input: "Run multiple parallel tools",
	}
	apiResp, err := ag.Run(ctx, req, &output)
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

func Test_GetDescriptions(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock assistants
	mockAssistant1 := mockassitants.NewMockIAssistant(ctrl)
	mockAssistant1.EXPECT().Name().Return("Assistant1").Times(1)
	mockAssistant1.EXPECT().Description().Return("Description 1 with\nmultiple\nlines").Times(1)

	mockAssistant2 := mockassitants.NewMockIAssistant(ctrl)
	mockAssistant2.EXPECT().Name().Return("Assistant2").Times(1)
	mockAssistant2.EXPECT().Description().Return("Description 2").Times(1)

	// Test GetDescriptions
	desc := assistants.GetDescriptions(mockAssistant1, mockAssistant2)
	assert.Contains(t, desc, "Assistant1")
	assert.Contains(t, desc, "Assistant2")
	assert.Contains(t, desc, "Description 1 with multiple lines")
	assert.Contains(t, desc, "Description 2")
}

func Test_GetDescriptionsWithTools(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock tools
	mockTool1 := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
	mockTool1.EXPECT().Name().Return("Tool1").AnyTimes()
	mockTool1.EXPECT().Description().Return("Tool Description 1\nwith\nmultiple\nlines").AnyTimes()

	mockTool2 := mocktools.NewMockTool[tavily.SearchRequest, tavily.SearchResult](ctrl)
	mockTool2.EXPECT().Name().Return("Tool2").AnyTimes()
	mockTool2.EXPECT().Description().Return("Tool Description 2").AnyTimes()

	// Create mock assistant with tools
	mockAssistant := mockassitants.NewMockIAssistant(ctrl)
	mockAssistant.EXPECT().Name().Return("Assistant1").AnyTimes()
	mockAssistant.EXPECT().Description().Return("Assistant Description").AnyTimes()
	mockAssistant.EXPECT().GetTools().Return([]tools.ITool{mockTool1, mockTool2}).AnyTimes()

	// Test GetDescriptionsWithTools
	desc := assistants.GetDescriptionsWithTools(mockAssistant)
	assert.Contains(t, desc, "Assistant1")
	assert.Contains(t, desc, "Assistant Description")
	assert.Contains(t, desc, "Tool1")
	assert.Contains(t, desc, "Tool Description 1 with multiple lines")
	assert.Contains(t, desc, "Tool2")
	assert.Contains(t, desc, "Tool Description 2")
}

func Test_MapAssistants(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Test empty list
	result := assistants.MapAssistants()
	assert.Nil(t, result)

	// Create mock assistants
	mockAssistant1 := mockassitants.NewMockIAssistant(ctrl)
	mockAssistant1.EXPECT().Name().Return("Assistant1").AnyTimes()

	mockAssistant2 := mockassitants.NewMockIAssistant(ctrl)
	mockAssistant2.EXPECT().Name().Return("Assistant2").AnyTimes()

	// Test with assistants
	result = assistants.MapAssistants(mockAssistant1, mockAssistant2)
	assert.NotNil(t, result)
	assert.Equal(t, 2, len(result))
	assert.Equal(t, mockAssistant1, result["Assistant1"])
	assert.Equal(t, mockAssistant2, result["Assistant2"])
}

func Test_Run_WithCallback(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock assistant with callback
	mockAssistant := mockassitants.NewMockTypeableAssistant[chatmodel.OutputResult](ctrl)
	mockCallback := mockassitants.NewMockCallback(ctrl)
	mockAssistant.EXPECT().GetCallback().Return(mockCallback).AnyTimes()

	// Set up expectations
	mockCallback.EXPECT().OnAssistantStart(gomock.Any(), mockAssistant, "test input")
	mockCallback.EXPECT().OnAssistantEnd(gomock.Any(), mockAssistant, "test input", gomock.Any())

	// Mock successful Run
	mockAssistant.EXPECT().Run(gomock.Any(), &assistants.CallInput{
		Input: "test input",
	}, gomock.Any()).
		Return(&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					Content: `{"Content":"Test response"}`,
				},
			},
		}, nil)

	// Test Run
	var output chatmodel.OutputResult
	req := &assistants.CallInput{
		Input: "test input",
	}
	apiResp, err := assistants.Run(context.Background(), mockAssistant, req, &output)
	require.NoError(t, err)
	assert.NotNil(t, apiResp)
	assert.Equal(t, `{"Content":"Test response"}`, apiResp.Choices[0].Content)
}

func Test_Run_WithError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock assistant with callback
	mockAssistant := mockassitants.NewMockTypeableAssistant[chatmodel.OutputResult](ctrl)
	mockCallback := mockassitants.NewMockCallback(ctrl)
	mockAssistant.EXPECT().GetCallback().Return(mockCallback).AnyTimes()

	// Set up expectations
	mockCallback.EXPECT().OnAssistantStart(gomock.Any(), mockAssistant, "test input")
	mockCallback.EXPECT().OnAssistantError(gomock.Any(), mockAssistant, "test input", gomock.Any())

	// Mock error in Run
	expectedErr := errors.New("test error")
	mockAssistant.EXPECT().Run(gomock.Any(), &assistants.CallInput{
		Input: "test input",
	}, gomock.Any()).
		Return(nil, expectedErr)

	// Test Run with error
	var output chatmodel.OutputResult
	req := &assistants.CallInput{
		Input: "test input",
	}
	apiResp, err := assistants.Run(context.Background(), mockAssistant, req, &output)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, apiResp)
}

func Test_Call_WithCallback(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock assistant with callback
	mockAssistant := mockassitants.NewMockIAssistant(ctrl)
	mockCallback := mockassitants.NewMockCallback(ctrl)

	// Create a mock that implements both IAssistant and HasCallback
	mockAssistantWithCallback := struct {
		*mockassitants.MockIAssistant
		*mockassitants.MockHasCallback
	}{
		MockIAssistant:  mockAssistant,
		MockHasCallback: mockassitants.NewMockHasCallback(ctrl),
	}
	mockAssistantWithCallback.MockHasCallback.EXPECT().GetCallback().Return(mockCallback).AnyTimes()

	// Set up expectations
	mockCallback.EXPECT().OnAssistantStart(gomock.Any(), mockAssistantWithCallback, "test input")
	mockCallback.EXPECT().OnAssistantEnd(gomock.Any(), mockAssistantWithCallback, "test input", gomock.Any())

	// Mock successful Call
	mockAssistant.EXPECT().Call(gomock.Any(), &assistants.CallInput{
		Input: "test input",
	}).
		Return(&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					Content: `{"Content":"Test response"}`,
				},
			},
		}, nil)

	// Test Call
	req := &assistants.CallInput{
		Input: "test input",
	}
	apiResp, err := assistants.Call(context.Background(), mockAssistantWithCallback, req)
	require.NoError(t, err)
	assert.NotNil(t, apiResp)
	assert.Equal(t, `{"Content":"Test response"}`, apiResp.Choices[0].Content)
}

func Test_Call_WithError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock assistant with callback
	mockAssistant := mockassitants.NewMockIAssistant(ctrl)
	mockCallback := mockassitants.NewMockCallback(ctrl)

	// Create a mock that implements both IAssistant and HasCallback
	mockAssistantWithCallback := struct {
		*mockassitants.MockIAssistant
		*mockassitants.MockHasCallback
	}{
		MockIAssistant:  mockAssistant,
		MockHasCallback: mockassitants.NewMockHasCallback(ctrl),
	}
	mockAssistantWithCallback.MockHasCallback.EXPECT().GetCallback().Return(mockCallback).AnyTimes()

	// Set up expectations
	mockCallback.EXPECT().OnAssistantStart(gomock.Any(), mockAssistantWithCallback, "test input")
	mockCallback.EXPECT().OnAssistantError(gomock.Any(), mockAssistantWithCallback, "test input", gomock.Any())

	// Mock error in Call
	expectedErr := errors.New("test error")
	mockAssistant.EXPECT().Call(gomock.Any(), &assistants.CallInput{
		Input: "test input",
	}).
		Return(nil, expectedErr)

	// Test Call with error
	req := &assistants.CallInput{
		Input: "test input",
	}
	apiResp, err := assistants.Call(context.Background(), mockAssistantWithCallback, req)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, apiResp)
}
