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
	// GetUserID retrieves the user ID from the context.
	// User ID is used to store message history for the user.
	GetUserID() string
	// GetChatID retrieves the chat ID from the context.
	// Chat ID is used to store message history for the chat.
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
	// This is also used in metrics and message history storage.
	// Used by some providers to identify the organization for multi-tenant use cases.
	GetOrgID() string
	// SetOrgID updates the org ID in the context for multi-tenant use cases.
	SetOrgID(id string)
}

type chatContext struct {
	orgID    string
	userID   string
	chatID   string
	runID    string
	metadata sync.Map
	appData  any
}

func (c *chatContext) GetOrgID() string {
	return c.orgID
}

func (c *chatContext) GetUserID() string {
	return c.userID
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

// NewChatContext constructs a new ChatContext. If chatID is empty, a new one is
// generated. The returned context also gets a fresh RunID. AppData is stored as
// immutable payload and can be retrieved via AppData().
func NewChatContext(userID, chatID string, appData any) ChatContext {
	if userID == "" {
		panic("userID is required")
	}
	if chatID == "" {
		chatID = NewChatID()
	}
	return &chatContext{
		userID:   userID,
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

// SetChatID updates the chat ID on the ChatContext already carried by ctx, so
// an inbound request can continue an existing conversation. Returns
// ErrInvalidChatContext when ctx carries no chat context.
//
// Note that this mutates the shared ChatContext rather than deriving a new one.
func SetChatID(ctx context.Context, chatID string) (context.Context, error) {
	if v, ok := ctx.Value(keyChatContext).(ChatContext); ok {
		v.SetChatID(chatID)
		return ctx, nil
	}
	return nil, errors.WithStack(ErrInvalidChatContext)
}

// NewChatID generates a new chat ID using the flake ID generator.
func NewChatID() string {
	return strconv.FormatUint(flake.DefaultIDGenerator.NextID(), 10)
}
