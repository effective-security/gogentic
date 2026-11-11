package prompts

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
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

const (
	varName     = "name"
	varHistory  = "history"
	nameValue   = "Bob"
	systemText  = "You are helpful."
	humanTmpl   = "Hi {{.name}}"
	humanText   = "Hi Bob"
	errBadParam = "unexpected type for bad: invalid partial variable type"
)

func newTestChatTemplate() ChatPromptTemplate {
	return NewChatPromptTemplate([]MessageFormatter{
		NewSystemMessagePromptTemplate(systemText, nil),
		NewHumanMessagePromptTemplate(humanTmpl, []string{varName}),
		MessagesPlaceholder{
			VariableName: varHistory,
		},
		NewGenericMessagePromptTemplate("critic", "Again {{.name}}", []string{varName}),
	})
}

func TestChatPromptTemplateFormat(t *testing.T) {
	t.Parallel()

	tmpl := newTestChatTemplate()
	values := map[string]any{
		varName: nameValue,
		varHistory: []llms.Message{
			llms.MessageFromTextParts(llms.RoleAI, "earlier"),
		},
	}

	got, err := tmpl.Format(values)
	require.NoError(t, err)
	assert.Equal(t, "System: You are helpful.\nHuman: Hi Bob\nAI: earlier\nGeneric: Again Bob\ncritic\n", got)

	msgs, err := tmpl.FormatMessages(values)
	require.NoError(t, err)
	assert.Equal(t, []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, systemText),
		llms.MessageFromTextParts(llms.RoleHuman, humanText),
		llms.MessageFromTextParts(llms.RoleAI, "earlier"),
		llms.MessageFromTextParts(llms.RoleGeneric, "Again Bob", "critic"),
	}, msgs)
}

func TestChatPromptTemplateErrors(t *testing.T) {
	t.Parallel()

	tmpl := newTestChatTemplate()
	tmpl.PartialVariables = map[string]any{
		"bad": 1,
	}
	_, err := tmpl.FormatPrompt(map[string]any{})
	require.ErrorIs(t, err, ErrInvalidPartialVariableType)
	require.EqualError(t, err, errBadParam)

	// F-012: Format used to panic on the nil prompt value.
	text, err := ChatPromptTemplate{
		PartialVariables: map[string]any{
			"x": 1,
		},
	}.Format(nil)
	require.ErrorIs(t, err, ErrInvalidPartialVariableType)
	assert.EqualError(t, err, "unexpected type for x: invalid partial variable type")
	assert.Empty(t, text)

	msgs, err := newTestChatTemplate().FormatMessages(map[string]any{
		varName: nameValue,
	})
	require.ErrorIs(t, err, ErrNeedChatMessageList)
	assert.Nil(t, msgs)

	text, err = newTestChatTemplate().Format(map[string]any{
		varName: nameValue,
	})
	require.ErrorIs(t, err, ErrNeedChatMessageList)
	assert.Empty(t, text)
}

func TestChatPromptTemplateGetInputVariables(t *testing.T) {
	t.Parallel()

	assert.ElementsMatch(t, []string{varName, varHistory}, newTestChatTemplate().GetInputVariables())
	assert.Empty(t, NewChatPromptTemplate(nil).GetInputVariables())
}
