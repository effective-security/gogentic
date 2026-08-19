package llmfactory_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationOpenRouter makes a real, billed OpenRouter request. It is
// intentionally skipped unless both OPENROUTER_API_KEY and OPENROUTER_MODEL
// are set, so normal unit-test and CI runs remain offline and free of charges.
func TestIntegrationOpenRouter(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY is not set; skipping billed OpenRouter smoke test")
	}
	modelName := os.Getenv("OPENROUTER_MODEL")
	if modelName == "" {
		t.Skip("OPENROUTER_MODEL is not set; skipping billed OpenRouter smoke test")
	}

	headers := make(map[string]string)
	if referer := os.Getenv("OPENROUTER_HTTP_REFERER"); referer != "" {
		headers["HTTP-Referer"] = referer
	}
	if title := os.Getenv("OPENROUTER_TITLE"); title != "" {
		headers["X-OpenRouter-Title"] = title
	}

	cfg := &llmfactory.Config{
		DefaultProvider: "openrouter",
		Providers: []*llmfactory.ProviderConfig{
			{
				Name:            "openrouter",
				Token:           apiKey,
				AvailableModels: []string{modelName},
				Headers:         headers,
				OpenAI: llmfactory.OpenAIConfig{
					APIType: string(llms.ProviderOpenRouter),
				},
			},
		},
	}

	factory := llmfactory.New(cfg)
	model, err := factory.GetModel(context.Background(), llmfactory.ModelOptions{
		PreferredModels: []string{"openrouter/" + modelName},
	})
	require.NoError(t, err)
	assert.Equal(t, llms.ProviderOpenRouter, model.GetProviderType())
	assert.Equal(t, modelName, model.GetName())

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	response, err := model.GenerateContent(ctx, []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Reply with exactly OPENROUTER_OK and no other text."),
	}, llms.WithMaxTokens(32))
	require.NoError(t, err)
	require.NotEmpty(t, response.Choices)
	assert.Contains(t, strings.ToUpper(response.Choices[0].Content), "OPENROUTER_OK")

	t.Logf(
		"OpenRouter smoke test passed: model=%s input_tokens=%d output_tokens=%d",
		model.GetName(),
		response.Choices[0].Usage.InputTokens,
		response.Choices[0].Usage.OutputTokens,
	)
}
