package llms

import (
	"context"

	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/invopop/jsonschema"
)

// CallOption is a function that configures a CallOptions.
type CallOption func(*CallOptions)

// CallOptions is a set of options for calling models. Not all models support
// all options.
type CallOptions struct {
	// Model is the model to use.
	Model string
	// CandidateCount is the number of response candidates to generate.
	CandidateCount int
	// MaxTokens is the maximum number of tokens to generate.
	MaxTokens int
	// Temperature is the temperature for sampling, between 0 and 1.
	Temperature float64
	// StopWords is a list of words to stop on.
	StopWords []string
	// StreamingFunc is a function to be called for each chunk of a streaming response.
	// Return an error to stop streaming early.
	StreamingFunc func(ctx context.Context, chunk []byte) error
	// StreamingReasoningFunc is a function to be called for each chunk of a streaming response.
	// Return an error to stop streaming early.
	StreamingReasoningFunc func(ctx context.Context, reasoningChunk, chunk []byte) error
	// TopK is the number of tokens to consider for top-k sampling.
	TopK int
	// TopP is the cumulative probability for top-p sampling.
	TopP float64
	// Seed is a seed for deterministic sampling.
	Seed int
	// MinLength is the minimum length of the generated text.
	MinLength int
	// MaxLength is the maximum length of the generated text.
	MaxLength int
	// N is how many chat completion choices to generate for each input message.
	N int
	// RepetitionPenalty is the repetition penalty for sampling.
	RepetitionPenalty float64
	// FrequencyPenalty is the frequency penalty for sampling.
	FrequencyPenalty float64
	// PresencePenalty is the presence penalty for sampling.
	PresencePenalty float64

	// Tools is a list of tools to use. Each tool can be a specific tool or a function.
	Tools []Tool
	// ToolChoice is the choice of tool to use, it can either be "none", "auto" (the default behavior), or a specific tool as described in the ToolChoice type.
	ToolChoice any

	// Metadata is a map of metadata to include in the request.
	// The meaning of this field is specific to the backend in use.
	Metadata map[string]any

	// ResponseFormat is a custom response format.
	// If it's not set the response MIME type is text/plain.
	// Otherwise, from response format the JSON mode is derived.
	ResponseFormat *schema.ResponseFormat

	// JSONMode is a flag to enable JSON mode.
	// JSONMode bool `json:"json"`

	// ResponseMIMEType MIME type of the generated candidate text.
	// Supported MIME types are: text/plain: (default) Text output.
	// application/json: JSON response in the response candidates.
	//ResponseMIMEType string `json:"response_mime_type,omitempty"`
}

// Tool is a tool that can be used by the model.
type Tool struct {
	// Type is the type of the tool.
	Type string `json:"type"`
	// Function is the function to call.
	Function *FunctionDefinition `json:"function,omitempty"`
}

// FunctionDefinition is a definition of a function that can be called by the model.
type FunctionDefinition struct {
	// Name is the name of the function.
	Name string `json:"name"`
	// Description is a description of the function.
	Description string `json:"description"`
	// Parameters is a list of parameters for the function.
	Parameters *jsonschema.Schema `json:"parameters,omitempty"`
	// Strict is a flag to indicate if the function should be called strictly. Only used for openai llm structured output.
	Strict bool `json:"strict,omitempty"`
}

// ToolChoice is a specific tool to use.
type ToolChoice struct {
	// Type is the type of the tool.
	Type string `json:"type"`
	// Function is the function to call (if the tool is a function).
	Function *FunctionReference `json:"function,omitempty"`
}

// FunctionReference is a reference to a function.
type FunctionReference struct {
	// Name is the name of the function.
	Name string `json:"name"`
}

// FunctionCallBehavior is the behavior to use when calling functions.
type FunctionCallBehavior string

const (
	// FunctionCallBehaviorNone will not call any functions.
	FunctionCallBehaviorNone FunctionCallBehavior = "none"
	// FunctionCallBehaviorAuto will call functions automatically.
	FunctionCallBehaviorAuto FunctionCallBehavior = "auto"
)

// WithModel specifies which model name to use.
func WithModel(model string) CallOption {
	return func(o *CallOptions) {
		o.Model = model
	}
}

// WithMaxTokens specifies the max number of tokens to generate.
func WithMaxTokens(maxTokens int) CallOption {
	return func(o *CallOptions) {
		o.MaxTokens = maxTokens
	}
}

// WithCandidateCount specifies the number of response candidates to generate.
func WithCandidateCount(c int) CallOption {
	return func(o *CallOptions) {
		o.CandidateCount = c
	}
}

// WithTemperature specifies the model temperature, a hyperparameter that
// regulates the randomness, or creativity, of the AI's responses.
func WithTemperature(temperature float64) CallOption {
	return func(o *CallOptions) {
		o.Temperature = temperature
	}
}

// WithStopWords specifies a list of words to stop generation on.
func WithStopWords(stopWords []string) CallOption {
	return func(o *CallOptions) {
		o.StopWords = stopWords
	}
}

// WithOptions specifies options.
func WithOptions(options CallOptions) CallOption {
	return func(o *CallOptions) {
		(*o) = options
	}
}

// WithStreamingFunc specifies the streaming function to use.
func WithStreamingFunc(streamingFunc func(ctx context.Context, chunk []byte) error) CallOption {
	return func(o *CallOptions) {
		o.StreamingFunc = streamingFunc
	}
}

// WithStreamingReasoningFunc specifies the streaming reasoning function to use.
func WithStreamingReasoningFunc(streamingReasoningFunc func(ctx context.Context, reasoningChunk, chunk []byte) error) CallOption {
	return func(o *CallOptions) {
		o.StreamingReasoningFunc = streamingReasoningFunc
	}
}

// WithTopK will add an option to use top-k sampling.
func WithTopK(topK int) CallOption {
	return func(o *CallOptions) {
		o.TopK = topK
	}
}

// WithTopP	will add an option to use top-p sampling.
func WithTopP(topP float64) CallOption {
	return func(o *CallOptions) {
		o.TopP = topP
	}
}

// WithSeed will add an option to use deterministic sampling.
func WithSeed(seed int) CallOption {
	return func(o *CallOptions) {
		o.Seed = seed
	}
}

// WithMinLength will add an option to set the minimum length of the generated text.
func WithMinLength(minLength int) CallOption {
	return func(o *CallOptions) {
		o.MinLength = minLength
	}
}

// WithMaxLength will add an option to set the maximum length of the generated text.
func WithMaxLength(maxLength int) CallOption {
	return func(o *CallOptions) {
		o.MaxLength = maxLength
	}
}

// WithN will add an option to set how many chat completion choices to generate for each input message.
func WithN(n int) CallOption {
	return func(o *CallOptions) {
		o.N = n
	}
}

// WithRepetitionPenalty will add an option to set the repetition penalty for sampling.
func WithRepetitionPenalty(repetitionPenalty float64) CallOption {
	return func(o *CallOptions) {
		o.RepetitionPenalty = repetitionPenalty
	}
}

// WithFrequencyPenalty will add an option to set the frequency penalty for sampling.
func WithFrequencyPenalty(frequencyPenalty float64) CallOption {
	return func(o *CallOptions) {
		o.FrequencyPenalty = frequencyPenalty
	}
}

// WithPresencePenalty will add an option to set the presence penalty for sampling.
func WithPresencePenalty(presencePenalty float64) CallOption {
	return func(o *CallOptions) {
		o.PresencePenalty = presencePenalty
	}
}

// WithToolChoice will add an option to set the choice of tool to use.
// It can either be "none", "auto" (the default behavior), or a specific tool as described in the ToolChoice type.
func WithToolChoice(choice any) CallOption {
	// TODO: Add type validation for choice.
	return func(o *CallOptions) {
		o.ToolChoice = choice
	}
}

// WithTools will add an option to set the tools to use.
func WithTools(tools []Tool) CallOption {
	return func(o *CallOptions) {
		o.Tools = tools
	}
}

// WithMetadata will add an option to set metadata to include in the request.
// The meaning of this field is specific to the backend in use.
func WithMetadata(metadata map[string]any) CallOption {
	return func(o *CallOptions) {
		o.Metadata = metadata
	}
}

// WithResponseFormat allows setting a custom response format.
// If it's not set the response MIME type is text/plain.
// Otherwise, from response format the JSON mode is derived.
func WithResponseFormat(responseFormat *schema.ResponseFormat) CallOption {
	return func(o *CallOptions) {
		o.ResponseFormat = responseFormat
	}
}
