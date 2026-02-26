package llms_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	promptCachePolicy := &llms.PromptCachePolicy{
		Request: &llms.PromptCacheRequestPolicy{
			Key:       "test",
			Retention: llms.PromptCacheRetentionInMemory,
		},
	}
	opts := []llms.CallOption{
		llms.WithModel("test"),
		llms.WithPromptCachePolicy(promptCachePolicy),
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
		PromptCachePolicy:      promptCachePolicy,
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

func TestWithPromptCachePolicy(t *testing.T) {
	t.Parallel()

	policy := &llms.PromptCachePolicy{
		Request: &llms.PromptCacheRequestPolicy{
			Key:       "cache-key",
			Retention: llms.PromptCacheRetentionInMemory,
		},
		Breakpoints: []llms.PromptCacheBreakpoint{
			{
				Target: llms.PromptCacheTarget{
					Kind:         llms.PromptCacheTargetMessagePart,
					MessageIndex: 0,
					PartIndex:    1,
				},
				TTL: llms.PromptCacheTTL5m,
			},
		},
	}

	var cfg llms.CallOptions
	llms.WithPromptCachePolicy(policy)(&cfg)

	require.NotNil(t, cfg.PromptCachePolicy)
	assert.Same(t, policy, cfg.PromptCachePolicy)
	assert.Equal(t, "cache-key", cfg.PromptCachePolicy.Request.Key)
	assert.Equal(t, llms.PromptCacheRetentionInMemory, cfg.PromptCachePolicy.Request.Retention)
	require.Len(t, cfg.PromptCachePolicy.Breakpoints, 1)
	assert.Equal(t, llms.PromptCacheTargetMessagePart, cfg.PromptCachePolicy.Breakpoints[0].Target.Kind)
}
