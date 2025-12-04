package llms_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestOptions(t *testing.T) {
	streamingFunc := func(ctx context.Context, chunk []byte) error {
		return nil
	}
	streamingReasoningFunc := func(ctx context.Context, reasoningChunk, chunk []byte) error {
		return nil
	}
	tools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name: "test",
			},
		},
	}
	meta := map[string]any{"test": "test"}
	rf := &schema.ResponseFormat{
		Type: "json",
	}
	stopWords := []string{"stop"}
	opts := []llms.CallOption{
		llms.WithModel("test"),
		llms.WithPromptCacheMode(llms.PromptCacheModeInMemory),
		llms.WithPromptCacheKey("test"),
		llms.WithMaxTokens(100),
		llms.WithTemperature(0.5),
		llms.WithStopWords(stopWords),
		llms.WithStreamingFunc(streamingFunc),
		llms.WithStreamingReasoningFunc(streamingReasoningFunc),
		llms.WithTopK(10),
		llms.WithTopP(0.5),
		llms.WithSeed(123),
		llms.WithMinLength(10),
		llms.WithMaxLength(100),
		llms.WithN(1),
		llms.WithRepetitionPenalty(0.5),
		llms.WithFrequencyPenalty(0.5),
		llms.WithPresencePenalty(0.5),
		llms.WithTools(tools),
		llms.WithToolChoice("test"),
		llms.WithMetadata(meta),
		llms.WithResponseFormat(rf),
		llms.WithReasoningEffort(llms.ReasoningEffortLow),
	}

	var cfg llms.CallOptions
	for _, opt := range opts {
		opt(&cfg)
	}

	expected := llms.CallOptions{
		Model:                  "test",
		PromptCacheMode:        llms.PromptCacheModeInMemory,
		PromptCacheKey:         "test",
		MaxTokens:              100,
		Temperature:            0.5,
		StopWords:              stopWords,
		StreamingFunc:          streamingFunc,
		StreamingReasoningFunc: streamingReasoningFunc,
		TopK:                   10,
		TopP:                   0.5,
		Seed:                   123,
		MinLength:              10,
		MaxLength:              100,
		N:                      1,
		RepetitionPenalty:      0.5,
		FrequencyPenalty:       0.5,
		PresencePenalty:        0.5,
		Tools:                  tools,
		ToolChoice:             "test",
		Metadata:               meta,
		ResponseFormat:         rf,
		ReasoningEffort:        llms.ReasoningEffortLow,
	}
	assert.Equal(t, llmutils.ToJSON(&expected), llmutils.ToJSON(&cfg))
}
