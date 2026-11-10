package store_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/store"
	"github.com/effective-security/x/maps"
	"github.com/effective-security/xlog"
	"github.com/moby/moby/api/types/container"
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
	source := &llms.MessageSource{
		Name:     "test",
		RunID:    "1234",
		ActionID: "action1",
	}

	msg1 := llms.MessageFromTextParts(llms.RoleHuman, "Hello").WithSource(source)
	msg2 := llms.MessageFromTextParts(llms.RoleAI, "Hi there!").WithSource(source)

	expErr := "invalid chat context"
	assert.EqualError(t, st.Reset(ctx), expErr)
	assert.EqualError(t, st.Add(ctx, msg1), expErr)

	_, err = st.UpdateChat(ctx, "", nil, nil)
	assert.EqualError(t, err, expErr)
	_, err = st.ListChatIDs(ctx)
	assert.EqualError(t, err, expErr)
	_, err = st.GetChatInfo(ctx, "", false)
	assert.EqualError(t, err, expErr)
	assert.Empty(t, st.Messages(ctx))

	chatCtx := chatmodel.NewChatContext(tenantID, chatID, appData)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	tID, cID, err := store.GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, tenantID, tID)
	assert.Equal(t, chatID, cID)

	require.NoError(t, st.Add(ctx, msg1))
	require.NoError(t, st.Add(ctx, msg2))

	// Test GetChatTitle for existing chat
	chi, err := st.GetChatInfo(ctx, cID, true)
	require.NoError(t, err)
	assert.Equal(t, "New Chat", chi.Title)
	assert.Empty(t, chi.Tags)
	assert.Equal(t, 2, len(chi.Messages))
	assert.Equal(t, source, chi.Messages[0].Source)
	assert.Equal(t, source, chi.Messages[1].Source)

	// Update chat title and test again
	chi2, err := st.UpdateChat(ctx, "Updated Title", map[string]any{"key": "value"}, []string{"tag1", "tag2"})
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", chi2.Title)
	assert.Equal(t, []string{"tag1", "tag2"}, chi2.Tags)
	chi3, err := st.UpdateChat(ctx, "", map[string]any{"key2": "value2"}, []string{"tag3", "tag4"})
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", chi3.Title)
	assert.Equal(t, []string{"tag1", "tag2", "tag3", "tag4"}, chi3.Tags)
	assert.Equal(t, []string{"key", "key2"}, maps.OrderedKeys(chi3.Metadata))

	chi, err = st.GetChatInfo(ctx, cID, false)
	require.NoError(t, err)
	assert.Equal(t, chi3, chi)

	// Retrieve messages from the store
	messages := st.Messages(ctx)
	require.Equal(t, 2, len(messages))
	assert.Equal(t, msg1, messages[0])
	assert.Equal(t, msg2, messages[1])

	chi, err = st.GetChatInfo(ctx, cID, false)
	require.NoError(t, err)
	assert.Equal(t, tenantID, chi.UserID)
	assert.Equal(t, chatID, chi.ChatID)
	assert.Equal(t, []string{"tag1", "tag2", "tag3", "tag4"}, chi.Tags)
	assert.Equal(t, []string{"key", "key2"}, maps.OrderedKeys(chi.Metadata))

	list, err := st.ListChatIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(list))

	// create a new chat context with the same tenant ID but different chat ID
	chatCtx = chatmodel.NewChatContext(tenantID, "", nil)
	ctx = chatmodel.WithChatContext(ctx, chatCtx)

	tID, cID, err = store.GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, tenantID, tID)
	assert.NotEqual(t, chatID, cID)

	now := time.Now()
	time.Sleep(2 * time.Millisecond)
	ci, err := st.UpdateChat(ctx, "New chat", map[string]any{"key": "value"}, nil)
	require.NoError(t, err)

	assert.Equal(t, chatCtx.GetUserID(), ci.UserID)
	assert.Equal(t, chatCtx.GetChatID(), ci.ChatID)
	assert.True(t, ci.CreatedAt.After(now))
	assert.True(t, ci.UpdatedAt.After(now))
	updatedAt := ci.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, st.Add(ctx, msg1))
	ci2, err := st.GetChatInfo(ctx, "", false)
	require.NoError(t, err)
	assert.Equal(t, chatCtx.GetUserID(), ci2.UserID)
	assert.Equal(t, chatCtx.GetChatID(), ci2.ChatID)
	assert.True(t, ci2.UpdatedAt.After(updatedAt))

	chats, err := st.ListChatIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, len(chats))
	for _, chat := range chats {
		ci, err := st.GetChatInfo(ctx, chat, false)
		require.NoError(t, err)
		assert.Equal(t, chatCtx.GetUserID(), ci.UserID)
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

	tID, cID, err := store.GetTenantAndChatID(ctx)
	require.NoError(t, err)
	assert.Equal(t, tenantID, tID)
	assert.Equal(t, chatID, cID)

	_ = st.Add(ctx, msg1)
	_ = st.Add(ctx, msg2)

	chats, err := st.ListChatIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))
	assert.Equal(t, chatID, chats[0])

	chi, err := st.GetChatInfo(ctx, cID, false)
	require.NoError(t, err)
	assert.Equal(t, tenantID, chi.UserID)
	assert.Equal(t, chatID, chi.ChatID)

	time.Sleep(2 * time.Millisecond)
	err = st.Add(ctx, msg1)
	require.NoError(t, err)
	chi, err = st.GetChatInfo(ctx, cID, false)
	require.NoError(t, err)
	assert.Equal(t, tenantID, chi.UserID)
	assert.Equal(t, chatID, chi.ChatID)

	chats, err = st.ListChatIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))
	assert.Equal(t, chatID, chats[0])

	mgr := store.NewRedisStoreManager(client, root)

	tenants, err := mgr.ListTenants(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(tenants))
	assert.Equal(t, tenantID, tenants[0])

	chats, err = st.ListChatIDs(ctx)
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

	chats, err = st.ListChatIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, len(chats))

	time.Sleep(2 * time.Second)
	deleted, err = mgr.Cleanup(ctx, tenantID, 1*time.Second)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), deleted)

	chats, err = st.ListChatIDs(ctx)
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
	defer func() {
		_ = client.Close()
	}()

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
				_, err := st.UpdateChat(ctx, fmt.Sprintf("Title from goroutine %d", goroutineID), metadata, nil)
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
	chatInfo, err := st.GetChatInfo(ctx, chatID, false)
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
	defer func() {
		_ = client.Close()
	}()

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
			chatInfo, err := st.GetChatInfo(ctx, chatID, false)
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
			assert.Equal(t, firstChatInfo.UserID, chatInfo.UserID)
			assert.Equal(t, firstChatInfo.Title, chatInfo.Title)
			// Don't compare timestamps as they may vary slightly due to creation time
		}
	}

	require.Equal(t, numGoroutines, chatCount, "All goroutines should have received chat info")
	require.NotNil(t, firstChatInfo, "Chat should have been created")
}

const (
	cmdGet      = "get"
	cmdSet      = "set"
	cmdDel      = "del"
	cmdRPush    = "rpush"
	cmdLRange   = "lrange"
	cmdSMembers = "smembers"
	cmdScan     = "scan"

	errWrongType = "WRONGTYPE Operation against a key holding the wrong kind of value"
	testRepo     = "github.com/effective-security/gogentic"
)

var errBoom = errors.New("boom")

// faultHook fails the named commands without reaching the server; other
// commands pass through. With noDial set, any command that would reach the
// network fails instead, so a test without a server never dials.
type faultHook struct {
	errs   map[string]error
	noDial bool
}

func (h *faultHook) DialHook(next redis.DialHook) redis.DialHook {
	if !h.noDial {
		return next
	}
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("dial disabled in test")
	}
}

func (h *faultHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err, ok := h.errs[cmd.Name()]; ok {
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}

func (h *faultHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if err, ok := h.errs[cmd.Name()]; ok {
				cmd.SetErr(err)
				return err
			}
		}
		return next(ctx, cmds)
	}
}

// newFaultyClient returns a client that never reaches a server: the named
// commands fail with the given errors, and anything else fails to dial.
func newFaultyClient(t *testing.T, errs map[string]error) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr:       "127.0.0.1:1",
		MaxRetries: -1,
	})
	client.AddHook(&faultHook{
		errs:   errs,
		noDial: true,
	})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func newRedisTestCtx(tenantID, chatID string) context.Context {
	return chatmodel.WithChatContext(context.Background(), chatmodel.NewChatContext(tenantID, chatID, nil))
}

func Test_RedisStore_CommandErrors(t *testing.T) {
	ctx := newRedisTestCtx("tenant1", "chat1")
	msg := llms.MessageFromTextParts(llms.RoleHuman, "Hello")

	tests := []struct {
		name   string
		errs   map[string]error
		call   func(st store.MessageStore, mgr store.MessageStoreManager) error
		expErr string
	}{
		{
			name: "Add",
			errs: map[string]error{cmdRPush: errBoom},
			call: func(st store.MessageStore, _ store.MessageStoreManager) error {
				return st.Add(ctx, msg)
			},
			expErr: "failed to store messages in Redis: boom",
		},
		{
			name: "Reset",
			errs: map[string]error{cmdDel: errBoom},
			call: func(st store.MessageStore, _ store.MessageStoreManager) error {
				return st.Reset(ctx)
			},
			expErr: "failed to reset chat in Redis: boom",
		},
		{
			name: "UpdateChat",
			errs: map[string]error{cmdGet: errBoom},
			call: func(st store.MessageStore, _ store.MessageStoreManager) error {
				_, err := st.UpdateChat(ctx, "title", nil, nil)
				return err
			},
			expErr: "failed to get chat info: failed to get chat info from Redis: boom",
		},
		{
			name: "GetChatInfo",
			errs: map[string]error{cmdGet: errBoom},
			call: func(st store.MessageStore, _ store.MessageStoreManager) error {
				_, err := st.GetChatInfo(ctx, "", false)
				return err
			},
			expErr: "failed to get chat info from Redis: boom",
		},
		{
			name: "GetChatInfo create fails",
			errs: map[string]error{
				cmdGet: redis.Nil,
				cmdSet: errBoom,
			},
			call: func(st store.MessageStore, _ store.MessageStoreManager) error {
				_, err := st.GetChatInfo(ctx, "", false)
				return err
			},
			expErr: "failed to initialize new chat info: failed to store chat info: boom",
		},
		{
			name: "ListChatIDs",
			errs: map[string]error{cmdSMembers: errBoom},
			call: func(st store.MessageStore, _ store.MessageStoreManager) error {
				_, err := st.ListChatIDs(ctx)
				return err
			},
			expErr: "failed to list chats from Redis: boom",
		},
		{
			name: "ListTenants",
			errs: map[string]error{cmdScan: errBoom},
			call: func(_ store.MessageStore, mgr store.MessageStoreManager) error {
				_, err := mgr.ListTenants(ctx)
				return err
			},
			expErr: "failed to scan tenants from Redis: boom",
		},
		{
			name: "Cleanup",
			errs: map[string]error{cmdSMembers: errBoom},
			call: func(_ store.MessageStore, mgr store.MessageStoreManager) error {
				_, err := mgr.Cleanup(ctx, "tenant1", time.Hour)
				return err
			},
			expErr: "failed to list chats from Redis: boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newFaultyClient(t, tc.errs)
			err := tc.call(store.NewRedisStore(client, "test"), store.NewRedisStoreManager(client, "test"))
			require.Error(t, err)
			assert.EqualError(t, err, tc.expErr)
			assert.True(t, errors.Is(err, errBoom), "the redis error must stay reachable")
		})
	}
}

func Test_RedisStore_ListChatIDs_Nil(t *testing.T) {
	client := newFaultyClient(t, map[string]error{cmdSMembers: redis.Nil})
	ids, err := store.NewRedisStore(client, "test").ListChatIDs(newRedisTestCtx("tenant1", "chat1"))
	require.NoError(t, err)
	assert.Nil(t, ids)
}

// captureStoreLogs routes xlog output to a buffer for the duration of the
// test. The formatter is process-wide, so callers must not run in parallel.
func captureStoreLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := xlog.GetFormatter()
	xlog.SetFormatter(xlog.NewStringFormatter(&buf, xlog.FormatSkipTime(true), xlog.FormatWithCaller(false)))
	t.Cleanup(func() {
		xlog.SetFormatter(prev)
	})
	return &buf
}

func Test_RedisStore_Messages_Errors(t *testing.T) {
	ctx := newRedisTestCtx("tenant1", "chat1")

	tests := []struct {
		name   string
		err    error
		expLog string
	}{
		{
			name:   "error",
			err:    errBoom,
			expLog: "level=E pkg=store reason=GetRedisMessages err=boom\n",
		},
		{
			name:   "timeout is a warning",
			err:    context.DeadlineExceeded,
			expLog: "level=W pkg=store reason=GetRedisMessages err=\"context deadline exceeded\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureStoreLogs(t)
			client := newFaultyClient(t, map[string]error{cmdLRange: tc.err})
			assert.Nil(t, store.NewRedisStore(client, "test").Messages(ctx))
			assert.Equal(t, tc.expLog, buf.String())
		})
	}

	// Without a chat context nothing is read.
	buf := captureStoreLogs(t)
	client := newFaultyClient(t, nil)
	assert.Nil(t, store.NewRedisStore(client, "test").Messages(context.Background()))
	assert.Equal(t, "level=E pkg=store reason=GetTenantAndChatID err=\"invalid chat context\"\n", buf.String())
}

// startRedis runs a Redis container for the test and returns its options.
func startRedis(t *testing.T) *redis.Options {
	t.Helper()
	ctx := context.Background()
	redisContainer, err := rediscon.Run(ctx, "redis:7")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, redisContainer.Terminate(ctx))
	})
	host, err := redisContainer.ConnectionString(ctx)
	require.NoError(t, err)
	options, err := redis.ParseURL(host)
	require.NoError(t, err)
	return options
}

func Test_RedisStore_StoredDataEdgeCases(t *testing.T) {
	options := startRedis(t)
	client := redis.NewClient(options)
	t.Cleanup(func() {
		_ = client.Close()
	})
	ctx := context.Background()
	require.NoError(t, client.Ping(ctx).Err())

	const prefix = "edge"
	key := func(tenantID, kind, id string) string {
		return fmt.Sprintf("%s/chatstore/%s/%s/%s", prefix, tenantID, kind, id)
	}
	chatsKey := func(tenantID string) string {
		return fmt.Sprintf("%s/chatstore/%s/chats", prefix, tenantID)
	}
	st := store.NewRedisStore(client, prefix)
	mgr := store.NewRedisStoreManager(client, prefix)

	t.Run("Messages skips corrupted entries", func(t *testing.T) {
		cctx := newRedisTestCtx("t-msgs", "c1")
		msg := llms.MessageFromTextParts(llms.RoleHuman, "Hello")
		require.NoError(t, st.Add(cctx, msg))
		require.NoError(t, client.RPush(ctx, key("t-msgs", "messages", "c1"), "not json").Err())

		assert.Equal(t, []llms.Message{msg}, st.Messages(cctx))
	})

	t.Run("UpdateChat adds metadata to a stored chat without it", func(t *testing.T) {
		cctx := newRedisTestCtx("t-meta", "c1")
		stored := `{"user_id":"t-meta","chat_id":"c1","title":"Stored"}`
		require.NoError(t, client.Set(ctx, key("t-meta", "info", "c1"), stored, 0).Err())

		info, err := st.UpdateChat(cctx, "", map[string]any{"k": "v"}, nil)
		require.NoError(t, err)
		assert.Equal(t, "Stored", info.Title)
		assert.Equal(t, map[string]any{"k": "v"}, info.Metadata)
	})

	t.Run("UpdateChat fails on unmarshalable metadata", func(t *testing.T) {
		cctx := newRedisTestCtx("t-marshal", "c1")
		info, err := st.UpdateChat(cctx, "", map[string]any{"bad": make(chan int)}, nil)
		assert.Nil(t, info)
		assert.EqualError(t, err, "failed to update chat info: failed to marshal chat info: json: unsupported type: chan int")
	})

	t.Run("GetChatInfo fails on corrupted chat info", func(t *testing.T) {
		cctx := newRedisTestCtx("t-corrupt", "c1")
		require.NoError(t, client.Set(ctx, key("t-corrupt", "info", "c1"), "{", 0).Err())

		info, err := st.GetChatInfo(cctx, "", false)
		assert.Nil(t, info)
		assert.EqualError(t, err, "failed to unmarshal chat info: unexpected end of JSON input")
	})

	t.Run("GetChatInfo fails to register a new chat", func(t *testing.T) {
		cctx := newRedisTestCtx("t-wrongtype", "c1")
		// The chat list is a string, so adding the new chat to it fails.
		require.NoError(t, client.Set(ctx, chatsKey("t-wrongtype"), "x", 0).Err())

		info, err := st.GetChatInfo(cctx, "", false)
		assert.Nil(t, info)
		assert.EqualError(t, err, "failed to initialize new chat info: failed to store chat info: "+errWrongType)
	})

	t.Run("Cleanup skips chats without info", func(t *testing.T) {
		require.NoError(t, client.SAdd(ctx, chatsKey("t-missing"), "c1").Err())

		deleted, err := mgr.Cleanup(ctx, "t-missing", -time.Hour)
		require.NoError(t, err)
		assert.Equal(t, uint32(0), deleted)
	})

	t.Run("Cleanup fails on chat info of the wrong type", func(t *testing.T) {
		require.NoError(t, client.SAdd(ctx, chatsKey("t-cleanup-type"), "c1").Err())
		require.NoError(t, client.RPush(ctx, key("t-cleanup-type", "info", "c1"), "x").Err())

		deleted, err := mgr.Cleanup(ctx, "t-cleanup-type", -time.Hour)
		assert.Equal(t, uint32(0), deleted)
		assert.EqualError(t, err, "failed to get chat info: "+errWrongType)
	})

	t.Run("Cleanup fails on corrupted chat info", func(t *testing.T) {
		require.NoError(t, client.SAdd(ctx, chatsKey("t-cleanup-json"), "c1").Err())
		require.NoError(t, client.Set(ctx, key("t-cleanup-json", "info", "c1"), "{", 0).Err())

		deleted, err := mgr.Cleanup(ctx, "t-cleanup-json", -time.Hour)
		assert.Equal(t, uint32(0), deleted)
		assert.EqualError(t, err, "failed to unmarshal chat info: unexpected end of JSON input")
	})

	t.Run("Cleanup fails to delete an expired chat", func(t *testing.T) {
		cctx := newRedisTestCtx("t-cleanup-del", "c1")
		_, err := st.UpdateChat(cctx, "expired", nil, nil)
		require.NoError(t, err)

		faulty := redis.NewClient(options)
		faulty.AddHook(&faultHook{
			errs: map[string]error{cmdDel: errBoom},
		})
		t.Cleanup(func() {
			_ = faulty.Close()
		})

		// A negative age puts the cutoff in the future, so the chat is expired.
		deleted, err := store.NewRedisStoreManager(faulty, prefix).Cleanup(ctx, "t-cleanup-del", -time.Hour)
		assert.Equal(t, uint32(0), deleted)
		assert.EqualError(t, err, "failed to delete chat info and messages from Redis: boom")

		// The chat survives the failed cleanup.
		ids, err := st.ListChatIDs(cctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"c1"}, ids)
	})
}

func Test_RedisStore_Add_NoMessages(t *testing.T) {
	// Every command would fail, so success proves nothing is sent.
	client := newFaultyClient(t, map[string]error{
		cmdRPush: errBoom,
		cmdGet:   errBoom,
	})
	assert.NoError(t, store.NewRedisStore(client, "test").Add(newRedisTestCtx("tenant1", "chat1")))
}
