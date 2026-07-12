package encoding

import (
	"strings"

	"github.com/effective-security/gogentic/chatmodel"
)

// SimpleOutputParser is a no‑op output parser that returns trimmed text.
// It is useful when you want to surface raw model output without imposing
// any structure.
type SimpleOutputParser struct{}

// NewSimpleOutputParser constructs a new SimpleOutputParser.
func NewSimpleOutputParser() chatmodel.OutputParser[chatmodel.String] { return &SimpleOutputParser{} }

var _ chatmodel.OutputParser[chatmodel.String] = (*SimpleOutputParser)(nil)

// GetFormatInstructions returns an empty string because no structure is
// enforced.
func (p *SimpleOutputParser) GetFormatInstructions() string { return "" }

// Parse trims whitespace and returns the result as chatmodel.String.
func (p *SimpleOutputParser) Parse(text string) (*chatmodel.String, error) {
	return chatmodel.NewString(strings.TrimSpace(text)), nil
}

// func (p *SimpleOutputParser) ParseWithPrompt(text string, _ llms.PromptValue) (*string, error) {
// 	return p.Parse(text)
// }

// Type returns a stable identifier for this parser.
func (p *SimpleOutputParser) Type() string { return "simple_parser" }
