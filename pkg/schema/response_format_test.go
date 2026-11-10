package schema

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type withMapField struct {
	Labels map[string]string `json:"labels" jsonschema:"description=Free-form labels"`
}

func TestNewResponseFormatMapField(t *testing.T) {
	t.Parallel()

	rf, err := NewResponseFormat(reflect.TypeOf(withMapField{}), true)
	require.NoError(t, err)
	exp := `{
  "type": "json_schema",
  "json_schema": {
    "name": "withMapField",
    "strict": true,
    "schema": {
      "type": "object",
      "properties": {
        "labels": {
          "type": "object",
          "description": "Free-form labels",
          "additionalProperties": true
        }
      },
      "additionalProperties": false,
      "required": [
        "labels"
      ]
    }
  }
}`
	js, err := json.MarshalIndent(rf, "", "  ")
	require.NoError(t, err)
	assert.Equal(t, exp, string(js))
}

func TestToOpenAISchemaNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, toOpenAISchema(nil, true))
}
