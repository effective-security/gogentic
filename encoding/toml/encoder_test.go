package toml

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type validated struct {
	Name string `toml:"name" validate:"required"`
}

// fakedConfig provides its own exemplar via schema.Faker.
type fakedConfig struct {
	Name string `toml:"name"`
}

func (fakedConfig) Fake() any {
	return fakedConfig{
		Name: "from-faker",
	}
}

// unencodableFaker returns a value TOML cannot encode.
type unencodableFaker struct{}

func (unencodableFaker) Fake() any {
	return make(chan int)
}

func TestEncoderUnmarshalValidate(t *testing.T) {
	t.Parallel()

	enc := NewEncoder(validated{})

	var v validated
	require.NoError(t, enc.Unmarshal([]byte("```toml\nname = \"n\"\n```"), &v))
	assert.Equal(t, validated{
		Name: "n",
	}, v)

	err := enc.Unmarshal([]byte("name = "), &v)
	require.EqualError(t, err, `toml: line 1 (last key "name"): unexpected EOF; expected value`)

	require.NoError(t, enc.Validate(v))
	err = enc.Validate(validated{})
	require.EqualError(t, err, "Key: 'validated.Name' Error:Field validation for 'Name' failed on the 'required' tag")
}

func TestEncoderGetFormatInstructionsFaker(t *testing.T) {
	t.Parallel()

	exp := "\nRespond with TOML in the following TOML schema:\n```toml\nname = \"from-faker\"\n```" +
		"\nMake sure to return an instance of the TOML, not the schema itself.\n"
	assert.Equal(t, exp, NewEncoder(fakedConfig{}).GetFormatInstructions())
	assert.Empty(t, NewEncoder(unencodableFaker{}).GetFormatInstructions())
}
