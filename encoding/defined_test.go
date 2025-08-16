package encoding

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/encoding/dummy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStruct struct {
	Field1 string `json:"field1"`
	Field2 int    `json:"field2"`
}

func (t *testStruct) Unmarshal(bs []byte) error {
	t.Field1 = string(bs)
	return nil
}

func TestNewTypedOutputParser_OK(t *testing.T) {
	t.Parallel()
	parser, err := NewTypedOutputParser(testStruct{}, ModeJSON)
	require.NoError(t, err)
	require.NotNil(t, parser)
	// Format instructions should come from the encoder
	assert.NotEmpty(t, parser.GetFormatInstructions())
	// Type should reference the struct type
	assert.Contains(t, parser.Type(), "testStruct")
}

func TestTypedOutputParser_Parse(t *testing.T) {
	t.Parallel()
	parser, err := NewTypedOutputParser(testStruct{}, ModeJSON)
	require.NoError(t, err)
	require.NotNil(t, parser)
	// Parse valid JSON
	input := `{"field1": "foo", "field2": 42}`
	result, err := parser.Parse(input)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "foo", result.Field1)
	assert.Equal(t, 42, result.Field2)

	// Parse invalid JSON: should return wrapped ErrFailedUnmarshalInput
	_, err = parser.Parse("{bad json}")
	require.Error(t, err)
	assert.True(t, errors.Is(err, chatmodel.ErrFailedUnmarshalOutput))
}

func TestTypedOutputParser_WithValidation(t *testing.T) {
	t.Parallel()
	parser, err := NewTypedOutputParser(testStruct{}, ModePlainText)
	require.NoError(t, err)
	parser.WithValidation(true)
	// Underlying dummy encoder validation always OK, so it parses
	val, err := parser.Parse("foobar")
	require.NoError(t, err)
	require.NotNil(t, val)

	// Make a validator encoder for full branch
	// This encoder fails Validate

	dummyParser := &TypedOutputParser[testStruct]{
		enc:      &badValidator{},
		name:     "bad",
		validate: true,
	}
	// Use plain text input since we're using ModePlainText
	_, err = dummyParser.Parse("test input")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate")
}

type badValidator struct{ dummy.Encoder }

func (badValidator) Validate(any) error            { return errors.New("fail validate") }
func (badValidator) GetFormatInstructions() string { return "" }
