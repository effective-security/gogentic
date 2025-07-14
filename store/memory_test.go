package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_MemoryStore(t *testing.T) {
	// Create a new in-memory store
	st := store.NewMemoryStore()

	// Create a new chat context
	tenantID := "tenant1"
	chatID := "chat1"
	appData := map[string]string{"key": "value"}
	msg1 := llms.MessageFromTextParts(llms.RoleHuman, "Hello")
	msg2 := llms.MessageFromTextParts(llms.RoleAI, "Hi there!")

	ctx := context.Background()
	expErr := "invalid chat context"
	assert.EqualError(t, st.Reset(ctx), expErr)
	assert.EqualError(t, st.Add(ctx, msg1), expErr)
	err := st.UpdateChat(ctx, "", nil)
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

	// Test GetChatTitle for existing chat
	title, err := st.GetChatTitle(ctx, cID)
	require.NoError(t, err)
	assert.Empty(t, title)

	require.NoError(t, st.Add(ctx, msg1))
	require.NoError(t, st.Add(ctx, msg2))

	// Retrieve messages from the store
	messages := st.Messages(ctx)
	require.Equal(t, 2, len(messages))
	assert.Equal(t, msg1, messages[0])
	assert.Equal(t, msg2, messages[1])

	chi, err := st.GetChatInfo(ctx, cID)
	require.NoError(t, err)
	assert.Equal(t, tenantID, chi.TenantID)
	assert.Equal(t, chatID, chi.ChatID)

	// Test GetChatTitle for existing chat
	title, err = st.GetChatTitle(ctx, cID)
	require.NoError(t, err)
	assert.Equal(t, "New Chat", title)

	// Update chat title and test again
	require.NoError(t, st.UpdateChat(ctx, "Updated Title", nil))
	title, err = st.GetChatTitle(ctx, cID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", title)

	// Test GetChatTitle for non-existing chat
	title, err = st.GetChatTitle(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, "", title)

	list, err := st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(list))

	// create a new chat context with the same tenant ID but different chat ID
	chatCtx = chatmodel.NewChatContext(tenantID, "", nil)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	tID, cID, err = chatmodel.GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, tenantID, tID)
	assert.NotEqual(t, chatID, cID)

	now := time.Now()
	time.Sleep(2 * time.Millisecond)
	err = st.UpdateChat(ctx, "New chat", map[string]any{"key": "value"})
	require.NoError(t, err)
	ci, err := st.GetChatInfo(ctx, "")
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

func Test_MemoryStoreManager(t *testing.T) {
	tenantID := "tenant1"
	chatID := "chat1"
	appData := map[string]string{"key": "value"}
	chatCtx := chatmodel.NewChatContext(tenantID, chatID, appData)
	ctx := chatmodel.WithChatContext(context.Background(), chatCtx)

	st := store.NewMemoryStore()
	_ = st.Add(ctx, llms.MessageFromTextParts(llms.RoleHuman, "Hello"))
	_ = st.Add(ctx, llms.MessageFromTextParts(llms.RoleAI, "Hi there!"))
	_ = st.Add(ctx, llms.MessageFromTextParts(llms.RoleHuman, "How are you?"))
	_ = st.Add(ctx, llms.MessageFromTextParts(llms.RoleAI, "I'm good, thank you!"))
	_ = st.Add(ctx, llms.MessageFromTextParts(llms.RoleHuman, "What is your name?"))
	_ = st.Add(ctx, llms.MessageFromTextParts(llms.RoleAI, "My name is John Doe."))
	_ = st.Add(ctx, llms.MessageFromParts(llms.RoleAI, llms.ToolCall{
		ID:   "123",
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      "add",
			Arguments: `{"a":1,"b":2}`,
		},
	}))
	_ = st.Add(ctx, llms.MessageFromParts(llms.RoleAI, llms.ToolCallResponse{
		ToolCallID: "123",
		Name:       "add",
		Content:    "42",
	}))

	mgr := store.NewMemoryStoreManager(st)

	tenants, err := mgr.ListTenants(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(tenants))
	assert.Equal(t, tenantID, tenants[0])

	deleted, err := mgr.Cleanup(ctx, tenantID, 1*time.Second)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), deleted)

	tenants, err = mgr.ListTenants(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(tenants))
	assert.Equal(t, tenantID, tenants[0])

	chats, err := st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))

	time.Sleep(2 * time.Second)
	deleted, err = mgr.Cleanup(ctx, tenantID, 1*time.Second)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), deleted)

	tenants, err = mgr.ListTenants(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(tenants))

	chats, err = st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, len(chats))
}
