package assistants_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/factory"
	"github.com/effective-security/gogentic/model"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
)

func loadOpenAIConfigOrSkipRealTest(t *testing.T) *factory.Config {
	// Uncommend to see logs, or change to DEBUG
	xlog.SetFormatter(xlog.NewStringFormatter(os.Stdout))
	xlog.SetGlobalLogLevel(xlog.ERROR)

	cfg, err := factory.LoadConfig("../factory/testdata/llm.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Providers)

	if cfg.Providers[0].Token == "" || cfg.Providers[0].Token == "faketoken" {
		t.Skip("skipping real test: no token provided")
		return cfg
	}
	// uncomment to run Real Tests
	t.Skip("skipping real test")

	return cfg
}

func Test_Real_Assistant(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	provCfg := cfg.Providers[0]
	require.NotEmpty(t, provCfg.Token)

	f := factory.New(cfg)
	llmModel, err := f.DefaultModel()
	require.NoError(t, err)

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSONSchema),
		assistants.WithJSONMode(true),
	}

	var buf strings.Builder
	ag := assistants.NewAssistant[model.Output](llmModel, systemPrompt, acfg...).
		WithCallback(assistants.NewLogHandler(&buf))

	apikey := os.Getenv("TAVILY_API_KEY")
	if apikey != "" {
		websearch, err := tavily.New()
		require.NoError(t, err)

		ag = ag.WithTools(websearch)
	}

	ctx := model.WithChatContext(context.Background(), model.NewChatContext(model.NewChatID(), nil))

	var output model.Output
	apiResp, err := ag.Run(ctx, "What is a capital of largest country in Europe?", nil, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	apiResp, err = ag.Run(ctx, "Search for weather there.", nil, &output)
	require.NoError(t, err)

	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	history := ag.MessageHistory(ctx)
	assert.NotEmpty(t, history)
	fmt.Println(llms.GetBufferString(history, "Human", "AI"))
}
