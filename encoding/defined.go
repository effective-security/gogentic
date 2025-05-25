package encoding

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
)

// TypedOutputParser parses output from an LLM into Go structs.
// By providing the NewDefined constructor with a struct, one or more TypeScript interfaces
// are generated to help LLMs format responses with the desired JSON structure.
type TypedOutputParser[T any] struct {
	enc      SchemaEncoder
	name     string
	validate bool
}

var _ chatmodel.OutputParser[any] = (*TypedOutputParser[any])(nil)

// NewTypedOutputParser creates an output parser that structures data according to
// a given schema, as defined by struct field names and types. Tagging the
// field with "json" will explicitly use that value as the field name. Tagging
// with "describe" will add a line comment for the LLM to understand how to
// generate data, helpful when the field's name is insufficient.
func NewTypedOutputParser[T any](sourceType T, mode Mode) (*TypedOutputParser[T], error) {
	enc, err := PredefinedSchemaEncoder(mode, sourceType)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create encoder")
	}

	return &TypedOutputParser[T]{
		enc:  enc,
		name: fmt.Sprintf("%T parser", sourceType),
	}, nil
}

func (p *TypedOutputParser[T]) WithValidation(validate bool) {
	p.validate = validate
}

// Parse parses the output of an LLM call.
func (p *TypedOutputParser[T]) Parse(text string) (*T, error) {
	var target T
	if err := p.enc.Unmarshal([]byte(text), &target); err != nil {
		return nil, errors.Wrap(err, "failed to decode")
	}
	if validator, ok := p.enc.(Validator); ok && p.validate {
		if err := validator.Validate(target); err != nil {
			return nil, errors.Wrap(err, "failed to validate")
		}
	}
	return &target, nil
}

// ParseWithPrompt parses the output of an LLM call with the prompt used.
// func (p *TypedOutputParser[T]) ParseWithPrompt(text string, prompt llms.PromptValue) (*T, error) {
// 	return p.Parse(text)
// }

// GetFormatInstructions returns a string describing the format of the output.
func (p *TypedOutputParser[T]) GetFormatInstructions() string {
	return p.enc.GetFormatInstructions()
}

// Type returns the string type key uniquely identifying this class of parser
func (p *TypedOutputParser[T]) Type() string {
	return p.name
}
