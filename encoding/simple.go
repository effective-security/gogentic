package encoding

import (
	"strings"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/tmc/langchaingo/llms"
)

// Simple is an output parser that does nothing.
type SimpleOutputParser struct{}

func NewSimpleOutputParser() chatmodel.OutputParser[string] { return &SimpleOutputParser{} }

var _ chatmodel.OutputParser[string] = (*SimpleOutputParser)(nil)

func (p *SimpleOutputParser) GetFormatInstructions() string { return "" }

func (p *SimpleOutputParser) Parse(text string) (*string, error) {
	out := strings.TrimSpace(text)
	return &out, nil
}

func (p *SimpleOutputParser) ParseWithPrompt(text string, _ llms.PromptValue) (*string, error) {
	return p.Parse(text)
}

func (p *SimpleOutputParser) Type() string { return "simple_parser" }
