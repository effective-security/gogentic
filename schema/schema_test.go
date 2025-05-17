package schema_test

import (
	"reflect"
	"testing"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/llmutils"
	"github.com/effective-security/gogentic/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type SearchType string

const (
	Web   SearchType = "web"
	Image SearchType = "image"
	Video SearchType = "video"
)

// Search represents a search request with various parameters.
type Search struct {
	Topic string     `json:"topic,omitempty" jsonschema:"title=Topic,description=Topic of the search\\, with coma.,example=golang"`
	Query string     `json:"query" jsonschema:"title=Query,description=Query to search for relevant content,example=what is golang"`
	Type  SearchType `json:"type"  jsonschema:"title=Type,description=Type of search,default=web,enum=web,enum=image,enum=video"`
	Args  []*KVPair  `json:"args,omitempty" jsonschema:"title=Args,description=Arguments for the search"`
	Prov  *KVPair    `json:"prov,omitempty" jsonschema:"title=Prov,description=Provider for the search"`
}

// KVPair represents a key-value pair.
type KVPair struct {
	Key   string `json:"key" jsonschema:"title=Key,description=Key of the pair"`
	Value string `json:"value" jsonschema:"title=Value,description=Value of the pair"`
}

func TestSchema(t *testing.T) {
	t.Parallel()

	t.Run("Input", func(t *testing.T) {
		t.Parallel()
		si, err := schema.New(reflect.TypeOf(chatmodel.InputRequest{}))
		require.NoError(t, err)
		exp := `{
	"properties": {
		"input": {
			"type": "string",
			"title": "Input",
			"description": "The message sent by the user to the assistant."
		}
	},
	"type": "object",
	"required": [
		"input"
	]
}`
		assert.Equal(t, exp, si.String())
		assert.Equal(t, exp, llmutils.ToJSONIndent(si.Parameters))
	})

	t.Run("Output", func(t *testing.T) {
		t.Parallel()
		so, err := schema.New(reflect.TypeOf(chatmodel.OutputResult{}))
		require.NoError(t, err)

		exp := `{
	"properties": {
		"content": {
			"type": "string",
			"title": "Response Content",
			"description": "The content returned by agent or tool."
		}
	},
	"type": "object",
	"required": [
		"content"
	]
}`
		assert.Equal(t, exp, so.String())
		assert.Equal(t, exp, llmutils.ToJSONIndent(so.Parameters))

	})

	t.Run("Search", func(t *testing.T) {
		t.Parallel()
		s, err := schema.New(reflect.TypeOf(Search{}))
		require.NoError(t, err)

		exp := `{
	"properties": {
		"topic": {
			"type": "string",
			"title": "Topic",
			"description": "Topic of the search, with coma.",
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
		},
		"args": {
			"items": {
				"properties": {
					"key": {
						"type": "string",
						"title": "Key",
						"description": "Key of the pair"
					},
					"value": {
						"type": "string",
						"title": "Value",
						"description": "Value of the pair"
					}
				},
				"type": "object",
				"required": [
					"key",
					"value"
				]
			},
			"type": "array",
			"title": "Args",
			"description": "Arguments for the search"
		},
		"prov": {
			"properties": {
				"key": {
					"type": "string",
					"title": "Key",
					"description": "Key of the pair"
				},
				"value": {
					"type": "string",
					"title": "Value",
					"description": "Value of the pair"
				}
			},
			"type": "object",
			"required": [
				"key",
				"value"
			],
			"title": "Prov",
			"description": "Provider for the search"
		}
	},
	"type": "object",
	"required": [
		"query",
		"type"
	]
}`
		assert.Equal(t, exp, s.String())
		assert.Equal(t, exp, llmutils.ToJSONIndent(s.Parameters))
	})
}
