package toml

import (
	"bufio"
	"bytes"
	"context"
	"reflect"

	"github.com/BurntSushi/toml"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/effective-security/gogentic/schema"
	"github.com/go-playground/validator/v10"
)

type StreamEncoder struct {
	reqType  reflect.Type
	buffer   *bytes.Buffer
	scanner  *bufio.Scanner
	validate bool
}

func NewStreamEncoder(req any) (*StreamEncoder, error) {
	t := reflect.TypeOf(req)
	buffer := new(bytes.Buffer)
	return &StreamEncoder{
		reqType: t,
		buffer:  buffer,
		scanner: bufio.NewScanner(buffer),
	}, nil
}

func (e *StreamEncoder) EnableValidate() {
	e.validate = true
}

func (e *StreamEncoder) Validate(req any) error {
	validate := validator.New()
	return validate.Struct(req)
}

func (e *StreamEncoder) Marshal(req any) ([]byte, error) {
	return toml.Marshal(req)
}

func (e *StreamEncoder) GetFormatInstructions() string {
	var b bytes.Buffer
	b.WriteString("\nRespond with a TOML array where the elements following TOML schema which is seperated by `----` for each elements:\n\n")
	for i := range 3 {
		if i > 0 {
			b.WriteString("\n----\n")
		}
		instance := reflect.New(e.reqType).Interface()
		if f, ok := instance.(schema.Faker); ok {
			instance = f.Fake()
		} else {
			_ = gofakeit.Struct(instance)
		}
		bs, err := e.Marshal(instance)
		if err != nil {
			return ""
		}
		b.Write(bs)
	}
	b.WriteString("\nMake sure to return an array with the elements an instance of the TOML, not the schema itself.\n")
	return b.String()
}

var (
	ignorePrefix = []byte("```toml")
	ignoreSuffix = []byte("```")
)

func (e *StreamEncoder) Read(ctx context.Context, ch <-chan string) <-chan any {
	parsedChan := make(chan any)
	e.buffer.Reset()
	go func() {
		defer close(parsedChan)
		defer e.buffer.Reset()
		for {
			select {
			case <-ctx.Done():
				return
			case text, ok := <-ch:
				if !ok {
					// Stream closed
					if e.buffer.Len() > 0 {
						bs := bytes.TrimSuffix(bytes.TrimPrefix(bytes.TrimSpace(e.buffer.Bytes()), ignorePrefix), ignoreSuffix)
						instance := reflect.New(e.reqType).Interface()
						if err := toml.Unmarshal(bs, instance); err == nil {
							if e.validate {
								// Validate the instance
								if err := e.Validate(instance); err == nil {
									return
								}
							}
							parsedChan <- instance
						}
					}
					return
				}
				e.buffer.WriteString(text)
				e.processBuffer(parsedChan)
			}
		}
	}()
	return parsedChan
}

func (e *StreamEncoder) processBuffer(parsedChan chan<- any) {
	block := new(bytes.Buffer)
	for e.scanner.Scan() {
		bs := e.scanner.Bytes()
		if trimmed := bytes.TrimSpace(bs); bytes.Equal(trimmed, []byte("----")) {
			if block.Len() > 0 {
				in := bytes.TrimSuffix(bytes.TrimPrefix(bytes.TrimSpace(block.Bytes()), ignorePrefix), ignoreSuffix)
				instance := reflect.New(e.reqType).Interface()
				if err := toml.Unmarshal(in, instance); err == nil {
					if e.validate {
						// Validate the instance
						if err := e.Validate(instance); err == nil {
							block.Reset()
							continue
						}
					}
					parsedChan <- instance
				}
			}
			block.Reset()
		} else {
			block.Write(bs)
		}
	}
	e.buffer.Reset()
	if block.Len() > 0 {
		e.buffer.Write(block.Bytes())
	}
}
