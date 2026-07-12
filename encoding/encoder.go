package encoding

import (
	"context"

	"github.com/cockroachdb/errors"
	dummyenc "github.com/effective-security/gogentic/encoding/dummy"
	jsonenc "github.com/effective-security/gogentic/encoding/json"
	tomlenc "github.com/effective-security/gogentic/encoding/toml"
	yamlenc "github.com/effective-security/gogentic/encoding/yaml"
)

// SchemaEncoder describes a codec that can marshal/unmarshal values and
// produce human‑readable “format instructions” describing the expected output
// schema for prompting.
type SchemaEncoder interface {
	// Marshal encodes a value into the underlying wire format (e.g. JSON).
	Marshal(req any) ([]byte, error)
	// Unmarshal decodes data in the underlying wire format into the provided
	// destination value (pointer required for structs/slices/maps).
	Unmarshal([]byte, any) error
	// GetFormatInstructions returns instructions (often including a schema)
	// that can be embedded in prompts to guide LLM output formatting.
	GetFormatInstructions() string
}

// Validator indicates that an encoder can validate decoded values according to
// struct tags or built‑in rules.
type Validator interface{ Validate(any) error }

// SchemaStreamEncoder describes a streaming decoder for incremental model
// outputs. Implementations read from a stream of text chunks and emit decoded
// values when enough data is available.
type SchemaStreamEncoder interface {
	// Read consumes text chunks and produces decoded values on the returned
	// channel until the context is done or the input closes.
	Read(context.Context, <-chan string) <-chan any
	// GetFormatInstructions returns instructions to guide streaming output.
	GetFormatInstructions() string
	// EnableValidate enables validation of decoded values if supported.
	EnableValidate()
}

// Mode defines the encoding/decoding strategy and schema style used for
// instructing and parsing model outputs.
type Mode = string

const (
	// ModeJSON marshals/unmarshals using plain JSON.
	ModeJSON Mode = "json"
	// ModeJSONSchema generates JSON Schema‑based instructions and uses JSON.
	ModeJSONSchema Mode = "json_schema"
	// ModeJSONSchemaStrict enforces required properties (provider support varies).
	ModeJSONSchemaStrict Mode = "json_schema_strict"
	// ModeYAML marshals/unmarshals using YAML.
	ModeYAML Mode = "yaml"
	// ModeTOML marshals/unmarshals using TOML.
	ModeTOML Mode = "toml"
	// ModePlainText accepts raw text without structure.
	ModePlainText Mode = "plain_text"
	// ModeCustom is reserved for application‑provided encoders.
	ModeCustom Mode = "custom"
)

// ModeDefault is the default mode for the encoder. Applications may override it.
var ModeDefault = ModeJSONSchema

// PredefinedSchemaEncoder returns a SchemaEncoder for a given Mode and example
// value (used to derive schema). For structured modes it inspects the provided
// value's type to build an appropriate schema for prompt instructions.
//
// Returns an error if the mode is not recognized.
func PredefinedSchemaEncoder(mode Mode, req any) (SchemaEncoder, error) {
	var (
		enc SchemaEncoder
		err error
	)
	switch mode {
	case ModeJSON, ModeJSONSchema, ModeJSONSchemaStrict:
		enc, err = jsonenc.NewEncoder(req)
	case ModeYAML:
		enc = yamlenc.NewEncoder(req)
	case ModeTOML:
		enc = tomlenc.NewEncoder(req)
	case ModePlainText:
		enc = dummyenc.NewEncoder()
	default:
		return nil, errors.New("no predefined encoder")
	}
	return enc, err
}

// func PredefinedStreamSchemaEncoder(mode Mode, req any) (SchemaStreamEncoder, error) {
// 	var (
// 		enc SchemaStreamEncoder
// 		err error
// 	)
// 	switch mode {
// 	case ModeToolCall, ModeToolCallStrict, ModeJSON, ModeJSONStrict, ModeJSONSchema:
// 		enc, err = jsonenc.NewStreamEncoder(req, false)
// 	case ModeYAML:
// 		enc, err = yamlenc.NewStreamEncoder(req)
// 	case ModeTOML:
// 		enc, err = tomlenc.NewStreamEncoder(req)
// 	case ModePlainText:
// 		enc = dummyenc.NewStreamEncoder()
// 	default:
// 		return nil, errors.New("no predefined encoder")
// 	}
// 	return enc, err
// }

var (
	_ SchemaEncoder = (*dummyenc.Encoder)(nil)
	_ SchemaEncoder = (*jsonenc.Encoder)(nil)
	_ SchemaEncoder = (*tomlenc.Encoder)(nil)
	_ SchemaEncoder = (*yamlenc.Encoder)(nil)

	// _ SchemaStreamEncoder = (*dummyenc.StreamEncoder)(nil)
	// _ SchemaStreamEncoder = (*jsonenc.StreamEncoder)(nil)
	// _ SchemaStreamEncoder = (*tomlenc.StreamEncoder)(nil)
	// _ SchemaStreamEncoder = (*yamlenc.StreamEncoder)(nil)
)
