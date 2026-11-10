package yaml

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEncoder(t *testing.T) {
	type TestStruct struct {
		Name string `yaml:"name"`
	}
	encoder := NewEncoder(TestStruct{})
	assert.NotNil(t, encoder)

	// Validate behavior using public APIs
	data, err := encoder.Marshal(TestStruct{Name: "Test"})
	require.NoError(t, err)
	assert.Equal(t, "name: Test\n", string(data))
}

func TestEncoder_Marshal(t *testing.T) {
	type TestStruct struct {
		Name string `yaml:"name" comment:"name"`
		Age  int    `yaml:"age" comment:"age"`
	}

	tests := []struct {
		name     string
		input    any
		style    CommentStyle
		expected string
	}{
		{
			name: "basic struct without comments",
			input: TestStruct{
				Name: "John",
				Age:  30,
			},
			style:    NoComment,
			expected: "name: John\nage: 30\n",
		},
		{
			name: "basic struct with line comments",
			input: TestStruct{
				Name: "John",
				Age:  30,
			},
			style:    LineComment,
			expected: "name: John # name\nage: 30 # age\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder := NewEncoder(tt.input).WithCommentStyle(tt.style)
			bs, err := encoder.Marshal(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(bs))
		})
	}
}

func TestEncoder_Unmarshal(t *testing.T) {
	type TestStruct struct {
		Name string `yaml:"name"`
		Age  int    `yaml:"age"`
	}

	tests := []struct {
		name     string
		input    string
		expected TestStruct
	}{
		{
			name:  "basic yaml",
			input: "name: John\nage: 30",
			expected: TestStruct{
				Name: "John",
				Age:  30,
			},
		},
		{
			name:  "yaml with backticks",
			input: "```yaml\nname: John\nage: 30\n```",
			expected: TestStruct{
				Name: "John",
				Age:  30,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder := NewEncoder(TestStruct{})
			var result TestStruct
			err := encoder.Unmarshal([]byte(tt.input), &result)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEncoder_Validate(t *testing.T) {
	type TestStruct struct {
		Name string `yaml:"name" validate:"required"`
		Age  int    `yaml:"age" validate:"gte=0"`
	}

	tests := []struct {
		name        string
		input       TestStruct
		expectError bool
	}{
		{
			name: "valid struct",
			input: TestStruct{
				Name: "John",
				Age:  30,
			},
			expectError: false,
		},
		{
			name: "invalid struct - missing name",
			input: TestStruct{
				Age: 30,
			},
			expectError: true,
		},
		{
			name: "invalid struct - negative age",
			input: TestStruct{
				Name: "John",
				Age:  -1,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder := NewEncoder(TestStruct{})
			err := encoder.Validate(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEncoder_WithCommentStyle(t *testing.T) {
	encoder := NewEncoder(struct{}{})
	assert.Equal(t, NoComment, encoder.commentStyle)

	encoder = encoder.WithCommentStyle(LineComment)
	assert.Equal(t, LineComment, encoder.commentStyle)

	encoder = encoder.WithCommentStyle(HeadComment)
	assert.Equal(t, HeadComment, encoder.commentStyle)

	encoder = encoder.WithCommentStyle(FootComment)
	assert.Equal(t, FootComment, encoder.commentStyle)
}

func TestEncoder_GetFormatInstructions(t *testing.T) {
	type TestStruct struct {
		Name string `yaml:"name" comment:"Full Name" jsonschema:"description=person name" fake:"John"`
		Age  int    `yaml:"age" jsonschema:"description=Age of a person" fake:"30"`
	}

	encoder := NewEncoder(TestStruct{}).WithCommentStyle(LineComment)
	instructions := encoder.GetFormatInstructions()
	assert.Contains(t, instructions, "name: John # Full Name")
	assert.Contains(t, instructions, "age: 30 # Age of a person")
}

func TestEncoder_ComplexTypes(t *testing.T) {
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

	encoder := NewEncoder(Person{}).WithCommentStyle(LineComment)
	instructions := encoder.GetFormatInstructions()
	assert.Contains(t, instructions, "name: Syd Xu # Full Name")
	assert.Contains(t, instructions, "age: 24 # Age of a person")
	assert.Contains(t, instructions, "details: # Details of a person")
	assert.Contains(t, instructions, "location: Beijing # location")
	assert.Contains(t, instructions, "gender: male # gender")
	assert.Contains(t, instructions, "details_list: # Details list of a person")
}

func TestEncoder_NilValues(t *testing.T) {
	type TestStruct struct {
		Name *string `yaml:"name"`
		Age  *int    `yaml:"age"`
	}

	encoder := NewEncoder(TestStruct{})
	bs, err := encoder.Marshal(TestStruct{})
	require.NoError(t, err)
	assert.Equal(t, "name: null\nage: null\n", string(bs))
}

func TestEncoder_MapAndSlice(t *testing.T) {
	type TestStruct struct {
		Map   map[string]int `yaml:"map" fake:"{key1:1,key2:2}"`
		Slice []string       `yaml:"slice" fake:"{item1,item2}"`
	}

	encoder := NewEncoder(TestStruct{})
	bs, err := encoder.Marshal(TestStruct{
		Map:   map[string]int{"key1": 1, "key2": 2},
		Slice: []string{"item1", "item2"},
	})
	require.NoError(t, err)
	assert.Contains(t, string(bs), "map:")
	assert.Contains(t, string(bs), "key1: 1")
	assert.Contains(t, string(bs), "key2: 2")
	assert.Contains(t, string(bs), "slice:")
	assert.Contains(t, string(bs), "- item1")
	assert.Contains(t, string(bs), "- item2")
}

const (
	allKindsTail = "labels:\n    k: 1\nany: x\nnil_any: null\nptr: null\nnested:\n    items:\n        - 1\n        - 2\n"
	headerText   = "\nRespond with YAML in the following YAML schema without comments:\n```yaml\n"
	footerText   = "```\nMake sure to return an instance of the YAML, not the schema itself.\n"
)

// allKinds exercises every value branch of the comment-aware encoder.
type allKinds struct {
	Untagged string
	Ignored  string            `yaml:"-"`
	Name     string            `yaml:"name" comment:"the name"`
	NoDoc    string            `yaml:"nodoc"`
	Ratio    float64           `yaml:"ratio" jsonschema:"title=Ratio,description=a ratio"`
	Enabled  bool              `yaml:"enabled"`
	Count    uint              `yaml:"count"`
	Labels   map[string]int    `yaml:"labels"`
	Any      any               `yaml:"any"`
	NilAny   any               `yaml:"nil_any"`
	Ptr      *int              `yaml:"ptr"`
	Nested   *allKindsChildren `yaml:"nested"`
}

type allKindsChildren struct {
	Items []int `yaml:"items"`
}

func newAllKinds() *allKinds {
	return &allKinds{
		Untagged: "skip",
		Ignored:  "skip",
		Name:     "n",
		NoDoc:    "d",
		Ratio:    0.5,
		Enabled:  true,
		Count:    7,
		Labels: map[string]int{
			"k": 1,
		},
		Any: "x",
		Nested: &allKindsChildren{
			Items: []int{1, 2},
		},
	}
}

func TestEncoderMarshalAllKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		style CommentStyle
		want  string
	}{
		{
			name:  "head",
			style: HeadComment,
			want: "# the name\nname: n\nnodoc: d\n# a ratio\nratio: 0.500000\nenabled: true\ncount: 7\n" +
				allKindsTail,
		},
		{
			name:  "foot",
			style: FootComment,
			want: "name: n\n# the name\n\nnodoc: d\nratio: 0.500000\n# a ratio\n\nenabled: true\ncount: 7\n" +
				allKindsTail,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			enc := NewEncoder(allKinds{}).WithCommentStyle(tc.style)
			bs, err := enc.Marshal(newAllKinds())
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(bs))
		})
	}
}

// fakedPerson provides its own exemplar via schema.Faker.
type fakedPerson struct {
	Name string `yaml:"name" jsonschema:"description=person name"`
}

func (fakedPerson) Fake() any {
	return fakedPerson{
		Name: "from-faker",
	}
}

// scalarFaker returns a non-struct exemplar, which the comment encoder rejects.
type scalarFaker struct{}

func (scalarFaker) Fake() any {
	return 42
}

func TestEncoderGetFormatInstructionsFaker(t *testing.T) {
	t.Parallel()

	assert.Equal(t, headerText+"name: from-faker\n"+footerText,
		NewEncoder(fakedPerson{}).GetFormatInstructions())
	assert.Equal(t, headerText+"name: from-faker # person name\n"+footerText,
		NewEncoder(fakedPerson{}).WithCommentStyle(LineComment).GetFormatInstructions())
	assert.Equal(t, headerText+"42\n"+footerText,
		NewEncoder(scalarFaker{}).GetFormatInstructions())
	assert.Empty(t, NewEncoder(scalarFaker{}).WithCommentStyle(LineComment).GetFormatInstructions())
}

func TestEncoderMarshalWithCommentsEdgeCases(t *testing.T) {
	t.Parallel()

	enc := NewEncoder(allKinds{}).WithCommentStyle(LineComment)

	bs, err := enc.Marshal((*allKinds)(nil))
	require.NoError(t, err)
	assert.Equal(t, "null\n", string(bs))

	bs, err = enc.Marshal(42)
	require.EqualError(t, err, "expected struct, got int")
	assert.Nil(t, bs)
}
