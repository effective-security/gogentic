package json

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/bububa/ljson"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/go-playground/validator/v10"
)

type Encoder struct {
	schema *schema.Schema
}

func NewEncoder(req any) (*Encoder, error) {
	t := reflect.TypeOf(req)
	schema, err := schema.New(t)
	if err != nil {
		return nil, err
	}
	return &Encoder{
		schema: schema,
	}, nil
}

func (e *Encoder) Marshal(req any) ([]byte, error) {
	return json.Marshal(req)
}

func (e *Encoder) Unmarshal(bs []byte, ret any) error {
	data := llmutils.CleanJSON(bs)
	return ljson.Unmarshal(data, ret)
}

func (e *Encoder) Validate(req any) error {
	validate := validator.New()
	return validate.Struct(req)
}

func (e *Encoder) GetFormatInstructions() string {
	var b bytes.Buffer
	b.WriteString("\nRespond with JSON in the following JSON schema:\n")
	b.WriteString("```json\n")
	b.Write([]byte(e.schema.String()))
	b.WriteString("\n```")
	b.WriteString("\nMake sure to return an instance of the JSON, not the schema itself.\n")
	b.WriteString("Use the exact field names as they are defined in the schema.\n")
	return b.String()
}

func (e *Encoder) Schema() *schema.Schema {
	return e.schema
}
