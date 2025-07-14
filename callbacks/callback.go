package callbacks

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/xlog"
)

// ensure that the callbacks implement the correct interfaces
var (
	_ assistants.Callback = (*Noop)(nil)
	_ tools.Callback      = (*Noop)(nil)
	_ assistants.Callback = (*Printer)(nil)
	_ tools.Callback      = (*Printer)(nil)
	_ assistants.Callback = (*PackageLogger)(nil)
	_ tools.Callback      = (*PackageLogger)(nil)
	_ assistants.Callback = (*Fanout)(nil)
	_ tools.Callback      = (*Fanout)(nil)
)

// Mode defines the mode for callback printing
type Mode int

const (
	// ModeDefault is the default mode for callback printing
	ModeDefault Mode = iota
	// ModeVerbose is the verbose mode for callback printing
	ModeVerbose
)

// Fanout is a callback handler that forwards the events to multiple callbacks.
type Fanout struct {
	callbacks []assistants.Callback
}

func NewFanout(callbacks ...assistants.Callback) *Fanout {
	return &Fanout{callbacks: callbacks}
}

func (l *Fanout) Add(callback assistants.Callback) {
	l.callbacks = append(l.callbacks, callback)
}

func (l *Fanout) OnAssistantStart(ctx context.Context, assistant assistants.IAssistant, input string) {
	for _, callback := range l.callbacks {
		callback.OnAssistantStart(ctx, assistant, input)
	}
}

func (l *Fanout) OnAssistantEnd(ctx context.Context, assistant assistants.IAssistant, input string, resp *llms.ContentResponse, messages []llms.Message) {
	for _, callback := range l.callbacks {
		callback.OnAssistantEnd(ctx, assistant, input, resp, messages)
	}
}

func (l *Fanout) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {
	for _, callback := range l.callbacks {
		callback.OnToolStart(ctx, tool, assistantName, input)
	}
}

func (l *Fanout) OnAssistantLLMParseError(ctx context.Context, a assistants.IAssistant, input string, response string, err error) {
	for _, callback := range l.callbacks {
		callback.OnAssistantLLMParseError(ctx, a, input, response, err)
	}
}

func (l *Fanout) OnAssistantError(ctx context.Context, assistant assistants.IAssistant, input string, err error, messages []llms.Message) {
	for _, callback := range l.callbacks {
		callback.OnAssistantError(ctx, assistant, input, err, messages)
	}
}

func (l *Fanout) OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input string, output string) {
	for _, callback := range l.callbacks {
		callback.OnToolEnd(ctx, tool, assistantName, input, output)
	}
}

func (l *Fanout) OnToolNotFound(ctx context.Context, agent assistants.IAssistant, tool string) {
	for _, callback := range l.callbacks {
		callback.OnToolNotFound(ctx, agent, tool)
	}
}

func (l *Fanout) OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error) {
	for _, callback := range l.callbacks {
		callback.OnToolError(ctx, tool, assistantName, input, err)
	}
}

func (l *Fanout) OnAssistantLLMCallStart(ctx context.Context, agent assistants.IAssistant, llm llms.Model, payload []llms.Message) {
	for _, callback := range l.callbacks {
		callback.OnAssistantLLMCallStart(ctx, agent, llm, payload)
	}
}

func (l *Fanout) OnAssistantLLMCallEnd(ctx context.Context, agent assistants.IAssistant, llm llms.Model, resp *llms.ContentResponse) {
	for _, callback := range l.callbacks {
		callback.OnAssistantLLMCallEnd(ctx, agent, llm, resp)
	}
}

// Noop does nothing.
type Noop struct{}

func NewNoop() *Noop {
	return &Noop{}
}

var _ assistants.Callback = (*Noop)(nil)

func (l *Noop) OnAssistantStart(ctx context.Context, assistant assistants.IAssistant, input string) {
}
func (l *Noop) OnAssistantEnd(ctx context.Context, assistant assistants.IAssistant, input string, resp *llms.ContentResponse, messages []llms.Message) {
}
func (l *Noop) OnAssistantError(ctx context.Context, assistant assistants.IAssistant, input string, err error, messages []llms.Message) {
}
func (l *Noop) OnAssistantLLMParseError(ctx context.Context, a assistants.IAssistant, input string, response string, err error) {
}
func (l *Noop) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {}
func (l *Noop) OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input string, output string) {
}
func (l *Noop) OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error) {
}
func (l *Noop) OnAssistantLLMCallStart(ctx context.Context, agent assistants.IAssistant, llm llms.Model, payload []llms.Message) {
}
func (l *Noop) OnAssistantLLMCallEnd(ctx context.Context, agent assistants.IAssistant, llm llms.Model, resp *llms.ContentResponse) {
}
func (l *Noop) OnToolNotFound(ctx context.Context, agent assistants.IAssistant, tool string) {
}

// Printer is a callback handler that prints to the Writer.
type Printer struct {
	Out  io.Writer
	Mode Mode

	lock sync.Mutex
}

func NewPrinter(out io.Writer, mode Mode) *Printer {
	return &Printer{Out: out, Mode: mode}
}

var _ assistants.Callback = (*Printer)(nil)

func (l *Printer) OnAssistantStart(ctx context.Context, assistant assistants.IAssistant, input string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant Start: %s\n", assistant.Name())
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *Printer) OnAssistantEnd(ctx context.Context, assistant assistants.IAssistant, input string, resp *llms.ContentResponse, messages []llms.Message) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant End: %s\n", assistant.Name())
	if l.Mode == ModeVerbose {
		for _, choice := range resp.Choices {
			if choice.Content != "" {
				fmt.Fprintln(l.Out, choice.Content)
			}
		}
	}
}

func (l *Printer) OnAssistantError(ctx context.Context, assistant assistants.IAssistant, input string, err error, messages []llms.Message) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant Error: %s: %s\n", assistant.Name(), err.Error())
}

func (l *Printer) OnAssistantLLMParseError(ctx context.Context, assistant assistants.IAssistant, input string, response string, err error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant LLM Parse Error: %s: %s\n", assistant.Name(), err.Error())
	fmt.Fprintf(l.Out, "Response: %s\n", response)
}

func (l *Printer) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool Start: %s (%s)\n", tool.Name(), assistantName)
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *Printer) OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input string, output string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool End: %s (%s)\n", tool.Name(), assistantName)
	if l.Mode == ModeVerbose {
		fmt.Fprintf(l.Out, "Output: %s\n", output)
	}
}

func (l *Printer) OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool Error: %s (%s): %s\n", tool.Name(), assistantName, err.Error())
}

func (l *Printer) OnAssistantLLMCallStart(ctx context.Context, agent assistants.IAssistant, llm llms.Model, payload []llms.Message) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant LLM Call: %s: %s model, %d messages\n", agent.Name(), llm.GetName(), len(payload))
	// if l.Mode == ModeVerbose {
	// 	llmutils.PrintMessageContents(l.Out, payload)
	// }
}

func (l *Printer) OnAssistantLLMCallEnd(ctx context.Context, agent assistants.IAssistant, llm llms.Model, resp *llms.ContentResponse) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant LLM Call End: %s: %s model, %d messages\n", agent.Name(), llm.GetName(), len(resp.Choices))
}

func (l *Printer) OnToolNotFound(ctx context.Context, agent assistants.IAssistant, tool string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool Not Found: %s\n", tool)
}

// PackageLogger is a callback handler that prints to the logger.
type PackageLogger struct {
	logger *xlog.PackageLogger
}

func NewPackageLogger(logger *xlog.PackageLogger) *PackageLogger {
	return &PackageLogger{logger: logger}
}

var _ assistants.Callback = (*PackageLogger)(nil)

func (l *PackageLogger) OnAssistantStart(ctx context.Context, assistant assistants.IAssistant, input string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_start",
		"assistant", assistant.Name(),
		"input", input,
	)
}

func (l *PackageLogger) OnAssistantEnd(ctx context.Context, assistant assistants.IAssistant, input string, resp *llms.ContentResponse, messages []llms.Message) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_end",
		"assistant", assistant.Name())
	for _, choice := range resp.Choices {
		if choice.Content != "" {
			l.logger.ContextKV(ctx, xlog.DEBUG, "result", choice.Content)
		}
	}
}

func (l *PackageLogger) OnAssistantError(ctx context.Context, assistant assistants.IAssistant, input string, err error, messages []llms.Message) {
	l.logger.ContextKV(ctx, xlog.ERROR,
		"event", "assistant_error",
		"assistant", assistant.Name(),
		"err", err.Error(),
	)
}

func (l *PackageLogger) OnAssistantLLMParseError(ctx context.Context, assistant assistants.IAssistant, input string, response string, err error) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_llm_parse_error",
		"assistant", assistant.Name(),
		"err", err.Error(),
		"response", response,
	)
}

func (l *PackageLogger) OnToolStart(ctx context.Context, tool tools.ITool, assistantName, input string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "tool_start",
		"assistant", assistantName,
		"tool", tool.Name(),
		"input", input,
	)
}

func (l *PackageLogger) OnToolEnd(ctx context.Context, tool tools.ITool, assistantName, input string, output string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "tool_end",
		"assistant", assistantName,
		"tool", tool.Name(),
		"output", output,
	)
}

func (l *PackageLogger) OnToolError(ctx context.Context, tool tools.ITool, assistantName, input string, err error) {
	l.logger.ContextKV(ctx, xlog.ERROR,
		"event", "tool_error",
		"assistant", assistantName,
		"tool", tool.Name(),
		"err", err.Error(),
	)
}

func (l *PackageLogger) OnAssistantLLMCallStart(ctx context.Context, agent assistants.IAssistant, llm llms.Model, payload []llms.Message) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_llm_call_start",
		"assistant", agent.Name(),
		"model", llm.GetName(),
		"messages", len(payload),
	)
}

func (l *PackageLogger) OnAssistantLLMCallEnd(ctx context.Context, agent assistants.IAssistant, llm llms.Model, resp *llms.ContentResponse) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_llm_call_end",
		"assistant", agent.Name(),
		"model", llm.GetName(),
		"messages", len(resp.Choices),
	)
}

func (l *PackageLogger) OnToolNotFound(ctx context.Context, agent assistants.IAssistant, tool string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "tool_not_found",
		"assistant", agent.Name(),
		"tool", tool,
	)
}
