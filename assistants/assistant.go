package assistants

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/metricskey"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/x/slices"
	"github.com/effective-security/x/values"
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
func (a *Assistant[O]) GetSystemPrompt(ctx context.Context, input string, promptInputs map[string]any) (string, error) {
	if a.onPrompt != nil {
		extra, err := a.onPrompt(ctx, input)
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

	req := &CallInput{
		Input: input.Input,
	}
	resp, err := a.Run(ctx, req, nil)
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

func (a *Assistant[O]) Call(ctx context.Context, input *CallInput) (*llms.ContentResponse, error) {
	var output O
	return a.Run(ctx, input, &output)
}

func (a *Assistant[O]) Run(ctx context.Context, input *CallInput, optionalOutputType *O) (*llms.ContentResponse, error) {
	started := time.Now()
	defer metricskey.PerfAssistantCall.MeasureSince(started, a.Name())

	resp, err := a.run(ctx, input, optionalOutputType)
	if err != nil {
		metricskey.StatsAssistantCallsFailed.IncrCounter(1, a.Name())
		return nil, err
	}
	metricskey.StatsAssistantCallsSucceeded.IncrCounter(1, a.Name())
	return resp, nil
}

// run executes the main logic of the Assistant, generating a response based on the input and prompt inputs.
func (a *Assistant[O]) run(ctx context.Context, input *CallInput, optionalOutputType *O) (*llms.ContentResponse, error) {
	chatID, _, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, errors.WithStack(chatmodel.ErrInvalidChatContext)
	}

	// create a per call config
	cfg := a.cfg.Apply(input.Options...)

	systemPrompt, err := a.GetSystemPrompt(ctx, input.Input, input.PromptInputs)
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

	parsedInput := input.Input

	var userMessage llms.MessageContent
	if parsedInput != "" {
		if a.inputParser != nil {
			parsedInput, err = a.inputParser(parsedInput)
			if err != nil {
				return nil, errors.WithMessage(err, "failed to parse input")
			}
		}

		role := llms.ChatMessageTypeHuman
		if !cfg.IsGeneric && cfg.Store != nil {
			_ = cfg.Store.Add(ctx, &llms.HumanChatMessage{Content: parsedInput})
		}
		// else {
		// 	// TODO: keep as Human?
		// 	// 	role = llms.ChatMessageTypeGeneric
		// }
		userMessage = llms.TextParts(role, parsedInput)
		messageHistory = append(messageHistory, userMessage)
	}

	if len(input.Messages) > 0 {
		messageHistory = append(messageHistory, input.Messages...)
	}

	callOpts := cfg.GetCallOptions(WithTools(a.llmToolDefs))

	var totalToolExecuted int
	var resp *llms.ContentResponse
	maxRetries := DefaultMaxRetries
	retryCount := 0
	consecutiveNotFoundCount := 0

	bytesLimit := uint64(values.NumbersCoalesce(cfg.MaxLength, DefaultMaxContentSize))
	toolsLimit := values.NumbersCoalesce(cfg.MaxToolCalls, DefaultMaxToolCalls)
	for {
		if len(messageHistory) >= cfg.MaxMessages {
			return nil, errors.Newf("assistant %s: the messages count exceeded limit", a.name)
		}
		bytesSent := llmutils.CountMessagesContentSize(messageHistory)
		if bytesSent > bytesLimit {
			return nil, errors.Newf("assistant %s: the content size exceeded limit", a.name)
		}

		if a.cfg.CallbackHandler != nil {
			a.cfg.CallbackHandler.OnAssistantLLMCall(ctx, a, messageHistory)
		}

		metricskey.StatsLLMMessagesSent.IncrCounter(float64(len(messageHistory)), a.Name())
		metricskey.StatsLLMBytesSent.IncrCounter(float64(bytesSent), a.Name())

		resp, err = a.LLM.GenerateContent(ctx, messageHistory, callOpts...)
		if err != nil {
			return nil, err
		}

		bytesReceived := llmutils.CountResponseContentSize(resp)
		metricskey.StatsLLMBytesReceived.IncrCounter(float64(bytesReceived), a.Name())
		metricskey.StatsLLMBytesTotal.IncrCounter(float64(bytesSent+bytesReceived), a.Name())

		// Check for empty response and retry if needed
		if len(resp.Choices) == 0 {
			retryCount++
			if retryCount >= maxRetries {
				logger.ContextKV(ctx, xlog.ERROR,
					"assistant", a.name,
					"status", "max_retries_exceeded",
					"input", slices.StringUpto(parsedInput, 64),
					"retry_count", retryCount,
				)
				return nil, errors.Newf("assistant %s: LLM returned empty response after %d retries", a.name, retryCount)
			}
			logger.ContextKV(ctx, xlog.WARNING,
				"assistant", a.name,
				"status", "retrying_empty_response",
				"retry_count", retryCount,
			)
			continue
		}

		// Perform Tool call
		var toolExecuted int
		var notFoundCount int
		toolExecuted, notFoundCount, messageHistory, err = a.executeToolCalls(ctx, cfg, messageHistory, resp, input.Options...)
		if err != nil {
			return nil, err
		}

		if toolExecuted == 0 {
			break
		}
		consecutiveNotFoundCount += notFoundCount
		totalToolExecuted += toolExecuted
		if consecutiveNotFoundCount > 3 {
			return nil, errors.Newf("assistant %s: the number of not found tools is exceeded", a.name)
		}
		// reset
		consecutiveNotFoundCount = 0
		if totalToolExecuted >= toolsLimit {
			return nil, errors.Newf("assistant %s: the tool calls limit is exceeded", a.name)
		}
	}

	choices := resp.Choices
	if len(choices) < 1 {
		logger.ContextKV(ctx, xlog.ERROR,
			"assistant", a.name,
			"status", "empty_choices",
			"input", slices.StringUpto(parsedInput, 64),
		)
		return nil, errors.Newf("assistant %s: LLM returned empty response with no choices", a.name)
	}

	// Log response analysis for debugging
	logger.ContextKV(ctx, xlog.DEBUG,
		"assistant", a.name,
		"status", "response_analysis",
		"choices_count", len(choices),
		"tool_calls", totalToolExecuted,
	)

	result := choices[0].Content
	if len(choices) > 1 {
		// Handle multiple choices by combining their content
		var combinedContent strings.Builder
		for i, choice := range choices {
			if i > 0 {
				combinedContent.WriteString("\n\n")
			}
			combinedContent.WriteString(choice.Content)
		}
		result = combinedContent.String()
	}

	if optionalOutputType != nil {
		finalOutput, err := a.OutputParser.Parse(result)
		if err != nil {
			metricskey.StatsAssistantLLMParseErrors.IncrCounter(1, a.Name())
			logger.ContextKV(ctx, xlog.DEBUG,
				"assistant", a.name,
				"status", "failed_to_parse_llm_response",
				"err", err.Error(),
				"output_parser", a.OutputParser.Type(),
				"result", result,
			)

			if a.cfg.CallbackHandler != nil {
				a.cfg.CallbackHandler.OnAssistantLLMParseError(ctx, a, input.Input, result, err)
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
			"human", slices.StringUpto(parsedInput, 64),
			"ai", slices.StringUpto(result, 64),
		)
	}

	a.runMessages = messageHistory

	return resp, nil
}

// executeToolCalls executes the tool calls in the response and returns the
// updated message history.
func (a *Assistant[O]) executeToolCalls(ctx context.Context, cfg *Config, messageHistory []llms.MessageContent, resp *llms.ContentResponse, options ...Option) (int, int, []llms.MessageContent, error) {
	executedCount := 0
	notFoundCount := 0

	// Create a type to hold tool call results
	type toolCallResult struct {
		toolCall llms.ToolCall
		response string
		err      error
		index    int // Index in the original toolCalls slice
	}

	var toolCalls []llms.ToolCall

	// Collect all tool calls first and add them to message history
	for _, choice := range resp.Choices {
		// Collect all tool calls from this choice
		for _, toolCall := range choice.ToolCalls {
			executedCount++
			toolCalls = append(toolCalls, toolCall)

			logger.ContextKV(ctx, xlog.DEBUG,
				"assistant", a.name,
				"status", "tool_call_found",
				"tool_call_id", toolCall.ID,
				"tool_call_name", toolCall.FunctionCall.Name,
			)
		}

		// Create a single assistant message with all tool calls
		parts := make([]llms.ContentPart, len(choice.ToolCalls))
		for i, toolCall := range choice.ToolCalls {
			parts[i] = llms.ToolCall{
				ID:   toolCall.ID,
				Type: toolCall.Type,
				FunctionCall: &llms.FunctionCall{
					Name:      toolCall.FunctionCall.Name,
					Arguments: toolCall.FunctionCall.Arguments,
				},
			}
		}

		assistantResponse := llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: parts,
		}
		messageHistory = append(messageHistory, assistantResponse)
	}

	if executedCount == 0 {
		return executedCount, notFoundCount, messageHistory, nil
	}

	// Channel to collect results - buffered to prevent deadlock
	resultChan := make(chan toolCallResult, len(toolCalls))

	// Create a wait group to ensure all tool calls complete
	var wg sync.WaitGroup
	wg.Add(len(toolCalls))

	// Launch goroutines for each tool call
	for i, toolCall := range toolCalls {
		go func(index int, tc llms.ToolCall) {
			defer wg.Done()
			toolName := tc.FunctionCall.Name
			toolArgs := tc.FunctionCall.Arguments

			// use lowercase for the key
			tool := a.toolsByName[strings.ToLower(toolName)]
			if tool == nil {
				notFoundCount++
				metricskey.StatsToolCallsNotFound.IncrCounter(1, toolName)
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

				resultChan <- toolCallResult{
					toolCall: tc,
					response: fmt.Sprintf("Tool `%s` not found. Please check the tool name and try again with exact match. Available tools: %s", toolName, availableTools),
					index:    index,
				}
				return
			}

			if cfg.CallbackHandler != nil {
				cfg.CallbackHandler.OnToolStart(ctx, tool, toolArgs)
			}

			started := time.Now()

			var res string
			var err error
			if assistant, ok := tool.(IAssistantTool); ok {
				res, err = assistant.CallAssistant(ctx, toolArgs, options...)
			} else {
				res, err = tool.Call(ctx, toolArgs)
			}
			metricskey.PerfToolCall.MeasureSince(started, toolName)

			if err != nil {
				metricskey.StatsToolCallsFailed.IncrCounter(1, toolName)

				if cfg.CallbackHandler != nil {
					cfg.CallbackHandler.OnToolError(ctx, tool, toolArgs, err)
				}

				if errors.Is(err, chatmodel.ErrFailedUnmarshalInput) {
					res = llmutils.AddComment("assistant", a.Name(), "error", "Failed to unmarshal input, check the JSON schema and try again.")
				} else {
					resultChan <- toolCallResult{
						toolCall: tc,
						err:      errors.WithMessagef(err, "failed to call tool %s", toolName),
						index:    index,
					}
					return
				}
			}
			metricskey.StatsToolCallsSucceeded.IncrCounter(1, toolName)

			if cfg.CallbackHandler != nil {
				cfg.CallbackHandler.OnToolEnd(ctx, tool, toolArgs, res)
			}

			resultChan <- toolCallResult{
				toolCall: tc,
				response: res,
				index:    index,
			}
		}(i, toolCall)
	}

	// Wait for all tool calls to complete
	wg.Wait()
	close(resultChan)

	// Collect results in order using the index
	results := make([]toolCallResult, len(toolCalls))
	for result := range resultChan {
		if result.index >= 0 && result.index < len(results) {
			results[result.index] = result
		}
	}

	// Ensure we have responses for all tool calls
	for i, result := range results {
		if result.toolCall.ID == "" {
			// If we somehow missed a result, create an error response
			toolCall := toolCalls[i]
			results[i] = toolCallResult{
				toolCall: toolCall,
				response: "Tool call failed: No response received",
				err:      errors.New("no response received from tool"),
				index:    i,
			}
			logger.ContextKV(ctx, xlog.WARNING,
				"assistant", a.name,
				"status", "tool_call_missing_response",
				"tool_call_id", toolCall.ID,
				"tool_name", toolCall.FunctionCall.Name,
			)
		}
	}

	// Process results in the same order as the original tool calls
	for _, result := range results {
		var content string
		if result.err != nil {
			// Format error as a message for the LLM
			content = fmt.Sprintf("Tool call failed: %s", result.err.Error())
			// Log the error for monitoring
			logger.ContextKV(ctx, xlog.WARNING,
				"assistant", a.name,
				"status", "tool_call_failed",
				"tool", result.toolCall.FunctionCall.Name,
				"err", result.err.Error(),
			)
		} else {
			content = result.response
		}

		// Create tool call response using the ID from the original tool call
		toolCallResponse := llms.MessageContent{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: result.toolCall.ID, // Use the ID from the original tool call
					Name:       result.toolCall.FunctionCall.Name,
					Content:    content,
				},
			},
		}

		// Log the tool call response for debugging
		logger.ContextKV(ctx, xlog.DEBUG,
			"assistant", a.name,
			"status", "tool_call_response",
			"tool_call_id", result.toolCall.ID,
			"tool_name", result.toolCall.FunctionCall.Name,
			"content_length", len(content),
		)

		// Add the response immediately after its corresponding tool call
		messageHistory = append(messageHistory, toolCallResponse)
	}

	return executedCount, notFoundCount, messageHistory, nil
}
