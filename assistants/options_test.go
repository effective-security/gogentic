package assistants_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/model"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func Test_ChainCallOptions(t *testing.T) {
	t.Parallel()

	// Test the default values of ChainCallOptions
	cfg := assistants.NewConfig(assistants.WithJSONMode(false), assistants.WithMode(encoding.ModePlainText))
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
	// Only StreamingFunc is set
	assert.Equal(t, 1, len(llmOpts))

	cfg = assistants.NewConfig(
		assistants.WithModel("gpt-3.5-turbo"),
		assistants.WithMode(encoding.ModeJSON),
		assistants.WithJSONMode(true),
		assistants.WithMaxTokens(100),
		assistants.WithTemperature(0.7),
		assistants.WithStopWords([]string{"foo", "bar"}),
		assistants.WithTopK(10),
		assistants.WithTopP(0.9),
		assistants.WithSeed(42),
		assistants.WithMinLength(5),
		assistants.WithMaxLength(200),
		assistants.WithRepetitionPenalty(1.2),
		assistants.WithStreamingFunc(func(context.Context, []byte) error {
			// Handle streaming response
			return nil
		}),
		assistants.WithTool(llms.Tool{
			Type: "tool2",
		}),
		assistants.WithTools([]llms.Tool{
			{
				Type: "tool1",
			},
		}),
		assistants.WithToolChoice("tool1"),
		assistants.WithExamples(model.FewShotExamples{
			{
				Prompt:     "example prompt",
				Completion: "example answer",
			},
		}),
		assistants.WithCallback(nil),
		assistants.WithPromptInput(map[string]any{"Input": "input"}),
		//assistants.WithCallback(callbacks.StreamLogHandler{}),
	)
	llmOpts = cfg.GetCallOptions()
	assert.Equal(t, 14, len(llmOpts))
}
