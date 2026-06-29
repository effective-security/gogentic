package callbacks

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/tools"
)

// ensure ScratchpadCallback implements assistants.Callback
var _ assistants.Callback = (*Scratchpad)(nil)

var TimeNowFn = time.Now

type RunStats struct {
	ChatID string
	RunID  string

	Duration                time.Duration
	TotalMessages           uint32
	Usage                   llms.UsageStats
	AssistantCalls          uint32
	AssistantCallsSucceeded uint32
	AssistantCallsFailed    uint32
	ToolsCalls              uint32
	ToolsCallsSucceeded     uint32
	ToolsCallsFailed        uint32
	ToolNotFound            uint32
}

// ScratchpadCallback is a callback handler that prints to the Writer.
type Scratchpad struct {
	runs map[string]*run // chatID -> run
	mode Mode
	lock sync.Mutex
}

func NewScratchpad(mode Mode) *Scratchpad {
	return &Scratchpad{
		runs: make(map[string]*run),
		mode: mode,
	}
}

func (l *Scratchpad) StartRun(ctx context.Context) {
	l.lock.Lock()
	defer l.lock.Unlock()

	chatCtx := chatmodel.GetChatContext(ctx)
	if chatCtx == nil {
		return
	}

	chatID := chatCtx.GetChatID()
	runID := chatCtx.GetRunID()
	l.runs[chatID] = &run{
		stats: RunStats{
			ChatID: chatID,
			RunID:  runID,
		},
		chatCtx: chatCtx,
		started: TimeNowFn(),
	}

	l.runs[chatID].printEntry(fmt.Sprintf("=== Run Started: %s ===", chatID))
}

func (l *Scratchpad) EndRun(ctx context.Context) (*RunStats, []byte) {
	run := l.getRun(ctx)
	if run == nil {
		return nil, nil
	}
	run.lock.Lock()

	stats := run.stats
	stats.Duration = TimeNowFn().Sub(run.started).Round(time.Millisecond)

	run.printEntry(fmt.Sprintf("Assistant calls: %d, Failed: %d",
		stats.AssistantCalls,
		stats.AssistantCallsFailed,
	))
	run.printEntry(fmt.Sprintf("Tool calls: %d, Failed: %d, Not Found: %d",
		stats.ToolsCalls,
		stats.ToolsCallsFailed,
		stats.ToolNotFound,
	))
	run.printEntry(fmt.Sprintf("LLM calls: %d, Messages: %d, Bytes Out: %d, Bytes In: %d, Bytes Total: %d, Input Tokens: %d, Output Tokens: %d, Total Tokens: %d",
		stats.Usage.LlmCallCount,
		stats.TotalMessages,
		stats.Usage.BytesOut,
		stats.Usage.BytesIn,
		stats.Usage.BytesOut+stats.Usage.BytesIn,
		stats.Usage.InputTokens,
		stats.Usage.OutputTokens,
		stats.Usage.TotalTokens,
	))

	run.printEntry(fmt.Sprintf("=== Run Ended. Duration: %s ===", stats.Duration))
	data := run.w.Bytes()
	run.lock.Unlock()

	l.lock.Lock()
	delete(l.runs, run.chatCtx.GetChatID())
	l.lock.Unlock()

	return &stats, data
}

func (l *Scratchpad) getRun(ctx context.Context) *run {
	l.lock.Lock()
	defer l.lock.Unlock()

	chatCtx := chatmodel.GetChatContext(ctx)
	if chatCtx == nil {
		return nil
	}

	return l.runs[chatCtx.GetChatID()]
}

func (l *Scratchpad) OnAssistantStart(ctx context.Context, assistant assistants.IAssistant, input string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.AssistantCalls, 1)
	name := assistant.Name()
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, name, "*** Assistant Start ***")
	skillList := assistant.GetSkills()
	if len(skillList) > 0 {
		run.printEntry(actionID, name, "Skills:", strings.Join(skillList.Names(), ", "))
	}
	run.printEntry(actionID, name, "Input:")
	run.printNewLine(input)
}

func (l *Scratchpad) OnAssistantEnd(ctx context.Context, assistant assistants.IAssistant, input string, resp *assistants.Response, messageHistory llms.Messages) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.AssistantCallsSucceeded, 1)
	// NOTE: usage is intentionally NOT accumulated here. resp.Usage is the
	// aggregated subtree total (it already includes nested assistants/tools
	// invoked via tool.CallAssistant), so adding it per assistant would double
	// count when the callback is propagated to nested assistants. Usage is
	// accumulated at the LLM-call boundary instead (OnAssistantLLMCallStart and
	// OnAssistantLLMCallEnd), where each call is counted exactly once.

	name := assistant.Name()
	actionID := chatmodel.GetActionID(ctx)
	if l.mode == ModeVerbose {
		run.printEntry(actionID, name, "Assistant Output:")
		for _, choice := range resp.Choices {
			if choice.Content != "" {
				run.printNewLine(choice.Content)
			}
		}
	}
	if l.mode == ModeVerbose {
		run.printEntry(actionID, name, l.printMessages(messageHistory))
	}
	run.printEntry(actionID, name, "*** Assistant End ***")
}

func (l *Scratchpad) OnAssistantError(ctx context.Context, assistant assistants.IAssistant, input string, err error, messageHistory llms.Messages) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.AssistantCallsFailed, 1)
	name := assistant.Name()
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, name, "*** Error ***", err.Error())
	run.printEntry(actionID, name, l.printMessages(messageHistory))
}

func (l *Scratchpad) printMessages(messages []llms.Message) string {
	var buf strings.Builder
	buf.WriteString("Messages:\n")
	for idx, msg := range messages {
		fmt.Fprintf(&buf, "[%d] %s:\n", idx, msg.Role)
		textParts := 0
		toolParts := 0
		toolResponseParts := 0
		totalLength := 0
		for _, part := range msg.Parts {
			totalLength += part.ContentLength()
			switch typ := part.(type) {
			case llms.TextContent:
				textParts++
				str := stringUpto(typ.String(), 80)
				buf.WriteString("  - ")
				buf.WriteString(str)
				buf.WriteString("\n")
			case llms.ToolCall:
				toolParts++
				buf.WriteString("  - ")
				buf.WriteString(typ.String())
				buf.WriteString("\n")
			case llms.ToolCallResponse:
				toolResponseParts++
				buf.WriteString("  - ")
				buf.WriteString(typ.String())
				buf.WriteString("\n")
				str := typ.Content
				if l.mode != ModeVerbose {
					str = stringUpto(str, 160)
				}
				buf.WriteString("  ")
				buf.WriteString(str)
				buf.WriteString("\n")
			}
		}

		fmt.Fprintf(&buf, "  * %d texts, %d tool calls, %d tool responses, content length: %d\n",
			textParts, toolParts, toolResponseParts, totalLength)
		if msg.Source != nil {
			fmt.Fprintf(&buf, "  * source: %s\n", msg.Source.String())
		}
	}
	return buf.String()
}

var newLineReplacer = strings.NewReplacer("\r\n", "\\n", "\n", "\\n", "\r", "\\n")

func stringUpto(s string, maxLen int) string {
	normalized := newLineReplacer.Replace(s)
	runes := []rune(normalized)
	size := len(runes)

	if size > maxLen {
		return fmt.Sprintf("%s... (%d more)", string(runes[:maxLen]), size-maxLen)
	}
	return normalized
}

func (l *Scratchpad) OnAssistantLLMCallStart(ctx context.Context, agent assistants.IAssistant, llm llms.Model, payload []llms.Message) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	count := uint32(len(payload))
	atomic.AddUint32(&run.stats.TotalMessages, count)

	// Accumulate at the LLM-call boundary so each call is counted exactly once,
	// regardless of nesting depth or whether the callback is propagated to
	// nested assistants. This mirrors the accounting done in Assistant.run.
	run.stats.Usage.LlmCallCount++
	run.stats.Usage.BytesOut += llmutils.CountMessagesContentSize(payload)

	name := agent.Name()
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, name, "*** LLM Call ***", fmt.Sprintf("%s model, %d messages", llm.GetName(), count))
	if l.mode == ModeVerbose {
		run.printEntry(actionID, name, l.printMessages(payload))
	}
}

func (l *Scratchpad) OnAssistantLLMCallEnd(ctx context.Context, agent assistants.IAssistant, llm llms.Model, resp *llms.ContentResponse) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	stats := resp.Usage()
	// Accumulate token usage and received bytes once per LLM call. stats only
	// carries token fields (no BytesIn/LlmCallCount), so we add BytesIn here and
	// the call count is incremented in OnAssistantLLMCallStart.
	run.stats.Usage.Usage.Add(stats)
	run.stats.Usage.BytesIn += resp.ContentSize()

	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, agent.Name(), "*** LLM Call End ***", fmt.Sprintf("%s model, %d input tokens, %d output tokens, %d total tokens",
		llm.GetName(), stats.InputTokens, stats.OutputTokens, stats.TotalTokens))
}

func (l *Scratchpad) OnAssistantLLMParseError(ctx context.Context, assistant assistants.IAssistant, input string, response string, err error) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.AssistantCallsFailed, 1)
	name := assistant.Name()
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, name, "*** LLM Parse Error ***", err.Error())
	run.printEntry(actionID, name, " Response:", response)
}

func (l *Scratchpad) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.ToolsCalls, 1)
	tname := tool.Name()
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, assistantName, tname, "*** Tool Start ***")
	run.printEntry(actionID, assistantName, tname, "Tool Input:")
	run.printNewLine(input)
}

func (l *Scratchpad) OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input string, output string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.ToolsCallsSucceeded, 1)
	tname := tool.Name()
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, assistantName, tname, "Tool Output:")
	if l.mode != ModeVerbose {
		output = stringUpto(output, 160)
	}
	run.printNewLine(output)
	run.printEntry(actionID, assistantName, tname, "*** Tool End ***")
}

func (l *Scratchpad) OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.ToolsCallsFailed, 1)
	tname := tool.Name()
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, assistantName, tname, "*** Tool Error ***", err.Error())
}

func (l *Scratchpad) OnToolNotFound(ctx context.Context, agent assistants.IAssistant, tool string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	run.lock.Lock()
	defer run.lock.Unlock()

	atomic.AddUint32(&run.stats.ToolNotFound, 1)
	actionID := chatmodel.GetActionID(ctx)
	run.printEntry(actionID, agent.Name(), "*** Tool Not Found ***", tool)
}

type run struct {
	chatCtx chatmodel.ChatContext
	w       bytes.Buffer
	started time.Time
	lock    sync.Mutex
	stats   RunStats
}

// printEntry writes the entries to the run's output.
// The entries are written in the following format:
// [timestamp runID] entry entry\n
func (r *run) printEntry(entries ...string) {
	now := TimeNowFn()
	ts := now.Format("2006-01-02 15:04:05")

	_, _ = r.w.WriteString(ts)
	_, _ = r.w.WriteString(" ")
	_, _ = r.w.WriteString(r.chatCtx.GetRunID())
	_, _ = r.w.WriteString(":")

	for _, entry := range entries {
		if entry != "" {
			_, _ = r.w.WriteString(" ")
			_, _ = r.w.WriteString(entry)
		}
	}
	_, _ = r.w.WriteString("\n")
}

func (r *run) printNewLine(entries ...string) {
	for _, entry := range entries {
		if entry != "" {
			_, _ = r.w.WriteString(entry)
			_, _ = r.w.WriteString("\n")
		}
	}
}
