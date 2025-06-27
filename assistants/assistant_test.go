package assistants_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/mocks/mocktools"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func Test_Assistant_BuilderMethods(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)

	// Test WithOutputParser
	outputParser, err := encoding.NewTypedOutputParser[chatmodel.OutputResult](chatmodel.OutputResult{}, encoding.ModeJSON)
	require.NoError(t, err)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt)
	assistant = assistant.WithOutputParser(outputParser)
	assert.NotNil(t, assistant)

	// Test WithInputParser
	inputParser := func(input string) (string, error) {
		return "parsed: " + input, nil
	}
	assistant.WithInputParser(inputParser)
	assert.NotNil(t, assistant)

	// Test GetCallback
	callback := assistant.GetCallback()
	assert.Nil(t, callback) // Should be nil by default

	// Test WithName
	assistant = assistant.WithName("TestAssistant")
	assert.Equal(t, "TestAssistant", assistant.Name())

	// Test WithDescription
	assistant = assistant.WithDescription("Test Description")
	assert.Equal(t, "Test Description", assistant.Description())

	// Test GetTools
	tools := assistant.GetTools()
	assert.Empty(t, tools) // Should be empty by default

	// Test WithTools
	mockTool := mocktools.NewMockTool[any, any](ctrl)
	mockTool.EXPECT().Name().Return("test_tool").AnyTimes()
	mockTool.EXPECT().Description().Return("Test tool description").AnyTimes()
	mockTool.EXPECT().Parameters().Return(map[string]any{}).AnyTimes()

	assistant = assistant.WithTools(mockTool)
	tools = assistant.GetTools()
	assert.Len(t, tools, 1)
	assert.Equal(t, "test_tool", tools[0].Name())

	// Test LastRunMessages
	messages := assistant.LastRunMessages()
	assert.Empty(t, messages) // Should be empty by default

	// Test GetPromptInputVariables
	variables := assistant.GetPromptInputVariables()
	assert.Empty(t, variables) // Should be empty for our test prompt

	// Test WithPromptInputProvider
	provider := func(ctx context.Context, input string) (map[string]any, error) {
		return map[string]any{"test": "value"}, nil
	}
	assistant.WithPromptInputProvider(provider)
	assert.NotNil(t, assistant)
}

func Test_Assistant_MCPMethods(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)

	// Setup mock LLM for CallMCP test
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&llms.ContentResponse{
			Choices: []*llms.ContentChoice{
				{
					Content: "Test response",
				},
			},
		}, nil,
	).AnyTimes()

	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt)

	// Test RegisterMCP
	registrator := &mockMcpRegistrator{}
	err := assistant.RegisterMCP(registrator)
	assert.NoError(t, err)
	assert.True(t, registrator.registered)

	// Test CallMCP
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
	input := chatmodel.MCPInputRequest{
		ChatID: chatCtx.GetChatID(),
		Input:  "test input",
	}

	resp, err := assistant.CallMCP(ctx, input)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Messages, 1)
	assert.Equal(t, "Test response", resp.Messages[0].Content.TextContent.Text)
}

// Mock MCP registrator for testing
type mockMcpRegistrator struct {
	registered bool
}

func (m *mockMcpRegistrator) RegisterPrompt(name, description string, handler any) error {
	m.registered = true
	return nil
}

func Test_Assistant_GetSystemPrompt_ErrorCases(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{"input"})
	mockLLM := mockllms.NewMockModel(ctrl)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt)

	// Simulate onPrompt error
	onPromptErr := func(ctx context.Context, input string) (map[string]any, error) {
		return nil, assert.AnError
	}
	assistant.WithPromptInputProvider(onPromptErr)
	_, err := assistant.GetSystemPrompt(context.Background(), "input", nil)
	assert.Error(t, err)

	// Simulate FormatPrompt error
	badPrompt := prompts.NewPromptTemplate("{{missing}}", []string{"input"})
	assistant = assistants.NewAssistant[chatmodel.OutputResult](mockLLM, badPrompt)
	_, err = assistant.GetSystemPrompt(context.Background(), "input", nil)
	assert.Error(t, err)
}

func Test_Assistant_RegisterMCP_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt)

	registrator := &mockMcpRegistratorError{}
	err := assistant.RegisterMCP(registrator)
	assert.Error(t, err)
}

type mockMcpRegistratorError struct{}

func (m *mockMcpRegistratorError) RegisterPrompt(name, description string, handler any) error {
	return assert.AnError
}

func Test_Assistant_CallMCP_ErrorCases(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt)

	// SetChatID error (no chat context)
	input := chatmodel.MCPInputRequest{ChatID: "id", Input: "input"}
	_, err := assistant.CallMCP(context.Background(), input)
	assert.Error(t, err)

	// Run error
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, assert.AnError).AnyTimes()
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
	input = chatmodel.MCPInputRequest{ChatID: chatCtx.GetChatID(), Input: "input"}
	_, err = assistant.CallMCP(ctx, input)
	assert.Error(t, err)
}

func Test_Assistant_Run_EdgeCases(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt)

	// LLM returns no choices
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{Choices: []*llms.ContentChoice{}}, nil).AnyTimes()
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
	_, err := assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	assert.Error(t, err)

	// OutputParser returns error
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "bad json"}}}, nil).AnyTimes()
	outputParser, _ := encoding.NewTypedOutputParser[chatmodel.OutputResult](chatmodel.OutputResult{}, encoding.ModeJSONSchema)
	assistant = assistant.WithOutputParser(outputParser)
	_, err = assistant.Run(ctx, &assistants.CallInput{Input: "input"}, new(chatmodel.OutputResult))
	assert.Error(t, err)
}

func Test_Assistant_Run_ToolCallEdgeCases(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockLLM, systemPrompt)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	// Tool not found case
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID: "1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "not_found", Arguments: "{}"},
			}},
		}},
	}, nil).Times(1)
	// Final response after tool error
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content: "I apologize, but I couldn't find the requested tool.",
		}},
	}, nil).Times(1)
	_, err := assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	assert.NoError(t, err)

	// Tool returns error case
	mockTool := mocktools.NewMockTool[any, any](ctrl)
	mockTool.EXPECT().Name().Return("err_tool").Times(1)
	mockTool.EXPECT().Description().Return("desc").Times(1)
	mockTool.EXPECT().Parameters().Return(map[string]any{}).Times(1)
	mockTool.EXPECT().Call(gomock.Any(), gomock.Any()).Return("", assert.AnError).Times(1)
	assistant = assistant.WithTools(mockTool)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID: "2", Type: "function", FunctionCall: &llms.FunctionCall{Name: "err_tool", Arguments: "{}"},
			}},
		}},
	}, nil).Times(1)
	// Final response after tool error
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content: "I encountered an error while trying to use the tool.",
		}},
	}, nil).Times(1)
	_, err = assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	assert.NoError(t, err)

	// Tool returns success case
	mockTool = mocktools.NewMockTool[any, any](ctrl)
	mockTool.EXPECT().Name().Return("success_tool").Times(1)
	mockTool.EXPECT().Description().Return("desc").Times(1)
	mockTool.EXPECT().Parameters().Return(map[string]any{}).Times(1)
	mockTool.EXPECT().Call(gomock.Any(), gomock.Any()).Return("tool result", nil).Times(1)
	assistant = assistant.WithTools(mockTool)
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID: "3", Type: "function", FunctionCall: &llms.FunctionCall{Name: "success_tool", Arguments: "{}"},
			}},
		}},
	}, nil).Times(1)
	// Final response after successful tool execution
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(&llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content: "Based on the tool result, here is my response.",
		}},
	}, nil).Times(1)
	_, err = assistant.Run(ctx, &assistants.CallInput{Input: "input"}, nil)
	assert.NoError(t, err)
}
