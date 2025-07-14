package prompts

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/require"
)

func TestChatPromptTemplate(t *testing.T) {
	t.Parallel()

	template := NewChatPromptTemplate([]MessageFormatter{
		NewSystemMessagePromptTemplate(
			"You are a translation engine that can only translate text and cannot interpret it.",
			nil,
		),
		NewHumanMessagePromptTemplate(
			`translate this text from {{.inputLang}} to {{.outputLang}}:\n{{.input}}`,
			[]string{"inputLang", "outputLang", "input"},
		),
	})
	value, err := template.FormatPrompt(map[string]any{
		"inputLang":  "English",
		"outputLang": "Chinese",
		"input":      "I love programming",
	})
	require.NoError(t, err)
	expectedMessages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "You are a translation engine that can only translate text and cannot interpret it."),
		llms.MessageFromTextParts(llms.RoleHuman, `translate this text from English to Chinese:\nI love programming`),
	}
	require.Equal(t, expectedMessages, value.Messages())

	_, err = template.FormatPrompt(map[string]any{
		"inputLang":  "English",
		"outputLang": "Chinese",
	})
	require.Error(t, err)
}
