package encoding_test

import (
	"testing"

	"github.com/effective-security/gogentic/encoding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_JSON_Encoding(t *testing.T) {
	e, err := encoding.PredefinedSchemaEncoder(encoding.ModeJSON, Search{})
	require.NoError(t, err)

	exp := `
Respond with JSON in the following JSON schema:
` + "```json" + `
{
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
		"topic",
		"query",
		"type"
	]
}
` + "```" + `
Make sure to return an instance of the JSON, not the schema itself.
Use the exact field names as they are defined in the schema.
`
	assert.Equal(t, exp, e.GetFormatInstructions())
}

func Test_YAML_Encoding(t *testing.T) {
	e, err := encoding.PredefinedSchemaEncoder(encoding.ModeYAML, Search{})
	require.NoError(t, err)

	exp := `
Respond with YAML in the following YAML schema without comments:
` + "```yaml" + `
topic: golang
query: what is golang
type: web
` + "```" + `
Make sure to return an instance of the YAML, not the schema itself.
`

	assert.Equal(t, exp, e.GetFormatInstructions())
}

func Test_TOML_Encoding(t *testing.T) {
	e, err := encoding.PredefinedSchemaEncoder(encoding.ModeTOML, Search{})
	require.NoError(t, err)

	exp := `
Respond with TOML in the following TOML schema:
` + "```toml" + `
Topic = "golang"
Query = "what is golang"
Type = "web"
` + "```" + `
Make sure to return an instance of the TOML, not the schema itself.
`

	assert.Equal(t, exp, e.GetFormatInstructions())
}

type SearchType string

const (
	Web   SearchType = "web"
	Image SearchType = "image"
	Video SearchType = "video"
)

type Search struct {
	Topic string     `json:"topic" jsonschema:"title=Topic,description=Topic of the search,example=golang" fake:"golang"`
	Query string     `json:"query" jsonschema:"title=Query,description=Query to search for relevant content,example=what is golang" fake:"what is golang"`
	Type  SearchType `json:"type"  jsonschema:"title=Type,description=Type of search,default=web,enum=web,enum=image,enum=video" fake:"web"`
}
