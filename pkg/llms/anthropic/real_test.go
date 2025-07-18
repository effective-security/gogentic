package anthropic_test

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
	"github.com/effective-security/gogentic/pkg/llmfactory"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/effective-security/gogentic/store"
	"github.com/effective-security/xlog"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadOpenAIConfigOrSkipRealTest(t *testing.T) *llmfactory.Config {
	// uncomment to run Real Tests
	t.Skip("skipping real test")

	// Uncommend to see logs, or change to DEBUG
	xlog.SetFormatter(xlog.NewStringFormatter(os.Stdout))
	xlog.SetGlobalLogLevel(xlog.ERROR)

	cfg, err := llmfactory.LoadConfig("../../llmfactory/testdata/llm.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Providers)

	return cfg
}

func Test_Real_Providers(t *testing.T) {
	cfg := loadOpenAIConfigOrSkipRealTest(t)

	f := llmfactory.New(cfg)
	llmModel, err := f.ModelByType("ANTHROPIC")
	require.NoError(t, err)

	chatCtx := chatmodel.NewChatContext(chatmodel.NewChatID(), chatmodel.NewChatID(), nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	wt, err := NewStatusTool()
	require.NoError(t, err)

	memstore := store.NewMemoryStore()

	var buf strings.Builder
	acfg := []assistants.Option{
		assistants.WithMessageStore(memstore),
		assistants.WithCallback(callbacks.NewPrinter(&buf, callbacks.ModeVerbose)),
	}

	systemPrompt := prompts.NewPromptTemplate("You can answer questions about the gogentic status using only the provided `gogentic_status` tool. Do not search Web.", []string{})

	ag := assistants.NewAssistant[StatusResult](llmModel, systemPrompt, acfg...).
		WithTools(wt)

	req := &assistants.CallInput{
		Input: "Return the gogentic status for location: 1012340123?",
	}
	_, err = ag.Call(ctx, req)
	fmt.Println("*** logs")
	fmt.Println(buf.String())

	require.NoError(t, err)
	assert.Equal(t, 1, wt.called)

	// fmt.Println("--------------------------------")
	// fmt.Println(apiResp.Choices[0].Content)
}

type testTool struct {
	name        string
	description string
	funcParams  *jsonschema.Schema
	called      int
}

type StatusRequest struct {
	Location string `json:"location" yaml:"Location" jsonschema:"title=Location,description=The location to get the status for."`
}

type StatusResult struct {
	Location string `json:"location"`
	Status   string `json:"status"`
}

func (r StatusResult) GetContent() string {
	return llmutils.ToJSON(r)
}

func NewStatusTool() (*testTool, error) {
	sc, err := schema.New(reflect.TypeOf(StatusRequest{}))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create schema")
	}
	tool := &testTool{
		name:        "gogentic_status",
		description: "A tool that provides a gogentic status.",
		funcParams:  sc.Parameters,
	}
	return tool, nil
}

func (t *testTool) Name() string {
	return t.name
}

func (t *testTool) Description() string {
	return t.description
}

func (t *testTool) Parameters() *jsonschema.Schema {
	return t.funcParams
}

func (t *testTool) Run(ctx context.Context, req *StatusRequest) (*StatusResult, error) {
	t.called++
	return &StatusResult{
		Location: req.Location,
		Status:   "sunny",
	}, nil
}

func (t *testTool) Call(ctx context.Context, input string) (string, error) {
	var req StatusRequest
	if err := json.Unmarshal(llmutils.CleanJSON([]byte(input)), &req); err != nil {
		return "", errors.WithStack(chatmodel.ErrFailedUnmarshalInput)
	}
	out, err := t.Run(ctx, &req)
	if err != nil {
		return "", err
	}
	return llmutils.ToJSON(out), nil
}
