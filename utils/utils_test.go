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

	llmOutput = `Text
<!-- @type=tool @name=tool1 @content=clarification -->
Some text
<!-- @type=assistant @name=agent2 @content=clarification -->
I need more information about the tool
<!-- @type=tool @name=tool1 @content=error -->
I need more information about the tool
`
	clean = utils.RemoveAllComments(llmOutput)
	expected = `Text
Some text
I need more information about the tool
I need more information about the tool
`
	assert.Equal(t, expected, clean)
}

func Test_ClarificationComment(t *testing.T) {
	exp := `<!-- @type=tool @name=tool1 @content=clarification -->
I need more information about the tool
`
	assert.Equal(t, exp, utils.ToolClarificationComment("tool1", "I need more information about the tool\n"))

	exp2 := `<!-- @type=assistant @name=agent2 @content=clarification -->
I need more information about the tool
`
	assert.Equal(t, exp2, utils.AssistantClarificationComment("agent2", "I need more information about the tool\n"))
}

func Test_ErrorComment(t *testing.T) {
	exp := `<!-- @type=tool @name=tool1 @content=error -->
I need more information about the tool
`
	assert.Equal(t, exp, utils.ToolErrorComment("tool1", "I need more information about the tool\n"))

	exp2 := `<!-- @type=assistant @name=agent2 @content=error -->
I need more information about the tool
`
	assert.Equal(t, exp2, utils.AssistantErrorComment("agent2", "I need more information about the tool\n"))
}

func Test_ObservationComment(t *testing.T) {
	exp := `<!-- @type=assistant @name=agent2 @content=observation -->
I need more information about the tool
`
	assert.Equal(t, exp, utils.AssistantObservationComment("agent2", "I need more information about the tool\n"))
}
