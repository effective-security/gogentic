package llms_test

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProviderType(t *testing.T) {
	tests := []struct {
		in   string
		want llms.ProviderType
	}{
		{"ANTHROPIC", llms.ProviderAnthropic},
		{"CLAUDE", llms.ProviderAnthropic},
		{"ANTHROPIC_BEDROCK", llms.ProviderAnthropicBedrock},
		{"BEDROCK_ANTHROPIC", llms.ProviderAnthropicBedrock},
		{"AZURE", llms.ProviderAzure},
		{"AZURE_OPENAI", llms.ProviderAzure},
		{"AZURE_AD", llms.ProviderAzureAD},
		{"BEDROCK", llms.ProviderBedrock},
		{"CLOUDFLARE", llms.ProviderCloudflare},
		{"GOOGLEAI", llms.ProviderGoogleAI},
		{"GOOGLE_AI", llms.ProviderGoogleAI},
		{"GEMINI", llms.ProviderGoogleAI},
		{"OPENAI", llms.ProviderOpenAI},
		{"OPEN_AI", llms.ProviderOpenAI},
		{"OPENAI_BEDROCK", llms.ProviderOpenAIBedrock},
		{"BEDROCK_OPENAI", llms.ProviderOpenAIBedrock},
		{"OPENROUTER", llms.ProviderOpenRouter},
		{"PERPLEXITY", llms.ProviderPerplexity},
		// Matching ignores case.
		{"gemini", llms.ProviderGoogleAI},
		{"Azure_Ad", llms.ProviderAzureAD},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := llms.ParseProviderType(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, invalid := range []string{"", "MISTRAL", "ANTHROPIC/claude"} {
		t.Run("invalid "+invalid, func(t *testing.T) {
			got, err := llms.ParseProviderType(invalid)
			require.Error(t, err)
			assert.Equal(t, "invalid provider type: "+invalid, err.Error())
			assert.Empty(t, got)
		})
	}
}

func TestParseProviderType_CoversEveryProvider(t *testing.T) {
	// Every provider type with a capability entry must be parseable from its
	// own name, so a config api_type always round-trips.
	providers := []llms.ProviderType{
		llms.ProviderAnthropic,
		llms.ProviderAnthropicBedrock,
		llms.ProviderAzure,
		llms.ProviderAzureAD,
		llms.ProviderBedrock,
		llms.ProviderCloudflare,
		llms.ProviderGoogleAI,
		llms.ProviderOpenAI,
		llms.ProviderOpenAIBedrock,
		llms.ProviderOpenRouter,
		llms.ProviderPerplexity,
	}
	for _, provider := range providers {
		got, err := llms.ParseProviderType(string(provider))
		require.NoError(t, err)
		assert.Equal(t, provider, got)
		assert.NotZero(t, llms.ProviderCapabilities(provider), "provider %s has no capabilities", provider)
	}
}

func TestCapabilityModels(t *testing.T) {
	listers := map[llms.ProviderType]bool{
		llms.ProviderOpenAI:     true,
		llms.ProviderOpenRouter: true,
		llms.ProviderAnthropic:  true,
		llms.ProviderGoogleAI:   true,
	}
	all := []llms.ProviderType{
		llms.ProviderAnthropic,
		llms.ProviderAnthropicBedrock,
		llms.ProviderAzure,
		llms.ProviderAzureAD,
		llms.ProviderBedrock,
		llms.ProviderCloudflare,
		llms.ProviderGoogleAI,
		llms.ProviderOpenAI,
		llms.ProviderOpenAIBedrock,
		llms.ProviderOpenRouter,
		llms.ProviderPerplexity,
	}
	for _, provider := range all {
		assert.Equal(t, listers[provider], provider.Supports(llms.CapabilityModels), "provider %s", provider)
	}
}
