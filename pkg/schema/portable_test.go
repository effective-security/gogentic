package schema

import (
	"encoding/json"
	"testing"

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
