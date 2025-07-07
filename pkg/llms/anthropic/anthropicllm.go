package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/x/values"
)

var (
	ErrEmptyResponse            = errors.New("anthropic: no response")
	ErrMissingToken             = errors.New("anthropic: missing API key, set it in the ANTHROPIC_API_KEY environment variable")
	ErrUnexpectedResponseLength = errors.New("anthropic: unexpected length of response")
	ErrInvalidContentType       = errors.New("anthropic: invalid content type")
	ErrUnsupportedMessageType   = errors.New("anthropic: unsupported message type")
	ErrUnsupportedContentType   = errors.New("anthropic: unsupported content type")
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"

	DefaultMaxTokens = 4096
)

type LLM struct {
	Client  *anthropic.Client
	Options *Options
}

var _ llms.Model = (*LLM)(nil)

// New creates a new Anthropic LLM client using the official Anthropic SDK.
//
// This function initializes an Anthropic client with the provided options.
// If no token is provided via options, it will attempt to read the API key
// from the ANTHROPIC_API_KEY environment variable.
//
// Required configuration:
//   - API token (via WithToken option or ANTHROPIC_API_KEY env var)
//   - Model (via WithModel option)
//
// Example usage:
//
//	llm, err := anthropic.New(
//	    anthropic.WithToken("your-api-key"),
//	    anthropic.WithModel("claude-3-5-sonnet-20241022"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Generate content
//	resp, err := llm.GenerateContent(ctx, messages)
func New(opts ...Option) (*LLM, error) {
	options := &Options{
		Token:      os.Getenv(TokenEnvVarName),
		BaseURL:    "https://api.anthropic.com",
		HttpClient: http.DefaultClient,
	}

	for _, opt := range opts {
		opt(options)
	}

	if len(options.Token) == 0 {
		return nil, ErrMissingToken
	}
	if options.Model == "" {
		return nil, errors.New("anthropic: model is required")
	}

	c, err := newClient(options)
	if err != nil {
		return nil, errors.Wrap(err, "anthropic: failed to create client")
	}
	return &LLM{
		Client:  c,
		Options: options,
	}, nil
}

func newClient(options *Options) (*anthropic.Client, error) {
	// Build SDK options
	sdkOpts := []option.RequestOption{
		option.WithAPIKey(options.Token),
		option.WithMaxRetries(2),
		option.WithRequestTimeout(5 * time.Minute),
	}

	if options.BaseURL != "" {
		sdkOpts = append(sdkOpts, option.WithBaseURL(options.BaseURL))
	}

	if options.HttpClient != nil {
		sdkOpts = append(sdkOpts, option.WithHTTPClient(options.HttpClient))
	}

	if options.AnthropicBetaHeader != "" {
		sdkOpts = append(sdkOpts, option.WithHeader("anthropic-beta", options.AnthropicBetaHeader))
	}

	client := anthropic.NewClient(sdkOpts...)

	return &client, nil
}

// GetName implements the Model interface.
func (o *LLM) GetName() string {
	return o.Options.Model
}

// GetProviderType implements the Model interface.
func (o *LLM) GetProviderType() llms.ProviderType {
	return llms.ProviderAnthropic
}

// GenerateContent implements the Model interface.
//
// This method generates content using the Anthropic API. It supports:
//   - Text and image inputs (multimodal)
//   - Tool/function calling
//   - Streaming responses
//   - Custom parameters (temperature, max tokens, etc.)
//
// Example usage:
//
//	messages := []llms.MessageContent{
//	    {
//	        Role: llms.ChatMessageTypeHuman,
//	        Parts: []llms.ContentPart{llms.TextPart("Hello, how are you?")},
//	    },
//	}
//
//	resp, err := llm.GenerateContent(ctx, messages,
//	    llms.WithTemperature(0.7),
//	    llms.WithMaxTokens(1000),
//	)
func (o *LLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	opts := llms.CallOptions{
		Model: o.Options.Model,
	}
	for _, opt := range options {
		opt(&opts)
	}
	return GenerateMessagesContent(ctx, o, messages, &opts)
}

// GenerateMessagesContent generates content using the Anthropic API with processed messages.
//
// This function handles the core logic for generating content, including:
//   - Message processing and conversion to Anthropic format
//   - Tool definition handling
//   - Parameter setup (temperature, max tokens, etc.)
//   - Streaming and non-streaming responses
//
// The function processes input messages to separate system prompts from conversation
// messages, converts tools to the Anthropic format, and handles both streaming
// and non-streaming responses.
func GenerateMessagesContent(ctx context.Context, o *LLM, messages []llms.MessageContent, opts *llms.CallOptions) (*llms.ContentResponse, error) {
	sdkMessages, systemPrompt, err := ProcessMessages(messages)
	if err != nil {
		return nil, errors.Wrap(err, "anthropic: failed to process messages")
	}

	tools := ToTools(opts.Tools)

	// Build message parameters
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(opts.Model),
		Messages:  sdkMessages,
		MaxTokens: values.NumbersCoalesce(int64(opts.MaxTokens), DefaultMaxTokens),
	}

	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{
				Type: "text",
				Text: systemPrompt,
			},
		}
	}

	if opts.Temperature > 0 {
		params.Temperature = anthropic.Float(opts.Temperature)
	}

	if opts.TopP > 0 {
		params.TopP = anthropic.Float(opts.TopP)
	}

	if len(opts.StopWords) > 0 {
		params.StopSequences = opts.StopWords
	}

	if len(tools) > 0 {
		params.Tools = tools
	}

	// Handle streaming
	if opts.StreamingFunc != nil {
		return GenerateStreamingContent(ctx, o, params, opts.StreamingFunc)
	}

	// Non-streaming message creation
	result, err := o.Client.Messages.New(ctx, params)
	if err != nil {
		return nil, errors.Wrap(err, "anthropic: failed to create message")
	}

	choices := make([]*llms.ContentChoice, len(result.Content))
	for i, contentBlock := range result.Content {
		switch content := contentBlock.AsAny().(type) {
		case anthropic.TextBlock:
			choices[i] = &llms.ContentChoice{
				Content:    content.Text,
				StopReason: string(result.StopReason),
				GenerationInfo: map[string]any{
					"InputTokens":  result.Usage.InputTokens,
					"OutputTokens": result.Usage.OutputTokens,
					"TotalTokens":  result.Usage.InputTokens + result.Usage.OutputTokens,
					"ID":           result.ID,
					"Index":        i,
				},
			}
		case anthropic.ToolUseBlock:
			argumentsJSON, err := json.Marshal(content.Input)
			if err != nil {
				return nil, errors.Wrap(err, "anthropic: failed to marshal tool use arguments")
			}
			choices[i] = &llms.ContentChoice{
				ToolCalls: []llms.ToolCall{
					{
						ID: content.ID,
						FunctionCall: &llms.FunctionCall{
							Name:      content.Name,
							Arguments: string(argumentsJSON),
						},
					},
				},
				StopReason: string(result.StopReason),
				GenerationInfo: map[string]any{
					"InputTokens":  result.Usage.InputTokens,
					"OutputTokens": result.Usage.OutputTokens,
					"TotalTokens":  result.Usage.InputTokens + result.Usage.OutputTokens,
					"ID":           result.ID,
					"Index":        i,
				},
			}
		default:
			return nil, errors.WithMessagef(ErrUnsupportedContentType, "anthropic: %T", content)
		}
	}

	resp := &llms.ContentResponse{
		Choices: choices,
	}
	return resp, nil
}

// GenerateStreamingContent handles streaming responses from the Anthropic API.
//
// This function establishes a streaming connection to the Anthropic API and processes
// real-time response chunks. It handles:
//   - Text content streaming (delta updates)
//   - Tool call streaming (partial JSON assembly)
//   - Usage statistics collection
//   - Error handling for streaming failures
//
// The streaming function is called for each text chunk received, allowing for
// real-time display or processing of the generated content.
func GenerateStreamingContent(ctx context.Context, o *LLM, params anthropic.MessageNewParams, streamingFunc func(context.Context, []byte) error) (*llms.ContentResponse, error) {
	stream := o.Client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var content strings.Builder
	var toolCalls []llms.ToolCall
	var currentToolCall *llms.ToolCall
	var stopReason string
	var inputTokens, outputTokens int64

	for stream.Next() {
		event := stream.Current()

		switch evt := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			inputTokens = evt.Message.Usage.InputTokens
		case anthropic.ContentBlockStartEvent:
			switch block := evt.ContentBlock.AsAny().(type) {
			case anthropic.ToolUseBlock:
				currentToolCall = &llms.ToolCall{
					ID: block.ID,
					FunctionCall: &llms.FunctionCall{
						Name: block.Name,
					},
				}
			}
		case anthropic.ContentBlockDeltaEvent:
			switch delta := evt.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				content.WriteString(delta.Text)
				if streamingFunc != nil {
					if err := streamingFunc(ctx, []byte(delta.Text)); err != nil {
						return nil, errors.Wrap(err, "anthropic: streaming function error")
					}
				}
			case anthropic.InputJSONDelta:
				// Handle partial JSON for tool calls
				if currentToolCall != nil {
					currentToolCall.FunctionCall.Arguments += delta.PartialJSON
				}
			}
		case anthropic.ContentBlockStopEvent:
			if currentToolCall != nil {
				toolCalls = append(toolCalls, *currentToolCall)
				currentToolCall = nil
			}
		case anthropic.MessageDeltaEvent:
			stopReason = string(evt.Delta.StopReason)
			outputTokens = evt.Usage.OutputTokens
		}
	}

	if err := stream.Err(); err != nil {
		return nil, errors.Wrap(err, "anthropic: streaming error")
	}

	// Build response
	var choices []*llms.ContentChoice
	if content.Len() > 0 {
		choices = append(choices, &llms.ContentChoice{
			Content:    content.String(),
			StopReason: stopReason,
			GenerationInfo: map[string]any{
				"InputTokens":  inputTokens,
				"OutputTokens": outputTokens,
				"TotalTokens":  inputTokens + outputTokens,
			},
		})
	}

	if len(toolCalls) > 0 {
		choices = append(choices, &llms.ContentChoice{
			ToolCalls:  toolCalls,
			StopReason: stopReason,
			GenerationInfo: map[string]any{
				"InputTokens":  inputTokens,
				"OutputTokens": outputTokens,
				"TotalTokens":  inputTokens + outputTokens,
			},
		})
	}

	return &llms.ContentResponse{
		Choices: choices,
	}, nil
}

// ToTools converts LLM tool definitions to Anthropic SDK tool parameters.
//
// This function transforms the generic llms.Tool format into the specific
// anthropic.ToolUnionParam format required by the Anthropic SDK. It handles:
//   - Function definition conversion
//   - JSON schema property mapping from orderedmap to regular map
//   - Required parameter specification
//   - Tool description formatting
//
// Returns nil if no tools are provided, which is handled gracefully by the API.
func ToTools(tools []llms.Tool) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	sdkTools := make([]anthropic.ToolUnionParam, len(tools))
	for i, tool := range tools {
		// Convert Properties from orderedmap to regular map for Anthropic SDK
		var properties map[string]any
		if tool.Function.Parameters.Properties != nil {
			properties = make(map[string]any)
			for pair := tool.Function.Parameters.Properties.Oldest(); pair != nil; pair = pair.Next() {
				properties[pair.Key] = pair.Value
			}
		}

		inputSchema := anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: properties,
		}
		if len(tool.Function.Parameters.Required) > 0 {
			inputSchema.Required = tool.Function.Parameters.Required
		}

		sdkTools[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        tool.Function.Name,
				Description: anthropic.String(tool.Function.Description),
				InputSchema: inputSchema,
			},
		}
	}
	return sdkTools
}

// ProcessMessages converts generic message content to Anthropic SDK message parameters.
//
// This function processes an array of message content and converts them to the format
// expected by the Anthropic API. It handles:
//   - System message extraction (returned as separate system prompt)
//   - Human message conversion (text and image content)
//   - AI message conversion (text and tool calls)
//   - Tool message conversion (tool call responses)
//   - Error handling for unsupported message types
//
// Returns the converted messages, extracted system prompt, and any error encountered.
func ProcessMessages(messages []llms.MessageContent) ([]anthropic.MessageParam, string, error) {
	chatMessages := make([]anthropic.MessageParam, 0, len(messages))
	systemPrompt := ""
	for _, msg := range messages {
		if len(msg.Parts) == 0 {
			continue
		}
		switch msg.Role {
		case llms.ChatMessageTypeSystem:
			content, err := HandleSystemMessage(msg)
			if err != nil {
				return nil, "", errors.Wrap(err, "anthropic: failed to handle system message")
			}
			if systemPrompt != "" {
				systemPrompt += "\n" + content
			} else {
				systemPrompt = content
			}
		case llms.ChatMessageTypeHuman:
			chatMessage, err := HandleHumanMessage(msg)
			if err != nil {
				return nil, "", errors.Wrap(err, "anthropic: failed to handle human message")
			}
			chatMessages = append(chatMessages, chatMessage)
		case llms.ChatMessageTypeAI, llms.ChatMessageTypeGeneric:
			chatMessage, err := HandleAIMessage(msg)
			if err != nil {
				return nil, "", errors.Wrap(err, "anthropic: failed to handle AI message")
			}
			chatMessages = append(chatMessages, chatMessage)
		case llms.ChatMessageTypeTool:
			chatMessage, err := HandleToolMessage(msg)
			if err != nil {
				return nil, "", errors.WithMessage(err, "anthropic: failed to handle tool message")
			}
			chatMessages = append(chatMessages, chatMessage)
		case llms.ChatMessageTypeFunction:
			return nil, "", errors.WithMessagef(ErrUnsupportedMessageType, "anthropic: %v", msg.Role)
		default:
			return nil, "", errors.WithMessagef(ErrUnsupportedMessageType, "anthropic: %v", msg.Role)
		}
	}
	return chatMessages, systemPrompt, nil
}

// HandleSystemMessage extracts text content from system messages.
//
// System messages in Anthropic are handled separately from conversation messages
// and are passed as a distinct system parameter. This function validates that
// the system message contains only text content and returns it as a string.
func HandleSystemMessage(msg llms.MessageContent) (string, error) {
	if textContent, ok := msg.Parts[0].(llms.TextContent); ok {
		return textContent.Text, nil
	}
	return "", errors.WithMessagef(ErrInvalidContentType, "anthropic: for system message")
}

// HandleHumanMessage converts human messages to Anthropic user message format.
//
// This function handles human/user messages and converts them to the Anthropic
// user message format. It supports:
//   - Text content
//   - Image content (PNG, JPEG, GIF, WebP)
//   - Base64 encoding for binary content
//   - Multiple content parts in a single message
//
// Images are automatically base64-encoded and formatted for the Anthropic API.
func HandleHumanMessage(msg llms.MessageContent) (anthropic.MessageParam, error) {
	var contents []anthropic.ContentBlockParamUnion

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case llms.TextContent:
			contents = append(contents, anthropic.NewTextBlock(p.Text))
		case llms.BinaryContent:
			if strings.HasPrefix(p.MIMEType, "image/") {
				encodedData := base64.StdEncoding.EncodeToString(p.Data)
				contents = append(contents, anthropic.NewImageBlockBase64(p.MIMEType, encodedData))
			} else {
				return anthropic.MessageParam{}, errors.Errorf("anthropic: unsupported binary content type: %s", p.MIMEType)
			}
		default:
			return anthropic.MessageParam{}, errors.Errorf("anthropic: unsupported human message part type: %T", part)
		}
	}

	if len(contents) == 0 {
		return anthropic.MessageParam{}, errors.New("anthropic: no valid content in human message")
	}

	return anthropic.NewUserMessage(contents...), nil
}

// HandleAIMessage converts AI assistant messages to Anthropic assistant message format.
//
// This function handles AI/assistant messages and converts them to the Anthropic
// assistant message format. It supports:
//   - Text responses from the assistant
//   - Tool calls with function names and JSON arguments
//   - Mixed content (text + tool calls)
//
// Tool call arguments are validated as proper JSON before conversion.
func HandleAIMessage(msg llms.MessageContent) (anthropic.MessageParam, error) {
	var contents []anthropic.ContentBlockParamUnion

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case llms.ToolCall:
			var inputJSON json.RawMessage
			if err := json.Unmarshal([]byte(p.FunctionCall.Arguments), &inputJSON); err != nil {
				return anthropic.MessageParam{}, errors.Wrap(err, "anthropic: failed to unmarshal tool call arguments")
			}

			contents = append(contents, anthropic.NewToolUseBlock(
				p.ID,
				inputJSON,
				p.FunctionCall.Name,
			))
		case llms.TextContent:
			contents = append(contents, anthropic.NewTextBlock(p.Text))
		default:
			return anthropic.MessageParam{}, errors.Errorf("anthropic: unsupported AI message part type: %T", part)
		}
	}

	if len(contents) == 0 {
		return anthropic.MessageParam{}, errors.New("anthropic: no valid content in AI message")
	}

	return anthropic.NewAssistantMessage(contents...), nil
}

// HandleToolMessage converts tool response messages to Anthropic user message format.
//
// This function handles tool call response messages and converts them to the
// Anthropic user message format with tool result content. Tool responses
// in Anthropic are sent as user messages containing tool result blocks.
//
// The function validates that the message contains only tool call response
// content and formats it appropriately for the API.
func HandleToolMessage(msg llms.MessageContent) (anthropic.MessageParam, error) {
	var contents []anthropic.ContentBlockParamUnion

	for _, part := range msg.Parts {
		if toolCallResponse, ok := part.(llms.ToolCallResponse); ok {
			contents = append(contents, anthropic.NewToolResultBlock(
				toolCallResponse.ToolCallID,
				toolCallResponse.Content,
				false, // isError
			))
		} else {
			return anthropic.MessageParam{}, errors.WithMessagef(ErrInvalidContentType, "anthropic: for tool message part type: %T", part)
		}
	}

	if len(contents) == 0 {
		return anthropic.MessageParam{}, errors.New("anthropic: no valid content in tool message")
	}

	return anthropic.NewUserMessage(contents...), nil
}
