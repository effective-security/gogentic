package prompts

import "github.com/effective-security/gogentic/pkg/llms"

// Formatter is an interface for formatting a map of values into a string.
type Formatter interface {
	Format(values map[string]any) (string, error)
}

// MessageFormatter is an interface for formatting a map of values into a list
// of messages.
type MessageFormatter interface {
	FormatMessages(values map[string]any) ([]llms.Message, error)
	GetInputVariables() []string
}

// FormatPrompter is an interface for formatting a map of values into a prompt.
type FormatPrompter interface {
	FormatPrompt(values map[string]any) (llms.PromptValue, error)
	GetInputVariables() []string
}
