package chatmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	stringerValue = "from String"
	contentValue  = "from GetContent"
)

type stringerAndContent struct{}

func (stringerAndContent) String() string     { return stringerValue }
func (stringerAndContent) GetContent() string { return contentValue }

type contentOnly struct{}

func (contentOnly) GetContent() string { return contentValue }

type plainValue struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestStringifyAndToBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  any
		exp  string
	}{
		{
			name: "Stringer wins over ContentProvider",
			val:  stringerAndContent{},
			exp:  stringerValue,
		},
		{
			name: "ContentProvider",
			val:  contentOnly{},
			exp:  contentValue,
		},
		{
			name: "JSON fallback",
			val: plainValue{
				Name:  "a",
				Count: 2,
			},
			exp: `{"name":"a","count":2}`,
		},
		{
			name: "nil",
			val:  nil,
			exp:  "null",
		},
		{
			name: "unsupported type marshals to empty",
			val:  make(chan int),
			exp:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.exp, Stringify(tc.val))
			assert.Equal(t, tc.exp, string(ToBytes(tc.val)))
		})
	}
}
