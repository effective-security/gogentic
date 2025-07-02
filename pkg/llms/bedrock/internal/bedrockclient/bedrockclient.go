package bedrockclient

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// Client is a Bedrock client.
type Client struct {
	client *bedrockruntime.Client
}

// Message is a chunk of text or an data
// that will be sent to the provider.
//
// The provider may then transform the message to its own
// format before sending it to the LLM model API.
type Message struct {
	Role    llms.ChatMessageType
	Content string
	// Type may be "text", "image", "tool_use", "tool_result"
	Type string
	// MimeType is the MIME type
	MimeType string
	// Tool-specific fields
	ToolCallID string // For tool results
	ToolName   string // For tool use
	ToolInput  string // For tool use (JSON)
}

func getProvider(modelID string) string {
	// Handle Inference Profiles (e.g., "us.anthropic.claude-3-5-sonnet-20241022-v2:0")
	// and direct model IDs (e.g., "anthropic.claude-3-sonnet-20240229-v1:0")
	parts := strings.Split(modelID, ".")
	if len(parts) >= 2 {
		// Check if first part is a region (like "us", "eu", etc.)
		if len(parts[0]) == 2 && strings.ToLower(parts[0]) == parts[0] {
			// This looks like a region prefix, use the second part as provider
			return parts[1]
		}
		// Otherwise use the first part as provider (direct model ID)
		return parts[0]
	}
	return parts[0]
}

// NewClient creates a new Bedrock client.
func NewClient(client *bedrockruntime.Client) *Client {
	return &Client{
		client: client,
	}
}

// CreateCompletion creates a new completion response from the provider
// after sending the messages to the provider.
func (c *Client) CreateCompletion(ctx context.Context,
	modelID string,
	messages []Message,
	options llms.CallOptions,
) (*llms.ContentResponse, error) {
	provider := getProvider(modelID)
	switch provider {
	case "ai21":
		return createAi21Completion(ctx, c.client, modelID, messages, options)
	case "amazon":
		return createAmazonCompletion(ctx, c.client, modelID, messages, options)
	case "anthropic":
		return createAnthropicCompletion(ctx, c.client, modelID, messages, options)
	case "cohere":
		return createCohereCompletion(ctx, c.client, modelID, messages, options)
	case "meta":
		return createMetaCompletion(ctx, c.client, modelID, messages, options)
	default:
		return nil, errors.New("bedrock: unsupported provider")
	}
}

// Helper function to process input text chat
// messages as a single string.
func processInputMessagesGeneric(messages []Message) string {
	var sb strings.Builder
	var hasRole bool
	for _, message := range messages {
		if message.Role != "" {
			hasRole = true
			sb.WriteString("\n")
			sb.WriteString(string(message.Role))
			sb.WriteString(": ")
		}
		if message.Type == "text" {
			sb.WriteString(message.Content)
		}
	}
	if hasRole {
		sb.WriteString("\n")
		sb.WriteString("AI: ")
	}
	return sb.String()
}

func getMaxTokens(maxTokens, defaultValue int) int {
	if maxTokens <= 0 {
		return defaultValue
	}
	return maxTokens
}
