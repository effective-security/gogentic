package bedrock

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/bedrock/internal/bedrockclient"
)

const defaultModel = ModelAmazonTitanTextLiteV1

// LLM is a Bedrock LLM implementation.
type LLM struct {
	modelID string
	client  *bedrockclient.Client
}

// New creates a new Bedrock LLM implementation.
func New(opts ...Option) (*LLM, error) {
	o, c, err := newClient(opts...)
	if err != nil {
		return nil, err
	}
	return &LLM{
		client:  c,
		modelID: o.modelID,
	}, nil
}

func newClient(opts ...Option) (*options, *bedrockclient.Client, error) {
	options := &options{
		modelID: defaultModel,
	}

	for _, opt := range opts {
		opt(options)
	}

	if options.client == nil {
		cfg, err := config.LoadDefaultConfig(context.Background())
		if err != nil {
			return options, nil, err
		}
		options.client = bedrockruntime.NewFromConfig(cfg)
	}

	return options, bedrockclient.NewClient(options.client), nil
}

// GetName implements the Model interface.
func (l *LLM) GetName() string {
	return l.modelID
}

// GetProviderType implements the Model interface.
func (l *LLM) GetProviderType() llms.ProviderType {
	return llms.ProviderBedrock
}

// GenerateContent implements llms.Model.
func (l *LLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	opts := llms.CallOptions{
		Model: l.modelID,
	}
	for _, opt := range options {
		opt(&opts)
	}

	m, err := processMessages(messages)
	if err != nil {
		return nil, err
	}

	res, err := l.client.CreateCompletion(ctx, opts.Model, m, opts)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CreateEmbedding creates embeddings for the given input texts.
func (l *LLM) CreateEmbedding(ctx context.Context, texts []string) ([][]float32, error) {
	return l.client.CreateEmbedding(ctx, l.modelID, texts)
}

func processMessages(messages []llms.MessageContent) ([]bedrockclient.Message, error) {
	bedrockMsgs := make([]bedrockclient.Message, 0, len(messages))

	for _, m := range messages {
		for _, part := range m.Parts {
			switch part := part.(type) {
			case llms.TextContent:
				bedrockMsgs = append(bedrockMsgs, bedrockclient.Message{
					Role:    m.Role,
					Content: part.Text,
					Type:    "text",
				})
			case llms.BinaryContent:
				bedrockMsgs = append(bedrockMsgs, bedrockclient.Message{
					Role:     m.Role,
					Content:  string(part.Data),
					MimeType: part.MIMEType,
					Type:     "image", // TODO: wrong
				})
			case llms.ToolCall:
				// Handle tool calls from AI messages
				bedrockMsgs = append(bedrockMsgs, bedrockclient.Message{
					Role:       m.Role,
					Content:    part.ID, // Tool call ID
					Type:       "tool_use",
					ToolCallID: part.ID,
					ToolName:   part.FunctionCall.Name,
					ToolInput:  part.FunctionCall.Arguments, // JSON arguments
				})
			case llms.ToolCallResponse:
				// Handle tool call responses
				bedrockMsgs = append(bedrockMsgs, bedrockclient.Message{
					Role:       m.Role,
					Content:    part.Content,
					Type:       "tool_result",
					ToolCallID: part.ToolCallID,
				})
			default:
				return nil, errors.New("unsupported message type")
			}
		}
	}
	return bedrockMsgs, nil
}

var (
	_ llms.Model    = (*LLM)(nil)
	_ llms.Embedder = (*LLM)(nil)
)
