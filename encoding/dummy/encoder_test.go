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

const plainText = "plain text"

// textTarget implements Unmarshaler to capture raw input.
type textTarget struct {
	raw string
}

func (t *textTarget) Unmarshal(bs []byte) error {
	t.raw = string(bs)
	return nil
}

func TestMarshalTypes(t *testing.T) {
	t.Parallel()

	str := plainText
	bs := []byte(plainText)
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "string",
			value: plainText,
			want:  plainText,
		},
		{
			name:  "bytes",
			value: []byte(plainText),
			want:  plainText,
		},
		{
			name:  "string pointer",
			value: &str,
			want:  plainText,
		},
		{
			name:  "bytes pointer",
			value: &bs,
			want:  plainText,
		},
		{
			name: "json fallback",
			value: map[string]int{
				"a": 1,
			},
			want: `{"a":1}`,
		},
	}
	enc := NewEncoder()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := enc.Marshal(tc.value)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}

	_, err := enc.Marshal(make(chan int))
	require.EqualError(t, err, "json: unsupported type: chan int")
}

func TestUnmarshalTypes(t *testing.T) {
	t.Parallel()

	enc := NewEncoder()
	input := []byte(plainText)

	var target textTarget
	require.NoError(t, enc.Unmarshal(input, &target))
	assert.Equal(t, plainText, target.raw)

	var s string
	require.NoError(t, enc.Unmarshal(input, &s))
	assert.Equal(t, plainText, s)

	var b []byte
	require.NoError(t, enc.Unmarshal(input, &b))
	assert.Equal(t, input, b)

	var m map[string]int
	require.NoError(t, enc.Unmarshal([]byte(`{"a":1}`), &m))
	assert.Equal(t, map[string]int{
		"a": 1,
	}, m)

	var n int
	err := enc.Unmarshal(input, &n)
	require.EqualError(t, err, "invalid character 'p' looking for beginning of value")

	assert.NoError(t, enc.Validate(nil))
}
