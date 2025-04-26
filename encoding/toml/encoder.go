package toml

import (
	"bytes"
	"reflect"

	"github.com/BurntSushi/toml"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/effective-security/gogentic/schema"
	"github.com/effective-security/gogentic/utils"
	"github.com/go-playground/validator/v10"
)

type Encoder struct {
	reqType reflect.Type
}

func NewEncoder(req any) *Encoder {
	t := reflect.TypeOf(req)
	return &Encoder{
		reqType: t,
	}
}

func (e *Encoder) Marshal(v any) ([]byte, error) {
	return toml.Marshal(v)
}

func (e *Encoder) Unmarshal(bs []byte, ret any) error {
	data := utils.BytesTrimBackticks(bs)
	return toml.Unmarshal(data, ret)
}

func (e *Encoder) Validate(req any) error {
	validate := validator.New()
	return validate.Struct(req)
}

func (e *Encoder) GetFormatInstructions() string {
	tValue := reflect.New(e.reqType)
	instance := tValue.Interface()
	if f, ok := tValue.Elem().Interface().(schema.Faker); ok {
		instance = f.Fake()
	} else {
		_ = gofakeit.Struct(instance)
	}
	bs, err := e.Marshal(instance)
	if err != nil {
		return ""
	}
	var b bytes.Buffer
	b.WriteString("\nRespond with TOML in the following TOML schema:\n")
	b.WriteString("```toml\n")
	b.Write(bs)
	b.WriteString("```")
	b.WriteString("\nMake sure to return an instance of the TOML, not the schema itself.\n")
	return b.String()
}
