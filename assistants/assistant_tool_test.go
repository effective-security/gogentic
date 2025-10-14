package assistants_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
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
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type testInput struct {
	Content *string `json:"content" jsonschema:"required"`
}

func (t testInput) GetContent() string {
	return *t.Content
}

type testOutput struct {
	Content string `json:"Content"`
}

func (t testOutput) GetContent() string {
	return t.Content
}

type mockToolRegistrator struct {
	registered bool
}

func (m *mockToolRegistrator) RegisterTool(name, description string, handler any) error {
	m.registered = true
	return nil
}

func Test_AssistantTool(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	calls := 0
	// Create a mock LLM
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(3)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) {
			calls++
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: fmt.Sprintf("This is a test answer %d.", calls),
					},
				},
			}, nil
		}).Times(2)

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModePlainText),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	ag := assistants.NewAssistant[chatmodel.String](mockLLM, systemPrompt, acfg...)

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

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	exp := `Human: What is a capital of largest country in Europe?
AI: This is a test answer 1.
`
	buf.Reset()
	llmutils.PrintMessages(&buf, history)
	chat := buf.String()
	assert.Equal(t, exp, chat)

	tool, err := assistants.NewAssistantTool[chatmodel.InputRequest](ag)
	require.NoError(t, err)
	assert.Equal(t, "Generic Assistant", tool.Name())
	assert.Equal(t, ag.Description(), tool.Description())
	exp = `{
	"properties": {
		"input": {
			"type": "string",
			"title": "Input",
			"description": "The message sent by the user to the assistant."
		}
	},
	"type": "object",
	"required": [
		"input"
	]
}`
	assert.Equal(t, exp, llmutils.ToJSONIndent(tool.Parameters()))

	_, err = tool.CallAssistant(ctx, "plain string", assistants.WithMessageStore(memstore))
	assert.True(t, errors.Is(err, chatmodel.ErrFailedUnmarshalInput))
	assert.EqualError(t, err, "failed to unmarshal input: check the schema and try again")

	input := llmutils.ToJSONIndent(&chatmodel.InputRequest{
		Input: "What is a capital of largest country in Europe?",
	})

	tres, err := tool.CallAssistant(ctx, input, assistants.WithMessageStore(memstore))
	require.NoError(t, err)
	assert.Equal(t, "This is a test answer 2.", tres)
}

func Test_AssistantTool_BuilderMethods(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)
	assistant := assistants.NewAssistant[testOutput](mockLLM, systemPrompt)

	// Test WithName and WithDescription
	tool, err := assistants.NewAssistantTool[testInput](assistant)
	require.NoError(t, err)

	// Test Name and Description
	assert.Equal(t, assistant.Name(), tool.Name())
	assert.Equal(t, assistant.Description(), tool.Description())

	// Test Parameters
	params := tool.Parameters()
	assert.NotNil(t, params)

	exp := `{
	"properties": {
		"content": {
			"type": "string"
		}
	},
	"type": "object",
	"required": [
		"content"
	]
}`
	assert.Equal(t, exp, llmutils.ToJSONIndent(params))

}

func Test_AssistantTool_Call(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(2)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					Content: `{"content":"Test response"}`,
				},
			},
		}, nil,
	).AnyTimes()

	assistant := assistants.NewAssistant[testOutput](mockLLM, systemPrompt)
	tool, err := assistants.NewAssistantTool[testInput](assistant)
	require.NoError(t, err)

	// Add valid chat context
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	result, err := tool.Call(ctx, `{"content":"test input"}`)
	require.NoError(t, err)
	assert.Equal(t, "Test response", result)
}

func Test_AssistantTool_CallAssistant(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(3)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	// First call - success case
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					Content: `{"content":"Test response"}`,
				},
			},
		}, nil,
	).Times(1)

	// Second call - error case
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		nil, errors.New("test error"),
	).Times(1)

	assistant := assistants.NewAssistant[testOutput](mockLLM, systemPrompt)
	tool, err := assistants.NewAssistantTool[testInput](assistant)
	require.NoError(t, err)

	// Add valid chat context
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	// Test successful call
	result, err := tool.CallAssistant(ctx, `{"content":"test input"}`)
	require.NoError(t, err)
	assert.Equal(t, "Test response", result)

	// Test error case
	_, err = tool.CallAssistant(ctx, `{"content":"test input"}`)
	assert.Error(t, err)
}

func Test_AssistantTool_MCPMethods(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					Content: `{"content":"Test response"}`,
				},
			},
		}, nil,
	).AnyTimes()

	assistant := assistants.NewAssistant[testOutput](mockLLM, systemPrompt)
	tool, err := assistants.NewAssistantTool[testInput](assistant)
	require.NoError(t, err)

	// Test RegisterMCP
	registrator := &mockToolRegistrator{}
	err = tool.RegisterMCP(registrator)
	assert.NoError(t, err)
	assert.True(t, registrator.registered)
}

func Test_AssistantTool_WithName(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAssistant := mockassitants.NewMockTypeableAssistant[chatmodel.OutputResult](ctrl)
	mockAssistant.EXPECT().Name().Return("test-assistant").AnyTimes()
	mockAssistant.EXPECT().Description().Return("test description").AnyTimes()

	tool, err := assistants.NewAssistantTool[chatmodel.OutputResult](mockAssistant)
	require.NoError(t, err)

	// Test WithName
	at := tool.(*assistants.AssistantTool[chatmodel.OutputResult, chatmodel.OutputResult])
	at = at.WithName("test-tool")
	assert.Equal(t, "test-tool", at.Name())
}

func Test_AssistantTool_WithDescription(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAssistant := mockassitants.NewMockTypeableAssistant[chatmodel.OutputResult](ctrl)
	mockAssistant.EXPECT().Name().Return("test-assistant").AnyTimes()
	mockAssistant.EXPECT().Description().Return("test description").AnyTimes()

	tool, err := assistants.NewAssistantTool[chatmodel.OutputResult](mockAssistant)
	require.NoError(t, err)

	// Test WithDescription
	at := tool.(*assistants.AssistantTool[chatmodel.OutputResult, chatmodel.OutputResult])
	desc := "This is a test tool description"
	at = at.WithDescription(desc)
	assert.Equal(t, desc, at.Description())
}

func Test_AssistantTool_RunMCP(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAssistant := mockassitants.NewMockTypeableAssistant[chatmodel.OutputResult](ctrl)
	mockAssistant.EXPECT().Name().Return("test-assistant").AnyTimes()
	mockAssistant.EXPECT().Description().Return("test description").AnyTimes()

	tool, err := assistants.NewAssistantTool[chatmodel.InputRequest](mockAssistant)
	require.NoError(t, err)

	// Test RunMCP
	ctx := context.Background()
	input := &chatmodel.InputRequest{
		Input: "test input",
	}

	// Mock successful Run
	mockAssistant.EXPECT().Run(gomock.Any(), &assistants.CallInput{
		Input: "test input",
	}, gomock.Any()).DoAndReturn(
		func(ctx context.Context, input *assistants.CallInput, optionalOutputType *chatmodel.OutputResult) (*llms.ContentResponse, error) {
			if optionalOutputType != nil {
				*optionalOutputType = chatmodel.OutputResult{
					Content: "test response",
				}
			}
			return &llms.ContentResponse{
				Choices: []*llms.ContentChoice{
					{
						Content: `{"Content":"test response"}`,
					},
				},
			}, nil
		}).Times(1)

	at := tool.(*assistants.AssistantTool[chatmodel.InputRequest, chatmodel.OutputResult])
	resp, err := at.RunMCP(ctx, input)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test response", resp.Content[0].TextContent.Text)

	// Test error case
	expectedErr := errors.New("test error")
	mockAssistant.EXPECT().Run(gomock.Any(), &assistants.CallInput{
		Input: "test input",
	}, gomock.Any()).
		Return(nil, expectedErr)

	resp, err = at.RunMCP(ctx, input)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, resp)
}

func Test_Assistant_ToolCallIDMapping(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	// Create mock tools
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
		time.Sleep(50 * time.Millisecond)
		return `{"answer": "Result from search tool"}`, nil
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
		time.Sleep(100 * time.Millisecond) // Longer processing time
		return `{"answer": "Result from analyze tool"}`, nil
	}).AnyTimes()

	// Create a mock LLM that returns multiple tool calls with specific IDs
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) {
			// First call: return multiple tool calls with specific IDs
			if len(messages) == 2 {
				toolCalls := []llms.ToolCall{
					{
						ID:   "call_NAktZKJ3MIME00B4IHvUpNcr", // Specific ID that was causing issues
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "search_tool",
							Arguments: `{"query":"test query 1"}`,
						},
					},
					{
						ID:   "call_XYZ123456789", // Another specific ID
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "analyze_tool",
							Arguments: `{"query":"test query 2"}`,
						},
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
						Content: `{"Content":"Final response after tool calls"}`,
					},
				},
			}, nil
		}).Times(2)

	// Create assistant with both tools
	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt).
		WithTools(mockTool1, mockTool2)

	// Create chat context
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	// Run the assistant
	var output chatmodel.OutputResult
	req := &assistants.CallInput{
		Input: "Run tools with specific IDs",
	}
	apiResp, err := ag.Run(ctx, req, &output)

	// Verify results
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	// Verify the final response
	assert.Contains(t, output.Content, "Final response after tool calls")

	// Verify that the message history contains the correct tool call responses
	// The assistant should have added tool call responses with the correct IDs
	runMessages := ag.LastRunMessages()

	// Find tool call responses in the message history
	toolCallCount := 0
	for _, msg := range runMessages {
		if msg.Role == llms.RoleTool {
			toolCallCount++
			// Verify that the tool call response has the correct structure
			require.Len(t, msg.Parts, 1)
			toolCallResponse, ok := msg.Parts[0].(llms.ToolCallResponse)
			require.True(t, ok, "Message part should be ToolCallResponse")

			// Verify that the tool call ID is one of the expected IDs
			expectedIDs := []string{"call_NAktZKJ3MIME00B4IHvUpNcr", "call_XYZ123456789"}
			assert.Contains(t, expectedIDs, toolCallResponse.ToolCallID,
				"Tool call response should have correct ID")

			// Verify that the tool name matches
			expectedNames := []string{"search_tool", "analyze_tool"}
			assert.Contains(t, expectedNames, toolCallResponse.Name,
				"Tool call response should have correct tool name")
		}
	}

	// Verify that we have exactly 2 tool call responses
	assert.Equal(t, 2, toolCallCount, "Should have exactly 2 tool call responses")
}

func Test_Assistant_ToolCallMessageStructure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	// Create mock tools
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
		return `{"answer": "Result from search tool"}`, nil
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
		return `{"answer": "Result from analyze tool"}`, nil
	}).AnyTimes()

	// Create a mock LLM that returns multiple tool calls in a single choice
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).AnyTimes()
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) {
			// First call: return multiple tool calls in a single choice
			if len(messages) == 2 {
				toolCalls := []llms.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "search_tool",
							Arguments: `{"query":"test query 1"}`,
						},
					},
					{
						ID:   "call_2",
						Type: "function",
						FunctionCall: &llms.FunctionCall{
							Name:      "analyze_tool",
							Arguments: `{"query":"test query 2"}`,
						},
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
						Content: `{"Content":"Final response after tool calls"}`,
					},
				},
			}, nil
		}).Times(2)

	// Create assistant with both tools
	ag := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt).
		WithTools(mockTool1, mockTool2)

	// Create chat context
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	// Run the assistant
	var output chatmodel.OutputResult
	req := &assistants.CallInput{
		Input: "Run tools with grouped message structure",
	}
	apiResp, err := ag.Run(ctx, req, &output)

	// Verify results
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	// Verify the final response
	assert.Contains(t, output.Content, "Final response after tool calls")

	// Verify that the message history contains the correct structure
	runMessages := ag.LastRunMessages()

	// Find the assistant message with tool calls
	var assistantMessageWithTools *llms.Message
	for i, msg := range runMessages {
		if msg.Role == llms.RoleAI && len(msg.Parts) > 0 {
			// Check if this message contains tool calls
			hasToolCalls := false
			for _, part := range msg.Parts {
				if _, ok := part.(llms.ToolCall); ok {
					hasToolCalls = true
					break
				}
			}
			if hasToolCalls {
				assistantMessageWithTools = &runMessages[i]
				break
			}
		}
	}

	require.NotNil(t, assistantMessageWithTools, "Should find assistant message with tool calls")

	// Verify that all tool calls are in a single assistant message
	assert.Equal(t, llms.RoleAI, assistantMessageWithTools.Role)
	assert.Equal(t, 2, len(assistantMessageWithTools.Parts), "Should have exactly 2 tool calls in one message")

	// Verify the tool calls have the correct IDs
	toolCallIDs := make([]string, 0, 2)
	for _, part := range assistantMessageWithTools.Parts {
		if toolCall, ok := part.(llms.ToolCall); ok {
			toolCallIDs = append(toolCallIDs, toolCall.ID)
		}
	}

	expectedIDs := []string{"call_1", "call_2"}
	assert.ElementsMatch(t, expectedIDs, toolCallIDs, "Tool call IDs should match expected values")

	// Verify that tool responses come after the assistant message
	toolResponseCount := 0
	for _, msg := range runMessages {
		if msg.Role == llms.RoleTool {
			toolResponseCount++
		}
	}
	assert.Equal(t, 2, toolResponseCount, "Should have exactly 2 tool responses")
}
