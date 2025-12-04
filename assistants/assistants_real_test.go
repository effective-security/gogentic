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
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadOpenAIConfigOrSkipRealTest(t *testing.T) *llmfactory.Config {
	// comment to run Real Tests
	t.Skip("skipping real test")

	// Uncommend to see logs, or change to DEBUG
	xlog.SetFormatter(xlog.NewStringFormatter(os.Stdout))
	xlog.SetGlobalLogLevel(xlog.DEBUG)

	cfg, err := llmfactory.LoadConfig("../pkg/llmfactory/testdata/llm.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Providers)

	return cfg
}

func Test_Real_Assistant(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByType("ANTHROPIC")
	require.NoError(t, err)

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant.", []string{})

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
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

	req := &assistants.CallInput{
		Input: "What is a capital of largest country in Europe?",
	}
	var output1 chatmodel.OutputResult
	apiResp, err := ag.Run(ctx, req, &output1)
	require.NoError(t, err)
	assert.NotEmpty(t, output1.Content)
	assert.NotEmpty(t, apiResp.Choices)

	req = &assistants.CallInput{
		Input: "Search for weather there.",
	}
	var output2 chatmodel.OutputResult
	apiResp, err = ag.Run(ctx, req, &output2)
	require.NoError(t, err)

	assert.NotEmpty(t, output2.Content)
	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	buf.Reset()
	llmutils.PrintMessages(&buf, history)
	fmt.Println(buf.String())
}

type CVEResult struct {
	chatmodel.BaseClarificationResult
	CVE     string   `json:"cve" yaml:"cve" jsonschema:"title=CVE,description=The most recent CVE with High severity and Denial of Service."`
	Sources []string `json:"sources" yaml:"sources" jsonschema:"title=Sources,description=The sources of the CVE."`
}

func (r CVEResult) GetContent() string {
	return llmutils.ToJSON(r)
}

func Test_Real_GoogleAI_Search(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByType("GOOGLEAI")
	require.NoError(t, err)

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant capable of Web Search. You return responses in JSON format.", []string{})

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
		assistants.WithMessageStore(memstore),
		assistants.WithMode(encoding.ModeJSON),
		assistants.WithTools([]llms.Tool{
			{
				Type: "google_search",
			},
		}),
	}

	ag := assistants.NewAssistant[CVEResult](llmModel, systemPrompt, acfg...)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	req := &assistants.CallInput{
		Input: "What is the most recent CVE with Critical severity and Denial of Service? provide at least 2 sources.",
	}
	var output1 CVEResult
	apiResp, err := ag.Run(ctx, req, &output1)
	require.NoError(t, err)
	assert.NotEmpty(t, output1.CVE)
	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	buf.Reset()
	llmutils.PrintMessages(&buf, history)
	fmt.Println(buf.String())
}

func Test_Real_WebSearch_JSON(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByName("gemini-2.5-pro")
	//llmModel, err := f.ModelByType("ANTHROPIC")
	require.NoError(t, err)

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant capable of Web Search. You return responses in JSON format.", []string{})

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
		assistants.WithMessageStore(memstore),
		assistants.WithMode(encoding.ModeJSON),
		assistants.WithTools([]llms.Tool{
			{
				Type: "web_search",
				WebSearchOptions: &llms.WebSearchOptions{
					AllowedDomains: []string{
						"cvedetails.com",
						"cve.org",
						"nvd.nist.gov",
						"cisa.gov",
						"first.org",
						"api.first.org",
						"epss.empiricalsecurity.com",
						"cve2epss.com",
						"vulners.com",
						"projectdiscovery.io",
						"redhat.com",
						"en.wikipedia.org",
					},
					MaxUses: 5,
				},
			},
		}),
	}

	ag := assistants.NewAssistant[CVEResult](llmModel, systemPrompt, acfg...)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	req := &assistants.CallInput{
		Input: "What is the most recent CVE with Critical severity and Denial of Service? provide at least 2 sources.",
	}
	var output1 CVEResult
	apiResp, err := ag.Run(ctx, req, &output1)
	require.NoError(t, err)
	assert.NotEmpty(t, output1.CVE)
	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	buf.Reset()
	llmutils.PrintMessages(&buf, history)
	fmt.Println(buf.String())
}

func Test_Real_WebSearch_Text(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	//llmModel, err := f.ModelByName("gpt-5")
	llmModel, err := f.ModelByType("ANTHROPIC")
	require.NoError(t, err)

	systemPrompt := prompts.NewPromptTemplate("You are helpful and friendly AI assistant capable of Web Search", []string{})

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
		assistants.WithMessageStore(memstore),
		assistants.WithMode(encoding.ModePlainText),
		assistants.WithTools([]llms.Tool{
			{
				Type: "web_search",
				WebSearchOptions: &llms.WebSearchOptions{
					AllowedDomains: []string{
						"cvedetails.com",
						"cve.org",
						"nvd.nist.gov",
						"cisa.gov",
						"first.org",
						"api.first.org",
						"epss.empiricalsecurity.com",
						"cve2epss.com",
						"vulners.com",
						"projectdiscovery.io",
						"redhat.com",
						"en.wikipedia.org",
					},
					MaxUses: 5,
				},
			},
		}),
	}

	ag := assistants.NewAssistant[chatmodel.String](llmModel, systemPrompt, acfg...).
		WithOutputParser(encoding.NewSimpleOutputParser())

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	req := &assistants.CallInput{
		Input: "What is the most recent CVE with Critical severity and Denial of Service? provide at least 2 sources.",
	}
	var output1 chatmodel.String
	apiResp, err := ag.Run(ctx, req, &output1)
	require.NoError(t, err)
	assert.NotEmpty(t, output1.String())
	assert.NotEmpty(t, apiResp.Choices)

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)
	buf.Reset()
	llmutils.PrintMessages(&buf, history)
	fmt.Println(buf.String())
}

func Test_Real_Providers(t *testing.T) {
	//providers := []string{"OPENAI","ANTHROPIC", "GOOGLEAI", "PERPLEXITY", "BEDROCK"}

	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByType("AZURE")
	require.NoError(t, err)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	wt, err := NewWeatherTool()
	require.NoError(t, err)

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
		//assistants.WithPromptCacheMode(llms.PromptCacheModeInMemory),
	}

	printHistory := func(ctx context.Context) {
		history := memstore.Messages(ctx)
		var buf strings.Builder
		llmutils.PrintMessages(&buf, history)
		t.Log(buf.String())
	}

	systemPrompt := prompts.NewPromptTemplate("You can answer questions about the gogentic status using only the provided `gogentic_status` tool. Do not search Web.", []string{})

	ag := assistants.NewAssistant[WeatherResult](llmModel, systemPrompt, acfg...).
		WithTools(wt)

	req := &assistants.CallInput{
		Input: "Return the gogentic status for location: 1012340123?",
	}
	apiResp, err := ag.Call(ctx, req)
	if err != nil {
		printHistory(ctx)
	}
	require.NoError(t, err)
	fmt.Println(apiResp.Choices[0].Content)

	req = &assistants.CallInput{
		Input: "Return the gogentic status for location: 1012340125?",
	}
	apiResp, err = ag.Call(ctx, req)
	if err != nil {
		printHistory(ctx)
	}
	require.NoError(t, err)
	fmt.Println(apiResp.Choices[0].Content)

	assert.Equal(t, 2, wt.called)

	fmt.Println("--- History ---")
	fmt.Println(buf.String())

	history := memstore.Messages(ctx)
	assert.NotEmpty(t, history)

	buf.Reset()
	llmutils.PrintMessages(&buf, history)
	chat := buf.String()
	fmt.Println("--- Chat ---")
	fmt.Println(chat)
}

type weatherTool struct {
	name        string
	description string
	funcParams  *jsonschema.Schema
	called      int
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

func (t *weatherTool) Parameters() *jsonschema.Schema {
	return t.funcParams
}

func (t *weatherTool) Run(ctx context.Context, req *WeatherRequest) (*WeatherResult, error) {
	t.called++
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
