package schema

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test structs to demonstrate pointer vs non-pointer behavior
type TestStructWithString struct {
	RequiredField string `json:"requiredField" jsonschema:"title=Required Field,description=A required string field"`
	OptionalField string `json:"optionalField,omitempty" jsonschema:"title=Optional Field,description=An optional string field"`
}

type TestStructWithPointer struct {
	RequiredField string  `json:"requiredField" jsonschema:"title=Required Field,description=A required string field"`
	OptionalField *string `json:"optionalField,omitempty" jsonschema:"title=Optional Field,description=An optional string field"`
}

func TestPointerTypeSchemaGeneration(t *testing.T) {
	t.Run("String field with omitempty", func(t *testing.T) {
		rf, err := NewResponseFormat(reflect.TypeOf(TestStructWithString{}), true)
		require.NoError(t, err)

		// The schema should include optionalField in properties but not in required
		assert.Contains(t, rf.JSONSchema.Schema.Properties, "optionalField")
		assert.NotContains(t, rf.JSONSchema.Schema.Required, "optionalField")
		assert.Contains(t, rf.JSONSchema.Schema.Required, "requiredField")

		jsonBytes, _ := json.MarshalIndent(rf, "", "\t")
		exp := `{
	"type": "json_schema",
	"json_schema": {
		"name": "TestStructWithString",
		"strict": true,
		"schema": {
			"type": "object",
			"properties": {
				"optionalField": {
					"type": "string",
					"title": "Optional Field",
					"description": "An optional string field"
				},
				"requiredField": {
					"type": "string",
					"title": "Required Field",
					"description": "A required string field"
				}
			},
			"additionalProperties": false,
			"required": [
				"requiredField"
			]
		}
	}
}`
		assert.Equal(t, exp, string(jsonBytes))
		t.Logf("Full schema with string field:\n%s", string(jsonBytes))
	})

	t.Run("Pointer field with omitempty", func(t *testing.T) {
		rf, err := NewResponseFormat(reflect.TypeOf(TestStructWithPointer{}), true)
		require.NoError(t, err)

		// The schema should include optionalField in properties but not in required
		assert.Contains(t, rf.JSONSchema.Schema.Properties, "optionalField")
		assert.NotContains(t, rf.JSONSchema.Schema.Required, "optionalField")
		assert.Contains(t, rf.JSONSchema.Schema.Required, "requiredField")

		jsonBytes, _ := json.MarshalIndent(rf, "", "\t")
		t.Logf("Full schema with pointer field:\n%s", string(jsonBytes))
	})
}
