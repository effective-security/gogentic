package callbacks

import (
	"bytes"
	"context"
	"fmt"
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
	runs map[string]*run
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
	l.runs[chatCtx.GetChatID()] = &run{
		stats: RunStats{
			ChatID: chatCtx.GetChatID(),
			RunID:  chatCtx.RunID(),
		},
		chatCtx: chatCtx,
		started: time.Now(),
	}

	l.runs[chatCtx.GetChatID()].print("*** Run Started ***")
}

func (l *Scratchpad) EndRun(ctx context.Context) (*RunStats, []byte) {
	run := l.getRun(ctx)
	if run == nil {
		return nil, nil
	}

	stats := run.stats
	stats.Duration = time.Since(run.started)

	run.print(fmt.Sprintf("Assistant calls: %d, Failed: %d",
		stats.AssistantCalls,
		stats.AssistantCallsFailed,
	))
	run.print(fmt.Sprintf("Tool calls: %d, Failed: %d, Not Found: %d",
		stats.ToolsCalls,
		stats.ToolsCallsFailed,
		stats.ToolNotFound,
	))
	run.print(fmt.Sprintf("LLM calls: %d, Messages: %d,	Bytes Out: %d, Bytes In: %d, Bytes Total: %d",
		stats.AssistantLLMCalls,
		stats.TotalMessages,
		stats.LLMBytesOut,
		stats.LLMBytesIn,
		stats.LLMBytesOut+stats.LLMBytesIn,
	))

	run.print(fmt.Sprintf("*** Run Ended. Duration: %s ***", stats.Duration))

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
	run.print(assistant.Name(), "*** Assistant Start ***")
	run.print("Input:", input)
}

func (l *Scratchpad) OnAssistantEnd(ctx context.Context, assistant assistants.IAssistant, input string, resp *llms.ContentResponse) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.AssistantCallsSucceeded, 1)
	atomic.AddUint64(&run.stats.LLMBytesIn, llmutils.CountResponseContentSize(resp))

	run.print(assistant.Name(), "*** Assistant End ***")
	if l.mode == ModeVerbose {
		run.print("Output:")
		for _, choice := range resp.Choices {
			if choice.Content != "" {
				run.print(choice.Content)
			}
		}
	}
}

func (l *Scratchpad) OnAssistantLLMCall(ctx context.Context, agent assistants.IAssistant, payload []llms.MessageContent) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}

	atomic.AddUint64(&run.stats.LLMBytesOut, llmutils.CountMessagesContentSize(payload))
	atomic.AddUint32(&run.stats.AssistantLLMCalls, 1)
	count := uint32(len(payload))
	atomic.AddUint32(&run.stats.TotalMessages, count)
	run.print(agent.Name(), "*** LLM Call ***", fmt.Sprintf("%d messages", count))

	// count payload messages by role and print
	for _, msg := range payload {
		textParts := 0
		toolParts := 0
		for _, part := range msg.Parts {
			if _, ok := part.(llms.TextContent); ok {
				textParts++
			}
			if _, ok := part.(llms.ToolCall); ok {
				toolParts++
			}
		}
		run.print(string(msg.Role), fmt.Sprintf("%d text parts, %d tool parts", textParts, toolParts))
	}
}

func (l *Scratchpad) OnAssistantLLMParseError(ctx context.Context, assistant assistants.IAssistant, input string, response string, err error) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.AssistantCallsFailed, 1)
	run.print(assistant.Name(), "*** LLM Parse Error ***", err.Error())
	run.print("Response:", response)
}

func (l *Scratchpad) OnAssistantError(ctx context.Context, assistant assistants.IAssistant, input string, err error) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.AssistantCallsFailed, 1)
	run.print(assistant.Name(), "*** Error ***", err.Error())
}

func (l *Scratchpad) OnToolStart(ctx context.Context, tool tools.ITool, input string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolsCalls, 1)
	run.print(tool.Name(), "*** Tool Start ***")
	run.print("Input:", input)
}

func (l *Scratchpad) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolsCallsSucceeded, 1)
	run.print(tool.Name(), "*** Tool End ***")
	if l.mode == ModeVerbose {
		run.print("Output:", output)
	}
}

func (l *Scratchpad) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolsCallsFailed, 1)
	run.print(tool.Name(), "*** Tool Error ***", err.Error())
}

func (l *Scratchpad) OnToolNotFound(ctx context.Context, agent assistants.IAssistant, tool string) {
	run := l.getRun(ctx)
	if run == nil {
		return
	}
	atomic.AddUint32(&run.stats.ToolNotFound, 1)
	run.print(agent.Name(), "*** Tool Not Found ***", tool)
}

type run struct {
	chatCtx chatmodel.ChatContext
	w       bytes.Buffer
	started time.Time
	lock    sync.Mutex
	stats   RunStats
}

// print writes the entries to the run's output.
// The entries are written in the following format:
// [timestamp chatID.runID] entry entry\n
func (r *run) print(entries ...string) {
	r.lock.Lock()
	defer r.lock.Unlock()

	now := TimeNowFn()
	ts := now.Format("2006-01-02 15:04:05")

	_, _ = r.w.WriteString(ts)
	_, _ = r.w.WriteString(" ")
	_, _ = r.w.WriteString(r.chatCtx.GetChatID())
	_, _ = r.w.WriteString(".")
	_, _ = r.w.WriteString(r.chatCtx.RunID())
	_, _ = r.w.WriteString(" ")

	for i, entry := range entries {
		if i > 0 {
			_, _ = r.w.WriteString(" ")
		}
		_, _ = r.w.WriteString(entry)
	}
	_, _ = r.w.WriteString("\n")
}
