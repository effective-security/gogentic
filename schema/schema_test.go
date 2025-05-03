package schema_test

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/effective-security/gogentic/chatmodel"
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
	t.Parallel()

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			si, err := schema.New(reflect.TypeOf(chatmodel.Input{}))
			require.NoError(t, err)
			exp := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://github.com/effective-security/gogentic/chatmodel/7363504996712283583","$ref":"#/$defs/7363504996712283583","$defs":{"7363504996712283583":{"properties":{"Content":{"type":"string","title":"Content","description":"The chat message sent by the user to the assistant."}},"additionalProperties":false,"type":"object","required":["Content"]}}}`
			assert.Equal(t, exp, si.String)
			assert.Equal(t, 1, len(si.Functions))
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			so, err := schema.New(reflect.TypeOf(chatmodel.Output{}))
			require.NoError(t, err)
			exp := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://github.com/effective-security/gogentic/chatmodel/15276760590566660215","$ref":"#/$defs/15276760590566660215","$defs":{"15276760590566660215":{"properties":{"Content":{"type":"string","title":"Content","description":"The chat message exchanged between the user and the chat agent."}},"additionalProperties":false,"type":"object","required":["Content"]}}}`
			assert.Equal(t, exp, so.String)
			assert.Equal(t, 1, len(so.Functions))
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := schema.New(reflect.TypeOf(Search{}))
			require.NoError(t, err)
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
		}()
	}
}
