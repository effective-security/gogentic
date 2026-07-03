package assistants_test

import (
	"context"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/mocks/mockllms"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func Test_ChainCallOptions(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockLLM := mockllms.NewMockModel(ctrl)
	mockLLM.EXPECT().GetName().Return("gpt-3.5-turbo").Times(1)
	mockLLM.EXPECT().GetProviderType().Return(llms.ProviderOpenAI).Times(1)

	// Test the default values of ChainCallOptions
	cfg := assistants.NewConfig()
	assert.Equal(t, "", cfg.ModelName)
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
		assistants.WithModel(mockLLM),
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
		assistants.WithTools(llms.Tool{
			Type: "tool2",
		}),
		assistants.WithTools(llms.Tool{
			Type: "tool1",
		}),
		assistants.WithTools(llms.Tool{
			Type: "tool1",
		}),
		// add again
		assistants.WithTools(llms.Tool{
			Type: "tool1",
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
		assistants.WithPromptCachePolicy(&llms.PromptCachePolicy{
			Request: &llms.PromptCacheRequestPolicy{
				Key:       "test",
				Retention: llms.PromptCacheRetentionInMemory,
			},
		}),
	)
	llmOpts = cfg.GetCallOptions()
	assert.Equal(t, 16, len(llmOpts))
}

func Test_ChainCallOptions_PromptCachePolicy(t *testing.T) {
	t.Parallel()

	policy := &llms.PromptCachePolicy{
		Request: &llms.PromptCacheRequestPolicy{
			Key:       "policy-key",
			Retention: llms.PromptCacheRetention24h,
		},
	}

	cfg := assistants.NewConfig(
		assistants.WithPromptCachePolicy(policy),
	)

	var got llms.CallOptions
	for _, opt := range cfg.GetCallOptions() {
		opt(&got)
	}

	require.NotNil(t, got.PromptCachePolicy)
	assert.Same(t, policy, got.PromptCachePolicy)
}

func Test_Config_AddTool(t *testing.T) {
	t.Parallel()

	cfg := assistants.NewConfig()

	fnTool := llms.Tool{Type: "function", Function: &llms.FunctionDefinition{Name: "search"}}
	cfg.AddTool(fnTool)
	// duplicate of the same type+name is ignored
	cfg.AddTool(fnTool)
	// same type but a different function name is kept
	cfg.AddTool(llms.Tool{Type: "function", Function: &llms.FunctionDefinition{Name: "lookup"}})
	// type-only tool keyed by type
	cfg.AddTool(llms.Tool{Type: "web_search"})
	// duplicate type-only tool is ignored
	cfg.AddTool(llms.Tool{Type: "web_search"})

	require.Len(t, cfg.Tools, 3)
	assert.Equal(t, "search", cfg.Tools[0].Function.Name)
	assert.Equal(t, "lookup", cfg.Tools[1].Function.Name)
	assert.Equal(t, "web_search", cfg.Tools[2].Type)
}

func Test_WithTools_Dedup(t *testing.T) {
	t.Parallel()

	cfg := assistants.NewConfig(
		assistants.WithTools(
			llms.Tool{Type: "function", Function: &llms.FunctionDefinition{Name: "a"}},
			llms.Tool{Type: "function", Function: &llms.FunctionDefinition{Name: "b"}},
			// duplicate of "a"
			llms.Tool{Type: "function", Function: &llms.FunctionDefinition{Name: "a"}},
			llms.Tool{Type: "web_search"},
			// duplicate web_search
			llms.Tool{Type: "web_search"},
		),
	)

	require.Len(t, cfg.Tools, 3)
	assert.Equal(t, "a", cfg.Tools[0].Function.Name)
	assert.Equal(t, "b", cfg.Tools[1].Function.Name)
	assert.Equal(t, "web_search", cfg.Tools[2].Type)
}

func Test_WithModelOptions(t *testing.T) {
	t.Parallel()

	// mirrors the documented usage: only add top_p for non-Anthropic providers.
	onModelOptions := func(model llms.Model) []assistants.Option {
		if model.GetProviderType() != llms.ProviderAnthropic {
			return []assistants.Option{
				assistants.WithTopP(0.95),
			}
		}
		return nil
	}

	tests := []struct {
		name        string
		provider    llms.ProviderType
		wantTopPSet bool
		wantTopP    float64
	}{
		{name: "non-anthropic adds top_p", provider: llms.ProviderOpenAI, wantTopPSet: true, wantTopP: 0.95},
		{name: "anthropic skips top_p", provider: llms.ProviderAnthropic, wantTopPSet: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockLLM := mockllms.NewMockModel(ctrl)
			mockLLM.EXPECT().GetName().Return("test-model").Times(1)
			mockLLM.EXPECT().GetProviderType().Return(tc.provider).Times(1)

			cfg := assistants.NewConfig(
				assistants.WithModel(mockLLM),
				assistants.WithTemperature(0.6),
				assistants.WithModelOptions(onModelOptions),
			)

			var got llms.CallOptions
			for _, opt := range cfg.GetCallOptions() {
				opt(&got)
			}

			assert.Equal(t, 0.6, got.Temperature)
			if tc.wantTopPSet {
				assert.Equal(t, tc.wantTopP, got.TopP)
			} else {
				assert.Equal(t, 0.0, got.TopP)
			}
		})
	}
}

func Test_WithModelOptions_NoModel(t *testing.T) {
	t.Parallel()

	called := false
	cfg := assistants.NewConfig(
		assistants.WithModelOptions(func(llms.Model) []assistants.Option {
			called = true
			return []assistants.Option{assistants.WithTopP(0.5)}
		}),
	)

	var got llms.CallOptions
	for _, opt := range cfg.GetCallOptions() {
		opt(&got)
	}

	// Without a model the per-model options callback must not be invoked.
	assert.False(t, called)
	assert.Equal(t, 0.0, got.TopP)
}

func Test_GetCallOptions_WebSearchTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		provider      llms.ProviderType
		wantWebSearch int
		wantTotal     int
	}{
		{
			name:          "provider supports web_search",
			provider:      llms.ProviderOpenAI,
			wantWebSearch: 1,
			wantTotal:     2,
		},
		{
			name:          "provider without web_search",
			provider:      llms.ProviderPerplexity,
			wantWebSearch: 0,
			wantTotal:     1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			mockLLM := mockllms.NewMockModel(ctrl)
			mockLLM.EXPECT().GetName().Return("test-model").Times(1)
			mockLLM.EXPECT().GetProviderType().Return(tc.provider).Times(1)

			cfg := assistants.NewConfig(
				assistants.WithModel(mockLLM),
				assistants.WithTools(
					llms.Tool{Type: "web_search"},
					llms.Tool{Type: "function", Function: &llms.FunctionDefinition{Name: "a"}},
				),
			)

			var got llms.CallOptions
			for _, opt := range cfg.GetCallOptions() {
				opt(&got)
			}

			require.Len(t, got.Tools, tc.wantTotal)

			var webSearch int
			for _, tool := range got.Tools {
				if tool.Type == "web_search" {
					webSearch++
				}
			}
			assert.Equal(t, tc.wantWebSearch, webSearch)
		})
	}
}

func Test_GetCallOptions_WebSearchTool_NoModel(t *testing.T) {
	t.Parallel()

	cfg := assistants.NewConfig(
		assistants.WithTools(
			llms.Tool{Type: "web_search"},
			llms.Tool{Type: "function", Function: &llms.FunctionDefinition{Name: "a"}},
		),
	)

	var got llms.CallOptions
	for _, opt := range cfg.GetCallOptions() {
		opt(&got)
	}

	// Without a model the tools are passed through untouched.
	require.Len(t, got.Tools, 2)
	assert.Equal(t, "web_search", got.Tools[0].Type)
	assert.Equal(t, "function", got.Tools[1].Type)
}
