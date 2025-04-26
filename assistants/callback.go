package assistants

import (
	"context"
	"fmt"
	"io"

	"github.com/effective-security/gogentic/tools"
	"github.com/tmc/langchaingo/llms"
)

// LogHandler is a callback handler that prints to the standard output.
type LogHandler struct {
	Out io.Writer
}

func NewLogHandler(out io.Writer) *LogHandler {
	return &LogHandler{Out: out}
}

var _ Callback = (*LogHandler)(nil)

func (l *LogHandler) OnAssistantStart(ctx context.Context, agent IAssistant, input string) {
	fmt.Fprintf(l.Out, "Assistant Start: %s\n", agent.Name())
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *LogHandler) OnAssistantEnd(ctx context.Context, agent IAssistant, input string, resp *llms.ContentResponse) {
	fmt.Fprintf(l.Out, "Assistant End: %s\n", agent.Name())
	for _, choice := range resp.Choices {
		if choice.Content != "" {
			fmt.Fprintln(l.Out, choice.Content)
		}
	}
}

func (l *LogHandler) OnAssistantError(cyx context.Context, agent IAssistant, input string, err error) {
	fmt.Fprintf(l.Out, "Assistant Error: %s: %s\n", agent.Name(), err.Error())
}

func (l *LogHandler) OnToolStart(ctx context.Context, tool tools.ITool, input string) {
	fmt.Fprintf(l.Out, "Tool Start: %s\n", tool.Name())
	fmt.Fprintf(l.Out, "Input: %s\n", input)
}

func (l *LogHandler) OnToolEnd(ctx context.Context, tool tools.ITool, input string, output string) {
	fmt.Fprintf(l.Out, "Tool End: %s\n", tool.Name())
	fmt.Fprintf(l.Out, "Output: %s\n", output)
}

func (l *LogHandler) OnToolError(ctx context.Context, tool tools.ITool, input string, err error) {
	fmt.Fprintf(l.Out, "Tool Error: %s: %s\n", tool.Name(), err.Error())
}
