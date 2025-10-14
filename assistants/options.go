package assistants

import (
	"context"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/gogentic/store"
)

// Option is a function that can be used to modify the behavior of the Agent Config.
type Option func(*Config)

const (
	DefaultMaxToolCalls   = 50
	DefaultMaxMessages    = 100
	DefaultMaxContentSize = 500000
	DefaultMaxRetries     = 2
)

type Config struct {
	// Model is the model to use in an LLM call.
	Model    string
	modelSet bool

	// MaxTokens is the maximum number of tokens to generate to use in an LLM call.
	MaxTokens    int
	maxTokensSet bool

	// Temperature is the temperature for sampling to use in an LLM call, between 0 and 1.
	Temperature    float64
	temperatureSet bool

	// StopWords is a list of words to stop on to use in an LLM call.
	StopWords    []string
	stopWordsSet bool

	// TopK is the number of tokens to consider for top-k sampling in an LLM call.
	TopK    int
	topkSet bool

	// TopP is the cumulative probability for top-p sampling in an LLM call.
	TopP    float64
	toppSet bool

	// Seed is a seed for deterministic sampling in an LLM call.
	Seed    int
	seedSet bool

	// MinLength is the minimum length of the generated text in an LLM call.
	MinLength    int
	minLengthSet bool

	// MaxLength is the maximum length of the generated text in an LLM call.
	MaxLength    int
	maxLengthSet bool

	// RepetitionPenalty is the repetition penalty for sampling in an LLM call.
	RepetitionPenalty    float64
	repetitionPenaltySet bool

	// CallbackHandler is the callback handler for Chain
	CallbackHandler Callback

	// Tools is a list of tools to use. Each tool can be a specific tool or a function.
	Tools    []llms.Tool
	toolsSet bool

	// ToolChoice is the choice of tool to use, it can either be "none", "auto" (the default behavior), or a specific tool as described in the ToolChoice type.
	ToolChoice    any
	toolChoiceSet bool

	// ResponseFormat is a custom response format.
	// If it's not set the response MIME type is text/plain.
	// Otherwise, from response format the JSON mode is derived.
	ResponseFormat *schema.ResponseFormat

	//
	// Below are the options for the Agent, not related to LLM call
	//

	// StreamingFunc is a function to be called for each chunk of a streaming response.
	// Return an error to stop streaming early.
	StreamingFunc func(ctx context.Context, chunk []byte) error

	Store       store.MessageStore
	PromptInput map[string]any
	Examples    chatmodel.FewShotExamples
	// Mode is the encoding mode to use.
	// If ModeJSON then JSON schema instructions are added to the system prompt.
	// If ModeJSONSchema or ModeJSONSchemaStrict and the Model supports it,
	// then the response format is set to json_object.
	Mode encoding.Mode
	// SkipMessageHistory is a flag to skip adding Assistant messages to History.
	SkipMessageHistory bool
	// SkipToolHistory is a flag to skip adding Tool messages to History.
	SkipToolHistory bool
	// IsGeneric is a flag to indicate that the assistant should add a generic message to the history,
	// instead of the human
	IsGeneric bool
	// EnableFunctionCalls is a flag to indicate that the assistant should enable legacy function calls.
	EnableFunctionCalls bool
	// MaxToolCalls is the maximum number of tool calls per run.
	MaxToolCalls int
	// MaxMessages is the maximum number of messages per run.
	MaxMessages int
}

func NewConfig(opts ...Option) *Config {
	cfg := &Config{
		Mode:         encoding.ModeDefault,
		MaxToolCalls: DefaultMaxToolCalls,
		MaxMessages:  DefaultMaxMessages,
	}
	return cfg.Apply(opts...)
}

// Apply applies the options to the Config.
func (c *Config) Apply(opts ...Option) *Config {
	cfg := *c
	for _, opt := range opts {
		opt(&cfg)
	}
	return &cfg
}

func WithResponseFormat(responseFormat *schema.ResponseFormat) Option {
	return func(o *Config) {
		o.ResponseFormat = responseFormat
	}
}

func WithMaxToolCalls(maxToolCalls int) Option {
	return func(o *Config) {
		o.MaxToolCalls = maxToolCalls
	}
}

func WithMaxMessages(maxMessages int) Option {
	return func(o *Config) {
		o.MaxMessages = maxMessages
	}
}

// WithMessageStore is an option that allows to specify the message store.
func WithMessageStore(store store.MessageStore) Option {
	return func(o *Config) {
		o.Store = store
	}
}

// WithMode is an option that allows to specify the encoding mode.
func WithMode(mode encoding.Mode) Option {
	return func(o *Config) {
		o.Mode = mode
	}
}

// WithEnableFunctionCalls is an option to indicate that the assistant should enable legacy function calls.
func WithEnableFunctionCalls(val bool) Option {
	return func(o *Config) {
		o.EnableFunctionCalls = val
	}
}

// WithGeneric is an option to indicate that the assistant should add a generic message to the history,
// instead of the human
func WithGeneric(val bool) Option {
	return func(o *Config) {
		o.IsGeneric = val
	}
}

// WithSkipToolHistory is an option that allows to skip adding Tool messages to History.
func WithSkipToolHistory(skip bool) Option {
	return func(o *Config) {
		o.SkipToolHistory = skip
	}
}

// WithExamples is an option that allows to specify the few-shot examples for the system prompt.
func WithExamples(examples chatmodel.FewShotExamples) Option {
	return func(o *Config) {
		o.Examples = examples
	}
}

// WithSkipMessageHistory is an option that allows to skip adding Assistant messages to History.
func WithSkipMessageHistory(skip bool) Option {
	return func(o *Config) {
		o.SkipMessageHistory = skip
	}
}

// WithPromptInput is an option that allows the user to specify the system prompt input.
func WithPromptInput(input map[string]any) Option {
	return func(o *Config) {
		o.PromptInput = input
	}
}

// WithModel is an option for LLM.Call.
func WithModel(model string) Option {
	return func(o *Config) {
		o.Model = model
		o.modelSet = true
	}
}

// WithMaxTokens is an option for LLM.Call.
func WithMaxTokens(maxTokens int) Option {
	return func(o *Config) {
		o.MaxTokens = maxTokens
		o.maxTokensSet = true
	}
}

// WithTemperature is an option for LLM.Call.
func WithTemperature(temperature float64) Option {
	return func(o *Config) {
		o.Temperature = temperature
		o.temperatureSet = true
	}
}

// WithStreamingFunc is an option for LLM.Call that allows streaming responses.
func WithStreamingFunc(streamingFunc func(ctx context.Context, chunk []byte) error) Option {
	return func(o *Config) {
		o.StreamingFunc = streamingFunc
	}
}

// WithTopK will add an option to use top-k sampling for LLM.Call.
func WithTopK(topK int) Option {
	return func(o *Config) {
		o.TopK = topK
		o.topkSet = true
	}
}

// WithTopP	will add an option to use top-p sampling for LLM.Call.
func WithTopP(topP float64) Option {
	return func(o *Config) {
		o.TopP = topP
		o.toppSet = true
	}
}

// WithSeed will add an option to use deterministic sampling for LLM.Call.
func WithSeed(seed int) Option {
	return func(o *Config) {
		o.Seed = seed
		o.seedSet = true
	}
}

// WithMinLength will add an option to set the minimum length of the generated text for LLM.Call.
func WithMinLength(minLength int) Option {
	return func(o *Config) {
		o.MinLength = minLength
		o.minLengthSet = true
	}
}

// WithMaxLength will add an option to set the maximum length of the generated text for LLM.Call.
func WithMaxLength(maxLength int) Option {
	return func(o *Config) {
		o.MaxLength = maxLength
		o.maxLengthSet = true
	}
}

// WithRepetitionPenalty will add an option to set the repetition penalty for sampling.
func WithRepetitionPenalty(repetitionPenalty float64) Option {
	return func(o *Config) {
		o.RepetitionPenalty = repetitionPenalty
		o.repetitionPenaltySet = true
	}
}

// WithStopWords is an option for setting the stop words for LLM.Call.
func WithStopWords(stopWords []string) Option {
	return func(o *Config) {
		o.StopWords = stopWords
		o.stopWordsSet = true
	}
}

// WithCallback allows setting a custom Callback Handler.
func WithCallback(callbackHandler Callback) Option {
	return func(o *Config) {
		o.CallbackHandler = callbackHandler
	}
}

// WithTools is an option for LLM.Call.
func WithTools(tools []llms.Tool) Option {
	return func(o *Config) {
		if len(tools) > 0 {
			// Create a map to track existing tool identifiers for deduplication
			existingTools := make(map[string]bool)
			for _, existingTool := range o.Tools {
				key := getToolKey(existingTool)
				existingTools[key] = true
			}

			// Add only unique tools
			for _, tool := range tools {
				key := getToolKey(tool)
				if !existingTools[key] {
					o.Tools = append(o.Tools, tool)
					existingTools[key] = true
				}
			}
			o.toolsSet = true
		}
	}
}

// getToolKey returns a unique identifier for a tool based on its Type and Function.Name
func getToolKey(tool llms.Tool) string {
	if tool.Function != nil && tool.Function.Name != "" {
		return tool.Type + ":" + tool.Function.Name
	}
	return tool.Type
}

// WithTool is an option for LLM.Call.
func WithTool(tool llms.Tool) Option {
	return func(o *Config) {
		key := getToolKey(tool)
		for _, existingTool := range o.Tools {
			if getToolKey(existingTool) == key {
				return // Tool already exists, don't add it
			}
		}

		o.Tools = append(o.Tools, tool)
		o.toolsSet = true
	}
}

// WithToolChoice is an option for LLM.Call.
func WithToolChoice(choice any) Option {
	return func(o *Config) {
		o.ToolChoice = choice
		o.toolChoiceSet = true
	}
}

func (cfg *Config) GetCallOptions(options ...Option) []llms.CallOption {
	c := *cfg
	for _, opt := range options {
		opt(&c)
	}

	var chainCallOption []llms.CallOption
	if c.modelSet {
		chainCallOption = append(chainCallOption, llms.WithModel(c.Model))
	}
	if c.maxTokensSet {
		chainCallOption = append(chainCallOption, llms.WithMaxTokens(c.MaxTokens))
	}
	if c.temperatureSet {
		chainCallOption = append(chainCallOption, llms.WithTemperature(c.Temperature))
	}
	if c.stopWordsSet {
		chainCallOption = append(chainCallOption, llms.WithStopWords(c.StopWords))
	}
	if c.topkSet {
		chainCallOption = append(chainCallOption, llms.WithTopK(c.TopK))
	}
	if c.toppSet {
		chainCallOption = append(chainCallOption, llms.WithTopP(c.TopP))
	}
	if c.seedSet {
		chainCallOption = append(chainCallOption, llms.WithSeed(c.Seed))
	}
	if c.minLengthSet {
		chainCallOption = append(chainCallOption, llms.WithMinLength(c.MinLength))
	}
	if c.maxLengthSet {
		chainCallOption = append(chainCallOption, llms.WithMaxLength(c.MaxLength))
	}
	if c.repetitionPenaltySet {
		chainCallOption = append(chainCallOption, llms.WithRepetitionPenalty(c.RepetitionPenalty))
	}
	if c.toolsSet && len(c.Tools) > 0 {
		chainCallOption = append(chainCallOption, llms.WithTools(c.Tools))
	}
	if c.toolChoiceSet {
		chainCallOption = append(chainCallOption, llms.WithToolChoice(c.ToolChoice))
	}
	if c.ResponseFormat != nil {
		chainCallOption = append(chainCallOption, llms.WithResponseFormat(c.ResponseFormat))
	}

	if c.StreamingFunc != nil {
		chainCallOption = append(chainCallOption, llms.WithStreamingFunc(c.StreamingFunc))
	}

	return chainCallOption
}
