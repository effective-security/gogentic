package assistants

import (
	"context"
	"fmt"
	"io"

	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/xlog"
	"github.com/tmc/langchaingo/llms"
)

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
func (l *NoopCallback) OnToolStart(ctx context.Context, tool tools.ITool, input string) {}
func (l *NoopCallback) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
}
func (l *NoopCallback) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {}

// PrinterCallback is a callback handler that prints to the Writer.
type PrinterCallback struct {
	Out io.Writer
}

func NewPrinterCallback(out io.Writer) *PrinterCallback {
	return &PrinterCallback{Out: out}
}

var _ Callback = (*PrinterCallback)(nil)

func (l *PrinterCallback) OnAssistantStart(ctx context.Context, assistant IAssistant, input string) {
	fmt.Fprintf(l.Out, "Assistant Start: %s\n", assistant.Name())
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *PrinterCallback) OnAssistantEnd(ctx context.Context, assistant IAssistant, input string, resp *llms.ContentResponse) {
	fmt.Fprintf(l.Out, "Assistant End: %s\n", assistant.Name())
	for _, choice := range resp.Choices {
		if choice.Content != "" {
			fmt.Fprintln(l.Out, choice.Content)
		}
	}
}

func (l *PrinterCallback) OnAssistantError(ctx context.Context, assistant IAssistant, input string, err error) {
	fmt.Fprintf(l.Out, "Assistant Error: %s: %s\n", assistant.Name(), err.Error())
}

func (l *PrinterCallback) OnToolStart(ctx context.Context, tool tools.ITool, input string) {
	fmt.Fprintf(l.Out, "Tool Start: %s\n", tool.Name())
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *PrinterCallback) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
	fmt.Fprintf(l.Out, "Tool End: %s\n", tool.Name())
	fmt.Fprintf(l.Out, "Output: %s\n", output)
}

func (l *PrinterCallback) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {
	fmt.Fprintf(l.Out, "Tool Error: %s: %s\n", tool.Name(), err.Error())
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
