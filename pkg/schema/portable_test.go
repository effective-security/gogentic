package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortableSchema(t *testing.T) {
	valid := json.RawMessage(`{"type":"object","properties":{"list":{"type":"array","items":{"type":["integer","null"]}}},"required":["list"],"additionalProperties":false}`)
	require.NoError(t, ValidatePortableSchema(valid, true))
	require.NoError(t, ValidatePortableJSON(valid, `{"list":[1,null]}`))
	for _, out := range []string{`{"list":[1.2]}`, `{}`, `{"list":[],"extra":true}`, `null`, `{"list":[]} trailing`} {
		require.Error(t, ValidatePortableJSON(valid, out))
	}
	for _, schema := range []string{`{"type":"object","$ref":"https://example.com"}`, `{"type":"object","required":["missing"]}`, `{"type":"object","properties":{"x":{"type":"string","pattern":"x"}}}`, `{"type":"object","additionalProperties":{}}`, `{"type":"object","properties":{"x":{"type":"array"}}}`} {
		require.Error(t, ValidatePortableSchema([]byte(schema), false))
	}
	require.Error(t, ValidatePortableSchema([]byte(`{"type":"object"}`), true))
}

func TestPortableNumbersRetainPrecision(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","enum":[9007199254740993]}}}`)
	require.NoError(t, ValidatePortableJSON(raw, `{"n":9007199254740993}`))
	require.Error(t, ValidatePortableJSON(raw, `{"n":9007199254740992}`))
	require.NoError(t, ValidatePortableJSON([]byte(`{"type":"object","properties":{"n":{"type":"number","enum":[1]}}}`), `{"n":1.0}`))
	// Invalid caller schemas must return errors even when output validation is called directly.
	require.Error(t, ValidatePortableJSON([]byte(`{"type":"object","required":[1]}`), `{}`))
}

func TestPortableExponentBound(t *testing.T) {
	require.Error(t, ValidatePortableSchema([]byte(`{"type":"object","properties":{"n":{"type":"integer","enum":[1e999999999]}}}`), false))
	require.Error(t, ValidatePortableJSON([]byte(`{"type":"object","properties":{"n":{"type":"integer"}}}`), `{"n":1e999999999}`))
}

const (
	errStrictObjects   = "strict objects require all properties and additionalProperties:false"
	errOutputOutside   = "output is outside enum"
	errOutputType      = "output type does not match schema"
	errSchemaReqType   = "schema requires a type"
	errInvalidSchemaTy = "invalid schema type"
)

// nestedObjectSchema returns an object schema whose single property "a" is
// nested levels deep, so the innermost object sits at depth == levels.
func nestedObjectSchema(levels int) string {
	s := `{"type":"string"}`
	for range levels {
		s = `{"type":"object","properties":{"a":` + s + `}}`
	}
	return s
}

func TestValidatePortableSchemaErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schema  string
		strict  bool
		wantErr string
	}{
		{
			name:    "invalid JSON",
			schema:  `{`,
			wantErr: "decode JSON schema: invalid JSON",
		},
		{
			name:    "root is array",
			schema:  `[1]`,
			wantErr: "decode JSON schema: decode portable JSON: json: cannot unmarshal array into Go value of type map[string]interface {}",
		},
		{
			name:    "root is null",
			schema:  `null`,
			wantErr: "schema root must be an object",
		},
		{
			name:    "root is string type",
			schema:  `{"type":"string"}`,
			wantErr: "schema root must be an object",
		},
		{
			name:    "nesting too deep",
			schema:  nestedObjectSchema(portableSchemaDepth + 1),
			wantErr: "schema nesting exceeds 16",
		},
		{
			name:    "unsupported keyword",
			schema:  `{"type":"object","format":"x"}`,
			wantErr: "unsupported schema keyword: format",
		},
		{
			name:    "title not a string",
			schema:  `{"type":"object","title":1}`,
			wantErr: "title must be a string",
		},
		{
			name:    "description not a string",
			schema:  `{"type":"object","description":true}`,
			wantErr: "description must be a string",
		},
		{
			name:    "type list with non-string",
			schema:  `{"type":"object","properties":{"x":{"type":["string",1]}}}`,
			wantErr: errInvalidSchemaTy,
		},
		{
			name:    "missing type",
			schema:  `{"type":"object","properties":{"x":{"title":"x"}}}`,
			wantErr: errSchemaReqType,
		},
		{
			name:    "empty type list",
			schema:  `{"type":"object","properties":{"x":{"type":[]}}}`,
			wantErr: errSchemaReqType,
		},
		{
			name:    "duplicate type",
			schema:  `{"type":"object","properties":{"x":{"type":["string","string"]}}}`,
			wantErr: "duplicate schema type",
		},
		{
			name:    "unknown type",
			schema:  `{"type":"object","properties":{"x":{"type":"date"}}}`,
			wantErr: errInvalidSchemaTy,
		},
		{
			name:    "enum not array",
			schema:  `{"type":"object","properties":{"x":{"type":"string","enum":"a"}}}`,
			wantErr: "enum must be nonempty",
		},
		{
			name:    "enum empty",
			schema:  `{"type":"object","properties":{"x":{"type":"string","enum":[]}}}`,
			wantErr: "enum must be nonempty",
		},
		{
			name:    "enum type mismatch",
			schema:  `{"type":"object","properties":{"x":{"type":"string","enum":[1]}}}`,
			wantErr: "enum does not match type",
		},
		{
			name:    "properties not object",
			schema:  `{"type":"object","properties":[]}`,
			wantErr: "properties must be an object",
		},
		{
			name:    "required not array",
			schema:  `{"type":"object","properties":{"x":{"type":"string"}},"required":"x"}`,
			wantErr: "required must be an array",
		},
		{
			name:    "required duplicate",
			schema:  `{"type":"object","properties":{"x":{"type":"string"}},"required":["x","x"]}`,
			wantErr: "invalid required property",
		},
		{
			name:    "required missing from properties",
			schema:  `{"type":"object","required":["missing"]}`,
			wantErr: "required property missing from properties",
		},
		{
			name:    "additionalProperties not boolean",
			schema:  `{"type":"object","additionalProperties":{}}`,
			wantErr: "additionalProperties must be boolean",
		},
		{
			name:    "strict without additionalProperties",
			schema:  `{"type":"object"}`,
			strict:  true,
			wantErr: errStrictObjects,
		},
		{
			name:    "strict with optional property",
			schema:  `{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":false}`,
			strict:  true,
			wantErr: errStrictObjects,
		},
		{
			name:    "property schema not object",
			schema:  `{"type":"object","properties":{"x":1}}`,
			wantErr: "invalid property schema",
		},
		{
			name:    "object keyword on non-object",
			schema:  `{"type":"object","properties":{"x":{"type":"string","required":[]}}}`,
			wantErr: "object keywords require object type",
		},
		{
			name:    "array without items",
			schema:  `{"type":"object","properties":{"x":{"type":"array"}}}`,
			wantErr: "arrays require an items schema",
		},
		{
			name:    "array items invalid",
			schema:  `{"type":"object","properties":{"x":{"type":"array","items":{"type":"date"}}}}`,
			wantErr: errInvalidSchemaTy,
		},
		{
			name:    "items on non-array",
			schema:  `{"type":"object","properties":{"x":{"type":"string","items":{"type":"string"}}}}`,
			wantErr: "items requires array type",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePortableSchema(json.RawMessage(tc.schema), tc.strict)
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestValidatePortableSchemaValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		strict bool
	}{
		{
			name:   "max nesting",
			schema: nestedObjectSchema(portableSchemaDepth),
		},
		{
			name:   "all scalar types",
			schema: `{"type":"object","title":"t","description":"d","properties":{"s":{"type":"string","enum":["a"]},"n":{"type":"number","enum":[1.5]},"i":{"type":"integer","enum":[2]},"b":{"type":"boolean","enum":[true]},"z":{"type":"null","enum":[null]}}}`,
		},
		{
			name:   "union type with enum",
			schema: `{"type":"object","properties":{"x":{"type":["integer","string"],"enum":[1,"a"]}}}`,
		},
		{
			name:   "strict object",
			schema: `{"type":"object","properties":{"x":{"type":"string"}},"required":["x"],"additionalProperties":false}`,
			strict: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidatePortableSchema(json.RawMessage(tc.schema), tc.strict))
		})
	}
}

func TestValidatePortableJSON(t *testing.T) {
	t.Parallel()

	const (
		arrayEnumSchema  = `{"type":"object","properties":{"x":{"type":"array","items":{"type":"integer"},"enum":[[1,2]]}}}`
		objectEnumSchema = `{"type":"object","properties":{"x":{"type":"object","enum":[{"a":1}]}}}`
		unionEnumSchema  = `{"type":"object","properties":{"x":{"type":["integer","string"],"enum":[1,"a"]}}}`
		hugeEnumSchema   = `{"type":"object","properties":{"x":{"type":"array","items":{"type":"number"},"enum":[[1e999]]}}}`
		scalarSchema     = `{"type":"object","properties":{"s":{"type":"string"},"b":{"type":"boolean"},"n":{"type":"number"}}}`
	)
	longNumber := strings.Repeat("1", portableNumberBytes+1)

	tests := []struct {
		name    string
		schema  string
		output  string
		wantErr string
	}{
		{
			name:   "array enum match",
			schema: arrayEnumSchema,
			output: `{"x":[1,2.0]}`,
		},
		{
			name:    "array enum length mismatch",
			schema:  arrayEnumSchema,
			output:  `{"x":[1]}`,
			wantErr: errOutputOutside,
		},
		{
			name:    "array enum element mismatch",
			schema:  arrayEnumSchema,
			output:  `{"x":[1,3]}`,
			wantErr: errOutputOutside,
		},
		{
			name:   "object enum match",
			schema: objectEnumSchema,
			output: `{"x":{"a":1}}`,
		},
		{
			name:    "object enum value mismatch",
			schema:  objectEnumSchema,
			output:  `{"x":{"a":2}}`,
			wantErr: errOutputOutside,
		},
		{
			name:    "object enum key mismatch",
			schema:  objectEnumSchema,
			output:  `{"x":{"b":1}}`,
			wantErr: errOutputOutside,
		},
		{
			name:    "object enum size mismatch",
			schema:  objectEnumSchema,
			output:  `{"x":{"a":1,"b":1}}`,
			wantErr: errOutputOutside,
		},
		{
			name:   "union enum string after number",
			schema: unionEnumSchema,
			output: `{"x":"a"}`,
		},
		{
			name:    "union enum miss",
			schema:  unionEnumSchema,
			output:  `{"x":"b"}`,
			wantErr: errOutputOutside,
		},
		{
			name:    "unbounded number in nested enum",
			schema:  hugeEnumSchema,
			output:  `{"x":[1e999]}`,
			wantErr: errOutputOutside,
		},
		{
			name:   "scalar types match",
			schema: scalarSchema,
			output: `{"s":"v","b":false,"n":-0.5,"extra":1}`,
		},
		{
			name:    "string type mismatch",
			schema:  scalarSchema,
			output:  `{"s":1}`,
			wantErr: errOutputType,
		},
		{
			name:    "boolean type mismatch",
			schema:  scalarSchema,
			output:  `{"b":"true"}`,
			wantErr: errOutputType,
		},
		{
			name:    "number too long",
			schema:  scalarSchema,
			output:  `{"n":` + longNumber + `}`,
			wantErr: errOutputType,
		},
		{
			name:    "integer given string",
			schema:  `{"type":"object","properties":{"i":{"type":"integer"}}}`,
			output:  `{"i":"1"}`,
			wantErr: errOutputType,
		},
		{
			name:    "invalid output JSON",
			schema:  scalarSchema,
			output:  `{"s":`,
			wantErr: "decode JSON output: invalid JSON",
		},
		{
			name:    "invalid schema",
			schema:  `{"type":"string"}`,
			output:  `{}`,
			wantErr: "schema root must be an object",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePortableJSON(json.RawMessage(tc.schema), tc.output)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

// The guards below cannot be reached through ValidatePortableJSON because the
// schema is validated first; they protect direct callers of the helpers.
func TestPortableInternalGuards(t *testing.T) {
	t.Parallel()

	stringSchema := map[string]any{
		"type": "string",
	}
	err := checkPortableValue(stringSchema, "v", portableSchemaDepth+1)
	require.EqualError(t, err, "output nesting exceeds schema limit")

	arrayNoItems := map[string]any{
		"type": "array",
	}
	err = checkPortableValue(arrayNoItems, []any{}, 0)
	require.EqualError(t, err, "missing items schema")

	assert.False(t, portableType("date", "2024-01-01"))
	assert.True(t, portableType("string", "x"))
	assert.False(t, portableEqual(json.Number("1"), "1"))
	assert.False(t, portableEqual([]any{json.Number("1")}, map[string]any{}))
	assert.False(t, portableEqual(map[string]any{}, []any{}))
	assert.True(t, portableEqual(nil, nil))
}
