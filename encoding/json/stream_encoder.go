package json

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/effective-security/gogentic/schema"
	"github.com/effective-security/gogentic/utils"
	"github.com/go-playground/validator/v10"
)

type StreamWrapper[T any] struct {
	Items []T `json:"items"`
}

var WRAPPER_END = []byte(`"items": [`)

type StreamEncoder struct {
	schema   *schema.Schema
	reqType  reflect.Type
	buffer   *bytes.Buffer
	validate bool
}

func NewStreamEncoder(req any, validate bool) (*StreamEncoder, error) {
	t := reflect.TypeOf(req)
	streamWrapperType := reflect.StructOf([]reflect.StructField{
		{
			Name:      "Items",
			Type:      reflect.SliceOf(t),
			Tag:       `json:"items"`,
			Anonymous: false,
		},
	})
	s, err := schema.New(streamWrapperType)
	if err != nil {
		return nil, err
	}
	return &StreamEncoder{
		schema:   s,
		reqType:  t,
		buffer:   new(bytes.Buffer),
		validate: validate,
	}, nil
}

func (e *StreamEncoder) EnableValidate() {
	e.validate = true
}

func (e *StreamEncoder) Schema() *schema.Schema {
	return e.schema
}

func (e *StreamEncoder) Validate(req any) error {
	validate := validator.New()
	return validate.Struct(req)
}

func (e *StreamEncoder) Marshal(req any) ([]byte, error) {
	return []byte(e.schema.String()), nil
}

func (e *StreamEncoder) GetFormatInstructions() string {
	bs, err := e.Marshal(nil)
	if err != nil {
		return ""
	}
	var b bytes.Buffer
	b.WriteString("\nRespond with a JSON array where the elements following JSON schema:\n")
	b.WriteString("```json\n")
	b.Write(bs)
	b.WriteString("\n```")
	b.WriteString("\nMake sure to return an array with the elements an instance of the JSON, not the schema itself.\n")
	return b.String()
}

func (e *StreamEncoder) Read(ctx context.Context, ch <-chan string) <-chan any {
	parsedChan := make(chan any)
	e.buffer.Reset()
	go func() {
		defer close(parsedChan)

		inArray := false

		for {
			select {
			case <-ctx.Done():
				return
			case text, ok := <-ch:
				if !ok {
					// Stream closed
					e.processRemainingBuffer(parsedChan)
					return
				}

				e.buffer.WriteString(text)

				// Eat all input until elements stream starts
				if !inArray {
					inArray = startArray(e.buffer)
				}

				e.processBuffer(parsedChan)
			}
		}
	}()
	return parsedChan
}

func (e *StreamEncoder) processBuffer(parsedChan chan<- any) {
	data := e.buffer.Bytes()

	data, remaining := getFirstFullJSONElement(data)

	decoder := json.NewDecoder(bytes.NewReader(data))

	for decoder.More() {
		instance := reflect.New(e.reqType).Interface()
		if err := decoder.Decode(instance); err != nil {
			break
		}

		if e.validate {
			// Validate the instance
			if err := e.Validate(instance); err != nil {
				break
			}
		}

		parsedChan <- instance

		e.buffer.Reset()
		e.buffer.Write(remaining)
	}
}

func (e *StreamEncoder) processRemainingBuffer(parsedChan chan<- any) {
	remaining := utils.CleanJSON(e.buffer.Bytes())

	if idx := bytes.LastIndex(remaining, []byte{']'}); idx != -1 {
		remaining = remaining[:idx]
	}
	e.buffer.Reset()
	e.buffer.Write(remaining)

	e.processBuffer(parsedChan)
}

func startArray(buffer *bytes.Buffer) bool {
	data := buffer.Bytes()

	idx := bytes.Index(data, WRAPPER_END)
	if idx == -1 {
		return false
	}

	trimmed := bytes.TrimSpace(data[idx+len(WRAPPER_END):])
	buffer.Reset()
	buffer.Write(trimmed)

	return true
}

func findMatchingBracket(bs []byte, start int) int {
	stack := []int{}
	openBracket := '{'
	closeBracket := '}'

	for i := start; i < len(bs); i++ {
		if rune((bs)[i]) == openBracket {
			stack = append(stack, i)
		} else if rune((bs)[i]) == closeBracket {
			if len(stack) == 0 {
				return -1 // Unbalanced brackets
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i // Found the matching bracket
			}
		}
	}

	return -1 // Unbalanced brackets
}

func getFirstFullJSONElement(bs []byte) (element []byte, remaining []byte) {
	matchingBracketIdx := findMatchingBracket(bs, 0)

	if matchingBracketIdx == -1 {
		return nil, bs
	}

	element = bs[:matchingBracketIdx+1]

	if matchingBracketIdx+1 < len(bs) {
		remaining = bs[matchingBracketIdx+1:]

		if bs[matchingBracketIdx+1] == ',' {
			remaining = bs[matchingBracketIdx+2:]
		}
	}

	return element, remaining
}
