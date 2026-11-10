package json

import (
	"reflect"
	"testing"

	"github.com/effective-security/gogentic/pkg/schema"
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
      "type": "object",
      "required": [
        "Location",
        "Gender"
      ],
      "description": "Details of a person"
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
Use the exact field names as they are defined in the schema.
`

	assert.Equal(t, exp, enc.GetFormatInstructions())
}

type validated struct {
	Name string `json:"name" validate:"required"`
}

func TestEncoderMarshalValidateSchema(t *testing.T) {
	t.Parallel()

	enc, err := NewEncoder(validated{})
	require.NoError(t, err)

	bs, err := enc.Marshal(validated{
		Name: "n",
	})
	require.NoError(t, err)
	assert.Equal(t, `{"name":"n"}`, string(bs))

	require.NoError(t, enc.Validate(validated{
		Name: "n",
	}))
	err = enc.Validate(validated{})
	require.EqualError(t, err, "Key: 'validated.Name' Error:Field validation for 'Name' failed on the 'required' tag")

	sc, err := schema.New(reflect.TypeOf(validated{}))
	require.NoError(t, err)
	assert.Same(t, sc, enc.Schema())
}

func TestEncoderUnmarshal(t *testing.T) {
	t.Parallel()

	enc, err := NewEncoder(validated{})
	require.NoError(t, err)

	var v validated
	require.NoError(t, enc.Unmarshal([]byte("Sure, here you go:\n```json\n{\"name\":\"n\"}\n```\nDone."), &v))
	assert.Equal(t, validated{
		Name: "n",
	}, v)

	require.EqualError(t, enc.Unmarshal([]byte("no json here"), &v), "expected a struct in the input data")
}
