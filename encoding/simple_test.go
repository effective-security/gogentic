package encoding

import (
	"testing"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleOutputParser_Parse(t *testing.T) {
	t.Parallel()
	parser := NewSimpleOutputParser()
	require.NotNil(t, parser)
	// Covers interface assertion
	var _ chatmodel.OutputParser[chatmodel.String] = parser

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple text", "hello world", "hello world"},
		{"with whitespace", "  test\n", "test"},
		{"empty", "   ", ""},
		{"already trimmed", "foo", "foo"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			val, err := parser.Parse(tt.input)
			require.NoError(t, err)
			require.NotNil(t, val)
			assert.Equal(t, tt.want, val.String())
			// Ensure GetContent consistent
			assert.Equal(t, tt.want, val.GetContent())
		})
	}
}

func TestSimpleOutputParser_TypeAndFormat(t *testing.T) {
	t.Parallel()
	parser := NewSimpleOutputParser()
	require.NotNil(t, parser)
	assert.Equal(t, "simple_parser", parser.Type())
	assert.Equal(t, "", parser.GetFormatInstructions())
}
