package json

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJson(t *testing.T) {
	// Details provide additional information about a person
	type Details struct {
		Location string `yaml:"location" jsonschema:"description=location" fake:"Beijing"`
		Gender   string `yaml:"gender" jsonschema:"description=gender" fake:"male"`
	}

	type Person struct {
		Name       string    `yaml:"name" comment:"Full Name" jsonschema:"description=person name" fake:"Syd Xu"`
		Age        *int      `yaml:"age" jsonschema:"description=Age of a person" fake:"24"`
		Details    *Details  `yaml:"details" jsonschema:"description=Details of a person"`
		DetailList []Details `yaml:"details_list" jsonschema:"description=Details list of a person" fakesize:"1"`
	}
	var p Person
	enc, err := NewEncoder(p)
	require.NoError(t, err)
	exp := `
Respond with JSON in the following JSON schema:
` + "```json" + `
{
	"properties": {
		"Name": {
			"type": "string",
			"description": "person name"
		},
		"Age": {
			"type": "integer",
			"description": "Age of a person"
		},
		"Details": {
			"properties": {
				"Location": {
					"type": "string",
					"description": "location"
				},
				"Gender": {
					"type": "string",
					"description": "gender"
				}
			},
			"additionalProperties": false,
			"type": "object",
			"required": [
				"Location",
				"Gender"
			]
		},
		"DetailList": {
			"items": {
				"properties": {
					"Location": {
						"type": "string",
						"description": "location"
					},
					"Gender": {
						"type": "string",
						"description": "gender"
					}
				},
				"additionalProperties": false,
				"type": "object",
				"required": [
					"Location",
					"Gender"
				]
			},
			"type": "array",
			"description": "Details list of a person"
		}
	},
	"type": "object",
	"required": [
		"Name",
		"Age",
		"Details",
		"DetailList"
	]
}
` + "```" + `
Make sure to return an instance of the JSON, not the schema itself.
`

	assert.Equal(t, exp, enc.GetFormatInstructions())
}
