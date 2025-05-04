package encoding

import (
	"strings"

	"github.com/effective-security/gogentic/chatmodel"
)

// Simple is an output parser that does nothing.
type SimpleOutputParser struct{}

func NewSimpleOutputParser() chatmodel.OutputParser[chatmodel.String] { return &SimpleOutputParser{} }

var _ chatmodel.OutputParser[chatmodel.String] = (*SimpleOutputParser)(nil)

func (p *SimpleOutputParser) GetFormatInstructions() string { return "" }

func (p *SimpleOutputParser) Parse(text string) (*chatmodel.String, error) {
	return chatmodel.NewString(strings.TrimSpace(text)), nil
}

// func (p *SimpleOutputParser) ParseWithPrompt(text string, _ llms.PromptValue) (*string, error) {
// 	return p.Parse(text)
// }

func (p *SimpleOutputParser) Type() string { return "simple_parser" }
