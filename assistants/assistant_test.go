package assistants_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/mcp"
	"github.com/effective-security/gogentic/mocks/mockllmfactory"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/mocks/mocktools"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func Test_Assistant_BuilderMethods(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	mockFactory := mockllmfactory.NewMockFactory(ctrl)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetName().Return("gpt-4o").Times(1)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)

	// Test WithOutputParser
	outputParser, err := encoding.NewTypedOutputParser(chatmodel.OutputResult{}, encoding.ModeJSON)
	require.NoError(t, err)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockFactory, systemPrompt, assistants.WithModel(mockLLM))
	assistant = assistant.WithOutputParser(outputParser)
	assert.NotNil(t, assistant)

	// Test WithInputParser
	inputParser := func(input string) (string, error) {
		return "parsed: " + input, nil
	}
	assistant.WithInputParser(inputParser)
	assert.NotNil(t, assistant)

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
	mockTool.EXPECT().Parameters().Return(nil).AnyTimes()

	assistant = assistant.WithTools(mockTool)
	tools = assistant.GetTools()
	assert.Len(t, tools, 1)
	assert.Equal(t, "test_tool", tools[0].Name())

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

	mockFactory := mockllmfactory.NewMockFactory(ctrl)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()

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

	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockFactory, systemPrompt, assistants.WithModel(mockLLM))

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
	mockFactory := mockllmfactory.NewMockFactory(ctrl)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(2)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockFactory, systemPrompt, assistants.WithModel(mockLLM))

	// Simulate onPrompt error
	onPromptErr := func(ctx context.Context, input string) (map[string]any, error) {
		return nil, assert.AnError
	}
	assistant.WithPromptInputProvider(onPromptErr)
	_, err := assistant.GetSystemPrompt(context.Background(), "input", nil)
	assert.Error(t, err)

	// Simulate FormatPrompt error
	badPrompt := prompts.NewPromptTemplate("{{missing}}", []string{"input"})
	assistant = assistants.NewAssistant[chatmodel.OutputResult](mockFactory, badPrompt, assistants.WithModel(mockLLM))
	_, err = assistant.GetSystemPrompt(context.Background(), "input", nil)
	assert.Error(t, err)
}

func Test_Assistant_RegisterMCP_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	mockFactory := mockllmfactory.NewMockFactory(ctrl)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)
	mockLLM.EXPECT().GetName().Return("gpt-4o").Times(1)
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockFactory, systemPrompt, assistants.WithModel(mockLLM))

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

	mockFactory := mockllmfactory.NewMockFactory(ctrl)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)
	mockLLM.EXPECT().GetName().Return("gpt-4o").AnyTimes()
	assistant := assistants.NewAssistant[chatmodel.OutputResult](mockFactory, systemPrompt, assistants.WithModel(mockLLM))

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

const (
	runAssistantName = "RunAssistant"
	runModelName     = "gpt-4o"
	runSystemPrompt  = "You are helpful."
	runUserInput     = "What is the capital of France?"
	runAnswer        = "Paris"
	runAnswerJSON    = `{"content":"Paris"}`
	runToolName      = "lookup"
	runToolArgs      = "{}"
	runToolResult    = "lookup result"
	runRetryInput    = "Return the response in JSON format as requested."
	runUnparsable    = "not json"

	// noCapsProvider is a provider type with no capabilities: no function
	// calling and no JSON schema support.
	noCapsProvider llms.ProviderType = "NO_CAPABILITIES"
)

var errRun = errors.New("boom")

func newRunCtx() context.Context {
	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	return chatmodel.WithChatContext(context.Background(), chatCtx)
}

func newRunModel(ctrl *gomock.Controller, provider llms.ProviderType) *mockllms.MockModel {
	m := mockllms.NewMockModel(ctrl)
	m.EXPECT().GetProviderType().Return(provider).AnyTimes()
	m.EXPECT().GetName().Return(runModelName).AnyTimes()
	return m
}

func newRunAssistant(model llms.Model, opts ...assistants.Option) *assistants.Assistant[chatmodel.OutputResult] {
	opts = append([]assistants.Option{assistants.WithModel(model)}, opts...)
	return assistants.NewAssistant[chatmodel.OutputResult](nil, prompts.NewPromptTemplate(runSystemPrompt, nil), opts...).
		WithName(runAssistantName)
}

func newLookupTool(ctrl *gomock.Controller) *mocktools.MockTool[any, any] {
	tool := mocktools.NewMockTool[any, any](ctrl)
	tool.EXPECT().Name().Return(runToolName).AnyTimes()
	tool.EXPECT().Description().Return("Looks things up.").AnyTimes()
	tool.EXPECT().Parameters().Return(nil).AnyTimes()
	return tool
}

func textResponse(contents ...string) *llms.ContentResponse {
	resp := &llms.ContentResponse{}
	for _, c := range contents {
		resp.Choices = append(resp.Choices, &llms.ContentChoice{
			Content: c,
		})
	}
	return resp
}

func toolCallResponse(calls ...llms.ToolCall) *llms.ContentResponse {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				ToolCalls: calls,
			},
		},
	}
}

func functionCall(id, name string) llms.ToolCall {
	return llms.ToolCall{
		ID:   id,
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      name,
			Arguments: runToolArgs,
		},
	}
}

// messageTexts renders messages as "role: part|part" for compact, exact
// assertions on the history.
func messageTexts(msgs []llms.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		parts := make([]string, 0, len(m.Parts))
		for _, p := range m.Parts {
			switch typ := p.(type) {
			case llms.ToolCallResponse:
				parts = append(parts, "response "+typ.ToolCallID+" "+typ.Name+": "+typ.Content)
			default:
				parts = append(parts, p.String())
			}
		}
		out = append(out, string(m.Role)+": "+strings.Join(parts, "|"))
	}
	return out
}

// isUnmarshalOutputErr matches errors caused by unparsable model output.
var isUnmarshalOutputErr = gomock.Cond(func(err error) bool {
	return errors.Is(err, chatmodel.ErrFailedUnmarshalOutput)
})

func Test_Assistant_WithTools_WebSearch(t *testing.T) {
	ctrl := gomock.NewController(t)

	webSearch := mocktools.NewMockTool[any, any](ctrl)
	webSearch.EXPECT().Name().Return("Web_Search").AnyTimes()

	a := assistants.NewAssistant[chatmodel.OutputResult](nil, prompts.NewPromptTemplate(runSystemPrompt, nil))
	a.WithTools(webSearch)

	// web_search is a provider-native tool: it is not callable locally and is
	// sent to the provider as a tool definition instead.
	assert.Empty(t, a.GetTools())
	assert.Equal(t, []llms.Tool{{Type: "web_search"}}, a.GetCallConfig().Tools)
}

func Test_Assistant_WithSkills_Empty(t *testing.T) {
	a := assistants.NewAssistant[chatmodel.OutputResult](nil, prompts.NewPromptTemplate(runSystemPrompt, nil))
	assert.Same(t, a, a.WithSkills(nil))
	assert.Nil(t, a.GetSkills())
	assert.Empty(t, a.GetTools())
}

func Test_Assistant_GetSystemPrompt_PromptInputs(t *testing.T) {
	sysprompt := prompts.NewPromptTemplate("Hello {{.name}} from {{.city}}", []string{"name", "city"})
	a := assistants.NewAssistant[chatmodel.String](nil, sysprompt,
		assistants.WithMode(encoding.ModePlainText),
		assistants.WithPromptInput(map[string]any{
			"city": "Paris",
		}),
	)

	var extra map[string]any
	var gotInput string
	a.WithPromptInputProvider(func(_ context.Context, input string) (map[string]any, error) {
		gotInput = input
		return extra, nil
	})

	// The provider returns nothing: the call inputs are used.
	prompt, err := a.GetSystemPrompt(context.Background(), runUserInput, map[string]any{"name": "Ann"})
	require.NoError(t, err)
	assert.Equal(t, "Hello Ann from Paris", prompt)
	assert.Equal(t, runUserInput, gotInput)

	// Provider values win over the call inputs.
	extra = map[string]any{"name": "Bob"}
	prompt, err = a.GetSystemPrompt(context.Background(), runUserInput, map[string]any{"name": "Ann"})
	require.NoError(t, err)
	assert.Equal(t, "Hello Bob from Paris", prompt)
}

func Test_Assistant_GetSystemPrompt_OutputSchema(t *testing.T) {
	ctrl := gomock.NewController(t)

	// Without a model there is no native response format, so the format
	// instructions are appended to the system prompt.
	a := assistants.NewAssistant[chatmodel.OutputResult](nil, prompts.NewPromptTemplate(runSystemPrompt, nil),
		assistants.WithMode(encoding.ModeJSON))
	instructions := strings.TrimRight(a.OutputParser.GetFormatInstructions(), "\n")
	require.NotEmpty(t, instructions)

	prompt, err := a.GetSystemPrompt(context.Background(), runUserInput, nil)
	require.NoError(t, err)
	assert.Equal(t, runSystemPrompt+"\n\n# OUTPUT SCHEMA\n"+instructions, prompt)

	// A provider with JSON schema support gets a response format instead.
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	a = newRunAssistant(model, assistants.WithMode(encoding.ModeJSONSchema))
	require.NotNil(t, a.GetCallConfig().ResponseFormat)
	prompt, err = a.GetSystemPrompt(context.Background(), runUserInput, nil)
	require.NoError(t, err)
	assert.Equal(t, runSystemPrompt, prompt)
}

func Test_Assistant_GetSystemPrompt_SkillsProviderError(t *testing.T) {
	a := assistants.NewAssistant[chatmodel.String](nil, prompts.NewPromptTemplate(runSystemPrompt, nil),
		assistants.WithMode(encoding.ModePlainText)).
		WithSkills(skills.Skills{
			{
				Name:        "triage",
				Description: "Triage vulnerabilities.",
			},
		}).
		WithSkillsPromptProvider(func(context.Context, skills.Skills) (string, error) {
			return "", errRun
		})

	prompt, err := a.GetSystemPrompt(context.Background(), runUserInput, nil)
	assert.Empty(t, prompt)
	assert.EqualError(t, err, "failed to get skills prompt: boom")
	assert.True(t, errors.Is(err, errRun))
}

// capturingMcpRegistrator records the prompt handler an assistant registers.
type capturingMcpRegistrator struct {
	name        string
	description string
	handler     any
}

func (r *capturingMcpRegistrator) RegisterPrompt(name, description string, handler any) error {
	r.name = name
	r.description = description
	r.handler = handler
	return nil
}

func Test_Assistant_RegisterMCP_Handler(t *testing.T) {
	ctrl := gomock.NewController(t)
	model := newRunModel(ctrl, llms.ProviderOpenAI)
	model.EXPECT().GenerateContent(gomock.Any(), gomock.Any(), gomock.Any()).Return(textResponse(runAnswer), nil).Times(1)

	a := newRunAssistant(model).WithDescription("Answers questions.")
	reg := &capturingMcpRegistrator{}
	require.NoError(t, a.RegisterMCP(reg))
	assert.Equal(t, runAssistantName, reg.name)
	assert.Equal(t, "Answers questions.", reg.description)

	handler, ok := reg.handler.(func(context.Context, chatmodel.MCPInputRequest) (*mcp.PromptResponse, error))
	require.True(t, ok, "handler type %T", reg.handler)

	resp, err := handler(newRunCtx(), chatmodel.MCPInputRequest{Input: runUserInput})
	require.NoError(t, err)
	require.NotNil(t, resp.Description)
	assert.Equal(t, "Answers questions.", *resp.Description)
	require.Len(t, resp.Messages, 1)
	assert.Equal(t, runAnswer, resp.Messages[0].Content.TextContent.Text)
}
