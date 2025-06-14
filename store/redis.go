package store

import (
	"context"
	"encoding/json"
	"path"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/xlog"
	"github.com/redis/go-redis/v9"
	"github.com/tmc/langchaingo/llms"
)

// The redis store implements the MessageStore interface using Redis as the backend.
// It stores chat messages and metadata in Redis, allowing for retrieval and management of chat history.
// The Redis keys are structured to include tenant and chat IDs, ensuring that messages are correctly associated with the right chat context.
// It also provides methods to update chat metadata and list chats for a tenant.
// The Redis store requires a Redis client to be initialized and passed to the NewRedisStore function.
// The keys namespace is organized as follows:
// - `/<prefix>/chatstore/<tenantID>/messages/<chatID>` for storing chat messages
// - `/<prefix>/chatstore/<tenantID>/info/<chatID>` for storing chat metadata
// - `/<prefix>/chatstore/<tenantID>/chats` for storing a set of chat IDs associated with a tenant

type redisStore struct {
	client *redis.Client
	prefix string
}

func NewRedisStore(client *redis.Client, prefix string) MessageStore {
	return &redisStore{
		client: client,
		prefix: prefix,
	}
}

func (m *redisStore) getRedisMessagesKey(tenantID, chatID string) string {
	return path.Join(m.prefix, "chatstore", tenantID, "messages", chatID)
}

func (m *redisStore) getRedisChatInfoKey(tenantID, chatID string) string {
	return path.Join(m.prefix, "chatstore", tenantID, "info", chatID)
}

func (m *redisStore) getRedisChatListKey(tenantID string) string {
	return path.Join(m.prefix, "chatstore", tenantID, "chats")
}

func (m *redisStore) Messages(ctx context.Context) []llms.ChatMessage {
	return ToChatMessages(m.messages(ctx))
}

func (m *redisStore) messages(ctx context.Context) []llms.ChatMessageModel {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		logger.ContextKV(ctx, xlog.ERROR, "reason", "GetTenantAndChatID", "err", err.Error())
		return nil
	}

	key := m.getRedisMessagesKey(tenantID, chatID)
	// Get all messages in the list
	data, err := m.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		logger.ContextKV(ctx, xlog.ERROR, "reason", "GetRedisMessages", "err", err.Error())
		return nil
	}

	var messages []llms.ChatMessageModel
	for _, item := range data {
		var msg llms.ChatMessageModel
		if err := json.Unmarshal([]byte(item), &msg); err != nil {
			logger.ContextKV(ctx, xlog.ERROR, "reason", "unmarshal message", "err", err.Error())
			continue
		}
		messages = append(messages, msg)
	}
	return messages
}

func (m *redisStore) Add(ctx context.Context, msg llms.ChatMessage) error {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return err
	}

	model := llms.ConvertChatMessageToModel(msg)
	data, err := json.Marshal(model)
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	key := m.getRedisMessagesKey(tenantID, chatID)
	pipe := m.client.Pipeline()
	pipe.RPush(ctx, key, data)
	// Keep only the last 50 messages
	pipe.LTrim(ctx, key, -50, -1)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to store message in Redis")
	}

	// Update the time
	return m.UpdateChat(ctx, "", nil)
}

func (m *redisStore) Reset(ctx context.Context) error {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return err
	}

	messageKey := m.getRedisMessagesKey(tenantID, chatID)
	chatKey := m.getRedisChatInfoKey(tenantID, chatID)
	chatListKey := m.getRedisChatListKey(tenantID)

	pipe := m.client.Pipeline()
	pipe.Del(ctx, messageKey)
	pipe.Del(ctx, chatKey)
	pipe.SRem(ctx, chatListKey, chatID)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to reset chat in Redis")
	}

	return nil
}

// UpdateChat creates or updates a chat with the title, and metadata for a tenant and chat ID from context.
func (m *redisStore) UpdateChat(ctx context.Context, title string, metadata map[string]any) error {
	_, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return err
	}

	chat, err := m.getChatInfo(ctx, chatID)
	if err != nil {
		return errors.Wrap(err, "failed to get chat info")
	}

	if title != "" {
		chat.Title = title
	}
	if metadata != nil {
		if chat.Metadata == nil {
			chat.Metadata = make(map[string]any)
		}
		for k, v := range metadata {
			chat.Metadata[k] = v
		}
	}
	chat.UpdatedAt = time.Now()

	return m.updateChat(ctx, chat, false)
}

func (m *redisStore) updateChat(ctx context.Context, chat *ChatInfo, isNew bool) error {
	chatData, err := json.Marshal(chat)
	if err != nil {
		return errors.Wrap(err, "failed to marshal chat info")
	}

	chatKey := m.getRedisChatInfoKey(chat.TenantID, chat.ChatID)
	chatListKey := m.getRedisChatListKey(chat.TenantID)

	pipe := m.client.Pipeline()
	pipe.Set(ctx, chatKey, chatData, 0)
	if isNew {
		pipe.SAdd(ctx, chatListKey, chat.ChatID)
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to store chat info in Redis")
	}

	return nil
}

func (m *redisStore) ListChats(ctx context.Context) ([]string, error) {
	tenantID, _, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, err
	}

	chatListKey := m.getRedisChatListKey(tenantID)
	chatIDs, err := m.client.SMembers(ctx, chatListKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to list chats from Redis")
	}

	return chatIDs, nil
}

func (m *redisStore) GetChatInfo(ctx context.Context, id string) (*ChatInfo, error) {
	info, err := m.getChatInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	info.Messages = m.Messages(ctx)
	return info, nil
}

// returns the chat information for a tenant and chat ID from context,
// without messages
func (m *redisStore) getChatInfo(ctx context.Context, id string) (*ChatInfo, error) {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, err
	}
	if id == "" {
		id = chatID
	}

	chatKey := m.getRedisChatInfoKey(tenantID, id)
	var chat *ChatInfo
	data, err := m.client.Get(ctx, chatKey).Result()
	if err != nil {
		if err != redis.Nil && !errors.Is(err, redis.Nil) {
			return nil, errors.Wrap(err, "failed to get chat info from Redis")
		}
		chat = &ChatInfo{
			TenantID:  tenantID,
			ChatID:    chatID,
			Title:     "New Chat",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Metadata:  make(map[string]any),
		}

		err = m.updateChat(ctx, chat, true)
		if err != nil {
			return nil, errors.Wrap(err, "failed to initialize new chat info")
		}
	} else {
		chat = &ChatInfo{}
		err = json.Unmarshal([]byte(data), chat)
		if err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal chat info")
		}
	}

	return chat, nil
}

// GetChatTitle returns the title for a tenant and chat ID from context.
// If the chat does not exist or not persisted, it returns an empty string.
func (m *redisStore) GetChatTitle(ctx context.Context, id string) (string, error) {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return "", err
	}
	if id == "" {
		id = chatID
	}

	chatKey := m.getRedisChatInfoKey(tenantID, id)
	data, err := m.client.Get(ctx, chatKey).Result()
	if err != nil {
		if err != redis.Nil && !errors.Is(err, redis.Nil) {
			return "", errors.Wrap(err, "failed to get chat info from Redis")
		}
		return "", nil
	}

	var chat ChatInfo
	err = json.Unmarshal([]byte(data), &chat)
	if err != nil {
		return "", errors.Wrap(err, "failed to unmarshal chat info")
	}
	return chat.Title, nil
}

func NewRedisStoreManager(client *redis.Client, prefix string) MessageStoreManager {
	return &redisStore{
		client: client,
		prefix: prefix,
	}
}

func (m *redisStore) ListTenants(ctx context.Context) ([]string, error) {
	tenantListKey := path.Join(m.prefix, "chatstore")
	// Use SCAN instead of KEYS for better performance
	iter := m.client.Scan(ctx, 0, tenantListKey+"/*", 0).Iterator()
	tenants := make(map[string]struct{})

	for iter.Next(ctx) {
		key := iter.Val()
		// Only get immediate tenant directories
		parts := strings.Split(strings.TrimPrefix(key, tenantListKey+"/"), "/")
		if len(parts) > 0 {
			tenants[parts[0]] = struct{}{}
		}
	}

	if err := iter.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to scan tenants from Redis")
	}

	result := make([]string, 0, len(tenants))
	for tenant := range tenants {
		result = append(result, tenant)
	}
	return result, nil
}

func (m *redisStore) Cleanup(ctx context.Context, tenantID string, olderThan time.Duration) (uint32, error) {
	chatListKey := m.getRedisChatListKey(tenantID)
	chatIDs, err := m.client.SMembers(ctx, chatListKey).Result()
	if err != nil {
		return 0, errors.Wrap(err, "failed to list chats from Redis")
	}

	deleted := uint32(0)
	cutoff := time.Now().Add(-olderThan)
	for _, chatID := range chatIDs {
		chatKey := m.getRedisChatInfoKey(tenantID, chatID)
		data, err := m.client.Get(ctx, chatKey).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return 0, errors.Wrap(err, "failed to get chat info")
		}

		var chat ChatInfo
		if err := json.Unmarshal([]byte(data), &chat); err != nil {
			return 0, errors.Wrap(err, "failed to unmarshal chat info")
		}

		if chat.UpdatedAt.Before(cutoff) {
			pipe := m.client.Pipeline()
			pipe.Del(ctx, chatKey)
			pipe.Del(ctx, m.getRedisMessagesKey(tenantID, chatID))
			pipe.SRem(ctx, chatListKey, chatID)
			_, err = pipe.Exec(ctx)
			if err != nil {
				return 0, errors.Wrap(err, "failed to delete chat info and messages from Redis")
			}
			deleted++
		}
	}
	return deleted, nil
}
