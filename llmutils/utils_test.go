package llmutils_test

import (
	"strings"
	"testing"

	"github.com/effective-security/gogentic/llmutils"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
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
	msgs := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "What is the capital of Italy?"},
			},
		},
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "What is the capital of Germany?"},
			},
		},
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "1", Type: "tool", FunctionCall: &llms.FunctionCall{Name: "tool1", Arguments: "arg1"}},
			},
		},
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: "1", Name: "tool1", Content: "tool1 result"},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
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
	llmutils.PrintMessageContents(&buf, msgs)
	exp := `SYSTEM: What is the capital of Italy?
HUMAN: What is the capital of Germany?
TOOL: ToolCall ID=1, Type=tool, Func=tool1(arg1)
TOOL: ToolCallResponse ID=1, Name=tool1, Content=tool1 result
AI: What is the capital of France?
`
	assert.Equal(t, exp, buf.String())
}
