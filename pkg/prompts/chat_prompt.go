package prompts

import (
	"strings"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
)

var _ llms.PromptValue = ChatPromptValue{}

// ChatPromptValue is a prompt value that is a list of chat messages.
type ChatPromptValue []llms.Message

// String returns the chat message slice as a buffer string.
func (v ChatPromptValue) String() string {
	var buf strings.Builder
	llmutils.PrintMessages(&buf, v)
	return buf.String()
}

// Messages returns the ChatMessage slice.
func (v ChatPromptValue) Messages() []llms.Message {
	return v
}
