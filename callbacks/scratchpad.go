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
	LLMBytesOut             uint64
	LLMBytesIn              uint64
	LLMInputTokens          uint64
	LLMOutputTokens         uint64
	LLMCacheWriteTokens     uint64
	LLMCacheReadTokens      uint64
	LLMTotalTokens          uint64
	AssistantCalls          uint32
	AssistantCallsSucceeded uint32
	AssistantCallsFailed    uint32
	AssistantLLMCalls       uint32
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
		stats.AssistantLLMCalls,
		stats.TotalMessages,
		stats.LLMBytesOut,
		stats.LLMBytesIn,
		stats.LLMBytesOut+stats.LLMBytesIn,
		stats.LLMInputTokens,
		stats.LLMOutputTokens,
		stats.LLMTotalTokens,
	))

	run.printEntry(fmt.Sprintf("=== Run Ended. Duration: %s ===", stats.Duration))

	l.lock.Lock()
	delete(l.runs, run.chatCtx.GetChatID())
	l.lock.Unlock()

	return &stats, run.w.Bytes()
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
	atomic.AddUint32(&run.stats.AssistantCalls, 1)
	name := assistant.Name()
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, name, "*** Assistant Start ***")
	skillList := assistant.GetSkills()
	if len(skillList) > 0 {
		run.printEntry(stepID, name, "Skills:", strings.Join(skillList.Names(), ", "))
	}
	run.printEntry(stepID, name, "Input:")
	run.printNewLine(input)
}

func (l *Scratchpad) OnAssistantEnd(ctx context.Context, assistant assistants.IAssistant, input string, resp *llms.ContentResponse, messages []llms.Message) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.AssistantCallsSucceeded, 1)
	atomic.AddUint64(&run.stats.LLMBytesIn, llmutils.CountResponseContentSize(resp))

	name := assistant.Name()
	stepID := chatmodel.GetStepID(ctx)
	if l.mode == ModeVerbose {
		run.printEntry(stepID, name, "Assistant Output:")
		for _, choice := range resp.Choices {
			if choice.Content != "" {
				run.printNewLine(choice.Content)
			}
		}
	}
	if l.mode == ModeVerbose {
		run.printEntry(stepID, name, l.printMessages(messages))
	}
	run.printEntry(stepID, name, "*** Assistant End ***")
}

func (l *Scratchpad) OnAssistantError(ctx context.Context, assistant assistants.IAssistant, input string, err error, messages []llms.Message) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.AssistantCallsFailed, 1)
	name := assistant.Name()
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, name, "*** Error ***", err.Error())
	run.printEntry(stepID, name, l.printMessages(messages))
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

	atomic.AddUint64(&run.stats.LLMBytesOut, llmutils.CountMessagesContentSize(payload))
	atomic.AddUint32(&run.stats.AssistantLLMCalls, 1)
	count := uint32(len(payload))
	atomic.AddUint32(&run.stats.TotalMessages, count)

	name := agent.Name()
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, name, "*** LLM Call ***", fmt.Sprintf("%s model, %d messages", llm.GetName(), count))
	if l.mode == ModeVerbose {
		run.printEntry(stepID, name, l.printMessages(payload))
	}
}

func (l *Scratchpad) OnAssistantLLMCallEnd(ctx context.Context, agent assistants.IAssistant, llm llms.Model, resp *llms.ContentResponse) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}

	tokensIn, tokensOut, tokensCacheWrite, tokensCacheRead, tokensTotal := llmutils.CountTokens(resp)
	atomic.AddUint64(&run.stats.LLMInputTokens, uint64(tokensIn))
	atomic.AddUint64(&run.stats.LLMOutputTokens, uint64(tokensOut))
	atomic.AddUint64(&run.stats.LLMCacheWriteTokens, uint64(tokensCacheWrite))
	atomic.AddUint64(&run.stats.LLMCacheReadTokens, uint64(tokensCacheRead))
	atomic.AddUint64(&run.stats.LLMTotalTokens, uint64(tokensTotal))

	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, agent.Name(), "*** LLM Call End ***", fmt.Sprintf("%s model, %d input tokens, %d output tokens, %d total tokens", llm.GetName(), tokensIn, tokensOut, tokensTotal))
}

func (l *Scratchpad) OnAssistantLLMParseError(ctx context.Context, assistant assistants.IAssistant, input string, response string, err error) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.AssistantCallsFailed, 1)
	name := assistant.Name()
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, name, "*** LLM Parse Error ***", err.Error())
	run.printEntry(stepID, name, " Response:", response)
}

func (l *Scratchpad) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolsCalls, 1)
	tname := tool.Name()
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, assistantName, tname, "*** Tool Start ***")
	run.printEntry(stepID, assistantName, tname, "Tool Input:")
	run.printNewLine(input)
}

func (l *Scratchpad) OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input string, output string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolsCallsSucceeded, 1)
	tname := tool.Name()
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, assistantName, tname, "Tool Output:")
	if l.mode != ModeVerbose {
		output = stringUpto(output, 80)
	}
	run.printNewLine(output)
	run.printEntry(stepID, assistantName, tname, "*** Tool End ***")
}

func (l *Scratchpad) OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolsCallsFailed, 1)
	tname := tool.Name()
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, assistantName, tname, "*** Tool Error ***", err.Error())
}

func (l *Scratchpad) OnToolNotFound(ctx context.Context, agent assistants.IAssistant, tool string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolNotFound, 1)
	stepID := chatmodel.GetStepID(ctx)
	run.printEntry(stepID, agent.Name(), "*** Tool Not Found ***", tool)
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
	r.lock.Lock()
	defer r.lock.Unlock()

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
	r.lock.Lock()
	defer r.lock.Unlock()

	for _, entry := range entries {
		if entry != "" {
			_, _ = r.w.WriteString(entry)
			_, _ = r.w.WriteString("\n")
		}
	}
}
