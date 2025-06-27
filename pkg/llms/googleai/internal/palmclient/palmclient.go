package palmclient

import (
	"context"
	"fmt"
	"runtime"

	aiplatform "cloud.google.com/go/aiplatform/apiv1"
	"cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	// ErrMissingValue is returned when a value is missing.
	ErrMissingValue = errors.New("missing value")
	// ErrInvalidValue is returned when a value is invalid.
	ErrInvalidValue = errors.New("invalid value")
)

var defaultParameters = map[string]any{
	"temperature":     0.2, //nolint:gomnd
	"maxOutputTokens": 256, //nolint:gomnd
	"topP":            0.8, //nolint:gomnd
	"topK":            40,  //nolint:gomnd
}

const (
	embeddingModelName = "textembedding-gecko"
	TextModelName      = "text-bison"
	ChatModelName      = "chat-bison"

	defaultMaxConns = 4
)

// PaLMClient represents a Vertex AI based PaLM API client.
type PaLMClient struct {
	client    *aiplatform.PredictionClient
	projectID string
}

// New returns a new Vertex AI based PaLM API client.
func New(ctx context.Context, projectID, location string, opts ...option.ClientOption) (*PaLMClient, error) {
	numConns := runtime.GOMAXPROCS(0)
	if numConns > defaultMaxConns {
		numConns = defaultMaxConns
	}
	o := []option.ClientOption{
		option.WithGRPCConnectionPool(numConns),
		option.WithEndpoint(fmt.Sprintf("%s-aiplatform.googleapis.com:443", location)),
	}
	opts = append(o, opts...)
	// PredictionClient only support GRPC.
	opts = append(opts, option.WithHTTPClient(nil))

	client, err := aiplatform.NewPredictionClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &PaLMClient{
		client:    client,
		projectID: projectID,
	}, nil
}

// ErrEmptyResponse is returned when the OpenAI API returns an empty response.
var ErrEmptyResponse = errors.New("empty response")

// CompletionRequest is a request to create a completion.
type CompletionRequest struct {
	Prompts       []string `json:"prompts"`
	MaxTokens     int      `json:"max_tokens"`
	Temperature   float64  `json:"temperature"`
	TopP          int      `json:"top_p,omitempty"`
	TopK          int      `json:"top_k,omitempty"`
	StopSequences []string `json:"stop_sequences"`
}

// Completion is a completion.
type Completion struct {
	Text string `json:"text"`
}

// CreateCompletion creates a completion.
func (c *PaLMClient) CreateCompletion(ctx context.Context, r *CompletionRequest) ([]*Completion, error) {
	params := map[string]any{
		"maxOutputTokens": r.MaxTokens,
		"temperature":     r.Temperature,
		"top_p":           r.TopP,
		"top_k":           r.TopK,
		"stopSequences":   convertArray(r.StopSequences),
	}
	predictions, err := c.batchPredict(ctx, TextModelName, r.Prompts, params)
	if err != nil {
		return nil, err
	}
	completions := []*Completion{}
	for _, p := range predictions {
		value := p.GetStructValue().AsMap()
		text, ok := value["content"].(string)
		if !ok {
			return nil, errors.WithMessage(ErrMissingValue, "unexpected content type")
		}
		completions = append(completions, &Completion{
			Text: text,
		})
	}
	return completions, nil
}

// EmbeddingRequest is a request to create an embedding.
type EmbeddingRequest struct {
	Input []string `json:"input"`
}

// CreateEmbedding creates embeddings.
func (c *PaLMClient) CreateEmbedding(ctx context.Context, r *EmbeddingRequest) ([][]float32, error) {
	params := map[string]any{}
	responses, err := c.batchPredict(ctx, embeddingModelName, r.Input, params)
	if err != nil {
		return nil, err
	}

	embeddings := [][]float32{}
	for _, res := range responses {
		value := res.GetStructValue().AsMap()
		embedding, ok := value["embeddings"].(map[string]any)
		if !ok {
			return nil, errors.WithMessage(ErrMissingValue, "unexpected embeddings type")
		}
		values, ok := embedding["values"].([]any)
		if !ok {
			return nil, errors.WithMessage(ErrMissingValue, "unexpected values type")
		}
		floatValues := []float32{}
		for _, v := range values {
			val, ok := v.(float32)
			if !ok {
				valF64, ok := v.(float64)
				if !ok {
					return nil, errors.WithMessagef(ErrInvalidValue, "values is not a float64 or float32, it is a %T", v)
				}
				val = float32(valF64)
			}
			floatValues = append(floatValues, val)
		}
		embeddings = append(embeddings, floatValues)
	}
	return embeddings, nil
}

// ChatRequest is a request to create an embedding.
type ChatRequest struct {
	Context        string         `json:"context"`
	Messages       []*ChatMessage `json:"messages"`
	Temperature    float64        `json:"temperature"`
	TopP           int            `json:"top_p,omitempty"`
	TopK           int            `json:"top_k,omitempty"`
	CandidateCount int            `json:"candidate_count,omitempty"`
}

// ChatMessage is a message in a chat.
type ChatMessage struct {
	// The content of the message.
	Content string `json:"content"`
	// The name of the author of this message. user or bot
	Author string `json:"author,omitempty"`
}

// Statically assert that the types implement the interface.
var _ llms.ChatMessage = ChatMessage{}

// GetType returns the type of the message.
func (m ChatMessage) GetType() llms.ChatMessageType {
	switch m.Author {
	case "user":
		return llms.ChatMessageTypeHuman
	default:
		return llms.ChatMessageTypeAI
	}
}

// GetText returns the text of the message.
func (m ChatMessage) GetContent() string {
	return m.Content
}

// ChatResponse is a response to a chat request.
type ChatResponse struct {
	Candidates []ChatMessage
}

// CreateChat creates chat request.
func (c *PaLMClient) CreateChat(ctx context.Context, r *ChatRequest) (*ChatResponse, error) {
	responses, err := c.chat(ctx, r)
	if err != nil {
		return nil, err
	}
	chatResponse := &ChatResponse{}
	res := responses[0]
	value := res.GetStructValue().AsMap()
	candidates, ok := value["candidates"].([]any)
	if !ok {
		return nil, errors.WithMessage(ErrMissingValue, "unexpected candidates type")
	}
	for _, c := range candidates {
		candidate, ok := c.(map[string]any)
		if !ok {
			return nil, errors.WithMessage(ErrInvalidValue, "unexpected candidate type")
		}
		author, ok := candidate["author"].(string)
		if !ok {
			return nil, errors.WithMessage(ErrInvalidValue, "unexpected author type")
		}
		content, ok := candidate["content"].(string)
		if !ok {
			return nil, errors.WithMessage(ErrInvalidValue, "unexpected content type")
		}
		chatResponse.Candidates = append(chatResponse.Candidates, ChatMessage{
			Author:  author,
			Content: content,
		})
	}
	return chatResponse, nil
}

func mergeParams(defaultParams, params map[string]any) *structpb.Struct {
	mergedParams := cloneDefaultParameters()
	for paramKey, paramValue := range params {
		switch value := paramValue.(type) {
		case float64:
			if value != 0 {
				mergedParams[paramKey] = value
			}
		case int:
		case int32:
		case int64:
			if value != 0 {
				mergedParams[paramKey] = value
			}
		case []any:
			mergedParams[paramKey] = value
		}
	}
	return convertToOutputStruct(defaultParams, mergedParams)
}

func convertToOutputStruct(defaultParams map[string]any, mergedParams map[string]any) *structpb.Struct {
	smergedParams, err := structpb.NewStruct(mergedParams)
	if err != nil {
		smergedParams, _ = structpb.NewStruct(defaultParams)
		return smergedParams
	}
	return smergedParams
}

func cloneDefaultParameters() map[string]any {
	mergedParams := map[string]any{}
	for paramKey, paramValue := range defaultParameters {
		mergedParams[paramKey] = paramValue
	}
	return mergedParams
}

func convertArray(value []string) any {
	newArray := make([]any, len(value))
	for i, v := range value {
		newArray[i] = v
	}
	return newArray
}

func (c *PaLMClient) batchPredict(ctx context.Context, model string, prompts []string, params map[string]any) ([]*structpb.Value, error) { //nolint:lll
	mergedParams := mergeParams(defaultParameters, params)
	instances := []*structpb.Value{}
	for _, prompt := range prompts {
		content, _ := structpb.NewStruct(map[string]any{
			"content": prompt,
		})
		instances = append(instances, structpb.NewStructValue(content))
	}
	resp, err := c.client.Predict(ctx, &aiplatformpb.PredictRequest{
		Endpoint:   c.projectLocationPublisherModelPath(c.projectID, "us-central1", "google", model),
		Instances:  instances,
		Parameters: structpb.NewStructValue(mergedParams),
	})
	if err != nil {
		return nil, err
	}
	if len(resp.GetPredictions()) == 0 {
		return nil, ErrEmptyResponse
	}
	return resp.GetPredictions(), nil
}

func (c *PaLMClient) chat(ctx context.Context, r *ChatRequest) ([]*structpb.Value, error) {
	params := map[string]any{
		"temperature": r.Temperature,
		"top_p":       r.TopP,
		"top_k":       r.TopK,
	}
	mergedParams := mergeParams(defaultParameters, params)
	messages := []any{}
	for _, msg := range r.Messages {
		msgMap := map[string]any{
			"author":  msg.Author,
			"content": msg.Content,
		}
		messages = append(messages, msgMap)
	}
	instance, err := structpb.NewStruct(map[string]any{
		"context":  r.Context,
		"messages": messages,
	})
	if err != nil {
		return nil, err
	}
	instances := []*structpb.Value{
		structpb.NewStructValue(instance),
	}
	resp, err := c.client.Predict(ctx, &aiplatformpb.PredictRequest{
		Endpoint:   c.projectLocationPublisherModelPath(c.projectID, "us-central1", "google", ChatModelName),
		Instances:  instances,
		Parameters: structpb.NewStructValue(mergedParams),
	})
	if err != nil {
		return nil, err
	}
	if len(resp.GetPredictions()) == 0 {
		return nil, ErrEmptyResponse
	}
	return resp.GetPredictions(), nil
}

func (c *PaLMClient) projectLocationPublisherModelPath(projectID, location, publisher, model string) string {
	return fmt.Sprintf("projects/%s/locations/%s/publishers/%s/models/%s", projectID, location, publisher, model)
}
