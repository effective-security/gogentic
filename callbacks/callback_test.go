package callbacks_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/x/values"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/prompts"
)

func TestCallback(t *testing.T) {
	var buf bytes.Buffer
	cb := callbacks.NewPrinter(&buf, callbacks.ModeVerbose)

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

func TestDescriptions(t *testing.T) {
	tool1 := &fakeTool{name: "test-tool1", description: "test tool 1\nLine 1"}
	tool2 := &fakeTool{name: "test-tool2", description: "test tool 2\nLine 2"}
	tool3 := &fakeTool{name: "test-tool3", description: "test tool 3\nLine 3"}

	ast1 := &fakeAssistant{
		name:        "test-assistant1",
		description: "test assistant1\nLine 1",
		tools:       []tools.ITool{tool1, tool2},
	}
	ast2 := &fakeAssistant{
		name:        "test-assistant2",
		description: "test assistant2\nLine 2",
		tools:       []tools.ITool{tool2, tool3},
	}

	descr := assistants.GetDescriptions(ast1, ast2)
	exp := "\n```json" + `
{
	"Assistants": [
		{
			"Name": "test-assistant1",
			"Description": "test assistant1. Line 1."
		},
		{
			"Name": "test-assistant2",
			"Description": "test assistant2. Line 2."
		}
	]
}
` + "```\n"
	assert.Equal(t, exp, descr)

	descr = assistants.GetDescriptionsWithTools(ast1, ast2)
	exp = "\n```json" + `
{
	"Assistants": [
		{
			"Name": "test-assistant1",
			"Description": "test assistant1. Line 1.",
			"Tools": [
				{
					"Name": "test-tool1",
					"Description": "test tool 1. Line 1."
				},
				{
					"Name": "test-tool2",
					"Description": "test tool 2. Line 2."
				}
			]
		},
		{
			"Name": "test-assistant2",
			"Description": "test assistant2. Line 2.",
			"Tools": [
				{
					"Name": "test-tool2",
					"Description": "test tool 2. Line 2."
				},
				{
					"Name": "test-tool3",
					"Description": "test tool 3. Line 3."
				}
			]
		}
	]
}
` + "```\n"
	assert.Equal(t, exp, descr)
}

func TestFanoutCallback(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	cb1 := callbacks.NewPrinter(&buf1, callbacks.ModeVerbose)
	cb2 := callbacks.NewPrinter(&buf2, callbacks.ModeVerbose)

	fanout := callbacks.NewFanout(cb1, cb2)

	ast := &fakeAssistant{name: "test-assistant"}
	tool := &fakeTool{name: "test-tool"}

	// Test OnAssistantStart
	fanout.OnAssistantStart(context.Background(), ast, "test input")
	assert.Contains(t, buf1.String(), "Assistant Start: test-assistant")
	assert.Contains(t, buf2.String(), "Assistant Start: test-assistant")

	// Test OnAssistantEnd
	fanout.OnAssistantEnd(context.Background(), ast, "test input", &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "test output",
			},
		},
	})
	assert.Contains(t, buf1.String(), "Assistant End: test-assistant")
	assert.Contains(t, buf2.String(), "Assistant End: test-assistant")

	// Test OnAssistantError
	fanout.OnAssistantError(context.Background(), ast, "test input", errors.New("test error"))
	assert.Contains(t, buf1.String(), "Assistant Error: test-assistant")
	assert.Contains(t, buf2.String(), "Assistant Error: test-assistant")

	// Test OnToolStart
	fanout.OnToolStart(context.Background(), tool, "test input")
	assert.Contains(t, buf1.String(), "Tool Start: test-tool")
	assert.Contains(t, buf2.String(), "Tool Start: test-tool")

	// Test OnToolEnd
	fanout.OnToolEnd(context.Background(), tool, "test input", "test output")
	assert.Contains(t, buf1.String(), "Tool End: test-tool")
	assert.Contains(t, buf2.String(), "Tool End: test-tool")

	// Test OnToolError
	fanout.OnToolError(context.Background(), tool, "test input", errors.New("test error"))
	assert.Contains(t, buf1.String(), "Tool Error: test-tool")
	assert.Contains(t, buf2.String(), "Tool Error: test-tool")

	// Test OnAssistantLLMCall
	fanout.OnAssistantLLMCall(context.Background(), ast, []llms.MessageContent{})
	assert.Contains(t, buf1.String(), "Assistant LLM Call: test-assistant")
	assert.Contains(t, buf2.String(), "Assistant LLM Call: test-assistant")

	// Test OnToolNotFound
	fanout.OnToolNotFound(context.Background(), ast, "missing-tool")
	assert.Contains(t, buf1.String(), "Tool Not Found: missing-tool")
	assert.Contains(t, buf2.String(), "Tool Not Found: missing-tool")

	// Test OnAssistantLLMParseError
	fanout.OnAssistantLLMParseError(context.Background(), ast, "test input", "test response", errors.New("parse error"))
	assert.Contains(t, buf1.String(), "Assistant LLM Parse Error: test-assistant")
	assert.Contains(t, buf2.String(), "Assistant LLM Parse Error: test-assistant")

	// Test Add method
	var buf3 bytes.Buffer
	cb3 := callbacks.NewPrinter(&buf3, callbacks.ModeVerbose)
	fanout.Add(cb3)
	fanout.OnAssistantStart(context.Background(), ast, "test input")
	assert.Contains(t, buf3.String(), "Assistant Start: test-assistant")
}

func TestNoopCallback(t *testing.T) {
	noop := callbacks.NewNoop()
	ast := &fakeAssistant{name: "test-assistant"}
	tool := &fakeTool{name: "test-tool"}

	// Test all methods - they should not panic
	noop.OnAssistantStart(context.Background(), ast, "test input")
	noop.OnAssistantEnd(context.Background(), ast, "test input", &llms.ContentResponse{})
	noop.OnAssistantError(context.Background(), ast, "test input", errors.New("test error"))
	noop.OnAssistantLLMParseError(context.Background(), ast, "test input", "test response", errors.New("parse error"))
	noop.OnToolStart(context.Background(), tool, "test input")
	noop.OnToolEnd(context.Background(), tool, "test input", "test output")
	noop.OnToolError(context.Background(), tool, "test input", errors.New("test error"))
	noop.OnAssistantLLMCall(context.Background(), ast, []llms.MessageContent{})
	noop.OnToolNotFound(context.Background(), ast, "missing-tool")
}

type fakeAssistant struct {
	name        string
	description string
	tools       []tools.ITool
}

func (f *fakeAssistant) Name() string {
	return f.name
}
func (f *fakeAssistant) Description() string {
	return values.StringsCoalesce(f.description, "useful assistant")
}

func (f *fakeAssistant) FormatPrompt(values map[string]any) (llms.PromptValue, error) {
	return prompts.NewPromptTemplate("You are a helpful assistant.", []string{}).FormatPrompt(values)
}

func (f *fakeAssistant) GetPromptInputVariables() []string {
	return []string{}
}

func (f *fakeAssistant) Call(ctx context.Context, input string, promptInputs map[string]any, options ...assistants.Option) (*llms.ContentResponse, error) {
	return nil, nil
}

func (f *fakeAssistant) LastRunMessages() []llms.MessageContent {
	return nil
}

func (f *fakeAssistant) GetTools() []tools.ITool {
	return f.tools
}

type fakeTool struct {
	name        string
	description string
}

func (f *fakeTool) Name() string {
	return f.name
}
func (f *fakeTool) Description() string {
	return values.StringsCoalesce(f.description, "useful tool")
}
func (f *fakeTool) Parameters() any {
	return nil
}
func (f *fakeTool) Call(context.Context, string) (string, error) {
	return "", nil
}
