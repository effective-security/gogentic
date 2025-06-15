package chatmodel

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChatContext_Basics(t *testing.T) {
	t.Parallel()
	c := NewChatContext("tid", "cid", 123)
	require.NotNil(t, c)
	// IDs and AppData
	assert.Equal(t, "tid", c.GetTenantID())
	assert.Equal(t, "cid", c.GetChatID())
	assert.Equal(t, 123, c.AppData())
	// RunID present and not empty
	assert.NotEmpty(t, c.RunID())

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
	c := NewChatContext("", "", nil)
	require.NotNil(t, c)
	assert.NotEmpty(t, c.GetTenantID())
	assert.NotEmpty(t, c.GetChatID())
	assert.NotEmpty(t, c.RunID())
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

	// GetTenantAndChatID
	tenant, chat, err := GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, "x", tenant)
	assert.Equal(t, "bar", chat) // Already set just above

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
	// Getting IDs fails if not present
	_, _, err = GetTenantAndChatID(ctx)
	require.Error(t, err)
}

func TestNewChatID_Unique(t *testing.T) {
	id1 := NewChatID()
	id2 := NewChatID()
	assert.NotEqual(t, id1, id2)
}
