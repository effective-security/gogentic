package fstring

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const valueTemplate = "v={val}"

func TestFormatValueTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "nil",
			value: nil,
			want:  "v=nil",
		},
		{
			name:  "string",
			value: "s",
			want:  "v=s",
		},
		{
			name:  "runes",
			value: []rune("ru"),
			want:  "v=ru",
		},
		{
			name:  "bytes",
			value: []byte("by"),
			want:  "v=by",
		},
		{
			name:  "int",
			value: -1,
			want:  "v=-1",
		},
		{
			name:  "int8",
			value: int8(-8),
			want:  "v=-8",
		},
		{
			name:  "int16",
			value: int16(-16),
			want:  "v=-16",
		},
		{
			name:  "int32",
			value: int32(-32),
			want:  "v=-32",
		},
		{
			name:  "int64",
			value: int64(-64),
			want:  "v=-64",
		},
		{
			name:  "uint",
			value: uint(1),
			want:  "v=1",
		},
		{
			name:  "uint8",
			value: uint8(8),
			want:  "v=8",
		},
		{
			name:  "uint16",
			value: uint16(16),
			want:  "v=16",
		},
		{
			name:  "uint32",
			value: uint32(32),
			want:  "v=32",
		},
		{
			name:  "uint64",
			value: uint64(64),
			want:  "v=64",
		},
		{
			name:  "float32",
			value: float32(1.5),
			want:  "v=1.5",
		},
		{
			name:  "float64",
			value: 2.25,
			want:  "v=2.25",
		},
		{
			name:  "bool",
			value: true,
			want:  "v=true",
		},
		{
			name:  "default",
			value: []string{"a", "b"},
			want:  "v=[a b]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Format(valueTemplate, map[string]any{
				"val": tc.value,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFormatErrorsAreSentinels(t *testing.T) {
	t.Parallel()

	_, err := Format(valueTemplate, nil)
	require.ErrorIs(t, err, ErrArgsNotDefined)
	require.EqualError(t, err, "required: val: args not defined")

	_, err = Format("a}b", nil)
	require.ErrorIs(t, err, ErrRightBracketNotClosed)

	_, err = Format("a{", nil)
	require.ErrorIs(t, err, ErrLeftBracketNotClosed)

	_, err = Format("{ }", nil)
	require.ErrorIs(t, err, ErrEmptyExpression)
}
