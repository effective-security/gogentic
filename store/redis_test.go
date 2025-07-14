package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/store"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	rediscon "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
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
	msg1 := llms.MessageFromTextParts(llms.RoleHuman, "Hello")
	msg2 := llms.MessageFromTextParts(llms.RoleAI, "Hi there!")

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
	assert.Equal(t, msg1, messages[0])
	assert.Equal(t, msg2, messages[1])

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

func Test_RedisStoreManager(t *testing.T) {
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

	host, err := redisContainer.ConnectionString(ctx)
	require.NoError(t, err)

	options, err := redis.ParseURL(host)
	require.NoError(t, err)

	client := redis.NewClient(options)
	rs := client.Ping(ctx) // Ensure the connection is established
	require.NoError(t, rs.Err(), "failed to connect to Redis")

	root := fmt.Sprintf("test-%d", time.Now().Unix())

	st := store.NewRedisStore(client, root)

	tenantID := "tenant1"
	chatID := "chat1"
	appData := map[string]string{"key": "value"}
	msg1 := llms.MessageFromTextParts(llms.RoleHuman, "Hello")
	msg2 := llms.MessageFromTextParts(llms.RoleAI, "Hi there!")

	chatCtx := chatmodel.NewChatContext(tenantID, chatID, appData)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	tID, cID, err := chatmodel.GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, tenantID, tID)
	assert.Equal(t, chatID, cID)

	_ = st.Add(ctx, msg1)
	_ = st.Add(ctx, msg2)

	chats, err := st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))
	assert.Equal(t, chatID, chats[0])

	chi, err := st.GetChatInfo(ctx, cID)
	require.NoError(t, err)
	assert.Equal(t, tenantID, chi.TenantID)
	assert.Equal(t, chatID, chi.ChatID)

	time.Sleep(2 * time.Millisecond)
	err = st.Add(ctx, msg1)
	require.NoError(t, err)
	chi, err = st.GetChatInfo(ctx, cID)
	require.NoError(t, err)
	assert.Equal(t, tenantID, chi.TenantID)
	assert.Equal(t, chatID, chi.ChatID)

	chats, err = st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))
	assert.Equal(t, chatID, chats[0])

	mgr := store.NewRedisStoreManager(client, root)

	tenants, err := mgr.ListTenants(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(tenants))
	assert.Equal(t, tenantID, tenants[0])

	chats, err = st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))

	deleted, err := mgr.Cleanup(ctx, tenantID, 1*time.Second)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), deleted)

	time.Sleep(2 * time.Second)
	tenants, err = mgr.ListTenants(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(tenants))
	assert.Equal(t, tenantID, tenants[0])

	chats, err = st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))

	time.Sleep(2 * time.Second)
	deleted, err = mgr.Cleanup(ctx, tenantID, 1*time.Second)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), deleted)

	chats, err = st.ListChats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, len(chats))
}

func Test_RedisStore_ConcurrentUpdateChat(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections"),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() {
		err := container.Terminate(ctx)
		require.NoError(t, err)
	}()

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: host + ":" + port.Port(),
	})
	defer client.Close()

	st := store.NewRedisStore(client, "test")

	// Test concurrent UpdateChat calls
	tenantID := "tenant1"
	chatID := "chat1"
	chatCtx := chatmodel.NewChatContext(tenantID, chatID, nil)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	const numGoroutines = 10
	const updatesPerGoroutine = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*updatesPerGoroutine)

	// Launch concurrent goroutines that update the same chat
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < updatesPerGoroutine; j++ {
				metadata := map[string]any{
					"goroutine": goroutineID,
					"update":    j,
					"timestamp": time.Now().UnixNano(),
				}
				err := st.UpdateChat(ctx, fmt.Sprintf("Title from goroutine %d", goroutineID), metadata)
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent UpdateChat failed: %v", err)
	}

	// Verify that the chat was updated (should have the last update's metadata)
	chatInfo, err := st.GetChatInfo(ctx, chatID)
	require.NoError(t, err)
	require.NotNil(t, chatInfo)
	require.NotEmpty(t, chatInfo.Title)
	require.NotNil(t, chatInfo.Metadata)
}

func Test_RedisStore_ConcurrentChatCreation(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections"),
		},
		Started: true,
	})
	require.NoError(t, err)
	defer func() {
		err := container.Terminate(ctx)
		require.NoError(t, err)
	}()

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: host + ":" + port.Port(),
	})
	defer client.Close()

	st := store.NewRedisStore(client, "test")

	// Test concurrent chat creation
	tenantID := "tenant2"
	chatID := "chat2"
	chatCtx := chatmodel.NewChatContext(tenantID, chatID, nil)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)
	chatInfos := make(chan *store.ChatInfo, numGoroutines)

	// Launch concurrent goroutines that try to get/create the same chat
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			chatInfo, err := st.GetChatInfo(ctx, chatID)
			if err != nil {
				errors <- err
				return
			}
			chatInfos <- chatInfo
		}(i)
	}

	wg.Wait()
	close(errors)
	close(chatInfos)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent GetChatInfo failed: %v", err)
	}

	// Verify that only one chat was created and all goroutines got the same chat
	var firstChatInfo *store.ChatInfo
	chatCount := 0
	for chatInfo := range chatInfos {
		chatCount++
		if firstChatInfo == nil {
			firstChatInfo = chatInfo
		} else {
			// All chat infos should be identical (except timestamps which may vary slightly)
			assert.Equal(t, firstChatInfo.ChatID, chatInfo.ChatID)
			assert.Equal(t, firstChatInfo.TenantID, chatInfo.TenantID)
			assert.Equal(t, firstChatInfo.Title, chatInfo.Title)
			// Don't compare timestamps as they may vary slightly due to creation time
		}
	}

	require.Equal(t, numGoroutines, chatCount, "All goroutines should have received chat info")
	require.NotNil(t, firstChatInfo, "Chat should have been created")
}
