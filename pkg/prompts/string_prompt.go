package prompts

import "github.com/effective-security/gogentic/pkg/llms"

var _ llms.PromptValue = StringPromptValue("")

// StringPromptValue is a prompt value that is a string.
type StringPromptValue string

func (v StringPromptValue) String() string {
	return string(v)
}

// Messages returns a single-element ChatMessage slice.
func (v StringPromptValue) Messages() []llms.Message {
	return []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, string(v)),
	}
}
