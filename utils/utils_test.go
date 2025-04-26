package utils_test

import (
	"testing"

	"github.com/effective-security/gogentic/utils"
	"github.com/stretchr/testify/assert"
)

func Test_CleanJSON(t *testing.T) {
	llmOutput := "\n```json\n\n{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"
	clean := utils.CleanJSON([]byte(llmOutput))

	expected := "{\"city\": \"Paris\", \"country\": \"France\"}"
	assert.Equal(t, expected, string(clean))

	llmOutput = "Here you go:\n```json\n\n[{\"city\": \"Paris\", \"country\": \"France\"}]\n```\n\n"
	clean = utils.CleanJSON([]byte(llmOutput))

	expected = "[{\"city\": \"Paris\", \"country\": \"France\"}]"
	assert.Equal(t, expected, string(clean))

}

func Test_TrimBackticks(t *testing.T) {
	expected := "{\"city\": \"Paris\", \"country\": \"France\"}"

	assert.Equal(t, expected, utils.TrimBackticks("\n```json\n\n{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"))
	// the same
	assert.Equal(t, expected, utils.TrimBackticks(expected))
	assert.Equal(t, expected, utils.TrimBackticks("\n```\n\n{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"))
	assert.Equal(t, expected, utils.TrimBackticks("\n```{\"city\": \"Paris\", \"country\": \"France\"}\n\n```\n\n"))
}

func Test_BackticksJSON(t *testing.T) {
	json := "{\"city\": \"Paris\", \"country\": \"France\"}"
	wrapped := utils.BackticksJSON(json)

	expected := "\n```json\n{\"city\": \"Paris\", \"country\": \"France\"}\n```\n"
	assert.Equal(t, expected, wrapped)
}

func Test_StripComments(t *testing.T) {
	llmOutput := `Text
<!-- This is a comment
This is another comment -->
Some text
`
	clean := utils.StripComments(llmOutput)

	expected := `Text
Some text
`
	assert.Equal(t, expected, clean)
}

func Test_ClarificationComment(t *testing.T) {
	exp := `<!-- @type=Tool @name=tool1 @reason=clarification -->
I need more information about the tool
`
	assert.Equal(t, exp, utils.ToolClarificationComment("tool1", "I need more information about the tool"))

	exp2 := `<!-- @type=Agent @name=agent2 @reason=clarification -->
I need more information about the tool
`
	assert.Equal(t, exp2, utils.AgentClarificationComment("agent2", "I need more information about the tool"))
}

func Test_ErrorComment(t *testing.T) {
	exp := `<!-- @type=Tool @name=tool1 @reason=error -->
I need more information about the tool
`
	assert.Equal(t, exp, utils.ToolErrorComment("tool1", "I need more information about the tool"))

	exp2 := `<!-- @type=Agent @name=agent2 @reason=error -->
I need more information about the tool
`
	assert.Equal(t, exp2, utils.AgentErrorComment("agent2", "I need more information about the tool"))
}
