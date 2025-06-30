package genaiutils

import (
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/google/generative-ai-go/genai"
	"github.com/invopop/jsonschema"
)

// ConvertTools converts from a list of langchaingo tools to a list of genai
// tools.
func ConvertTools(tools []llms.Tool) ([]*genai.Tool, error) {
	genaiTools := make([]*genai.Tool, 0, len(tools))
	for i, tool := range tools {
		if tool.Type != "function" {
			return nil, errors.Errorf("tool [%d]: unsupported type %q, want 'function'", i, tool.Type)
		}

		// We have a llms.FunctionDefinition in tool.Function, and we have to
		// convert it to genai.FunctionDeclaration
		genaiFuncDecl := &genai.FunctionDeclaration{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
		}

		if tool.Function.Parameters != nil {
			var schema *genai.Schema
			var err error

			schema, err = ConvertJSONSchemaDefinition(tool.Function.Parameters)
			if err != nil {
				return nil, errors.Wrapf(err, "tool [%d]", i)
			}
			genaiFuncDecl.Parameters = schema
		}

		genaiTools = append(genaiTools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{genaiFuncDecl},
		})
	}

	return genaiTools, nil
}

// ConvertJSONSchemaDefinition converts a jsonschema.Definition to a genai.Schema.
func ConvertJSONSchemaDefinition(jschema *jsonschema.Schema) (*genai.Schema, error) {
	if jschema == nil {
		return nil, nil
	}

	schema := &genai.Schema{
		Type:        ConvertJSONSchemaType(jschema.Type),
		Description: jschema.Description,
		Required:    jschema.Required,
	}

	// Convert properties
	if jschema.Properties != nil {
		schema.Properties = make(map[string]*genai.Schema)
		for pair := jschema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			propSchema, err := ConvertJSONSchemaDefinition(pair.Value)
			if err != nil {
				return nil, errors.Wrapf(err, "property [%s]", pair.Key)
			}
			schema.Properties[pair.Key] = propSchema
		}
	}

	// Convert items for array types
	if jschema.Items != nil {
		itemsSchema, err := ConvertJSONSchemaDefinition(jschema.Items)
		if err != nil {
			return nil, errors.Wrap(err, "items")
		}
		schema.Items = itemsSchema
	}

	return schema, nil
}

// ConvertJSONSchemaType converts a jsonschema.DataType to a genai.Type.
func ConvertJSONSchemaType(dt string) genai.Type {
	switch dt {
	case "object":
		return genai.TypeObject
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	default:
		return genai.TypeUnspecified
	}
}

// ConvertToolSchemaType converts a tool's schema type from its langchaingo
// representation (string) to a genai enum.
func ConvertToolSchemaType(ty string) genai.Type {
	switch ty {
	case "object":
		return genai.TypeObject
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	default:
		return genai.TypeUnspecified
	}
}
