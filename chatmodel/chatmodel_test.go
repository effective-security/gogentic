package chatmodel

import (
	goerr "errors"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
)

func TestErrFailedUnmarshalInput(t *testing.T) {
	err := ErrFailedUnmarshalInput
	assert.True(t, goerr.Is(err, ErrFailedUnmarshalInput))
	assert.True(t, goerr.Is(errors.WithStack(err), ErrFailedUnmarshalInput))
	assert.True(t, goerr.Is(errors.Wrap(err, "test"), ErrFailedUnmarshalInput))
	assert.True(t, goerr.Is(errors.WithMessage(err, "test"), ErrFailedUnmarshalInput))
	assert.False(t, goerr.Is(err, ErrInvalidChatContext))
}
