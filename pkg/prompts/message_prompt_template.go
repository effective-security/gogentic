package prompts

import (
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// SystemMessagePromptTemplate is a message formatter that returns a system message.
type SystemMessagePromptTemplate struct {
	Prompt PromptTemplate
}

var _ MessageFormatter = SystemMessagePromptTemplate{}

// FormatMessages formats the message with the values given.
func (p SystemMessagePromptTemplate) FormatMessages(values map[string]any) ([]llms.Message, error) {
	text, err := p.Prompt.Format(values)
	return []llms.Message{llms.MessageFromTextParts(llms.RoleSystem, text)}, err
}

// GetInputVariables returns the input variables the prompt expects.
func (p SystemMessagePromptTemplate) GetInputVariables() []string {
	return p.Prompt.InputVariables
}

// NewSystemMessagePromptTemplate creates a new system message prompt template.
func NewSystemMessagePromptTemplate(template string, inputVariables []string) SystemMessagePromptTemplate {
	return SystemMessagePromptTemplate{
		Prompt: NewPromptTemplate(template, inputVariables),
	}
}

// AIMessagePromptTemplate is a message formatter that returns an AI message.
type AIMessagePromptTemplate struct {
	Prompt PromptTemplate
}

var _ MessageFormatter = AIMessagePromptTemplate{}

// FormatMessages formats the message with the values given.
func (p AIMessagePromptTemplate) FormatMessages(values map[string]any) ([]llms.Message, error) {
	text, err := p.Prompt.Format(values)
	return []llms.Message{llms.MessageFromTextParts(llms.RoleAI, text)}, err
}

// GetInputVariables returns the input variables the prompt expects.
func (p AIMessagePromptTemplate) GetInputVariables() []string {
	return p.Prompt.InputVariables
}

// NewAIMessagePromptTemplate creates a new AI message prompt template.
func NewAIMessagePromptTemplate(template string, inputVariables []string) AIMessagePromptTemplate {
	return AIMessagePromptTemplate{
		Prompt: NewPromptTemplate(template, inputVariables),
	}
}

// HumanMessagePromptTemplate is a message formatter that returns a human message.
type HumanMessagePromptTemplate struct {
	Prompt PromptTemplate
}

var _ MessageFormatter = HumanMessagePromptTemplate{}

// FormatMessages formats the message with the values given.
func (p HumanMessagePromptTemplate) FormatMessages(values map[string]any) ([]llms.Message, error) {
	text, err := p.Prompt.Format(values)
	return []llms.Message{llms.MessageFromTextParts(llms.RoleHuman, text)}, err
}

// GetInputVariables returns the input variables the prompt expects.
func (p HumanMessagePromptTemplate) GetInputVariables() []string {
	return p.Prompt.InputVariables
}

// NewHumanMessagePromptTemplate creates a new human message prompt template.
func NewHumanMessagePromptTemplate(template string, inputVariables []string) HumanMessagePromptTemplate {
	return HumanMessagePromptTemplate{
		Prompt: NewPromptTemplate(template, inputVariables),
	}
}

// GenericMessagePromptTemplate is a message formatter that returns message with the specified speaker.
type GenericMessagePromptTemplate struct {
	Prompt PromptTemplate
	Role   string
}

var _ MessageFormatter = GenericMessagePromptTemplate{}

// FormatMessages formats the message with the values given.
func (p GenericMessagePromptTemplate) FormatMessages(values map[string]any) ([]llms.Message, error) {
	text, err := p.Prompt.Format(values)
	return []llms.Message{llms.MessageFromTextParts(llms.RoleGeneric, text, p.Role)}, err
}

// GetInputVariables returns the input variables the prompt expects.
func (p GenericMessagePromptTemplate) GetInputVariables() []string {
	return p.Prompt.InputVariables
}

// NewGenericMessagePromptTemplate creates a new generic message prompt template.
func NewGenericMessagePromptTemplate(role, template string, inputVariables []string) GenericMessagePromptTemplate {
	return GenericMessagePromptTemplate{
		Prompt: NewPromptTemplate(template, inputVariables),
		Role:   role,
	}
}

type MessagesPlaceholder struct {
	VariableName string
}

// FormatMessages formats the messages from the values by variable name.
func (p MessagesPlaceholder) FormatMessages(values map[string]any) ([]llms.Message, error) {
	value, ok := values[p.VariableName]
	if !ok {
		return nil, errors.WithMessagef(ErrNeedChatMessageList, "%s should be a list of chat messages", p.VariableName)
	}
	baseMessages, ok := value.([]llms.Message)
	if !ok {
		return nil, errors.WithMessagef(ErrNeedChatMessageList, "%s should be a list of chat messages", p.VariableName)
	}
	return baseMessages, nil
}

// GetInputVariables returns the input variables the prompt expect.
func (p MessagesPlaceholder) GetInputVariables() []string {
	return []string{p.VariableName}
}
