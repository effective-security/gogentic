package store

import (
	"context"
	"time"

	"github.com/tmc/langchaingo/llms"
)

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
	UpdateChat(ctx context.Context, title string, metadata map[string]any) (*ChatInfo, error)
	// ListChats returns a list of chat IDs for a tenant and chat ID from context.
	ListChats(ctx context.Context) ([]string, error)
	// GetChatInfo returns the chat information for a tenant and chat ID from context.
	GetChatInfo(ctx context.Context, id string) (*ChatInfo, error)
}
