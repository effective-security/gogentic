package assistants_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/store"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
)

func loadOpenAIConfigOrSkipRealTest(t *testing.T) *llmfactory.Config {
	// uncomment to run Real Tests
	t.Skip("skipping real test")

	// Uncommend to see logs, or change to DEBUG
	xlog.SetFormatter(xlog.NewStringFormatter(os.Stdout))
	xlog.SetGlobalLogLevel(xlog.ERROR)

	cfg, err := llmfactory.LoadConfig("../pkg/llmfactory/testdata/llm.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Providers)

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

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSONSchema),
		assistants.WithJSONMode(true),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
		assistants.WithMessageStore(memstore),
	}

	ag := assistants.NewAssistant[chatmodel.OutputResult](llmModel, systemPrompt, acfg...)

	apikey := os.Getenv("TAVILY_API_KEY")
	if apikey != "" {
		websearch, err := tavily.New()
		require.NoError(t, err)

		ag = ag.WithTools(websearch)
	}

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	var output chatmodel.OutputResult
	req := &assistants.CallInput{
		Input: "What is a capital of largest country in Europe?",
	}
	apiResp, err := assistants.Run(ctx, ag, req, &output)
	require.NoError(t, err)
	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	req = &assistants.CallInput{
		Input: "Search for weather there.",
	}
	apiResp, err = assistants.Run(ctx, ag, req, &output)
	require.NoError(t, err)

	assert.NotEmpty(t, output.Content)
	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	fmt.Println(llms.GetBufferString(history, "Human", "AI"))
}
