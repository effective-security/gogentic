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

// ChatContext is the context for the LLM flow.
//
//	ChatID is the ID of the chat which is persisted across runs.
//	RunID identifies a single run of the LLM flow, usually it's a random ID.
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
	// GetRunID returns the run ID for the chat
	GetRunID() string
	// SetRunID updates the run ID in the context
	SetRunID(id string)
	// GetOrgID retrieves the org ID from the context.
	// This is also used in metrics.
	// Used by some providers to identify the organization of the tenant,
	// for example, "main" for the default organization.
	GetOrgID() string
	// SetOrgID updates the org ID in the context
	SetOrgID(id string)
}

type chatContext struct {
	orgID    string
	tenantID string
	chatID   string
	runID    string
	metadata sync.Map
	appData  any
}

func (c *chatContext) GetOrgID() string {
	return c.orgID
}

func (c *chatContext) GetTenantID() string {
	return c.tenantID
}

func (c *chatContext) GetChatID() string {
	return c.chatID
}

func (c *chatContext) GetRunID() string {
	return c.runID
}

func (c *chatContext) SetRunID(id string) {
	c.runID = id
}

// SetChatID updates the chat ID in the context
func (c *chatContext) SetChatID(id string) {
	c.chatID = id
}

func (c *chatContext) SetOrgID(id string) {
	c.orgID = id
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
		orgID:    "main",
		tenantID: tenantID,
		chatID:   chatID,
		runID:    NewChatID(),
		appData:  appData,
		metadata: sync.Map{},
	}
}

type contextKey int

const (
	keyChatContext contextKey = iota
	keyActionID
)

// WithChatContext returns a new context with ChatContext value
func WithChatContext(ctx context.Context, chatCtx ChatContext) context.Context {
	return context.WithValue(ctx, keyChatContext, chatCtx)
}

// WithActionID returns a new context with Action ID value.
// This is used to identify the action in the multi-step LLM flow.
func WithActionID(ctx context.Context, actionID string) context.Context {
	return context.WithValue(ctx, keyActionID, actionID)
}

// GetChatContext retrieves the ChatContext from the context
func GetChatContext(ctx context.Context) ChatContext {
	if v, ok := ctx.Value(keyChatContext).(ChatContext); ok {
		return v
	}
	return nil
}

// GetActionID retrieves the Action ID from the context
func GetActionID(ctx context.Context) string {
	if v, ok := ctx.Value(keyActionID).(string); ok {
		return v
	}
	return ""
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
	if v, ok := ctx.Value(keyChatContext).(ChatContext); ok {
		v.SetChatID(chatID)
		return ctx, nil
	}
	return nil, errors.WithStack(ErrInvalidChatContext)
}

// GetTenantAndChatID retrieves the tenant and chat ID from the provided context.
// If the context does not contain a ChatContext, it returns error.
func GetTenantAndChatID(ctx context.Context) (string, string, error) {
	if v, ok := ctx.Value(keyChatContext).(ChatContext); ok {
		return v.GetTenantID(), v.GetChatID(), nil
	}
	return "", "", errors.WithStack(ErrInvalidChatContext)
}

// GetOrgID retrieves the org ID from the provided context.
// If the context does not contain a ChatContext, it returns "main".
func GetOrgID(ctx context.Context) string {
	if v, ok := ctx.Value(keyChatContext).(ChatContext); ok {
		return v.GetOrgID()
	}
	return "main"
}

// NewChatID generates a new chat ID using the flake ID generator.
func NewChatID() string {
	return strconv.FormatUint(flake.DefaultIDGenerator.NextID(), 10)
}
