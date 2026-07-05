package store

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/xlog"
)

var logger = xlog.NewPackageLogger("github.com/effective-security/gogentic", "store")

type ChatInfo struct {
	UserID    string         `json:"user_id"`
	ChatID    string         `json:"chat_id"`
	Title     string         `json:"title"`
	Messages  []llms.Message `json:"messages"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Metadata  map[string]any `json:"metadata"`
	Tags      []string       `json:"tags"`
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
	UpdateChat(ctx context.Context, title string, metadata map[string]any, tags []string) (*ChatInfo, error)
	// ListChatIDs returns a list of chat IDs for a tenant and chat ID from context.
	ListChatIDs(ctx context.Context) ([]string, error)
	// GetChatInfo returns the chat information for a tenant and chat ID from context.
	GetChatInfo(ctx context.Context, id string, withMessages bool) (*ChatInfo, error)
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

func (c *ChatInfo) Clone() *ChatInfo {
	clone := &ChatInfo{
		UserID:    c.UserID,
		ChatID:    c.ChatID,
		Title:     c.Title,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}

	if c.Metadata != nil {
		clone.Metadata = make(map[string]any)
		for k, v := range c.Metadata {
			clone.Metadata[k] = v
		}
	}
	if len(c.Tags) > 0 {
		clone.Tags = append([]string{}, c.Tags...)
	}
	return clone
}

func GetTenantAndChatID(ctx context.Context) (string, string, error) {
	chatCtx := chatmodel.GetChatContext(ctx)
	if chatCtx == nil {
		return "", "", errors.WithStack(chatmodel.ErrInvalidChatContext)
	}
	chatID := chatCtx.GetChatID()
	orgID := chatCtx.GetOrgID()
	userID := chatCtx.GetUserID()
	if orgID != "" {
		userID = orgID + ":" + userID
	}
	return userID, chatID, nil
}
