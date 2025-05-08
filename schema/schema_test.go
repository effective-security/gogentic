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
	Topic string     `json:"Topic,omitempty" jsonschema:"title=Topic,description=Topic of the search\\, with coma.,example=golang"`
	Query string     `json:"Query" jsonschema:"title=Query,description=Query to search for relevant content,example=what is golang"`
	Type  SearchType `json:"Type"  jsonschema:"title=Type,description=Type of search,default=web,enum=web,enum=image,enum=video"`
	Args  []*KVPair  `json:"Args,omitempty" jsonschema:"title=Args,description=Arguments for the search"`
	Prov  *KVPair    `json:"Prov,omitempty" jsonschema:"title=Prov,description=Provider for the search"`
}

// KVPair represents a key-value pair.
type KVPair struct {
	Key   string `json:"Key" jsonschema:"title=Key,description=Key of the pair"`
	Value string `json:"Value" jsonschema:"title=Value,description=Value of the pair"`
}

func TestSchema(t *testing.T) {
	t.Parallel()

	t.Run("Input", func(t *testing.T) {
		t.Parallel()
		si, err := schema.New(reflect.TypeOf(chatmodel.InputRequest{}))
		require.NoError(t, err)
		exp := `{
	"properties": {
		"Input": {
			"type": "string",
			"title": "Input",
			"description": "The message sent by the user to the assistant."
		}
	},
	"type": "object",
	"required": [
		"Input"
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
		"Content": {
			"type": "string",
			"title": "Response Content",
			"description": "The content returned by agent or tool."
		}
	},
	"type": "object",
	"required": [
		"Content"
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
		"Topic": {
			"type": "string",
			"title": "Topic",
			"description": "Topic of the search, with coma.",
			"examples": [
				"golang"
			]
		},
		"Query": {
			"type": "string",
			"title": "Query",
			"description": "Query to search for relevant content",
			"examples": [
				"what is golang"
			]
		},
		"Type": {
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
		"Args": {
			"items": {
				"properties": {
					"Key": {
						"type": "string",
						"title": "Key",
						"description": "Key of the pair"
					},
					"Value": {
						"type": "string",
						"title": "Value",
						"description": "Value of the pair"
					}
				},
				"type": "object",
				"required": [
					"Key",
					"Value"
				]
			},
			"type": "array",
			"title": "Args",
			"description": "Arguments for the search"
		},
		"Prov": {
			"properties": {
				"Key": {
					"type": "string",
					"title": "Key",
					"description": "Key of the pair"
				},
				"Value": {
					"type": "string",
					"title": "Value",
					"description": "Value of the pair"
				}
			},
			"type": "object",
			"required": [
				"Key",
				"Value"
			],
			"title": "Prov",
			"description": "Provider for the search"
		}
	},
	"type": "object",
	"required": [
		"Query",
		"Type"
	]
}`
		assert.Equal(t, exp, s.String())
		assert.Equal(t, exp, llmutils.ToJSONIndent(s.Parameters))
	})
}
