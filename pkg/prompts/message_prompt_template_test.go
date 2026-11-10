package prompts

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	msgTmpl        = "Hello {{.who}}"
	msgVar         = "who"
	msgValue       = "world"
	msgText        = "Hello world"
	msgRole        = "narrator"
	placeholderVar = "chat"
	errMissingKey  = `template: template:1:8: executing "template" at <.who>: map has no entry for key "who"`
	errPlaceholder = "chat should be a list of chat messages: variable should be a list of chat messages"
)

func TestMessagePromptTemplates(t *testing.T) {
	t.Parallel()

	vars := []string{msgVar}
	values := map[string]any{
		msgVar: msgValue,
	}
	tests := []struct {
		name      string
		formatter MessageFormatter
		want      llms.Message
	}{
		{
			name:      "system",
			formatter: NewSystemMessagePromptTemplate(msgTmpl, vars),
			want:      llms.MessageFromTextParts(llms.RoleSystem, msgText),
		},
		{
			name:      "ai",
			formatter: NewAIMessagePromptTemplate(msgTmpl, vars),
			want:      llms.MessageFromTextParts(llms.RoleAI, msgText),
		},
		{
			name:      "human",
			formatter: NewHumanMessagePromptTemplate(msgTmpl, vars),
			want:      llms.MessageFromTextParts(llms.RoleHuman, msgText),
		},
		{
			name:      "generic",
			formatter: NewGenericMessagePromptTemplate(msgRole, msgTmpl, vars),
			want:      llms.MessageFromTextParts(llms.RoleGeneric, msgText, msgRole),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, vars, tc.formatter.GetInputVariables())

			msgs, err := tc.formatter.FormatMessages(values)
			require.NoError(t, err)
			assert.Equal(t, []llms.Message{tc.want}, msgs)

			_, err = tc.formatter.FormatMessages(map[string]any{})
			require.EqualError(t, err, errMissingKey)
		})
	}
}

func TestMessagesPlaceholder(t *testing.T) {
	t.Parallel()

	p := MessagesPlaceholder{
		VariableName: placeholderVar,
	}
	assert.Equal(t, []string{placeholderVar}, p.GetInputVariables())

	history := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "q"),
		llms.MessageFromTextParts(llms.RoleAI, "a"),
	}
	msgs, err := p.FormatMessages(map[string]any{
		placeholderVar: history,
	})
	require.NoError(t, err)
	assert.Equal(t, history, msgs)

	tests := []struct {
		name   string
		values map[string]any
	}{
		{
			name:   "missing",
			values: map[string]any{},
		},
		{
			name: "wrong type",
			values: map[string]any{
				placeholderVar: "not messages",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msgs, err := p.FormatMessages(tc.values)
			require.ErrorIs(t, err, ErrNeedChatMessageList)
			require.EqualError(t, err, errPlaceholder)
			assert.Nil(t, msgs)
		})
	}
}
