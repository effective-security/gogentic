package assistants

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/llmutils"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/x/slices"
	"github.com/effective-security/xlog"
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
)

// Assistant class for chat assistants.
// This class provides the core functionality for handling chat interactions, including managing memory,
// generating system prompts, and obtaining responses from a language model.
type Assistant[O chatmodel.ContentProvider] struct {
	LLM          llms.Model
	OutputParser chatmodel.OutputParser[O]

	toolsByName map[string]tools.ITool
	toolsNames  []string
	tools       []tools.ITool
	llmToolDefs []llms.Tool

	cfg         *Config
	name        string
	description string
	sysprompt   prompts.FormatPrompter
	runMessages []llms.MessageContent
	onPrompt    ProvidePromptInputsFunc
	inputParser func(string) (string, error)
}

var (
	_ TypeableAssistant[chatmodel.OutputResult] = (*Assistant[chatmodel.OutputResult])(nil)
	_ IMCPAssistant                             = (*Assistant[chatmodel.OutputResult])(nil)
)

// NewAssistant initializes the AgentAgent
func NewAssistant[O chatmodel.ContentProvider](
	llmModel llms.Model,
	sysprompt prompts.FormatPrompter,
	options ...Option) *Assistant[O] {
	ret := &Assistant[O]{
		cfg: NewConfig(options...),
		// By default no store is used.
		//Store:       store.NewMemoryStore(),
		LLM:         llmModel,
		sysprompt:   sysprompt,
		name:        "Generic Assistant",
		description: "An AI assistant that can perform various tasks.",
	}

	var output O
	ret.OutputParser, _ = encoding.NewTypedOutputParser(output, ret.cfg.Mode)

	return ret
}

// WithCallback sets the callback.
func (a *Assistant[O]) WithOutputParser(outputParser chatmodel.OutputParser[O]) *Assistant[O] {
	a.OutputParser = outputParser
	return a
}

// WithInputParser sets the input parser for the Assistant.
func (a *Assistant[O]) WithInputParser(inputParser func(string) (string, error)) {
	a.inputParser = inputParser
}

func (a *Assistant[O]) GetCallback() Callback {
	return a.cfg.CallbackHandler
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

func (a *Assistant[O]) GetTools() []tools.ITool {
	return a.tools
}

// WithTools adds new tools to the Assistant,
// existing tools are not replaced.
func (a *Assistant[O]) WithTools(list ...tools.ITool) *Assistant[O] {
	if a.toolsByName == nil {
		a.toolsByName = make(map[string]tools.ITool)
	}
	for _, tool := range list {
		name := tool.Name()
		// use lowercase for the key
		nameLowerCase := strings.ToLower(name)
		if a.toolsByName[nameLowerCase] == nil {
			a.toolsByName[nameLowerCase] = tool
			a.toolsNames = append(a.toolsNames, name)
			a.tools = append(a.tools, tool)
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

func (a *Assistant[O]) LastRunMessages() []llms.MessageContent {
	return a.runMessages
}

func (a *Assistant[O]) FormatPrompt(promptInputs map[string]any) (llms.PromptValue, error) {
	return a.sysprompt.FormatPrompt(llmutils.MergeInputs(a.cfg.PromptInput, promptInputs))
}

func (a *Assistant[O]) GetPromptInputVariables() []string {
	return a.sysprompt.GetInputVariables()
}

func (a *Assistant[O]) WithPromptInputProvider(cb ProvidePromptInputsFunc) {
	a.onPrompt = cb
}

// GetSystemPrompt generates the system prompt for the Assistant.
func (a *Assistant[O]) GetSystemPrompt(input string, promptInputs map[string]any) (string, error) {
	if a.onPrompt != nil {
		extra, err := a.onPrompt(input)
		if err != nil {
			return "", errors.WithMessage(err, "failed to get prompt inputs")
		}
		if len(extra) > 0 {
			promptInputs = llmutils.MergeInputs(promptInputs, extra)
		}
	}

	promptValue, err := a.FormatPrompt(promptInputs)
	if err != nil {
		return "", err
	}

	// Convert the prompt value to a string.
	systemPrompt := strings.TrimRight(promptValue.String(), "\n") // Ensure no trailing newline.
	// Get the output schema instructions and trim any trailing newlines.
	outputSchema := strings.TrimRight(a.OutputParser.GetFormatInstructions(), "\n")
	if outputSchema != "" {
		// Append the output schema to the system prompt with a separating newline.
		systemPrompt = fmt.Sprintf("%s\n\n# OUTPUT SCHEMA\n%s", systemPrompt, outputSchema)
	}
	return systemPrompt, nil
}

func (a *Assistant[O]) RegisterMCP(registrator McpServerRegistrator) error {
	return registrator.RegisterPrompt(a.Name(), a.Description(), func(ctx context.Context, input chatmodel.MCPInputRequest) (*mcp.PromptResponse, error) {
		return a.CallMCP(ctx, input)
	})
}

func (a *Assistant[O]) CallMCP(ctx context.Context, input chatmodel.MCPInputRequest) (*mcp.PromptResponse, error) {
	ctx, err := chatmodel.SetChatID(ctx, input.ChatID)
	if err != nil {
		return nil, err
	}

	resp, err := a.Run(ctx, input.Input, nil, nil)
	if err != nil {
		return nil, err
	}

	var messages []*mcp.PromptMessage
	for _, choice := range resp.Choices {
		messages = append(messages, mcp.NewPromptMessage(mcp.NewTextContent(choice.Content), mcp.RoleAssistant))
	}

	mcpres := mcp.NewPromptResponse(a.Description(), messages...)
	return mcpres, nil
}

func (a *Assistant[O]) Call(ctx context.Context, input string, promptInputs map[string]any, options ...Option) (*llms.ContentResponse, error) {
	var output O
	return a.Run(ctx, input, promptInputs, &output, options...)
}

// Run runs the chat agent with the given user input synchronously.
func (a *Assistant[O]) Run(ctx context.Context, input string, promptInputs map[string]any, optionalOutputType *O, options ...Option) (*llms.ContentResponse, error) {
	chatID, _, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, errors.WithStack(chatmodel.ErrInvalidChatContext)
	}

	// create a per call config
	cfg := a.cfg.Apply(options...)

	systemPrompt, err := a.GetSystemPrompt(input, promptInputs)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to format system prompt")
	}

	messageHistory := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}
	for _, example := range cfg.Examples {
		messageHistory = append(messageHistory, llms.TextParts(llms.ChatMessageTypeHuman, example.Prompt))
		messageHistory = append(messageHistory, llms.TextParts(llms.ChatMessageTypeAI, example.Completion))
	}
	if cfg.Store != nil {
		prevMessages := cfg.Store.Messages(ctx)
		logger.ContextKV(ctx, xlog.DEBUG,
			"assistant", a.name,
			"chat_id", chatID,
			"message_history", len(prevMessages))
		for _, msg := range prevMessages {
			messageHistory = append(messageHistory, llms.TextParts(msg.GetType(), msg.GetContent()))
		}
	}
	var userMessage llms.MessageContent
	if input != "" {
		if a.inputParser != nil {
			input, err = a.inputParser(input)
			if err != nil {
				return nil, errors.WithMessage(err, "failed to parse input")
			}
		}

		role := llms.ChatMessageTypeHuman
		if !cfg.IsGeneric && cfg.Store != nil {
			_ = cfg.Store.Add(ctx, &llms.HumanChatMessage{Content: input})
		}
		// else {
		// 	// TODO: keep as Human?
		// 	// 	role = llms.ChatMessageTypeGeneric
		// }
		userMessage = llms.TextParts(role, input)
		messageHistory = append(messageHistory, userMessage)
	}

	callOpts := cfg.GetCallOptions()
	if len(a.llmToolDefs) > 0 {
		callOpts = append(callOpts, llms.WithTools(a.llmToolDefs))
	}

	var toolExecuted bool
	var resp *llms.ContentResponse

	for {
		if a.cfg.CallbackHandler != nil {
			a.cfg.CallbackHandler.OnAssistantLLMCall(ctx, a, messageHistory)
		}

		resp, err = a.LLM.GenerateContent(ctx, messageHistory, callOpts...)
		if err != nil {
			return nil, err
		}

		// Perform Tool call
		toolExecuted, messageHistory, err = a.executeToolCalls(ctx, cfg, messageHistory, resp, options...)
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
		finalOutput, err := a.OutputParser.Parse(result)
		if err != nil {
			logger.ContextKV(ctx, xlog.DEBUG,
				"assistant", a.name,
				"status", "failed_to_parse_llm_response",
				"err", err.Error(),
				"output_parser", a.OutputParser.Type(),
				"result", result,
			)

			if a.cfg.CallbackHandler != nil {
				a.cfg.CallbackHandler.OnAssistantLLMParseError(ctx, a, input, result, err)
			}

			return nil, err
		}
		*optionalOutputType = *finalOutput

		if prov, ok := (any)(finalOutput).(chatmodel.ContentProvider); ok {
			result = prov.GetContent()
		}
	}

	messageHistory = append(messageHistory, llms.TextParts(llms.ChatMessageTypeAI, result))

	if cfg.Store != nil && !cfg.SkipMessageHistory {
		if cfg.IsGeneric {
			_ = cfg.Store.Add(ctx, &llms.GenericChatMessage{Content: llmutils.AddComment("assistant", a.Name(), "observation", result)})
		} else {
			_ = cfg.Store.Add(ctx, &llms.AIChatMessage{Content: result})
		}

		logger.ContextKV(ctx, xlog.DEBUG,
			"assistant", a.name,
			"chat_id", chatID,
			"status", "added_message_history",
			"human", slices.StringUpto(input, 32),
			"ai", slices.StringUpto(result, 32),
		)
	}

	a.runMessages = messageHistory

	return resp, nil
}

// executeToolCalls executes the tool calls in the response and returns the
// updated message history.
func (a *Assistant[O]) executeToolCalls(ctx context.Context, cfg *Config, messageHistory []llms.MessageContent, resp *llms.ContentResponse, options ...Option) (bool, []llms.MessageContent, error) {
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

			// use lowercase for the key
			tool := a.toolsByName[strings.ToLower(toolCall.FunctionCall.Name)]
			if tool == nil {
				if cfg.CallbackHandler != nil {
					cfg.CallbackHandler.OnToolNotFound(ctx, a, toolName)
				}

				availableTools := strings.Join(a.toolsNames, ", ")
				logger.ContextKV(ctx, xlog.WARNING,
					"assistant", a.name,
					"status", "tool_not_found",
					"tool_name", toolName,
					"available_tools", availableTools,
				)
				// Append tool_result to messageHistory
				toolCallResponse := llms.MessageContent{
					Role: llms.ChatMessageTypeTool,
					Parts: []llms.ContentPart{
						llms.ToolCallResponse{
							ToolCallID: toolCall.ID,
							Name:       toolName,
							Content:    fmt.Sprintf("Tool `%s` not found. Please check the tool name and try again with exact match. Available tools: %s", toolName, availableTools),
						},
					},
				}
				messageHistory = append(messageHistory, toolCallResponse)
				continue
			}

			if cfg.CallbackHandler != nil {
				cfg.CallbackHandler.OnToolStart(ctx, tool, toolArgs)
			}

			var res string
			var err error
			if assistant, ok := tool.(IAssistantTool); ok {
				res, err = assistant.CallAssistant(ctx, toolArgs, options...)
			} else {
				res, err = tool.Call(ctx, toolArgs)
			}
			if err != nil {
				if cfg.CallbackHandler != nil {
					cfg.CallbackHandler.OnToolError(ctx, tool, toolArgs, err)
				}

				if errors.Is(err, chatmodel.ErrFailedUnmarshalInput) {
					// Return an error to LLM to retry the tool call
					res = llmutils.AddComment("assistant", a.Name(), "error", "Failed to unmarshal input, check the JSON schema and try again.")
				} else {
					return false, nil, errors.WithMessagef(err, "failed to call tool %s", toolName)
				}
			}

			if cfg.CallbackHandler != nil {
				cfg.CallbackHandler.OnToolEnd(ctx, tool, toolArgs, res)
			}
			// Append tool_result to messageHistory
			toolCallResponse := llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: toolCall.ID,
						Name:       toolName,
						Content:    res,
					},
				},
			}
			messageHistory = append(messageHistory, toolCallResponse)
		}
	}
	return executed, messageHistory, nil
}
