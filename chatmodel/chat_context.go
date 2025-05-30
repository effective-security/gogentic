package chatmodel

import (
	"context"
	"strconv"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/xdb/pkg/flake"
)

var (
	ErrInvalidChatContext = errors.New("invalid chat context")
)

// ChatContext is the context for the chat agent,
type ChatContext interface {
	// GetTenantID retrieves the tenant ID from the context
	GetTenantID() string
	// GetChatID retrieves the chat ID from the context
	GetChatID() string
	// SetChatID updates the chat ID in the context
	SetChatID(id string)
	// AppData returns immutable app data
	AppData() any
	// GetMetadata retrieves metadata by key
	GetMetadata(key string) (value any, ok bool)
	// SetMetadata sets metadata by key
	SetMetadata(key string, value any)
	// RunID returns the run ID for the chat
	RunID() string
}

type chatContext struct {
	tenantID string
	chatID   string
	runID    string
	metadata sync.Map
	appData  any
}

func (c *chatContext) GetTenantID() string {
	return c.tenantID
}

func (c *chatContext) GetChatID() string {
	return c.chatID
}

func (c *chatContext) RunID() string {
	return c.runID
}

// SetChatID updates the chat ID in the context
func (c *chatContext) SetChatID(id string) {
	c.chatID = id
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
		runID:    NewChatID(),
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

// NewFromContext returns new Background context with ChatContext from incoming context.
// This is useful for passing the chat context to the background context of a service.
func NewFromContext(ctx context.Context) context.Context {
	chatCtx := GetChatContext(ctx)
	if chatCtx == nil {
		return context.Background()
	}
	return WithChatContext(context.Background(), chatCtx)
}

func SetChatID(ctx context.Context, chatID string) (context.Context, error) {
	if v, ok := ctx.Value(keyContext).(ChatContext); ok {
		v.SetChatID(chatID)
		return ctx, nil
	}
	return nil, errors.WithStack(ErrInvalidChatContext)
}

// GetTenantAndChatID retrieves the tenant and chat ID from the provided context.
// If the context does not contain a ChatContext, it returns error.
func GetTenantAndChatID(ctx context.Context) (string, string, error) {
	if v, ok := ctx.Value(keyContext).(ChatContext); ok {
		return v.GetTenantID(), v.GetChatID(), nil
	}
	return "", "", errors.WithStack(ErrInvalidChatContext)
}

// NewChatID generates a new chat ID using the flake ID generator.
func NewChatID() string {
	return strconv.FormatUint(flake.DefaultIDGenerator.NextID(), 10)
}
