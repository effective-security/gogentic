package prompts

import (
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
)

func TestChatPromptValue(t *testing.T) {
	t.Parallel()

	msgs := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "sys"),
		llms.MessageFromTextParts(llms.RoleHuman, "hello\n"),
	}
	v := ChatPromptValue(msgs)
	assert.Equal(t, "System: sys\nHuman: hello\n", v.String())
	assert.Equal(t, msgs, v.Messages())
	assert.Empty(t, ChatPromptValue(nil).String())
}
