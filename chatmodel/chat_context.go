package chatmodel

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/effective-security/xdb/pkg/flake"
)

// ChatContext is the context for the chat agent,
type ChatContext interface {
	// GetTenantID retrieves the tenant ID from the context
	GetTenantID() string
	// GetChatID retrieves the chat ID from the context
	GetChatID() string
	// AppData returns immutable app data
	AppData() any
	// GetMetadata retrieves metadata by key
	GetMetadata(key string) (value any, ok bool)
	// SetMetadata sets metadata by key
	SetMetadata(key string, value any)
}

type chatContext struct {
	tenantID string
	chatID   string
	metadata sync.Map
	appData  any
}

func (c *chatContext) GetTenantID() string {
	return c.tenantID
}

func (c *chatContext) GetChatID() string {
	return c.chatID
}

func (c *chatContext) AppData() any {
	return c.appData
}
func (c *chatContext) GetMetadata(key string) (value any, ok bool) {
	return c.metadata.Load(key)
}

func (c *chatContext) SetMetadata(key string, value any) {
	c.metadata.Store(key, value)
}

func NewChatContext(tenantID, chatID string, appData any) ChatContext {
	if tenantID == "" {
		tenantID = NewChatID()
	}
	if chatID == "" {
		chatID = NewChatID()
	}
	return &chatContext{
		tenantID: tenantID,
		chatID:   chatID,
		appData:  appData,
		metadata: sync.Map{},
	}
}

type contextKey int

const (
	keyContext contextKey = iota
)

// WithChatContext returns a new context with ChatContext value
func WithChatContext(ctx context.Context, chatCtx ChatContext) context.Context {
	return context.WithValue(ctx, keyContext, chatCtx)
}

// GetChatContext retrieves the ChatContext from the context
func GetChatContext(ctx context.Context) ChatContext {
	if v, ok := ctx.Value(keyContext).(ChatContext); ok {
		return v
	}
	return nil
}

// GetTenantAndChatID retrieves the tenant and chat ID from the provided context.
// If the context does not contain a ChatContext, it returns error.
func GetTenantAndChatID(ctx context.Context) (string, string, error) {
	if v, ok := ctx.Value(keyContext).(ChatContext); ok {
		return v.GetTenantID(), v.GetChatID(), nil
	}
	return "", "", errors.New("invalid chat context")
}

// NewChatID generates a new chat ID using the flake ID generator.
func NewChatID() string {
	return strconv.FormatUint(flake.DefaultIDGenerator.NextID(), 10)
}
