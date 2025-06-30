package encoding

import (
	"context"

	"github.com/cockroachdb/errors"
	dummyenc "github.com/effective-security/gogentic/encoding/dummy"
	jsonenc "github.com/effective-security/gogentic/encoding/json"
	tomlenc "github.com/effective-security/gogentic/encoding/toml"
	yamlenc "github.com/effective-security/gogentic/encoding/yaml"
)

type SchemaEncoder interface {
	Marshal(req any) ([]byte, error)
	Unmarshal([]byte, any) error
	// GetFormatInstructions returns the wrapped message with message schema for the prompt
	GetFormatInstructions() string
}

type Validator interface {
	Validate(any) error
}

type SchemaStreamEncoder interface {
	Read(context.Context, <-chan string) <-chan any
	GetFormatInstructions() string
	EnableValidate()
}

type Mode = string

const (
	ModeJSON             Mode = "json"
	ModeJSONSchema       Mode = "json_schema"
	ModeJSONSchemaStrict Mode = "json_schema_strict" // Not all providers support this and all props must be required
	ModeYAML             Mode = "yaml"
	ModeTOML             Mode = "toml"
	ModePlainText        Mode = "plain_text"
	ModeCustom           Mode = "custom"
)

// ModeDefault is the default mode for the encoder.
// Allow to everride in apps
var ModeDefault = ModeJSONSchema

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
