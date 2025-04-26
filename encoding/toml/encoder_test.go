package toml

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJson(t *testing.T) {
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
	enc := NewEncoder(p)
	exp := `
Respond with TOML in the following TOML schema:
` + "```toml" + `
Name = "Syd Xu"
Age = 24

[Details]
  Location = "Beijing"
  Gender = "male"

[[DetailList]]
  Location = "Beijing"
  Gender = "male"
` + "```" + `
Make sure to return an instance of the TOML, not the schema itself.
`

	assert.Equal(t, exp, enc.GetFormatInstructions())
}
