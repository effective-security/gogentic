package assistants_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mocks/mockassitants"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/store"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
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
	mockLLM.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
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
		assistants.WithJSONMode(false),
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
AI: This is a test answer 1.`
	chat, err := llms.GetBufferString(history, "Human", "AI")
	require.NoError(t, err)
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
	assistant := assistants.NewAssistant[testOutput](mockLLM, systemPrompt)

	// Test WithName and WithDescription
	tool, err := assistants.NewAssistantTool[testInput, testOutput](assistant)
	require.NoError(t, err)

	// Test Name and Description
	assert.Equal(t, assistant.Name(), tool.Name())
	assert.Equal(t, assistant.Description(), tool.Description())

	// Test Parameters
	params := tool.Parameters()
	if schema, ok := params.(*jsonschema.Schema); ok {
		fmt.Printf("DEBUG: schema properties: %#v\n", schema.Properties)
	}
	assert.NotNil(t, params)
}

func Test_AssistantTool_Call(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})
	mockLLM := mockllms.NewMockModel(ctrl)
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
	tool, err := assistants.NewAssistantTool[testInput, testOutput](assistant)
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
		nil, assert.AnError,
	).Times(1)

	assistant := assistants.NewAssistant[testOutput](mockLLM, systemPrompt)
	tool, err := assistants.NewAssistantTool[testInput, testOutput](assistant)
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
	tool, err := assistants.NewAssistantTool[testInput, testOutput](assistant)
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

	tool, err := assistants.NewAssistantTool[chatmodel.OutputResult, chatmodel.OutputResult](mockAssistant)
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

	tool, err := assistants.NewAssistantTool[chatmodel.OutputResult, chatmodel.OutputResult](mockAssistant)
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
	expectedErr := fmt.Errorf("test error")
	mockAssistant.EXPECT().Run(gomock.Any(), &assistants.CallInput{
		Input: "test input",
	}, gomock.Any()).
		Return(nil, expectedErr)

	resp, err = at.RunMCP(ctx, input)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, resp)
}
