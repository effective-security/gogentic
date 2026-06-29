package callbacks

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/skills"
	"github.com/effective-security/gogentic/tools"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAssistant struct{ name string }

func (a *fakeAssistant) Name() string                                          { return a.name }
func (a *fakeAssistant) Description() string                                   { return "desc" }
func (a *fakeAssistant) GetTools() []tools.ITool                               { return nil }
func (a *fakeAssistant) GetSkills() skills.Skills                              { return nil }
func (a *fakeAssistant) FormatPrompt(map[string]any) (llms.PromptValue, error) { return nil, nil }
func (a *fakeAssistant) GetPromptInputVariables() []string                     { return nil }
func (a *fakeAssistant) Call(context.Context, *assistants.CallInput) (*assistants.Response, error) {
	return nil, nil
}
func (a *fakeAssistant) LastRunMessages() []llms.Message { return nil }

type fakeTool struct{ name string }

func (t *fakeTool) Name() string                                           { return t.name }
func (t *fakeTool) Description() string                                    { return "desc" }
func (t *fakeTool) Parameters() *jsonschema.Schema                         { return nil }
func (t *fakeTool) Call(ctx context.Context, input string) (string, error) { return "", nil }

type fakeModel struct {
	name     string
	provider llms.ProviderType
}

func (m *fakeModel) GetName() string                    { return m.name }
func (m *fakeModel) GetProviderType() llms.ProviderType { return m.provider }
func (m *fakeModel) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, nil
}

func newTestChatContext() (context.Context, chatmodel.ChatContext) {
	tenantID := "tenant1"
	chatID := "chatid"
	chatCtx := chatmodel.NewChatContext(tenantID, chatID, nil)
	chatCtx.SetRunID("run1")
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)
	ctx = chatmodel.WithActionID(ctx, "step1")
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
	r.stats.Usage.LlmCallCount = 1
	r.stats.TotalMessages = 4
	r.stats.Usage.BytesOut = 10
	r.stats.Usage.BytesIn = 11

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
	TimeNowFn = func() time.Time { return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC) }
	defer func() { TimeNowFn = time.Now }()

	sp := NewScratchpad(ModeVerbose)
	ctx, _ := newTestChatContext()
	sp.StartRun(ctx)
	ast := &fakeAssistant{name: "A1"}
	tool := &fakeTool{name: "T1"}

	src := &llms.MessageSource{
		Name:     "A1",
		RunID:    "run1",
		ActionID: "step2",
	}

	resp := &assistants.Response{
		Choices: []*llms.ContentChoice{{Content: "Answer 1"}},
		Messages: []llms.Message{
			{Source: src, Role: llms.RoleHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "very long message that should be truncated shdgfkasjhdgfakjhs khasgdfkjhagsdfh\nagsjhdfgkajshdfg gajkshdgfkjasdhjfg ahsdfkgasjhdfga akjhsdgfakjhsdgfakj gasjdkhfgakjsdhga aksjdhfgakjdsfg"}}},
		},
		Usage: llms.UsageStats{
			Usage: llms.Usage{
				InputTokens:  10,
				OutputTokens: 11,
				TotalTokens:  21,
			},
			BytesOut:     12,
			BytesIn:      13,
			LlmCallCount: 2,
		},
	}

	// Test various callbacks
	sp.OnAssistantStart(ctx, ast, "input")
	sp.OnAssistantLLMCallStart(ctx, ast, &fakeModel{name: "gpt-4o", provider: llms.ProviderOpenAI}, []llms.Message{
		{Source: src, Role: llms.RoleHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "foo"}}},
	})
	sp.OnAssistantLLMCallEnd(ctx, ast, &fakeModel{name: "gpt-4o", provider: llms.ProviderOpenAI}, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Answer 1",
				Usage: llms.Usage{
					InputTokens:  10,
					OutputTokens: 11,
					TotalTokens:  21,
				},
			},
		},
	})
	sp.OnAssistantLLMParseError(ctx, ast, "input", "output", errors.New("parseerr"))
	sp.OnAssistantError(ctx, ast, "input", errors.New("fail"), []llms.Message{
		{Source: src, Role: llms.RoleHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "foo"}}},
	})
	sp.OnToolStart(ctx, tool, "A1", "tinput")
	sp.OnToolEnd(ctx, tool, "A1", "tinput", "toutput")
	sp.OnToolError(ctx, tool, "A1", "tinput", errors.New("terr"))
	sp.OnToolNotFound(ctx, ast, "T2")
	sp.OnAssistantEnd(ctx, ast, "input", resp, resp.Messages)

	// EndRun shows these calls
	stats, output := sp.EndRun(ctx)
	require.NotNil(t, stats)
	outStr := string(output)

	exp := `2024-01-01 12:00:00 run1: === Run Started: chatid ===
2024-01-01 12:00:00 run1: step1 A1 *** Assistant Start ***
2024-01-01 12:00:00 run1: step1 A1 Input:
input
2024-01-01 12:00:00 run1: step1 A1 *** LLM Call *** gpt-4o model, 1 messages
2024-01-01 12:00:00 run1: step1 A1 Messages:
[0] human:
  - foo
  * 1 texts, 0 tool calls, 0 tool responses, content length: 3
  * source: run1.step2.A1

2024-01-01 12:00:00 run1: step1 A1 *** LLM Call End *** gpt-4o model, 10 input tokens, 11 output tokens, 21 total tokens
2024-01-01 12:00:00 run1: step1 A1 *** LLM Parse Error *** parseerr
2024-01-01 12:00:00 run1: step1 A1  Response: output
2024-01-01 12:00:00 run1: step1 A1 *** Error *** fail
2024-01-01 12:00:00 run1: step1 A1 Messages:
[0] human:
  - foo
  * 1 texts, 0 tool calls, 0 tool responses, content length: 3
  * source: run1.step2.A1

2024-01-01 12:00:00 run1: step1 A1 T1 *** Tool Start ***
2024-01-01 12:00:00 run1: step1 A1 T1 Tool Input:
tinput
2024-01-01 12:00:00 run1: step1 A1 T1 Tool Output:
toutput
2024-01-01 12:00:00 run1: step1 A1 T1 *** Tool End ***
2024-01-01 12:00:00 run1: step1 A1 T1 *** Tool Error *** terr
2024-01-01 12:00:00 run1: step1 A1 *** Tool Not Found *** T2
2024-01-01 12:00:00 run1: step1 A1 Assistant Output:
Answer 1
2024-01-01 12:00:00 run1: step1 A1 Messages:
[0] human:
  - very long message that should be truncated shdgfkasjhdgfakjhs khasgdfkjhagsdfh\n... (105 more)
  * 1 texts, 0 tool calls, 0 tool responses, content length: 184
  * source: run1.step2.A1

2024-01-01 12:00:00 run1: step1 A1 *** Assistant End ***
2024-01-01 12:00:00 run1: Assistant calls: 1, Failed: 2
2024-01-01 12:00:00 run1: Tool calls: 1, Failed: 1, Not Found: 1
2024-01-01 12:00:00 run1: LLM calls: 1, Messages: 1, Bytes Out: 8, Bytes In: 8, Bytes Total: 16, Input Tokens: 10, Output Tokens: 11, Total Tokens: 21
2024-01-01 12:00:00 run1: === Run Ended. Duration: 0s ===
`
	assert.Equal(t, exp, outStr)

	ctx = chatmodel.WithActionID(ctx, "step2")

	resp.Messages = append(resp.Messages, llms.Message{Source: src, Role: llms.RoleHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "foo"}}})

	// test callback methods again: should still work if no run
	sp.OnAssistantStart(ctx, ast, "input")
	sp.OnAssistantEnd(ctx, ast, "input", resp, resp.Messages)
	sp.OnAssistantLLMCallStart(ctx, ast, &fakeModel{name: "gpt-4o", provider: llms.ProviderOpenAI}, []llms.Message{
		{Source: src, Role: llms.RoleHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "foo"}}},
	})
	sp.OnAssistantLLMParseError(ctx, ast, "input", "output", errors.New("parse2"))
	sp.OnAssistantError(ctx, ast, "input", errors.New("fail2"), []llms.Message{
		{Source: src, Role: llms.RoleHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "foo"}}},
	})
	sp.OnToolStart(ctx, tool, "A1", "tinput")
	sp.OnToolEnd(ctx, tool, "A1", "tinput", "toutput")
	sp.OnToolError(ctx, tool, "A1", "tinput", errors.New("terr2"))
	sp.OnToolNotFound(ctx, ast, "T3")
	sp.OnAssistantLLMCallEnd(ctx, ast, &fakeModel{name: "gpt-4o", provider: llms.ProviderOpenAI}, &llms.ContentResponse{
		Choices: resp.Choices,
	})
}

func Test_run_print_format(t *testing.T) {
	_, chatCtx := newTestChatContext()
	r := &run{chatCtx: chatCtx}

	TimeNowFn = func() time.Time { return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC) }
	defer func() { TimeNowFn = time.Now }()

	r.printEntry("hello", "again")

	exp := "2024-01-01 12:00:00 run1: hello again\n"
	assert.Equal(t, exp, r.w.String())
}
