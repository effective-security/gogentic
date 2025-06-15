package chatmodel

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewStringAndStringMethods(t *testing.T) {
	t.Parallel()
	s := NewString("foo")
	require.NotNil(t, s)
	assert.Equal(t, "foo", s.value)
	assert.Equal(t, "foo", s.String())
	assert.Equal(t, "foo", s.GetContent())
	assert.Equal(t, []byte("foo"), s.Bytes())
}

func TestParseInput(t *testing.T) {
	t.Parallel()
	s := NewString("")
	err := s.ParseInput("bar")
	require.NoError(t, err)
	assert.Equal(t, "bar", s.String())
	// parse empty
	err = s.ParseInput("")
	require.NoError(t, err)
	assert.Equal(t, "", s.String())
}

func TestUnmarshal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"basic", []byte("hello"), "hello"},
		{"quoted", []byte("\"foo\""), "foo"},
		{"both ends", []byte("\"test\""), "test"},
		{"empty", []byte{}, ""},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &String{}
			err := s.Unmarshal(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, s.String())
		})
	}
}
