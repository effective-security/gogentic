package assistants_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func Test_ChainCallOptions(t *testing.T) {
	t.Parallel()

	// Test the default values of ChainCallOptions
	cfg := assistants.NewConfig()
	assert.Equal(t, "", cfg.Model)
	assert.Equal(t, 0, cfg.MaxTokens)
	assert.Equal(t, 0.0, cfg.Temperature)
	assert.Empty(t, cfg.StopWords)
	assert.Nil(t, cfg.StreamingFunc)
	assert.Equal(t, 0, cfg.TopK)
	assert.Equal(t, 0.0, cfg.TopP)
	assert.Equal(t, 0, cfg.Seed)
	assert.Equal(t, 0, cfg.MinLength)
	assert.Equal(t, 0, cfg.MaxLength)
	assert.Empty(t, cfg.Tools)
	assert.Nil(t, cfg.ToolChoice)
	assert.Nil(t, cfg.CallbackHandler)

	llmOpts := cfg.GetCallOptions()
	assert.Equal(t, 0, len(llmOpts))

	cfg = assistants.NewConfig(
		assistants.WithModel("gpt-3.5-turbo"),
		assistants.WithResponseFormat(&schema.ResponseFormat{
			Type: "json_schema",
		}),
		assistants.WithMaxTokens(100),
		assistants.WithTemperature(0.7),
		assistants.WithStopWords([]string{"foo", "bar"}),
		assistants.WithTopK(10),
		assistants.WithTopP(0.9),
		assistants.WithSeed(42),
		assistants.WithMinLength(5),
		assistants.WithMaxLength(200),
		assistants.WithRepetitionPenalty(1.2),
		assistants.WithMaxToolCalls(10),
		assistants.WithMaxMessages(100),
		assistants.WithEnableFunctionCalls(true),
		assistants.WithGeneric(true),
		assistants.WithSkipMessageHistory(true),
		assistants.WithPromptInput(map[string]any{"Input": "input"}),
		assistants.WithStreamingFunc(func(context.Context, []byte) error {
			// Handle streaming response
			return nil
		}),
		assistants.WithTool(llms.Tool{
			Type: "tool2",
		}),
		assistants.WithTool(llms.Tool{
			Type: "tool1",
		}),
		assistants.WithTools([]llms.Tool{
			{
				Type: "tool1",
			},
		}),
		// add again
		assistants.WithTools([]llms.Tool{
			{
				Type: "tool1",
			},
		}),
		assistants.WithToolChoice("tool1"),
		assistants.WithExamples(chatmodel.FewShotExamples{
			{
				Prompt:     "example prompt",
				Completion: "example answer",
			},
		}),
		assistants.WithCallback(nil),
		assistants.WithPromptInput(map[string]any{"Input": "input"}),
		//assistants.WithCallback(callbacks.StreamLogHandler{}),
		assistants.WithReasoningEffort(llms.ReasoningEffortLow),
		assistants.WithPromptCacheMode(llms.PromptCacheModeInMemory),
		assistants.WithPromptCacheKey("test"),
	)
	llmOpts = cfg.GetCallOptions()
	assert.Equal(t, 17, len(llmOpts))
}
