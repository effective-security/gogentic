package chatmodel

import (
	"context"
	"strconv"
	"sync"

	"github.com/effective-security/x/values"
	"github.com/effective-security/xdb/pkg/flake"
)

// ChatContext is the context for the chat agent,
// It contains the user ID, org ID, cloud ID, and batch ID
type ChatContext interface {
	GetChatID() string
	// AppData returns immutable app data
	AppData() any
	// GetMetadata retrieves metadata by key
	GetMetadata(key string) (value any, ok bool)
	// SetMetadata sets metadata by key
	SetMetadata(key string, value any)
}

type chatContext struct {
	chatID   string
	metadata sync.Map
	appData  any
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

func NewChatContext(chatID string, appData any) ChatContext {
	return &chatContext{
		chatID:   values.StringsCoalesce(chatID, NewChatID()),
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

func NewChatID() string {
	return strconv.FormatUint(flake.DefaultIDGenerator.NextID(), 10)
}
