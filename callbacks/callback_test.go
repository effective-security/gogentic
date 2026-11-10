package callbacks_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/assistants"
	"github.com/effective-security/gogentic/callbacks"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/prompts"
	"github.com/effective-security/gogentic/skills"
	"github.com/effective-security/gogentic/tools"
	"github.com/effective-security/x/values"
	"github.com/effective-security/xlog"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
)

func TestCallback(t *testing.T) {
	var buf bytes.Buffer
	cb := callbacks.NewPrinter(&buf, callbacks.ModeVerbose)

	ast := &fakeAssistant{name: "test-assistant"}
	tool := &fakeTool{name: "test-tool"}

	cb.OnAssistantStart(context.Background(), ast, "test input")
	cb.OnAssistantEnd(context.Background(), ast, "test input", &assistants.Response{
		Choices: []*llms.ContentChoice{
			{
				Content: "test output",
			},
		},
		Messages: []llms.Message{
			{
				Role: llms.RoleHuman,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "test input"},
				},
			},
		},
	}, llms.Messages{})
	cb.OnAssistantError(context.Background(), ast, "test input", errors.New("test error"), []llms.Message{})
	cb.OnToolStart(context.Background(), tool, "test-assistant", "test input")
	cb.OnToolEnd(context.Background(), tool, "test-assistant", "test input", "test output")
	cb.OnToolError(context.Background(), tool, "test-assistant", "test input", errors.New("test error"))

	res := buf.String()
	assert.Contains(t, res, "Assistant Start: test-assistant")
	assert.Contains(t, res, "Input: test input")
	assert.Contains(t, res, "Assistant End: test-assistant")
	assert.Contains(t, res, "Tool Start: test-tool")
	assert.Contains(t, res, "Tool End: test-tool")
	assert.Contains(t, res, "Output: test output")
	assert.Contains(t, res, "Tool Error: test-tool (test-assistant): test error")
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

	descr := assistants.GetDescriptions(ast1, ast2).Render(llmutils.RenderFormatJSONWithBackticks)
	exp := "\n```json" + `
[
  {
    "Name": "test-assistant1",
    "Description": "test assistant1. Line 1."
  },
  {
    "Name": "test-assistant2",
    "Description": "test assistant2. Line 2."
  }
]
` + "```\n"
	assert.Equal(t, exp, descr)

	descr = assistants.GetDescriptions(ast1, ast2).Render(llmutils.RenderFormatMarkdown)
	exp = `- Name: test-assistant1
  Description: test assistant1. Line 1.
- Name: test-assistant2
  Description: test assistant2. Line 2.
`
	assert.Equal(t, exp, descr)

	descr = assistants.GetDescriptions(ast1, ast2).Render(llmutils.RenderFormatJSONIndent)
	exp = `[
  {
    "Name": "test-assistant1",
    "Description": "test assistant1. Line 1."
  },
  {
    "Name": "test-assistant2",
    "Description": "test assistant2. Line 2."
  }
]`
	assert.Equal(t, exp, descr)

	descr = assistants.GetDescriptions(ast1, ast2).Render(llmutils.RenderFormatYAMLWithBackticks)
	exp = "\n```yaml" + `
- Name: test-assistant1
  Description: test assistant1. Line 1.
- Name: test-assistant2
  Description: test assistant2. Line 2.
` + "```\n"
	assert.Equal(t, exp, descr)

	descr = assistants.GetDescriptions(ast1, ast2).Render(llmutils.RenderFormatYAML)
	exp = `- Name: test-assistant1
  Description: test assistant1. Line 1.
- Name: test-assistant2
  Description: test assistant2. Line 2.
`
	assert.Equal(t, exp, descr)

	descr = assistants.GetDescriptionsWithTools(ast1, ast2).Render(llmutils.RenderFormatJSONWithBackticks)
	exp = "\n```json" + `
[
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
` + "```\n"
	assert.Equal(t, exp, descr)

	descr = assistants.GetDescriptionsWithTools(ast1, ast2).Render(llmutils.RenderFormatYAMLWithBackticks)
	exp = "\n```yaml" + `
- Name: test-assistant1
  Description: test assistant1. Line 1.
  Tools:
    - Name: test-tool1
      Description: test tool 1. Line 1.
    - Name: test-tool2
      Description: test tool 2. Line 2.
- Name: test-assistant2
  Description: test assistant2. Line 2.
  Tools:
    - Name: test-tool2
      Description: test tool 2. Line 2.
    - Name: test-tool3
      Description: test tool 3. Line 3.
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
	fanout.OnAssistantEnd(context.Background(), ast, "test input", &assistants.Response{
		Choices: []*llms.ContentChoice{
			{
				Content: "test output",
			},
		},
		Messages: []llms.Message{
			{
				Role: llms.RoleHuman,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "test input"},
				},
			},
		},
	}, llms.Messages{})
	assert.Contains(t, buf1.String(), "Assistant End: test-assistant")
	assert.Contains(t, buf2.String(), "Assistant End: test-assistant")

	// Test OnAssistantError
	fanout.OnAssistantError(context.Background(), ast, "test input", errors.New("test error"), []llms.Message{})
	assert.Contains(t, buf1.String(), "Assistant Error: test-assistant")
	assert.Contains(t, buf2.String(), "Assistant Error: test-assistant")

	// Test OnToolStart
	fanout.OnToolStart(context.Background(), tool, "test-assistant", "test input")
	assert.Contains(t, buf1.String(), "Tool Start: test-tool (test-assistant)")
	assert.Contains(t, buf2.String(), "Tool Start: test-tool (test-assistant)")

	// Test OnToolEnd
	fanout.OnToolEnd(context.Background(), tool, "test-assistant", "test input", "test output")
	assert.Contains(t, buf1.String(), "Tool End: test-tool (test-assistant)")
	assert.Contains(t, buf2.String(), "Tool End: test-tool (test-assistant)")

	// Test OnToolError
	fanout.OnToolError(context.Background(), tool, "test-assistant", "test input", errors.New("test error"))
	assert.Contains(t, buf1.String(), "Tool Error: test-tool (test-assistant)")
	assert.Contains(t, buf2.String(), "Tool Error: test-tool (test-assistant)")

	// Test OnAssistantLLMCall
	fanout.OnAssistantLLMCallStart(context.Background(), ast, &fakeModel{name: "gpt-4o", provider: llms.ProviderOpenAI}, []llms.Message{})
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
	noop.OnAssistantEnd(context.Background(), ast, "test input", &assistants.Response{}, llms.Messages{})
	noop.OnAssistantError(context.Background(), ast, "test input", errors.New("test error"), llms.Messages{})
	noop.OnAssistantLLMParseError(context.Background(), ast, "test input", "test response", errors.New("parse error"))
	noop.OnToolStart(context.Background(), tool, "test-assistant", "test input")
	noop.OnToolEnd(context.Background(), tool, "test-assistant", "test input", "test output")
	noop.OnToolError(context.Background(), tool, "test-assistant", "test input", errors.New("test error"))
	noop.OnAssistantLLMCallStart(context.Background(), ast, &fakeModel{name: "gpt-4o", provider: llms.ProviderOpenAI}, []llms.Message{})
	noop.OnToolNotFound(context.Background(), ast, "missing-tool")
}

type fakeAssistant struct {
	name        string
	description string
	tools       []tools.ITool
	skills      skills.Skills
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

func (f *fakeAssistant) Call(ctx context.Context, input *assistants.CallInput) (*assistants.Response, error) {
	return nil, nil
}

func (f *fakeAssistant) LastRunMessages() []llms.Message {
	return nil
}

func (f *fakeAssistant) GetTools() []tools.ITool {
	return f.tools
}

func (f *fakeAssistant) GetSkills() skills.Skills {
	return f.skills
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
func (f *fakeTool) Parameters() *jsonschema.Schema {
	return nil
}
func (f *fakeTool) Call(context.Context, string) (string, error) {
	return "", nil
}

type fakeModel struct {
	name     string
	provider llms.ProviderType
}

func (m *fakeModel) GetName() string                    { return m.name }
func (m *fakeModel) GetProviderType() llms.ProviderType { return m.provider }
func (m *fakeModel) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return nil, nil
}

const (
	testRepo      = "github.com/effective-security/gogentic"
	testLoggerPkg = "callbacks_test"
	testModelName = "gpt-4o"
)

func newSkilledAssistant() *fakeAssistant {
	return &fakeAssistant{
		name: "test-assistant",
		skills: skills.Skills{
			{
				Name: "triage",
			},
			{
				Name: "search",
			},
		},
	}
}

func TestPrinter_DefaultMode(t *testing.T) {
	var buf bytes.Buffer
	cb := callbacks.NewPrinter(&buf, callbacks.ModeDefault)
	ctx := context.Background()
	ast := newSkilledAssistant()
	tool := &fakeTool{name: "test-tool"}
	model := &fakeModel{
		name:     testModelName,
		provider: llms.ProviderOpenAI,
	}

	cb.OnAssistantStart(ctx, ast, "test input")
	cb.OnAssistantLLMCallStart(ctx, ast, model, []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	})
	cb.OnAssistantLLMCallEnd(ctx, ast, model, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "a",
			},
			{
				Content: "b",
			},
		},
	})
	cb.OnToolEnd(ctx, tool, ast.Name(), "in", "not printed in default mode")
	cb.OnAssistantEnd(ctx, ast, "test input", &assistants.Response{
		Choices: []*llms.ContentChoice{
			{
				Content: "not printed in default mode",
			},
		},
	}, nil)
	cb.OnAssistantLLMParseError(ctx, ast, "test input", "raw", errors.New("bad json"))

	exp := `Assistant Start: test-assistant
Skills: triage, search
Input: test input
Assistant LLM Call: test-assistant: gpt-4o model, 1 messages
Assistant LLM Call End: test-assistant: gpt-4o model, 2 messages
Tool End: test-tool (test-assistant)
Assistant End: test-assistant
Assistant LLM Parse Error: test-assistant: bad json
Response: raw
`
	assert.Equal(t, exp, buf.String())
}

func TestFanout_OnAssistantLLMCallEnd(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	fanout := callbacks.NewFanout(
		callbacks.NewPrinter(&buf1, callbacks.ModeDefault),
		callbacks.NewPrinter(&buf2, callbacks.ModeDefault),
	)
	ast := &fakeAssistant{name: "test-assistant"}
	fanout.OnAssistantLLMCallEnd(context.Background(), ast, &fakeModel{name: testModelName}, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "a",
			},
		},
	})

	exp := "Assistant LLM Call End: test-assistant: gpt-4o model, 1 messages\n"
	assert.Equal(t, exp, buf1.String())
	assert.Equal(t, exp, buf2.String())
}

func TestNoop_Progress(t *testing.T) {
	ctx := context.Background()
	ast := &fakeAssistant{name: "test-assistant"}
	noop := callbacks.NewNoop()

	// Without a progress callback the events are dropped.
	noop.OnProgress(ctx, ast, "title", "message")
	noop.OnAssistantLLMCallEnd(ctx, ast, &fakeModel{name: testModelName}, &llms.ContentResponse{})

	var got []string
	noop.WithProgress(func(_ context.Context, a assistants.IAssistant, title, message string) {
		got = append(got, a.Name()+"|"+title+"|"+message)
	})
	noop.OnProgress(ctx, ast, "step 1", "working")
	noop.OnProgress(ctx, ast, "step 2", "done")
	assert.Equal(t, []string{
		"test-assistant|step 1|working",
		"test-assistant|step 2|done",
	}, got)
}

// captureLogs routes xlog output to a buffer for the duration of the test.
// The formatter is process-wide, so callers must not run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := xlog.GetFormatter()
	f := xlog.NewStringFormatter(&buf, xlog.FormatSkipTime(true), xlog.FormatWithCaller(false))
	xlog.SetFormatter(f)
	t.Cleanup(func() {
		xlog.SetFormatter(prev)
	})
	xlog.SetPackageLogLevel(testRepo, testLoggerPkg, xlog.DEBUG)
	return &buf
}

func TestPackageLogger(t *testing.T) {
	// The level must be set after the package logger is registered.
	cb := callbacks.NewPackageLogger(xlog.NewPackageLogger(testRepo, testLoggerPkg))
	buf := captureLogs(t)
	ctx := context.Background()
	ast := &fakeAssistant{name: "a1"}
	tool := &fakeTool{name: "t1"}
	model := &fakeModel{
		name:     testModelName,
		provider: llms.ProviderOpenAI,
	}

	cb.OnAssistantStart(ctx, ast, "hello")
	cb.OnAssistantEnd(ctx, ast, "hello", &assistants.Response{
		Choices: []*llms.ContentChoice{
			{
				Content: "answer",
			},
			{
				// empty content is not logged
				Content: "",
			},
		},
	}, nil)
	cb.OnAssistantError(ctx, ast, "hello", errors.New("boom"), nil)
	cb.OnAssistantError(ctx, ast, "hello", errors.WithStack(context.DeadlineExceeded), nil)
	cb.OnAssistantLLMParseError(ctx, ast, "hello", "raw", errors.New("bad json"))
	cb.OnToolStart(ctx, tool, "a1", "in")
	cb.OnToolEnd(ctx, tool, "a1", "in", "out")
	cb.OnToolError(ctx, tool, "a1", "in", errors.New("tool failed"))
	cb.OnToolError(ctx, tool, "a1", "in", errors.New("request timeout"))
	cb.OnAssistantLLMCallStart(ctx, ast, model, []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
		llms.MessageFromTextParts(llms.RoleAI, "there"),
	})
	cb.OnAssistantLLMCallEnd(ctx, ast, model, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "a",
			},
		},
	})
	cb.OnToolNotFound(ctx, ast, "missing")

	// Timeouts are logged as warnings, other failures as errors.
	exp := `level=D pkg=callbacks_test event=assistant_start assistant=a1 input=hello
level=D pkg=callbacks_test event=assistant_end assistant=a1
level=D pkg=callbacks_test result=answer
level=E pkg=callbacks_test event=assistant_error assistant=a1 err=boom
level=W pkg=callbacks_test event=assistant_error assistant=a1 err="context deadline exceeded"
level=D pkg=callbacks_test event=assistant_llm_parse_error assistant=a1 err="bad json" response=raw
level=D pkg=callbacks_test event=tool_start assistant=a1 tool=t1 input=in
level=D pkg=callbacks_test event=tool_end assistant=a1 tool=t1 output=out
level=E pkg=callbacks_test event=tool_error assistant=a1 tool=t1 err="tool failed"
level=W pkg=callbacks_test event=tool_error assistant=a1 tool=t1 err="request timeout"
level=D pkg=callbacks_test event=assistant_llm_call_start assistant=a1 model=gpt-4o messages=2
level=D pkg=callbacks_test event=assistant_llm_call_end assistant=a1 model=gpt-4o messages=1
level=D pkg=callbacks_test event=tool_not_found assistant=a1 tool=missing
`
	assert.Equal(t, exp, buf.String())
}

func TestIsTimeout(t *testing.T) {
	tests := []struct {
		err error
		exp bool
	}{
		{err: nil, exp: false},
		{err: errors.New("boom"), exp: false},
		{err: context.DeadlineExceeded, exp: true},
		{err: errors.Wrap(context.Canceled, "aborted"), exp: true},
		{err: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), exp: true},
		{err: errors.New("i/o timeout"), exp: true},
		{err: errors.New("deadline reached"), exp: true},
		{err: errors.New("request cancelled"), exp: true},
	}
	for _, tc := range tests {
		name := "nil"
		if tc.err != nil {
			name = tc.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.exp, callbacks.IsTimeout(tc.err))
		})
	}
}
