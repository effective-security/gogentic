package store

import (
	"context"
	"time"

	"github.com/effective-security/xlog"
	"github.com/tmc/langchaingo/llms"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "store")

type ChatInfo struct {
	TenantID  string
	ChatID    string
	Title     string
	Messages  []llms.ChatMessage
	CreatedAt time.Time
	UpdatedAt time.Time
	Metadata  map[string]any
}

// MessageStore is an interface for storing and retrieving chat messages.
// The supplied context must have ChatContext with tenantID and chatID,
// created by NewChatContext.
type MessageStore interface {
	// Messages returns the messages for a tenant and chat ID from context.
	Messages(ctx context.Context) []llms.ChatMessage
	// Add adds a message to the chat history for a tenant and chat ID from context.
	Add(ctx context.Context, msg llms.ChatMessage) error
	// Reset resets the chat history for a tenant and chat ID from context.
	Reset(ctx context.Context) error

	// UpdateChat creates or updates a chat with the title, and metadata for a tenant and chat ID from context.
	UpdateChat(ctx context.Context, title string, metadata map[string]any) error
	// ListChats returns a list of chat IDs for a tenant and chat ID from context.
	ListChats(ctx context.Context) ([]string, error)
	// GetChatInfo returns the chat information for a tenant and chat ID from context.
	GetChatInfo(ctx context.Context, id string) (*ChatInfo, error)

	// GetChatTitle returns the title for a tenant and chat ID from context.
	// If the chat does not exist or not persisted, it returns an empty string.
	GetChatTitle(ctx context.Context, id string) (string, error)
}

type MessageStoreManager interface {
	ListTenants(ctx context.Context) ([]string, error)
	Cleanup(ctx context.Context, tenantID string, olderThan time.Duration) (uint32, error)
}

func PopulateMemoryStore(ctx context.Context, store MessageStore) (MessageStore, error) {
	s := NewMemoryStore()
	if store != nil {
		for _, msg := range store.Messages(ctx) {
			err := s.Add(ctx, msg)
			if err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

// ConvertChatMessagesToModel Convert a ChatMessage list to a ChatMessageModel list.
func ConvertChatMessagesToModel(m []llms.ChatMessage) []llms.ChatMessageModel {
	models := make([]llms.ChatMessageModel, len(m))
	for i, msg := range m {
		models[i] = llms.ConvertChatMessageToModel(msg)
	}
	return models
}

func ToChatMessages(messages []llms.ChatMessageModel) []llms.ChatMessage {
	chatMessages := make([]llms.ChatMessage, len(messages))
	for i, msg := range messages {
		chatMessages[i] = msg.ToChatMessage()
	}
	return chatMessages
}
