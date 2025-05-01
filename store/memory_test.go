package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func Test_MemoryStore(t *testing.T) {
	// Create a new in-memory store
	st := store.NewMemoryStore()

	// Create a new chat context
	tenantID := "tenant1"
	chatID := "chat1"
	appData := map[string]string{"key": "value"}
	msg1 := &llms.HumanChatMessage{Content: "Hello"}
	msg2 := &llms.AIChatMessage{Content: "Hi there!"}

	ctx := context.Background()
	expErr := "invalid chat context"
	assert.EqualError(t, st.Reset(ctx), expErr)
	assert.EqualError(t, st.Add(ctx, msg1), expErr)
	_, err := st.UpdateChat(ctx, "", nil)
	assert.EqualError(t, err, expErr)
	_, err = st.ListChats(ctx)
	assert.EqualError(t, err, expErr)
	_, err = st.GetChatInfo(ctx, "")
	assert.EqualError(t, err, expErr)
	assert.Empty(t, st.Messages(ctx))

	chatCtx := chatmodel.NewChatContext(tenantID, chatID, appData)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	tID, cID, err := chatmodel.GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, tenantID, tID)
	assert.Equal(t, chatID, cID)

	require.NoError(t, st.Add(ctx, msg1))
	require.NoError(t, st.Add(ctx, msg2))

	// Retrieve messages from the store
	messages := st.Messages(ctx)
	require.Equal(t, 2, len(messages))
	assert.Equal(t, msg1.Content, messages[0].GetContent())
	assert.Equal(t, msg2.Content, messages[1].GetContent())

	chi, err := st.GetChatInfo(ctx, cID)
	require.NoError(t, err)
	assert.Equal(t, tenantID, chi.TenantID)
	assert.Equal(t, chatID, chi.ChatID)

	list, err := st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(list))

	chatCtx = chatmodel.NewChatContext(tenantID, "", nil)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	tID, cID, err = chatmodel.GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, tenantID, tID)
	assert.NotEqual(t, chatID, cID)

	now := time.Now()
	time.Sleep(2 * time.Millisecond)
	ci, err := st.UpdateChat(ctx, "New chat", map[string]any{"key": "value"})
	require.NoError(t, err)
	assert.Equal(t, chatCtx.GetTenantID(), ci.TenantID)
	assert.Equal(t, chatCtx.GetChatID(), ci.ChatID)
	assert.True(t, ci.CreatedAt.After(now))
	assert.True(t, ci.UpdatedAt.After(now))
	updatedAt := ci.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, st.Add(ctx, msg1))
	ci2, err := st.GetChatInfo(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, chatCtx.GetTenantID(), ci2.TenantID)
	assert.Equal(t, chatCtx.GetChatID(), ci2.ChatID)
	assert.True(t, ci2.UpdatedAt.After(updatedAt))

	chats, err := st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, len(chats))
	for _, chat := range chats {
		ci, err := st.GetChatInfo(ctx, chat)
		require.NoError(t, err)
		assert.Equal(t, chatCtx.GetTenantID(), ci.TenantID)
	}

	// Reset the chat
	err = st.Reset(ctx)
	require.NoError(t, err)

	// Verify that messages are cleared
	messages = st.Messages(ctx)
	assert.Equal(t, 0, len(messages))
}
