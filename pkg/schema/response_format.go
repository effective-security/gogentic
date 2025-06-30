package schema

import (
	"reflect"

	"github.com/invopop/jsonschema"
)

func NewResponseFormat(t reflect.Type, strict bool) (*ResponseFormat, error) {
	sc, err := New(t)
	if err != nil {
		return nil, err
	}
	return &ResponseFormat{
		Type: "json_schema",
		JSONSchema: &ResponseFormatJSONSchema{
			Name:   t.Name(),
			Strict: strict,
			Schema: toOpenAISchema(sc.Parameters, strict),
		},
	}, nil
}

type ResponseFormatJSONSchemaProperty struct {
	Type                 string                                       `json:"type"`
	Title                string                                       `json:"title,omitempty"`
	Description          string                                       `json:"description,omitempty"`
	Enum                 []any                                        `json:"enum,omitempty"`
	Default              any                                          `json:"default,omitempty"`
	Examples             []any                                        `json:"examples,omitempty"`
	Items                *ResponseFormatJSONSchemaProperty            `json:"items,omitempty"`
	Properties           map[string]*ResponseFormatJSONSchemaProperty `json:"properties,omitempty"`
	AdditionalProperties *bool                                        `json:"additionalProperties,omitempty"`
	Required             []string                                     `json:"required,omitempty"`
	Ref                  string                                       `json:"$ref,omitempty"`
}

type ResponseFormatJSONSchema struct {
	Name   string                            `json:"name"`
	Strict bool                              `json:"strict"`
	Schema *ResponseFormatJSONSchemaProperty `json:"schema"`
}

// ResponseFormat is the format of the response.
type ResponseFormat struct {
	Type       string                    `json:"type"`
	JSONSchema *ResponseFormatJSONSchema `json:"json_schema,omitempty"`
}

var (
	trueVal  = true
	falseVal = false
)

func toOpenAISchema(in *jsonschema.Schema, strict bool) *ResponseFormatJSONSchemaProperty {
	if in == nil {
		return nil
	}

	result := &ResponseFormatJSONSchemaProperty{
		Type:        in.Type,
		Title:       in.Title,
		Description: in.Description,
		Enum:        in.Enum,
		Default:     in.Default,
		Examples:    in.Examples,
		Required:    in.Required,
		Ref:         in.Ref,
	}

	// Handle AdditionalProperties - if it's a schema, we set it to true, otherwise nil
	if in.AdditionalProperties != nil {
		result.AdditionalProperties = &trueVal
	} else if in.Type == "object" {
		result.AdditionalProperties = &falseVal
	}

	// Convert properties if they exist
	if in.Properties != nil {
		result.Properties = make(map[string]*ResponseFormatJSONSchemaProperty)
		for pair := in.Properties.Oldest(); pair != nil; pair = pair.Next() {
			result.Properties[pair.Key] = toOpenAISchema(pair.Value, strict)
		}
	}

	// Convert items if they exist (for array types)
	if in.Items != nil {
		result.Items = toOpenAISchema(in.Items, strict)
	}

	return result
}
