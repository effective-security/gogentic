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
