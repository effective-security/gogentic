package chatmodel

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
)

var (
	ErrFailedUnmarshalInput = errors.New("failed to unmarshal input: check the schema and try again")
)

// OutputParser is an interface for parsing the output of an LLM call.
type OutputParser[T any] interface {
	// Parse parses the output of an LLM call.
	// If the assistant fails to parse the input, it should return ErrFailedUnmarshalInput error.
	Parse(text string) (*T, error)
	// GetFormatInstructions returns a string describing the format of the output.
	GetFormatInstructions() string
	// Type returns the string type key uniquely identifying this class of parser
	Type() string

	// TODO: is it necessary to have this?
	// ParseWithPrompt parses the output of an LLM call with the prompt used.
	//ParseWithPrompt(text string, prompt llms.PromptValue) (*T, error)
}

type Stringer interface {
	String() string
}

func Stringify(s any) string {
	if v, ok := s.(Stringer); ok {
		return v.String()
	}
	if v, ok := s.(ContentProvider); ok {
		return v.GetContent()
	}
	bs, _ := json.Marshal(s)
	return string(bs)
}

func ToBytes(s any) []byte {
	if v, ok := s.(Stringer); ok {
		return []byte(v.String())
	}
	if v, ok := s.(ContentProvider); ok {
		return []byte(v.GetContent())
	}
	bs, _ := json.Marshal(s)
	return bs
}

type FewShotExample struct {
	Prompt     string
	Completion string
}

type FewShotExamples []FewShotExample
