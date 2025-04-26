package dummy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func (p Person) String() string {
	return `Person information`
}

func TestJson(t *testing.T) {
	var p Person
	enc := NewEncoder()
	assert.Empty(t, enc.GetFormatInstructions())

	js, err := enc.Marshal(&p)
	require.NoError(t, err)

	exp := "Person information"
	assert.Equal(t, exp, string(js))
}
