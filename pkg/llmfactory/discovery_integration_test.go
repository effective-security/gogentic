package llmfactory_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realKey returns the value of an environment variable that holds a provider
// key, or "" when it is unset or the fake value the Makefile exports.
func realKey(name string) string {
	key := os.Getenv(name)
	if key == "fakekey" {
		return ""
	}
	return key
}

// TestIntegrationDiscovery resolves references against the model lists the
// providers serve right now. It reads lists only, which is free of charge,
// and runs for the providers whose key is set: ANTHROPIC_API_KEY,
// OPENAI_API_KEY, GOOGLEAI_TOKEN and OPENROUTER_API_KEY.
func TestIntegrationDiscovery(t *testing.T) {
	discovery := &llmfactory.DiscoveryConfig{
		Enabled: true,
	}
	type reference struct {
		reference string
		// retired is set for a model the provider no longer serves, which
		// must be replaced by one of its family.
		retired bool
		// prefix and part are what the selected model ID must start with
		// and contain.
		prefix string
		part   string
	}
	providers := []struct {
		keyEnv     string
		config     *llmfactory.ProviderConfig
		references []reference
	}{
		{
			keyEnv: "ANTHROPIC_API_KEY",
			config: &llmfactory.ProviderConfig{
				Name:            "ANTHROPIC",
				AvailableModels: []string{"claude-*"},
				Discovery:       discovery,
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "ANTHROPIC",
				},
			},
			references: []reference{
				{reference: "ANTHROPIC/claude-sonnet-4-20250514", retired: true, prefix: "claude-sonnet-"},
				{reference: "ANTHROPIC/claude-3-5-haiku-20241022", retired: true, prefix: "claude-haiku-"},
				{reference: "ANTHROPIC/claude-opus-*,high", prefix: "claude-opus-"},
				{reference: "ANTHROPIC/claude-haiku-4-5", prefix: "claude-haiku-"},
			},
		},
		{
			keyEnv: "OPENAI_API_KEY",
			config: &llmfactory.ProviderConfig{
				Name:            "OPENAI",
				AvailableModels: []string{"gpt-*"},
				Discovery:       discovery,
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "OPENAI",
				},
			},
			references: []reference{
				{reference: "OPENAI/gpt-4.5-preview", retired: true, prefix: "gpt-"},
				{reference: "OPENAI/gpt-5*-mini", prefix: "gpt-5", part: "-mini"},
			},
		},
		{
			keyEnv: "GOOGLEAI_TOKEN",
			config: &llmfactory.ProviderConfig{
				Name:            "GOOGLE",
				AvailableModels: []string{"gemini-*"},
				DisabledModels:  []string{"*-preview*"},
				Discovery:       discovery,
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "GOOGLEAI",
				},
			},
			references: []reference{
				{reference: "GOOGLE/gemini-1.5-pro", retired: true, prefix: "gemini-", part: "pro"},
				{reference: "GOOGLE/gemini-1.5-flash", retired: true, prefix: "gemini-", part: "flash"},
				{reference: "GOOGLE/gemini-*-flash", prefix: "gemini-", part: "-flash"},
			},
		},
		{
			keyEnv: "OPENROUTER_API_KEY",
			config: &llmfactory.ProviderConfig{
				Name:            "OPENROUTER",
				AvailableModels: []string{"openai/*", "anthropic/*"},
				Discovery:       discovery,
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "OPENROUTER",
				},
			},
			references: []reference{
				{reference: "OPENROUTER/anthropic/claude-3-opus", retired: true, prefix: "anthropic/claude-opus"},
				{reference: "OPENROUTER/openai/gpt-5.*-mini", prefix: "openai/gpt-5.", part: "-mini"},
			},
		},
	}

	cfg := &llmfactory.Config{}
	var references []reference
	for _, provider := range providers {
		key := realKey(provider.keyEnv)
		if key == "" {
			continue
		}
		provider.config.Token = key
		cfg.Providers = append(cfg.Providers, provider.config)
		references = append(references, provider.references...)
	}
	if len(cfg.Providers) == 0 {
		t.Skip("no provider key is set")
	}
	require.NoError(t, cfg.Validate())

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := llmfactory.New(cfg)
	require.NoError(t, llmfactory.RefreshModels(ctx, f))

	for _, ref := range references {
		t.Run(ref.reference, func(t *testing.T) {
			res, err := llmfactory.Resolve(ctx, f, llmfactory.ModelOptions{
				PreferredModels: []string{ref.reference},
			})
			require.NoError(t, err)
			t.Logf("%-42s -> %-34s %-8s lifecycle=%-10s efforts=%v",
				ref.reference, res.Model, res.Reason, res.Lifecycle, res.Effort.Supported.Levels())

			assert.Equal(t, ref.reference, res.Reference)
			assert.False(t, res.Default, "the reference selected nothing")
			assert.True(t, res.Verified, "the model is not in the provider's list")
			assert.NotEqual(t, llms.ModelLifecycleRetired, res.Lifecycle)
			assert.True(t, strings.HasPrefix(res.Model, ref.prefix), "model %s", res.Model)
			assert.Contains(t, res.Model, ref.part)
			if ref.retired {
				assert.Equal(t, llmfactory.ReasonFamily, res.Reason)
			}
		})
	}
}

// TestIntegrationRetiredModelStillAnswers shows the incident discovery is for,
// against the live Anthropic API: a request that names a retired model fails,
// and the same reference through a factory with discovery gets an answer from
// the newest model of the family.
//
// It makes one real, billed request to the current Haiku model, with a small
// answer. The request to the retired model is rejected and costs nothing.
func TestIntegrationRetiredModelStillAnswers(t *testing.T) {
	apiKey := realKey(anthropic.TokenEnvVarName)
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	const (
		retired   = "claude-3-5-haiku-20241022"
		maxTokens = 256
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	messages := []llms.Message{{
		Role: llms.RoleHuman,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "Reply with the single word: pong"},
		},
	}}

	// Before: the provider rejects the retired model.
	direct, err := anthropic.New(anthropic.WithToken(apiKey), anthropic.WithModel(retired))
	require.NoError(t, err)
	_, err = direct.GenerateContent(ctx, messages, llms.WithMaxTokens(maxTokens))
	require.Error(t, err, "the model %s is served again; pick another retired model for this test", retired)
	t.Logf("direct call to %s: %v", retired, firstLine(err.Error()))

	// After: the same reference, resolved with the provider's model list.
	cfg := &llmfactory.Config{
		Providers: []*llmfactory.ProviderConfig{
			{
				Name:            "ANTHROPIC",
				Token:           apiKey,
				AvailableModels: []string{"claude-haiku-*,low"},
				Discovery: &llmfactory.DiscoveryConfig{
					Enabled: true,
				},
				OpenAI: llmfactory.OpenAIConfig{
					APIType: "ANTHROPIC",
				},
			},
		},
	}
	require.NoError(t, cfg.Validate())
	f := llmfactory.New(cfg)
	opts := llmfactory.ModelOptions{
		PreferredModels: []string{"ANTHROPIC/" + retired},
	}

	res, err := llmfactory.Resolve(ctx, f, opts)
	require.NoError(t, err)
	assert.Equal(t, llmfactory.ReasonFamily, res.Reason)
	assert.True(t, strings.HasPrefix(res.Model, "claude-haiku-"), "model %s", res.Model)
	assert.NotEqual(t, retired, res.Model)

	model, err := f.GetModel(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, res.Model, model.GetName())

	resp, err := model.GenerateContent(ctx, messages, llms.WithMaxTokens(maxTokens))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Choices)
	answer := strings.TrimSpace(resp.Choices[0].Content)
	t.Logf("through the factory, %s answers as %s: %q", retired, model.GetName(), answer)
	assert.Contains(t, strings.ToLower(answer), "pong")
}

// firstLine returns the first line of a message.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
