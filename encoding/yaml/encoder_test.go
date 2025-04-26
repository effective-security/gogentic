package yaml

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestYaml(t *testing.T) {
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
	enc := NewEncoder(p).WithCommentStyle(LineComment)
	exp := `
Respond with YAML in the following YAML schema without comments:
` + "```yaml" + `
name: Syd Xu # Full Name
age: 24 # Age of a person
details: # Details of a person
    location: Beijing # location
    gender: male # gender
details_list: # Details list of a person
    - location: Beijing # location
      gender: male # gender
` + "```" + `
Make sure to return an instance of the YAML, not the schema itself.
`

	assert.Equal(t, exp, enc.GetFormatInstructions())
}
