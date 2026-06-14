package store

import (
	"context"
	"time"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/xlog"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "store")

type ChatInfo struct {
	TenantID  string
	ChatID    string
	Title     string
	Messages  []llms.Message
	CreatedAt time.Time
	UpdatedAt time.Time
	Metadata  map[string]any
	Tags      []string
}

// MessageStore is an interface for storing and retrieving chat messages.
// The supplied context must have ChatContext with tenantID and chatID,
// created by NewChatContext.
type MessageStore interface {
	// Messages returns the messages for a tenant and chat ID from context.
	Messages(ctx context.Context) []llms.Message
	// Add adds one or more messages to the chat history for a tenant and chat ID from context.
	// Multiple messages are added atomically for better performance and consistency.
	Add(ctx context.Context, msgs ...llms.Message) error
	// Reset resets the chat history for a tenant and chat ID from context.
	Reset(ctx context.Context) error

	// UpdateChat creates or updates a chat with the title, and metadata for a tenant and chat ID from context.
	// If title is empty, it will not be updated.
	// If metadata is nil, it will not be updated, otherwise merged with the existing metadata.
	// If tags are empty, it will not be updated, otherwise merged with the existing tags.
	UpdateChat(ctx context.Context, title string, metadata map[string]any, tags []string) error
	// ListChats returns a list of chat IDs for a tenant and chat ID from context.
	// If tags are provided, it returns a list of chat IDs that have all the tags.
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
		messages := store.Messages(ctx)
		if len(messages) > 0 {
			err := s.Add(ctx, messages...)
			if err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}
