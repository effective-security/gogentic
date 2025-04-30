package schema_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/effective-security/gogentic/schema"
	"github.com/effective-security/gogentic/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type SearchType string

const (
	Web   SearchType = "web"
	Image SearchType = "image"
	Video SearchType = "video"
)

type Search struct {
	Topic string     `json:"topic,omitempty" jsonschema:"title=Topic,description=Topic of the search,example=golang"`
	Query string     `json:"query" jsonschema:"title=Query,description=Query to search for relevant content,example=what is golang"`
	Type  SearchType `json:"type"  jsonschema:"title=Type,description=Type of search,default=web,enum=web,enum=image,enum=video"`
}

func TestSchema(t *testing.T) {
	s, err := schema.New(reflect.TypeOf(Search{}))
	require.NoError(t, err)

	exp := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://github.com/effective-security/gogentic/schema_test/909492315013507688","$ref":"#/$defs/909492315013507688","$defs":{"909492315013507688":{"properties":{"topic":{"type":"string","title":"Topic","description":"Topic of the search","examples":["golang"]},"query":{"type":"string","title":"Query","description":"Query to search for relevant content","examples":["what is golang"]},"type":{"type":"string","enum":["web","image","video"],"title":"Type","description":"Type of search","default":"web"}},"additionalProperties":false,"type":"object","required":["query","type"]}}}`
	assert.Equal(t, exp, s.String)
	assert.Equal(t, 1, len(s.Functions))

	exp2 := `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://github.com/effective-security/gogentic/schema_test/909492315013507688",
  "$ref": "#/$defs/909492315013507688",
  "$defs": {
    "909492315013507688": {
      "properties": {
        "topic": {
          "type": "string",
          "title": "Topic",
          "description": "Topic of the search",
          "examples": [
            "golang"
          ]
        },
        "query": {
          "type": "string",
          "title": "Query",
          "description": "Query to search for relevant content",
          "examples": [
            "what is golang"
          ]
        },
        "type": {
          "type": "string",
          "enum": [
            "web",
            "image",
            "video"
          ],
          "title": "Type",
          "description": "Type of search",
          "default": "web"
        }
      },
      "additionalProperties": false,
      "type": "object",
      "required": [
        "query",
        "type"
      ]
    }
  }
}`
	assert.Equal(t, exp2, utils.JSONIndent(s.String))

	s2, err := schema.New(reflect.TypeOf(Search{}))
	require.NoError(t, err)
	assert.Equal(t, exp, s2.String)

	js, err := json.MarshalIndent(s.Functions, "", "  ")
	require.NoError(t, err)
	expf := `[
  {
    "name": "909492315013507688",
    "description": "",
    "parameters": {
      "properties": {
        "topic": {
          "type": "string",
          "title": "Topic",
          "description": "Topic of the search",
          "examples": [
            "golang"
          ]
        },
        "query": {
          "type": "string",
          "title": "Query",
          "description": "Query to search for relevant content",
          "examples": [
            "what is golang"
          ]
        },
        "type": {
          "type": "string",
          "enum": [
            "web",
            "image",
            "video"
          ],
          "title": "Type",
          "description": "Type of search",
          "default": "web"
        }
      },
      "type": "object",
      "required": [
        "query",
        "type"
      ]
    }
  }
]`
	assert.Equal(t, expf, string(js))
}
