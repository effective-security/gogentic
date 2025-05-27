package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/store"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	rediscon "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/tmc/langchaingo/llms"
)

func Test_RedisStore(t *testing.T) {
	ctx := context.Background()
	redisContainer, err := rediscon.Run(ctx, "redis:7",
		testcontainers.WithConfigModifier(func(config *container.Config) {
			config.Env = []string{
				"ALLOW_EMPTY_PASSWORD=yes",
				"REDIS_PASSWORD=redis",
				"REDIS_TLS_PORT=16379",
			}
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, redisContainer.Terminate(ctx))
	})

	state, err := redisContainer.State(ctx)
	require.NoError(t, err)
	require.True(t, state.Running)

	root := fmt.Sprintf("test-%d", time.Now().Unix())

	host, err := redisContainer.ConnectionString(ctx)
	require.NoError(t, err)

	options, err := redis.ParseURL(host)
	require.NoError(t, err)

	// Create a new Redis store
	client := redis.NewClient(options)

	rs := client.Ping(ctx) // Ensure the connection is established
	require.NoError(t, rs.Err(), "failed to connect to Redis")

	st := store.NewRedisStore(client, root)

	// Create a new chat context
	tenantID := "tenant1"
	chatID := "chat1"
	appData := map[string]string{"key": "value"}
	msg1 := &llms.HumanChatMessage{Content: "Hello"}
	msg2 := &llms.AIChatMessage{Content: "Hi there!"}

	expErr := "invalid chat context"
	assert.EqualError(t, st.Reset(ctx), expErr)
	assert.EqualError(t, st.Add(ctx, msg1), expErr)
	err = st.UpdateChat(ctx, "", nil)
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

	title, err := st.GetChatTitle(ctx, cID)
	require.NoError(t, err)
	assert.Empty(t, title)

	require.NoError(t, st.Add(ctx, msg1))
	require.NoError(t, st.Add(ctx, msg2))

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
