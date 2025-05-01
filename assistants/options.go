package assistants

import (
	"context"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/tmc/langchaingo/llms"
)

// Option is a function that can be used to modify the behavior of the Agent Config.
type Option func(*Config)

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

	JSONMode bool

	//
	// Below are the options for the Agent, not related to LLM call
	//

	// StreamingFunc is a function to be called for each chunk of a streaming response.
	// Return an error to stop streaming early.
	StreamingFunc func(ctx context.Context, chunk []byte) error

	PromptInput        map[string]any
	Examples           chatmodel.FewShotExamples
	Mode               encoding.Mode
	SkipMessageHistory bool
}

func NewConfig(opts ...Option) *Config {
	cfg := &Config{
		Mode:     encoding.ModeDefault,
		JSONMode: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// WithMode is an option that allows to specify the encoding mode.
func WithMode(mode encoding.Mode) Option {
	return func(o *Config) {
		o.Mode = mode
		if mode == encoding.ModeJSON || mode == encoding.ModeJSONStrict || mode == encoding.ModeJSONSchema {
			o.JSONMode = true
		} else {
			o.JSONMode = false
		}
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

// WithJSONMode is an option for LLM.Call that allows the user to specify whether to use JSON mode.
func WithJSONMode(jsonMode bool) Option {
	return func(o *Config) {
		o.JSONMode = jsonMode
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
		o.Tools = tools
		o.toolsSet = true
	}
}

// WithTool is an option for LLM.Call.
func WithTool(tool llms.Tool) Option {
	return func(o *Config) {
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

func (c *Config) GetCallOptions(options ...Option) []llms.CallOption {
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
	if c.toolsSet {
		chainCallOption = append(chainCallOption, llms.WithTools(c.Tools))
	}
	if c.toolChoiceSet {
		chainCallOption = append(chainCallOption, llms.WithToolChoice(c.ToolChoice))
	}
	if c.JSONMode {
		chainCallOption = append(chainCallOption, llms.WithJSONMode())
	}

	chainCallOption = append(chainCallOption, llms.WithStreamingFunc(c.StreamingFunc))

	return chainCallOption
}
