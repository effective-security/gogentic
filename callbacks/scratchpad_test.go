package callbacks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

type fakeAssistant struct{ name string }

func (a *fakeAssistant) Name() string                                          { return a.name }
func (a *fakeAssistant) Description() string                                   { return "desc" }
func (a *fakeAssistant) GetTools() []tools.ITool                               { return nil }
func (a *fakeAssistant) FormatPrompt(map[string]any) (llms.PromptValue, error) { return nil, nil }
func (a *fakeAssistant) GetPromptInputVariables() []string                     { return nil }
func (a *fakeAssistant) Call(context.Context, *assistants.CallInput) (*llms.ContentResponse, error) {
	return nil, nil
}
func (a *fakeAssistant) LastRunMessages() []llms.MessageContent { return nil }

type fakeTool struct{ name string }

func (t *fakeTool) Name() string                                           { return t.name }
func (t *fakeTool) Description() string                                    { return "desc" }
func (t *fakeTool) Parameters() any                                        { return nil }
func (t *fakeTool) Call(ctx context.Context, input string) (string, error) { return "", nil }

func newTestChatContext() (context.Context, chatmodel.ChatContext) {
	tenantID := "tenant1"
	chatID := "chatid"
	chatCtx := chatmodel.NewChatContext(tenantID, chatID, nil)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
	return ctx, chatCtx
}

func TestScratchpad_StartRun_EndRun(t *testing.T) {
	t.Parallel()
	sp := NewScratchpad(ModeVerbose)
	ctx, cctx := newTestChatContext()
	sp.StartRun(ctx)
	// Add minimal data to run
	r := sp.runs[cctx.GetChatID()]
	// Populate stats for EndRun
	r.stats.AssistantCalls = 2
	r.stats.AssistantCallsFailed = 1
	r.stats.ToolsCalls = 3
	r.stats.ToolsCallsFailed = 2
	r.stats.ToolNotFound = 1
	r.stats.AssistantLLMCalls = 1
	r.stats.TotalMessages = 4
	r.stats.LLMBytesOut = 10
	r.stats.LLMBytesIn = 11

	// EndRun should print stats and cleanup
	stats, buf := sp.EndRun(ctx)
	require.NotNil(t, stats)
	require.Contains(t, string(buf), "Run Started")
	require.Contains(t, string(buf), "Run Ended")
	require.Contains(t, string(buf), "Assistant calls: 2, Failed: 1")
	// Should no longer be present in map
	_, ok := sp.runs[cctx.GetChatID()]
	assert.False(t, ok)

	// EndRun with no run (run already deleted)
	s2, _ := sp.EndRun(ctx)
	assert.Nil(t, s2)
}

func TestScratchpad_getRun_nil(t *testing.T) {
	t.Parallel()
	sp := NewScratchpad(ModeDefault)
	// No chat context at all
	assert.Nil(t, sp.getRun(context.Background()))
	// Chat context not in runs
	ctx, _ := newTestChatContext()
	assert.Nil(t, sp.getRun(ctx))
}

func TestScratchpad_OnCallbacks(t *testing.T) {
	t.Parallel()
	sp := NewScratchpad(ModeVerbose)
	ctx, _ := newTestChatContext()
	sp.StartRun(ctx)
	ast := &fakeAssistant{name: "A1"}
	tool := &fakeTool{name: "T1"}
	resp := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: "Answer 1"}}}
	// Test various callbacks
	sp.OnAssistantStart(ctx, ast, "input")
	sp.OnAssistantEnd(ctx, ast, "input", resp)
	sp.OnAssistantLLMCall(ctx, ast, []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "foo"}}},
	})
	sp.OnAssistantLLMParseError(ctx, ast, "input", "output", errors.New("parseerr"))
	sp.OnAssistantError(ctx, ast, "input", errors.New("fail"))
	sp.OnToolStart(ctx, tool, "tinput")
	sp.OnToolEnd(ctx, tool, "tinput", "toutput")
	sp.OnToolError(ctx, tool, "tinput", errors.New("terr"))
	sp.OnToolNotFound(ctx, ast, "T2")
	// EndRun shows these calls
	stats, output := sp.EndRun(ctx)
	require.NotNil(t, stats)
	outStr := string(output)
	assert.Contains(t, outStr, "A1 Start")
	assert.Contains(t, outStr, "A1 End")
	assert.Contains(t, outStr, "T1 Start")
	assert.Contains(t, outStr, "T1 End")
	assert.Contains(t, outStr, "LLM Call")
	assert.Contains(t, outStr, "LLM Parse Error")
	assert.Contains(t, outStr, "Error")
	assert.Contains(t, outStr, "Tool Not Found")
	// test callback methods again: should still work if no run
	sp.OnAssistantStart(ctx, ast, "input")
	sp.OnAssistantEnd(ctx, ast, "input", resp)
	sp.OnAssistantLLMCall(ctx, ast, nil)
	sp.OnAssistantLLMParseError(ctx, ast, "input", "output", errors.New("parse2"))
	sp.OnAssistantError(ctx, ast, "input", errors.New("fail2"))
	sp.OnToolStart(ctx, tool, "tinput")
	sp.OnToolEnd(ctx, tool, "tinput", "toutput")
	sp.OnToolError(ctx, tool, "tinput", errors.New("terr2"))
	sp.OnToolNotFound(ctx, ast, "T3")
}

func Test_run_print_format(t *testing.T) {
	t.Parallel()
	_, chatCtx := newTestChatContext()
	r := &run{chatCtx: chatCtx}
	oldTimeFn := TimeNowFn
	TimeNowFn = func() time.Time { return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC) }
	defer func() { TimeNowFn = oldTimeFn }()

	r.print("hello", "again")
	lines := strings.Split(r.w.String(), "\n")
	require.NotEmpty(t, lines[0])
	// Format: [timestamp chatID.runID] hello again
	assert.Contains(t, lines[0], "2024-01-01 12:00:00 "+chatCtx.GetChatID()+"."+chatCtx.RunID()+" hello again")
}
