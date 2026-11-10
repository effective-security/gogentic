package store_test

import (
	"context"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testUserID = "user1"
	testOrgID  = "org1"
	testChatID = "chat1"
)

// fixedStore is a MessageStore that returns the same messages regardless of
// the context. It is not a MessageStoreManager.
type fixedStore struct {
	store.MessageStore
	messages []llms.Message
}

func (s *fixedStore) Messages(context.Context) []llms.Message {
	return s.messages
}

func newChatCtx(orgID string) context.Context {
	chatCtx := chatmodel.NewChatContext(testUserID, testChatID, nil)
	if orgID != "" {
		chatCtx.SetOrgID(orgID)
	}
	return chatmodel.WithChatContext(context.Background(), chatCtx)
}

func Test_GetTenantAndChatID(t *testing.T) {
	_, _, err := store.GetTenantAndChatID(context.Background())
	assert.True(t, errors.Is(err, chatmodel.ErrInvalidChatContext))

	tenantID, chatID, err := store.GetTenantAndChatID(newChatCtx(""))
	require.NoError(t, err)
	assert.Equal(t, testUserID, tenantID)
	assert.Equal(t, testChatID, chatID)

	// With an org, the tenant is scoped to it.
	tenantID, chatID, err = store.GetTenantAndChatID(newChatCtx(testOrgID))
	require.NoError(t, err)
	assert.Equal(t, testOrgID+":"+testUserID, tenantID)
	assert.Equal(t, testChatID, chatID)
}

func Test_PopulateMemoryStore(t *testing.T) {
	ctx := newChatCtx("")
	msgs := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Hello"),
		llms.MessageFromTextParts(llms.RoleAI, "Hi"),
	}

	// A nil source yields an empty store.
	empty, err := store.PopulateMemoryStore(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty.Messages(ctx))

	src := store.NewMemoryStore()
	require.NoError(t, src.Add(ctx, msgs...))

	copied, err := store.PopulateMemoryStore(ctx, src)
	require.NoError(t, err)
	assert.Equal(t, msgs, copied.Messages(ctx))

	// The copy is independent of the source.
	require.NoError(t, copied.Add(ctx, llms.MessageFromTextParts(llms.RoleHuman, "More")))
	assert.Len(t, src.Messages(ctx), len(msgs))

	// A source without messages for the chat yields an empty store.
	copied, err = store.PopulateMemoryStore(newChatCtx(testOrgID), src)
	require.NoError(t, err)
	assert.Empty(t, copied.Messages(newChatCtx(testOrgID)))
}

func Test_PopulateMemoryStore_AddError(t *testing.T) {
	src := &fixedStore{
		messages: []llms.Message{
			llms.MessageFromTextParts(llms.RoleHuman, "Hello"),
		},
	}
	// The source returns messages, but the copy cannot be keyed without a
	// chat context.
	copied, err := store.PopulateMemoryStore(context.Background(), src)
	assert.Nil(t, copied)
	assert.True(t, errors.Is(err, chatmodel.ErrInvalidChatContext))
}
