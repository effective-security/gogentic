package assistants

import (
	"context"
	"fmt"
	"strings"

	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/model"
	"github.com/effective-security/gogentic/store"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/gogentic/utils"
	"github.com/pkg/errors"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
)

//go:generate mockgen -destination=../mocks/mockllms/llm_mock.gen.go -package mockllms github.com/tmc/langchaingo/llms  Model
//go:generate mockgen -source=assistants.go -destination=../mocks/mockassitants/assistants_mock.gen.go  -package mockassitants

type IAssistant interface {
	// Name returns the name of the Agent.
	Name() string
	// Description returns the description of the Agent, to be used in the prompt of other Agents or LLMs.
	// Should not exceed LLM model limit.
	Description() string
}

type TypeableAssistant[O model.ContentProvider] interface {
	IAssistant
	Run(ctx context.Context, input string, promptInputs map[string]any, optionalOutputType *O) (*llms.ContentResponse, error)
}

type Callback interface {
	tools.Callback
	OnAssistantStart(ctx context.Context, agent IAssistant, input string)
	OnAssistantEnd(ctx context.Context, agent IAssistant, input string, resp *llms.ContentResponse)
	OnAssistantError(cyx context.Context, agent IAssistant, input string, err error)
}

// Assistant class for chat assistants.
// This class provides the core functionality for handling chat interactions, including managing memory,
// generating system prompts, and obtaining responses from a language model.
type Assistant[O model.ContentProvider] struct {
	Config
	Store        store.MessageStore
	LLM          llms.Model
	OutputParser model.OutputParser[O]

	tools       map[string]tools.ITool
	llmToolDefs []llms.Tool

	name        string
	description string
	sysprompt   prompts.FormatPrompter
	callback    Callback
}

var (
	_ TypeableAssistant[*model.Output] = (*Assistant[*model.Output])(nil)
)

// NewAssistant initializes the AgentAgent
func NewAssistant[O model.ContentProvider](
	llmModel llms.Model,
	sysprompt prompts.FormatPrompter,
	options ...Option) *Assistant[O] {
	ret := &Assistant[O]{
		Config:      *NewConfig(options...),
		Store:       store.NewMemoryStore(),
		LLM:         llmModel,
		sysprompt:   sysprompt,
		name:        "Assistant",
		description: "An AI assistant that can perform various tasks.",
	}

	var output O
	ret.OutputParser, _ = encoding.NewTypedOutputParser(output, ret.Config.Mode)

	return ret
}

// WithCallback sets the callback.
func (a *Assistant[O]) WithOutputParser(outputParser model.OutputParser[O]) *Assistant[O] {
	a.OutputParser = outputParser
	return a
}

// WithCallback sets the callback.
func (a *Assistant[O]) WithCallback(cb Callback) *Assistant[O] {
	a.callback = cb
	return a
}

// MessageStore sets the messages store for the Assistant.
func (a *Assistant[O]) WithMessageStore(store store.MessageStore) *Assistant[O] {
	a.Store = store
	return a
}

// WithName sets the name of the Agent, when used in a prompt of another Agents or LLMs.
func (a *Assistant[O]) WithName(name string) *Assistant[O] {
	a.name = name
	return a
}

// WithDescription sets the description of the Agent, to be used in the prompt of other Agents or LLMs.
func (a *Assistant[O]) WithDescription(description string) *Assistant[O] {
	a.description = description
	return a
}

// Name returns the name of the Agent.
func (a *Assistant[O]) Name() string {
	return a.name
}

// Description returns the description of the Agent, to be used in the prompt of other Agents or LLMs.
// Should not exceed LLM model limit.
func (a *Assistant[O]) Description() string {
	return a.description
}

func (a *Assistant[O]) WithTools(list ...tools.ITool) *Assistant[O] {
	if a.tools == nil {
		a.tools = make(map[string]tools.ITool)
	}
	for _, tool := range list {
		name := tool.Name()
		if a.tools[name] == nil {
			a.tools[strings.ToUpper(name)] = tool

			t := llms.Tool{
				Type: "function",
				Function: &llms.FunctionDefinition{
					Name:        name,
					Description: tool.Description(),
					Parameters:  tool.Parameters(),
				},
			}
			a.llmToolDefs = append(a.llmToolDefs, t)
		}
	}

	return a
}

func (a *Assistant[O]) MessageHistory(ctx context.Context) []llms.ChatMessage {
	if a.Store == nil {
		return nil
	}
	chatCtx := model.GetChatContext(ctx)
	if chatCtx == nil {
		return nil
	}
	chatID := chatCtx.ChatID()
	return a.Store.Messages(chatID)
}

// Run runs the chat agent with the given user input synchronously.
func (a *Assistant[O]) Run(ctx context.Context, input string, promptInputs map[string]any, optionalOutputType *O) (*llms.ContentResponse, error) {
	if a.callback != nil {
		a.callback.OnAssistantStart(ctx, a, input)
	}

	apiResp, err := a.chat(ctx, input, promptInputs, optionalOutputType)
	if err != nil {
		if a.callback != nil {
			a.callback.OnAssistantError(ctx, a, input, err)
		}
		return nil, err
	}

	if a.callback != nil {
		a.callback.OnAssistantEnd(ctx, a, input, apiResp)
	}
	return apiResp, nil
}

// Response obtains a response from the language model synchronously
func (a *Assistant[O]) chat(ctx context.Context, input string, promptInputs map[string]any, optionalOutputType *O) (*llms.ContentResponse, error) {
	chatCtx := model.GetChatContext(ctx)
	if chatCtx == nil {
		return nil, errors.New("invalid context")
	}
	chatID := chatCtx.ChatID()

	promptValue, err := a.sysprompt.FormatPrompt(utils.MergeInputs(a.PromptInput, promptInputs))
	if err != nil {
		return nil, err
	}

	systemPrompt := promptValue.String()

	if optionalOutputType != nil {
		outputSchema := a.OutputParser.GetFormatInstructions()
		if outputSchema != "" {
			systemPrompt = fmt.Sprintf("%s\n\n#OUTPUT SCHEMA\n%s\n", systemPrompt, outputSchema)
		}
	}
	messageHistory := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}
	for _, example := range a.Examples {
		messageHistory = append(messageHistory, llms.TextParts(llms.ChatMessageTypeHuman, example.Prompt))
		messageHistory = append(messageHistory, llms.TextParts(llms.ChatMessageTypeAI, example.Completion))
	}
	if a.Store != nil {
		for _, msg := range a.Store.Messages(chatID) {
			messageHistory = append(messageHistory, llms.TextParts(msg.GetType(), msg.GetContent()))
		}
	}
	var userMessage llms.MessageContent
	if input != "" {
		userMessage = llms.TextParts(llms.ChatMessageTypeHuman, input)
		messageHistory = append(messageHistory, userMessage)
	}

	callOpts := a.Config.GetCallOptions()
	if len(a.llmToolDefs) > 0 {
		callOpts = append(callOpts, llms.WithTools(a.llmToolDefs))
	}

	var toolExecuted bool
	var resp *llms.ContentResponse

	for {
		resp, err = a.LLM.GenerateContent(ctx, messageHistory, callOpts...)
		if err != nil {
			return nil, err
		}

		// Perform Tool call
		toolExecuted, messageHistory, err = a.executeToolCalls(ctx, messageHistory, resp)
		if err != nil {
			return nil, err
		}

		if !toolExecuted {
			break
		}
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return nil, errors.New("empty response from LLM")
	}
	result := choices[0].Content

	if optionalOutputType != nil {
		finalOutput, err := a.OutputParser.ParseWithPrompt(result, promptValue)
		if err != nil {
			return nil, err
		}
		*optionalOutputType = *finalOutput

		if prov, ok := (any)(finalOutput).(model.ContentProvider); ok {
			result = prov.GetContent()
		}
	}

	if a.Store != nil {
		_ = a.Store.Add(chatID, &llms.HumanChatMessage{Content: input})
		_ = a.Store.Add(chatID, &llms.AIChatMessage{Content: result})
	}

	return resp, nil
}

// executeToolCalls executes the tool calls in the response and returns the
// updated message history.
func (a *Assistant[O]) executeToolCalls(ctx context.Context, messageHistory []llms.MessageContent, resp *llms.ContentResponse) (bool, []llms.MessageContent, error) {
	executed := false
	for _, choice := range resp.Choices {
		for _, toolCall := range choice.ToolCalls {
			executed = true

			toolName := toolCall.FunctionCall.Name
			toolArgs := toolCall.FunctionCall.Arguments

			// Append tool_use to messageHistory
			assistantResponse := llms.MessageContent{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   toolCall.ID,
						Type: toolCall.Type,
						FunctionCall: &llms.FunctionCall{
							Name:      toolName,
							Arguments: toolArgs,
						},
					},
				},
			}
			messageHistory = append(messageHistory, assistantResponse)

			tool := a.tools[strings.ToUpper(toolCall.FunctionCall.Name)]
			if tool == nil {
				return false, nil, errors.Errorf("tool %s not found", toolName)
			}

			if a.callback != nil {
				a.callback.OnToolStart(ctx, tool, toolArgs)
			}

			res, err := tool.Call(ctx, toolArgs)
			if err != nil {
				if a.callback != nil {
					a.callback.OnToolError(ctx, tool, toolArgs, err)
				}
				return false, nil, errors.WithMessagef(err, "failed to call tool %s", toolName)
			}

			if a.callback != nil {
				a.callback.OnToolEnd(ctx, tool, toolArgs, res)
			}
			// Append tool_result to messageHistory
			weatherCallResponse := llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: toolCall.ID,
						Name:       toolName,
						Content:    res,
					},
				},
			}
			messageHistory = append(messageHistory, weatherCallResponse)
		}
	}
	return executed, messageHistory, nil
}
