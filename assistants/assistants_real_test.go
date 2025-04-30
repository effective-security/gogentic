package assistants_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/llmfactory"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
)

func loadOpenAIConfigOrSkipRealTest(t *testing.T) *llmfactory.Config {
	// Uncommend to see logs, or change to DEBUG
	xlog.SetFormatter(xlog.NewStringFormatter(os.Stdout))
	xlog.SetGlobalLogLevel(xlog.ERROR)

	cfg, err := llmfactory.LoadConfig("../llmfactory/testdata/llm.yaml")
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

	f := llmfactory.New(cfg)
	llmModel, err := f.DefaultModel()
	require.NoError(t, err)

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSONSchema),
		assistants.WithJSONMode(true),
	}

	var buf strings.Builder
	ag := assistants.NewAssistant[chatmodel.Output](llmModel, systemPrompt, acfg...).
		WithCallback(assistants.NewPrinterCallback(&buf))

	apikey := os.Getenv("TAVILY_API_KEY")
	if apikey != "" {
		websearch, err := tavily.New()
		require.NoError(t, err)

		ag = ag.WithTools(websearch)
	}

	ctx := chatmodel.WithChatContext(context.Background(), chatmodel.NewChatContext(chatmodel.NewChatID(), nil))

	var output chatmodel.Output
	apiResp, err := assistants.Run(ctx, ag, "What is a capital of largest country in Europe?", nil, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	apiResp, err = assistants.Run(ctx, ag, "Search for weather there.", nil, &output)
	require.NoError(t, err)

	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	history := ag.MessageHistory(ctx)
	assert.NotEmpty(t, history)
	fmt.Println(llms.GetBufferString(history, "Human", "AI"))
}
