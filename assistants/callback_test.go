package assistants_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestCallback(t *testing.T) {
	var buf bytes.Buffer
	cb := assistants.NewLogHandler(&buf)

	ast := &fakeAssistant{name: "test-assistant"}
	tool := &fakeTool{name: "test-tool"}

	cb.OnAssistantStart(context.Background(), ast, "test input")
	cb.OnAssistantEnd(context.Background(), ast, "test input", &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "test output",
			},
		},
	})
	cb.OnAssistantError(context.Background(), ast, "test input", errors.New("test error"))
	cb.OnToolStart(context.Background(), tool, "test input")
	cb.OnToolEnd(context.Background(), tool, "test input", "test output")
	cb.OnToolError(context.Background(), tool, "test input", errors.New("test error"))

	res := buf.String()
	assert.Contains(t, res, "Assistant Start: test-assistant")
	assert.Contains(t, res, "Input: test input")
	assert.Contains(t, res, "Assistant End: test-assistant")
	assert.Contains(t, res, "Tool Start: test-tool")
	assert.Contains(t, res, "Tool End: test-tool")
	assert.Contains(t, res, "Output: test output")
	assert.Contains(t, res, "Tool Error: test-tool: ")
}

type fakeAssistant struct {
	name string
}

func (f *fakeAssistant) Name() string {
	return f.name
}
func (f *fakeAssistant) Description() string {
	return "useful assistant"
}

type fakeTool struct {
	name string
}

func (f *fakeTool) Name() string {
	return f.name
}
func (f *fakeTool) Description() string {
	return "useful tool"
}
func (f *fakeTool) Parameters() any {
	return nil
}
func (f *fakeTool) Call(context.Context, string) (string, error) {
	return "", nil
}
