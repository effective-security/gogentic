package assistants

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/xlog"
	"github.com/tmc/langchaingo/llms"
)

// ensure that the callbacks implement the correct interfaces
var (
	_ Callback       = (*NoopCallback)(nil)
	_ tools.Callback = (*NoopCallback)(nil)
	_ Callback       = (*PrinterCallback)(nil)
	_ tools.Callback = (*PrinterCallback)(nil)
	_ Callback       = (*PackageLoggerCallback)(nil)
	_ tools.Callback = (*PackageLoggerCallback)(nil)
	_ Callback       = (*FanoutCallback)(nil)
	_ tools.Callback = (*FanoutCallback)(nil)
)

// PrintMode defines the mode for callback printing
type PrintMode int

const (
	// PrintModeDefault is the default mode for callback printing
	PrintModeDefault PrintMode = iota
	// PrintModeVerbose is the verbose mode for callback printing
	PrintModeVerbose
)

// FanoutCallback is a callback handler that forwards the events to multiple callbacks.
type FanoutCallback struct {
	callbacks []Callback
}

func NewFanoutCallback(callbacks ...Callback) *FanoutCallback {
	return &FanoutCallback{callbacks: callbacks}
}

func (l *FanoutCallback) Add(callback Callback) {
	l.callbacks = append(l.callbacks, callback)
}

func (l *FanoutCallback) OnAssistantStart(ctx context.Context, assistant IAssistant, input string) {
	for _, callback := range l.callbacks {
		callback.OnAssistantStart(ctx, assistant, input)
	}
}

func (l *FanoutCallback) OnAssistantEnd(ctx context.Context, assistant IAssistant, input string, resp *llms.ContentResponse) {
	for _, callback := range l.callbacks {
		callback.OnAssistantEnd(ctx, assistant, input, resp)
	}
}

func (l *FanoutCallback) OnToolStart(ctx context.Context, tool tools.ITool, input string) {
	for _, callback := range l.callbacks {
		callback.OnToolStart(ctx, tool, input)
	}
}

func (l *FanoutCallback) OnAssistantLLMParseError(ctx context.Context, a IAssistant, input string, response string, err error) {
	for _, callback := range l.callbacks {
		callback.OnAssistantLLMParseError(ctx, a, input, response, err)
	}
}

func (l *FanoutCallback) OnAssistantError(ctx context.Context, assistant IAssistant, input string, err error) {
	for _, callback := range l.callbacks {
		callback.OnAssistantError(ctx, assistant, input, err)
	}
}

func (l *FanoutCallback) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
	for _, callback := range l.callbacks {
		callback.OnToolEnd(ctx, tool, input, output)
	}
}

func (l *FanoutCallback) OnToolNotFound(ctx context.Context, agent IAssistant, tool string) {
	for _, callback := range l.callbacks {
		callback.OnToolNotFound(ctx, agent, tool)
	}
}

func (l *FanoutCallback) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {
	for _, callback := range l.callbacks {
		callback.OnToolError(ctx, tool, input, err)
	}
}

func (l *FanoutCallback) OnAssistantLLMCall(ctx context.Context, agent IAssistant, payload []llms.MessageContent) {
	for _, callback := range l.callbacks {
		callback.OnAssistantLLMCall(ctx, agent, payload)
	}
}

func (l *FanoutCallback) OnToolLLMCall(ctx context.Context, tool tools.ITool, payload []llms.MessageContent) {
	for _, callback := range l.callbacks {
		callback.OnToolLLMCall(ctx, tool, payload)
	}
}

// NoopCallback does nothing.
type NoopCallback struct{}

func NewNoopCallback() *NoopCallback {
	return &NoopCallback{}
}

var _ Callback = (*NoopCallback)(nil)

func (l *NoopCallback) OnAssistantStart(ctx context.Context, assistant IAssistant, input string) {}
func (l *NoopCallback) OnAssistantEnd(ctx context.Context, assistant IAssistant, input string, resp *llms.ContentResponse) {
}
func (l *NoopCallback) OnAssistantError(ctx context.Context, assistant IAssistant, input string, err error) {
}
func (l *NoopCallback) OnAssistantLLMParseError(ctx context.Context, assistant IAssistant, input string, response string, err error) {
}
func (l *NoopCallback) OnToolStart(ctx context.Context, tool tools.ITool, input string) {}
func (l *NoopCallback) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
}
func (l *NoopCallback) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {}
func (l *NoopCallback) OnAssistantLLMCall(ctx context.Context, agent IAssistant, payload []llms.MessageContent) {
}
func (l *NoopCallback) OnToolLLMCall(ctx context.Context, tool tools.ITool, payload []llms.MessageContent) {
}
func (l *NoopCallback) OnToolNotFound(ctx context.Context, agent IAssistant, tool string) {
}

// PrinterCallback is a callback handler that prints to the Writer.
type PrinterCallback struct {
	Out  io.Writer
	Mode PrintMode

	lock sync.Mutex
}

func NewPrinterCallback(out io.Writer, mode PrintMode) *PrinterCallback {
	return &PrinterCallback{Out: out, Mode: mode}
}

var _ Callback = (*PrinterCallback)(nil)

func (l *PrinterCallback) OnAssistantStart(ctx context.Context, assistant IAssistant, input string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant Start: %s\n", assistant.Name())
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *PrinterCallback) OnAssistantEnd(ctx context.Context, assistant IAssistant, input string, resp *llms.ContentResponse) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant End: %s\n", assistant.Name())
	if l.Mode == PrintModeVerbose {
		for _, choice := range resp.Choices {
			if choice.Content != "" {
				fmt.Fprintln(l.Out, choice.Content)
			}
		}
	}
}

func (l *PrinterCallback) OnAssistantError(ctx context.Context, assistant IAssistant, input string, err error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant Error: %s: %s\n", assistant.Name(), err.Error())
}

func (l *PrinterCallback) OnAssistantLLMParseError(ctx context.Context, assistant IAssistant, input string, response string, err error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant LLM Parse Error: %s: %s\n", assistant.Name(), err.Error())
	fmt.Fprintf(l.Out, "Response: %s\n", response)
}

func (l *PrinterCallback) OnToolStart(ctx context.Context, tool tools.ITool, input string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool Start: %s\n", tool.Name())
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *PrinterCallback) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool End: %s\n", tool.Name())
	if l.Mode == PrintModeVerbose {
		fmt.Fprintf(l.Out, "Output: %s\n", output)
	}
}

func (l *PrinterCallback) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool Error: %s: %s\n", tool.Name(), err.Error())
}

func (l *PrinterCallback) OnAssistantLLMCall(ctx context.Context, agent IAssistant, payload []llms.MessageContent) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Assistant LLM Call: %s: %d messages\n", agent.Name(), len(payload))
}

func (l *PrinterCallback) OnToolLLMCall(ctx context.Context, tool tools.ITool, payload []llms.MessageContent) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool LLM Call: %s: %d messages\n", tool.Name(), len(payload))
}

func (l *PrinterCallback) OnToolNotFound(ctx context.Context, agent IAssistant, tool string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	fmt.Fprintf(l.Out, "Tool Not Found: %s\n", tool)
}

// PackageLoggerCallback is a callback handler that prints to the logger.
type PackageLoggerCallback struct {
	logger *xlog.PackageLogger
}

func NewPackageLoggerCallback(logger *xlog.PackageLogger) *PackageLoggerCallback {
	return &PackageLoggerCallback{logger: logger}
}

var _ Callback = (*PackageLoggerCallback)(nil)

func (l *PackageLoggerCallback) OnAssistantStart(ctx context.Context, assistant IAssistant, input string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_start",
		"assistant", assistant.Name(),
		"input", input,
	)
}

func (l *PackageLoggerCallback) OnAssistantEnd(ctx context.Context, assistant IAssistant, input string, resp *llms.ContentResponse) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_end",
		"assistant", assistant.Name())
	for _, choice := range resp.Choices {
		if choice.Content != "" {
			l.logger.ContextKV(ctx, xlog.DEBUG, "result", choice.Content)
		}
	}
}

func (l *PackageLoggerCallback) OnAssistantError(ctx context.Context, assistant IAssistant, input string, err error) {
	l.logger.ContextKV(ctx, xlog.ERROR,
		"event", "assistant_error",
		"assistant", assistant.Name(),
		"err", err.Error(),
	)
}

func (l *PackageLoggerCallback) OnAssistantLLMParseError(ctx context.Context, assistant IAssistant, input string, response string, err error) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_llm_parse_error",
		"assistant", assistant.Name(),
		"err", err.Error(),
		"response", response,
	)
}

func (l *PackageLoggerCallback) OnToolStart(ctx context.Context, tool tools.ITool, input string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "tool_start",
		"tool", tool.Name(),
		"input", input,
	)
}

func (l *PackageLoggerCallback) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "tool_end",
		"tool", tool.Name(),
		"output", output,
	)
}

func (l *PackageLoggerCallback) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {
	l.logger.ContextKV(ctx, xlog.ERROR,
		"event", "tool_error",
		"tool", tool.Name(),
		"err", err.Error(),
	)
}

func (l *PackageLoggerCallback) OnAssistantLLMCall(ctx context.Context, agent IAssistant, payload []llms.MessageContent) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "assistant_llm_call",
		"assistant", agent.Name(),
		"messages", len(payload),
	)
}

func (l *PackageLoggerCallback) OnToolLLMCall(ctx context.Context, tool tools.ITool, payload []llms.MessageContent) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "tool_llm_call",
		"tool", tool.Name(),
		"messages", len(payload),
	)
}

func (l *PackageLoggerCallback) OnToolNotFound(ctx context.Context, agent IAssistant, tool string) {
	l.logger.ContextKV(ctx, xlog.DEBUG,
		"event", "tool_not_found",
		"assistant", agent.Name(),
		"tool", tool,
	)
}
