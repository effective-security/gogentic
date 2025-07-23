package bedrockclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// Ref: https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages.html
// Also: https://docs.anthropic.com/claude/reference/messages_post

// anthropicBinGenerationInputSource is the source of the content.
type anthropicBinGenerationInputSource struct {
	// The type of the source. Required
	// One of: "base64"
	Type string `json:"type"`
	// The MIME type of the source. Required
	// One of: []"image/jpeg", "image/png", "image/gif", "image/bmp", "image/webp"]
	MediaType string `json:"media_type"`
	// The data of the source. Required
	// For example if type is "base64" then data is a base64 encoded string
	Data string `json:"data"`
}

// anthropicTextGenerationInputContent is a single message in the input.
type anthropicTextGenerationInputContent struct {
	// The type of the content. Required.
	// One of: "text", "image", "tool_use", "tool_result"
	Type string `json:"type"`
	// The source of the content. Required if type is "image"
	Source *anthropicBinGenerationInputSource `json:"source,omitempty"`
	// The text content. Required if type is "text"
	Text string `json:"text,omitempty"`
	// Tool use fields
	ID    string `json:"id,omitempty"`    // Required if type is "tool_use"
	Name  string `json:"name,omitempty"`  // Required if type is "tool_use"
	Input any    `json:"input,omitempty"` // Required if type is "tool_use"
	// Tool result fields
	ToolUseID string `json:"tool_use_id,omitempty"` // Required if type is "tool_result"
	Content   string `json:"content,omitempty"`     // Required if type is "tool_result"
	IsError   bool   `json:"is_error,omitempty"`    // Optional for type "tool_result"
}

type anthropicTextGenerationInputMessage struct {
	// The role of the message. Required
	// One of: ["user", "assistant"]
	// For system prompt, use the system field in the input
	Role string `json:"role"`
	// The content of the message. Required
	Content []anthropicTextGenerationInputContent `json:"content"`
}

// anthropicTool represents a tool that can be used by the model
type anthropicTool struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	InputSchema anthropicInputSchema `json:"input_schema"`
}

// anthropicInputSchema represents the JSON schema for tool input
type anthropicInputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

// anthropicTextGenerationInput is the input to the model.
type anthropicTextGenerationInput struct {
	// The version of the model to use. Required
	AnthropicVersion string `json:"anthropic_version"`
	// The maximum number of tokens to generate per result. Required
	MaxTokens int `json:"max_tokens"`
	// The system prompt to use. Optional
	System string `json:"system,omitempty"`
	// The messages to use. Required
	Messages []*anthropicTextGenerationInputMessage `json:"messages"`
	// The amount of randomness injected into the response. Optional, default = 1
	Temperature float64 `json:"temperature,omitempty"`
	// The probability mass from which tokens are sampled. Optional, default = 1
	TopP float64 `json:"top_p,omitempty"`
	// Only sample from the top K options for each subsequent token.
	// Use top_k to remove long tail low probability responses.
	// Optional, default = 250
	TopK int `json:"top_k,omitempty"`
	// Sequences that will cause the model to stop generating tokens. Optional
	StopSequences []string `json:"stop_sequences,omitempty"`
	// Tools to use. Optional
	Tools []anthropicTool `json:"tools,omitempty"`
}

// anthropicTextGenerationOutputContent represents a content block in the output
type anthropicTextGenerationOutputContent struct {
	Type string `json:"type"`
	// Text content fields
	Text string `json:"text,omitempty"`
	// Tool use fields
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

// anthropicTextGenerationOutput is the generated output.
type anthropicTextGenerationOutput struct {
	// Type of the content.
	// For messages, it is "message"
	Type string `json:"type"`
	// Conversational role of the generated message.
	// This will always be "assistant".
	Role string `json:"role"`
	// This is an array of content blocks, each of which has a type that determines its shape.
	// Can be "text" or "tool_use".
	Content []anthropicTextGenerationOutputContent `json:"content"`
	// The reason for the completion of the generation.
	// One of: ["end_turn", "max_tokens", "stop_sequence", "tool_use"]
	StopReason string `json:"stop_reason"`
	// Which custom stop sequence was matched, if any.
	StopSequence string `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Finish reason for the completion of the generation.
const (
	AnthropicCompletionReasonEndTurn      = "end_turn"
	AnthropicCompletionReasonMaxTokens    = "max_tokens"
	AnthropicCompletionReasonStopSequence = "stop_sequence"
	AnthropicCompletionReasonToolUse      = "tool_use"
)

// The latest version of the model.
const (
	AnthropicLatestVersion = "bedrock-2023-05-31"
)

// Role attribute for the anthropic message.
const (
	AnthropicSystem        = "system"
	AnthropicRoleUser      = "user"
	AnthropicRoleAssistant = "assistant"
)

// Type attribute for the anthropic message.
const (
	AnthropicMessageTypeText       = "text"
	AnthropicMessageTypeImage      = "image"
	AnthropicMessageTypeToolUse    = "tool_use"
	AnthropicMessageTypeToolResult = "tool_result"
)

func createAnthropicCompletion(ctx context.Context,
	client *bedrockruntime.Client,
	modelID string,
	messages []Message,
	options llms.CallOptions,
) (*llms.ContentResponse, error) {
	inputContents, systemPrompt, err := processInputMessagesAnthropic(messages)
	if err != nil {
		return nil, err
	}

	// Convert tools to Anthropic format
	var tools []anthropicTool
	if len(options.Tools) > 0 {
		tools = make([]anthropicTool, len(options.Tools))
		for i, tool := range options.Tools {
			var properties map[string]any
			if tool.Function.Parameters.Properties != nil {
				properties = make(map[string]any)
				for pair := tool.Function.Parameters.Properties.Oldest(); pair != nil; pair = pair.Next() {
					properties[pair.Key] = pair.Value
				}
			}

			tools[i] = anthropicTool{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				InputSchema: anthropicInputSchema{
					Type:       "object",
					Properties: properties,
					Required:   tool.Function.Parameters.Required,
				},
			}
		}
	}

	input := anthropicTextGenerationInput{
		AnthropicVersion: AnthropicLatestVersion,
		MaxTokens:        getMaxTokens(options.MaxTokens, 2048),
		System:           systemPrompt,
		Messages:         inputContents,
		Temperature:      options.Temperature,
		TopP:             options.TopP,
		TopK:             options.TopK,
		StopSequences:    options.StopWords,
		Tools:            tools,
	}

	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	if options.StreamingFunc != nil {
		modelInput := &bedrockruntime.InvokeModelWithResponseStreamInput{
			ModelId:     aws.String(modelID),
			Accept:      aws.String("*/*"),
			ContentType: aws.String("application/json"),
			Body:        body,
		}
		return parseStreamingCompletionResponse(ctx, client, modelInput, options)
	}

	modelInput := &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String("*/*"),
		ContentType: aws.String("application/json"),
		Body:        body,
	}
	resp, err := client.InvokeModel(ctx, modelInput)
	if err != nil {
		return nil, err
	}

	var output anthropicTextGenerationOutput
	err = json.Unmarshal(resp.Body, &output)
	if err != nil {
		return nil, err
	}

	if len(output.Content) == 0 {
		return nil, errors.New("no results")
	} else if stopReason := output.StopReason; stopReason != AnthropicCompletionReasonEndTurn &&
		stopReason != AnthropicCompletionReasonStopSequence &&
		stopReason != AnthropicCompletionReasonToolUse {
		return nil, errors.New("completed due to " + stopReason + ". Maybe try increasing max tokens")
	}

	// Process content blocks - handle both text and tool use
	var choices []*llms.ContentChoice
	var textContent string
	var toolCalls []llms.ToolCall

	for _, c := range output.Content {
		switch c.Type {
		case AnthropicMessageTypeText:
			textContent += c.Text
		case AnthropicMessageTypeToolUse:
			argumentsJSON, err := json.Marshal(c.Input)
			if err != nil {
				return nil, errors.Wrap(err, "failed to marshal tool arguments")
			}
			toolCalls = append(toolCalls, llms.ToolCall{
				ID: c.ID,
				FunctionCall: &llms.FunctionCall{
					Name:      c.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
	}

	// Create appropriate choices based on content type
	if textContent != "" {
		choices = append(choices, &llms.ContentChoice{
			Content:    textContent,
			StopReason: output.StopReason,
			GenerationInfo: map[string]any{
				"InputTokens":  output.Usage.InputTokens,
				"OutputTokens": output.Usage.OutputTokens,
				"TotalTokens":  output.Usage.InputTokens + output.Usage.OutputTokens,
			},
		})
	}

	if len(toolCalls) > 0 {
		choices = append(choices, &llms.ContentChoice{
			ToolCalls:  toolCalls,
			StopReason: output.StopReason,
			GenerationInfo: map[string]any{
				"InputTokens":  output.Usage.InputTokens,
				"OutputTokens": output.Usage.OutputTokens,
				"TotalTokens":  output.Usage.InputTokens + output.Usage.OutputTokens,
			},
		})
	}

	// If we have neither text nor tools, create a single choice with the first content
	if len(choices) == 0 && len(output.Content) > 0 {
		choices = append(choices, &llms.ContentChoice{
			Content:    output.Content[0].Text,
			StopReason: output.StopReason,
			GenerationInfo: map[string]any{
				"InputTokens":  output.Usage.InputTokens,
				"OutputTokens": output.Usage.OutputTokens,
				"TotalTokens":  output.Usage.InputTokens + output.Usage.OutputTokens,
			},
		})
	}

	return &llms.ContentResponse{
		Choices: choices,
	}, nil
}

type streamingCompletionResponseChunk struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type         string `json:"type"`
		Text         string `json:"text"`
		StopReason   string `json:"stop_reason"`
		StopSequence any    `json:"stop_sequence"`
	} `json:"delta"`
	AmazonBedrockInvocationMetrics struct {
		InputTokenCount   int `json:"inputTokenCount"`
		OutputTokenCount  int `json:"outputTokenCount"`
		InvocationLatency int `json:"invocationLatency"`
		FirstByteLatency  int `json:"firstByteLatency"`
	} `json:"amazon-bedrock-invocationMetrics"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Message struct {
		ID           string `json:"id"`
		Type         string `json:"type"`
		Role         string `json:"role"`
		Content      []any  `json:"content"`
		Model        string `json:"model"`
		StopReason   any    `json:"stop_reason"`
		StopSequence any    `json:"stop_sequence"`
		Usage        struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func parseStreamingCompletionResponse(ctx context.Context, client *bedrockruntime.Client, modelInput *bedrockruntime.InvokeModelWithResponseStreamInput, options llms.CallOptions) (*llms.ContentResponse, error) {
	output, err := client.InvokeModelWithResponseStream(ctx, modelInput)
	if err != nil {
		return nil, err
	}
	stream := output.GetStream()
	if stream == nil {
		return nil, errors.New("no stream")
	}
	defer func() {
		_ = stream.Close()
	}()

	contentchoices := []*llms.ContentChoice{{GenerationInfo: map[string]any{}}}
	for e := range stream.Events() {
		if err = stream.Err(); err != nil {
			return nil, err
		}

		if v, ok := e.(*types.ResponseStreamMemberChunk); ok {
			var resp streamingCompletionResponseChunk
			err := json.NewDecoder(bytes.NewReader(v.Value.Bytes)).Decode(&resp)
			if err != nil {
				return nil, err
			}

			switch resp.Type {
			case "message_start":
				contentchoices[0].GenerationInfo["InputTokens"] = resp.Message.Usage.InputTokens
			case "content_block_delta":
				if err = options.StreamingFunc(ctx, []byte(resp.Delta.Text)); err != nil {
					return nil, err
				}
				contentchoices[0].Content += resp.Delta.Text
			case "message_delta":
				contentchoices[0].StopReason = resp.Delta.StopReason
				contentchoices[0].GenerationInfo["OutputTokens"] = resp.Usage.OutputTokens
			}
		}
	}
	if err = stream.Err(); err != nil {
		return nil, err
	}

	return &llms.ContentResponse{
		Choices: contentchoices,
	}, nil
}

// process the input messages to anthropic supported input
// returns the input content and system prompt.
func processInputMessagesAnthropic(messages []Message) ([]*anthropicTextGenerationInputMessage, string, error) {
	chunkedMessages := make([][]Message, 0, len(messages))
	currentChunk := make([]Message, 0, len(messages))
	var lastRole llms.Role
	for _, message := range messages {
		if message.Role != lastRole {
			if len(currentChunk) > 0 {
				chunkedMessages = append(chunkedMessages, currentChunk)
			}
			currentChunk = make([]Message, 0, len(messages))
		}
		currentChunk = append(currentChunk, message)
		lastRole = message.Role
	}
	if len(currentChunk) > 0 {
		chunkedMessages = append(chunkedMessages, currentChunk)
	}

	inputContents := make([]*anthropicTextGenerationInputMessage, 0, len(messages))
	var systemPrompt string
	for _, chunk := range chunkedMessages {
		role, err := getAnthropicRole(chunk[0].Role)
		if err != nil {
			return nil, "", err
		}
		if role == AnthropicSystem {
			if systemPrompt != "" {
				return nil, "", errors.New("multiple system prompts")
			}
			for _, message := range chunk {
				c := getAnthropicInputContent(message)
				if c.Type != AnthropicMessageTypeText {
					return nil, "", errors.New("system prompt must be text")
				}
				systemPrompt += c.Text
			}
			continue
		}
		content := make([]anthropicTextGenerationInputContent, 0, len(chunk))
		for _, message := range chunk {
			content = append(content, getAnthropicInputContent(message))
		}
		inputContents = append(inputContents, &anthropicTextGenerationInputMessage{
			Role:    role,
			Content: content,
		})
	}
	return inputContents, systemPrompt, nil
}

// process the role of the message to anthropic supported role.
func getAnthropicRole(role llms.Role) (string, error) {
	switch role {
	case llms.RoleSystem:
		return AnthropicSystem, nil

	case llms.RoleAI:
		return AnthropicRoleAssistant, nil

	case llms.RoleGeneric:
		fallthrough
	case llms.RoleHuman:
		return AnthropicRoleUser, nil
	case llms.RoleTool:
		return AnthropicRoleUser, nil
	default:
		return "", errors.New("role not supported")
	}
}

func getAnthropicInputContent(message Message) anthropicTextGenerationInputContent {
	var c anthropicTextGenerationInputContent
	switch message.Type {
	case AnthropicMessageTypeText:
		c = anthropicTextGenerationInputContent{
			Type: message.Type,
			Text: message.Content,
		}
	case AnthropicMessageTypeImage:
		c = anthropicTextGenerationInputContent{
			Type: message.Type,
			Source: &anthropicBinGenerationInputSource{
				Type:      "base64",
				MediaType: message.MimeType,
				Data:      base64.StdEncoding.EncodeToString([]byte(message.Content)),
			},
		}
	case AnthropicMessageTypeToolUse:
		// Handle tool use (from AI messages)
		var input any
		if message.ToolInput != "" {
			_ = json.Unmarshal([]byte(message.ToolInput), &input)
		}
		c = anthropicTextGenerationInputContent{
			Type:  message.Type,
			ID:    message.ToolCallID,
			Name:  message.ToolName,
			Input: input,
		}
	case AnthropicMessageTypeToolResult:
		// Handle tool result (from tool response messages)
		c = anthropicTextGenerationInputContent{
			Type:      message.Type,
			ToolUseID: message.ToolCallID,
			Content:   message.Content,
			IsError:   false, // TODO: Add error handling
		}
	}
	return c
}
