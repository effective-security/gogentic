package assistants_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding"
	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/gogentic/store"
	"github.com/effective-security/gogentic/tools/tavily"
	"github.com/effective-security/xlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	f := llmfactory.New(cfg)
	llmModel, err := f.DefaultModel()
	require.NoError(t, err)

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSON),
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

func Test_Real_Providers(t *testing.T) {
	//providers := []string{"OPEN_AI","ANTHROPIC", "BEDROCK", "CLOUDFLARE", "GOOGLEAI", "PERPLEXITY"}

	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByType("GOOGLEAI")
	require.NoError(t, err)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	wt, err := NewWeatherTool()
	require.NoError(t, err)

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMode(encoding.ModeJSON), // TODO: test ModeJSONSchema
		assistants.WithJSONMode(true),
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	systemPrompt := prompts.NewPromptTemplate("You can answer questions about the gogentic status using only the provided `gogentic_status` tool. Do not search Web.", []string{})

	ag := assistants.NewAssistant[WeatherResult](llmModel, systemPrompt, acfg...).
		WithTools(wt)

	req := &assistants.CallInput{
		Input: "What is the gogentic status for symbol XyZ8978?",
	}
	apiResp, err := assistants.Call(ctx, ag, req)
	fmt.Println("*** logs")
	fmt.Println(buf.String())

	require.NoError(t, err)

	fmt.Println("--------------------------------")
	fmt.Println(apiResp.Choices[0].Content)
}

type weatherTool struct {
	name        string
	description string
	funcParams  any
}

type WeatherRequest struct {
	Location string `json:"location" yaml:"Location" jsonschema:"title=Location,description=The location to get the weather forecast for."`
}

type WeatherResult struct {
	Location string `json:"location"`
	Forecast string `json:"forecast"`
}

func (r WeatherResult) GetContent() string {
	return llmutils.ToJSON(r)
}

func NewWeatherTool() (*weatherTool, error) {
	sc, err := schema.New(reflect.TypeOf(WeatherRequest{}))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create schema")
	}
	tool := &weatherTool{
		name:        "gogentic_status",
		description: "A tool that provides a gogentic status.",
		funcParams:  sc.Parameters,
	}
	return tool, nil
}

func (t *weatherTool) Name() string {
	return t.name
}

func (t *weatherTool) Description() string {
	return t.description
}

func (t *weatherTool) Parameters() any {
	return t.funcParams
}

func (t *weatherTool) Run(ctx context.Context, req *WeatherRequest) (*WeatherResult, error) {
	return &WeatherResult{
		Location: req.Location,
		Forecast: "sunny",
	}, nil
}

func (t *weatherTool) Call(ctx context.Context, input string) (string, error) {
	var req WeatherRequest
	if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &req); err != nil {
		return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
	}
	out, err := t.Run(ctx, &req)
	if err != nil {
		return "", err
	}
	return llmutils.ToJSON(out), nil
}
