package llmutils_test

import (
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/stretchr/testify/assert"
)

func Test_CleanJSON(t *testing.T) {
	llmOutput := "\n```json\n\n{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"
	clean := llmutils.CleanJSON([]byte(llmOutput))

	expected := "{\"city\": \"Paris\", \"country\": \"France\"}"
	assert.Equal(t, expected, string(clean))

	llmOutput = "Here you go:\n```json\n\n[{\"city\": \"Paris\", \"country\": \"France\"}]\n```\n\n"
	clean = llmutils.CleanJSON([]byte(llmOutput))

	expected = "[{\"city\": \"Paris\", \"country\": \"France\"}]"
	assert.Equal(t, expected, string(clean))

	resp := "{\n\t\"answer\": \"Here is the search query used to find the top 5 assets under attack:\\n\\n```json\\n{\\n  \\\"queryId\\\": \\\"Asset\\\",\\n  \\\"filterQuery\\\": {\\n    \\\"term\\\": {\\n      \\\"asset.OnAttack\\\": true\\n    }\\n  },\\n  \\\"sort\\\": \\\"asset.AttackCount DESC\\\",\\n  \\\"limit\\\": 5\\n}\\n```\",\n\t\"chatTitle\": \"Top 5 Assets Under Attack\",\n\t\"actions\": []\n}"
	assert.Equal(t, resp, string(llmutils.CleanJSON([]byte(resp))))
}

func Test_TrimBackticks(t *testing.T) {
	expected := "{\"city\": \"Paris\", \"country\": \"France\"}"

	assert.Equal(t, expected, llmutils.TrimBackticks("\n```json\n\n{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"))
	// the same
	assert.Equal(t, expected, llmutils.TrimBackticks(expected))
	assert.Equal(t, expected, llmutils.TrimBackticks("\n```\n\n{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"))
	assert.Equal(t, expected, llmutils.TrimBackticks("\n```{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"))
}

func Test_BackticksJSON(t *testing.T) {
	json := "{\"city\": \"Paris\", \"country\": \"France\"}"
	wrapped := llmutils.BackticksJSON(json)

	expected := "\n```json\n{\"city\": \"Paris\", \"country\": \"France\"}\n```\n"
	assert.Equal(t, expected, wrapped)
}

func Test_StripComments(t *testing.T) {
	llmOutput := `Text
<!-- This is a comment
This is another comment -->
Some text
`
	clean := llmutils.StripComments(llmOutput)

	expected := `Text
Some text
`
	assert.Equal(t, expected, clean)

	llmOutput = `Text
<!-- @type=tool @name=tool1 @content=clarification -->
Some text
<!-- @type=assistant @name=agent2 @content=clarification -->
I need more information about the tool
<!-- @type=tool @name=tool1 @content=error -->
I need more information about the tool
`
	clean = llmutils.RemoveAllComments(llmOutput)
	expected = `Text
Some text
I need more information about the tool
I need more information about the tool
`
	assert.Equal(t, expected, clean)
}

func Test_AddComment(t *testing.T) {
	exp := `<!-- @role=tool @name=tool1 @content=clarification -->
I need more information about the tool
`
	assert.Equal(t, exp, llmutils.AddComment("tool", "tool1", "clarification", "I need more information about the tool\n"))
}

func Test_ExtractTag(t *testing.T) {
	assert.Equal(t, "tool1", llmutils.ExtractTag("#tool1 question", "#"))
	assert.Equal(t, "agent", llmutils.ExtractTag("@agent question", "@"))

	assert.Equal(t, "agent", llmutils.ExtractTag("@agent  \n  question", "@"))
	assert.Equal(t, "agent", llmutils.ExtractTag("@agent", "@"))
}

func Test_FindLastUserQuestion(t *testing.T) {
	msgs := []llms.Message{
		{
			Role: llms.RoleSystem,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "What is the capital of Italy?"},
			},
		},
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "What is the capital of Germany?"},
			},
		},
		{
			Role: llms.RoleTool,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "1", Type: "tool", FunctionCall: &llms.FunctionCall{Name: "tool1", Arguments: "arg1"}},
			},
		},
		{
			Role: llms.RoleTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: "1", Name: "tool1", Content: "tool1 result"},
			},
		},
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "What is the capital of France?"},
			},
		},
	}

	question := llmutils.FindLastUserQuestion(msgs)
	assert.Equal(t, "What is the capital of Germany?", question)

	// role, question := llmutils.GetLastMessage(msgs)
	// assert.Equal(t, string(llms.ChatMessageTypeAI), role)
	// assert.Equal(t, "What is the capital of France?", question)

	var buf strings.Builder
	llmutils.PrintMessages(&buf, msgs)
	exp := `System: What is the capital of Italy?
Human: What is the capital of Germany?
Tool: Tool Call: {"type":"tool_call","tool_call":{"function":{"name":"tool1","arguments":"arg1"},"id":"1","type":"tool"}}
Tool: tool1: Response: {"type":"tool_response","tool_response":{"tool_call_id":"1","name":"tool1","content":"tool1 result"}}
AI: What is the capital of France?
`
	assert.Equal(t, exp, buf.String())
}

func Test_EnsureNewline(t *testing.T) {
	assert.Equal(t, "", llmutils.EnsureEndsWithNewline(" \n"))
	assert.Equal(t, "Hello\n", llmutils.EnsureEndsWithNewline(" \nHello"))
	assert.Equal(t, "Hello\n", llmutils.EnsureEndsWithNewline("\nHello\n"))
	assert.Equal(t, "Hello\n", llmutils.EnsureEndsWithNewline("Hello\n\n"))
	assert.Equal(t, "Hello\n", llmutils.EnsureEndsWithNewline("Hello\n\n\n"))
	assert.Equal(t, "Hello\n", llmutils.EnsureEndsWithNewline("Hello\n\n\n\n"))
	assert.Equal(t, "Hello\n", llmutils.EnsureEndsWithNewline("Hello\n\n\n\n\n"))
}

func Test_JSONIndent(t *testing.T) {
	input := `{"name":"John","age":30}`
	expected := "{\n\t\"name\": \"John\",\n\t\"age\": 30\n}"
	assert.Equal(t, expected, llmutils.JSONIndent(input))
}

func Test_ToJSON(t *testing.T) {
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p := Person{Name: "John", Age: 30}
	expected := `{"name":"John","age":30}`
	assert.Equal(t, expected, llmutils.ToJSON(p))
}

func Test_ToJSONIndent(t *testing.T) {
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p := Person{Name: "John", Age: 30}
	expected := "{\n\t\"name\": \"John\",\n\t\"age\": 30\n}"
	assert.Equal(t, expected, llmutils.ToJSONIndent(p))
}

func Test_ToYAML(t *testing.T) {
	type Person struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age"`
	}
	p := Person{Name: "John", Age: 30}
	expected := "name: John\nage: 30\n"
	assert.Equal(t, expected, llmutils.ToYAML(p))
}

func Test_BackticksYAM(t *testing.T) {
	yaml := "name: John\nage: 30"
	expected := "\n```yaml\nname: John\nage: 30\n```\n"
	assert.Equal(t, expected, llmutils.BackticksYAM(yaml))
}

type CustomString struct{}

func (c CustomString) String() string { return "custom string" }

func Test_Stringify(t *testing.T) {
	// Test with string
	assert.Equal(t, "hello", llmutils.Stringify("hello"))

	// Test with struct
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p := Person{Name: "John", Age: 30}
	expected := "\n```json\n{\n\t\"name\": \"John\",\n\t\"age\": 30\n}\n```\n"
	assert.Equal(t, expected, llmutils.Stringify(p))

	// Test with Stringer interface
	assert.Equal(t, "custom string", llmutils.Stringify(CustomString{}))
}

func Test_NewContentResponse(t *testing.T) {
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p := Person{Name: "John", Age: 30}
	resp := llmutils.NewContentResponse(p)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Choices, 1)
	expected := "\n```json\n{\n\t\"name\": \"John\",\n\t\"age\": 30\n}\n```\n"
	assert.Equal(t, expected, resp.Choices[0].Content)
}

func Test_MergeInputs(t *testing.T) {
	configInputs := map[string]any{
		"name": "John",
		"age":  30,
	}
	userInputs := map[string]any{
		"age":  35,
		"city": "New York",
	}
	expected := map[string]any{
		"name": "John",
		"age":  35,
		"city": "New York",
	}
	assert.Equal(t, expected, llmutils.MergeInputs(configInputs, userInputs))
}

func Test_CountMessagesContentSize(t *testing.T) {
	msgs := []llms.Message{
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "Hello"},
			},
		},
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "Hi there"},
			},
		},
	}
	size := llmutils.CountMessagesContentSize(msgs)
	assert.Greater(t, size, uint64(0))
}

func Test_CountResponseContentSize(t *testing.T) {
	resp := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Hello world",
			},
		},
	}
	size := llmutils.CountResponseContentSize(resp)
	assert.Greater(t, size, uint64(0))
}

func TestPrintMessageContents(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		messages []llms.Message
		expected string
	}{
		{
			name:     "No messages",
			messages: []llms.Message{},
			expected: "",
		},
		{
			name: "Mixed messages",
			messages: []llms.Message{
				llms.MessageFromTextParts(llms.RoleSystem, "Please be polite."),
				llms.MessageFromTextParts(llms.RoleHuman, "Hello, how are you?"),
				llms.MessageFromTextParts(llms.RoleAI, "I'm doing great!"),
				llms.MessageFromTextParts(llms.RoleGeneric, "Keep the conversation on topic."),
				llms.MessageFromToolCalls(llms.RoleTool, llms.ToolCall{ID: "1", Type: "tool", FunctionCall: &llms.FunctionCall{Name: "tool1", Arguments: "arg1"}}),
				llms.MessageFromToolResponse(llms.RoleTool, llms.ToolCallResponse{ToolCallID: "1", Name: "tool1", Content: "tool1 result"}),
			},
			expected: `System: Please be polite.
Human: Hello, how are you?
AI: I'm doing great!
Generic: Keep the conversation on topic.
Tool: Tool Call: {"type":"tool_call","tool_call":{"function":{"name":"tool1","arguments":"arg1"},"id":"1","type":"tool"}}
Tool: tool1: Response: {"type":"tool_response","tool_response":{"tool_call_id":"1","name":"tool1","content":"tool1 result"}}
`, //nolint:lll
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			llmutils.PrintMessages(&buf, tc.messages)
			assert.Equal(t, tc.expected, buf.String())
		})
	}
}
