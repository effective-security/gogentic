package chatmodel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatContext_Basics(t *testing.T) {
	t.Parallel()
	c := NewChatContext("tid", "cid", 123)
	require.NotNil(t, c)
	// IDs and AppData
	assert.Equal(t, "tid", c.GetUserID())
	assert.Equal(t, "cid", c.GetChatID())
	assert.Equal(t, 123, c.AppData())
	// RunID present and not empty
	assert.NotEmpty(t, c.GetRunID())

	// SetChatID
	c.SetChatID("newid")
	assert.Equal(t, "newid", c.GetChatID())

	// Metadata
	val, ok := c.GetMetadata("not-found")
	assert.Nil(t, val)
	assert.False(t, ok)
	c.SetMetadata("foo", 1)
	v, ok := c.GetMetadata("foo")
	assert.True(t, ok)
	assert.Equal(t, 1, v)
}

func TestNewChatContext_DefaultIDs(t *testing.T) {
	t.Parallel()
	c := NewChatContext("u", "", nil)
	require.NotNil(t, c)
	assert.NotEmpty(t, c.GetUserID())
	assert.NotEmpty(t, c.GetChatID())
	assert.NotEmpty(t, c.GetRunID())

	assert.Panics(t, func() {
		NewChatContext("", "", nil)
	})
}

func TestContextPlumbing(t *testing.T) {
	t.Parallel()
	c := NewChatContext("x", "y", nil)
	// WithChatContext + GetChatContext
	ctx := context.Background()
	ctx = WithChatContext(ctx, c)
	got := GetChatContext(ctx)
	assert.Equal(t, c, got)

	// SetChatID successful
	newctx, err := SetChatID(ctx, "bar")
	require.NoError(t, err)
	assert.Equal(t, "bar", GetChatContext(newctx).GetChatID())

	chatCtx := GetChatContext(ctx)
	require.NotNil(t, chatCtx)
	userID := chatCtx.GetUserID()
	chatID := chatCtx.GetChatID()
	assert.Equal(t, "x", userID)
	assert.Equal(t, "bar", chatID) // Already set just above

	// NewFromContext preserves context
	back := NewFromContext(ctx)
	assert.Equal(t, c, GetChatContext(back))

	// Nil context returns background
	bc := NewFromContext(context.Background())
	assert.Nil(t, GetChatContext(bc))
}

func TestGetSetChatID_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Setting chatid fails if context does not have correct value
	_, err := SetChatID(ctx, "fail")
	require.Error(t, err)
	assert.Nil(t, GetChatContext(ctx))
}

func TestNewChatID_Unique(t *testing.T) {
	id1 := NewChatID()
	id2 := NewChatID()
	assert.NotEqual(t, id1, id2)
}
