package store

import (
	"context"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/x/slices"
	"github.com/effective-security/x/values"
)

type tenant struct {
	mu    sync.RWMutex
	id    string
	chats map[string]*ChatInfo
}

func (t *tenant) messages(chatID string) []llms.Message {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if chat, ok := t.chats[chatID]; ok {
		return chat.Messages
	}
	return nil
}

func (t *tenant) add(chatID string, msgs ...llms.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UTC()
	chat, ok := t.chats[chatID]
	if !ok {
		chat = &ChatInfo{
			TenantID:  t.id,
			ChatID:    chatID,
			Title:     "New Chat",
			CreatedAt: now,
			UpdatedAt: now,
		}
		t.chats[chatID] = chat
	}
	chat.UpdatedAt = now
	chat.Messages = append(chat.Messages, msgs...)
}

func (t *tenant) reset(chatID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.chats, chatID)
}

type inMemory struct {
	mu      sync.RWMutex
	tenants map[string]*tenant
}

func NewMemoryStore() MessageStore {
	return &inMemory{
		tenants: make(map[string]*tenant),
	}
}

func (m *inMemory) Messages(ctx context.Context) []llms.Message {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if tenant, ok := m.tenants[tenantID]; ok {
		return tenant.messages(chatID)
	}

	return nil
}

func (m *inMemory) Add(ctx context.Context, msgs ...llms.Message) error {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tenants[tenantID]
	if !ok {
		t = &tenant{
			id:    tenantID,
			chats: make(map[string]*ChatInfo),
		}
		m.tenants[tenantID] = t
	}
	t.add(chatID, msgs...)

	return nil
}

func (m *inMemory) Reset(ctx context.Context) error {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tenants[tenantID]
	if ok {
		t.reset(chatID)
	}
	return nil
}

// UpdateChat creates or updates a chat with the title, and metadata for a tenant and chat ID from context.
// If title is empty, it will not be updated.
// If metadata is nil, it will not be updated, otherwise merged with the existing metadata.
// If tags are empty, it will not be updated, otherwise merged with the existing tags.
func (m *inMemory) UpdateChat(ctx context.Context, title string, metadata map[string]any, tags []string) (*ChatInfo, error) {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tenants[tenantID]
	if !ok {
		t = &tenant{
			id:    tenantID,
			chats: make(map[string]*ChatInfo),
		}
		m.tenants[tenantID] = t
	}

	now := time.Now().UTC()
	chat, ok := t.chats[chatID]
	if !ok {
		chat = &ChatInfo{
			TenantID:  tenantID,
			ChatID:    chatID,
			CreatedAt: now,
			Title:     values.StringsCoalesce(title, "New Chat"),
			Tags:      tags,
		}
		t.chats[chatID] = chat
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
	if len(tags) > 0 {
		chat.Tags = slices.UniqueStrings(append(chat.Tags, tags...))
	}

	chat.UpdatedAt = now

	return chat.Clone(), nil
}

func (m *inMemory) ListChatIDs(ctx context.Context) ([]string, error) {
	tenantID, _, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, nil
	}
	var chatIDs []string
	for chatID := range t.chats {
		chatIDs = append(chatIDs, chatID)
	}
	return chatIDs, nil
}

func (m *inMemory) GetChatInfo(ctx context.Context, id string, withMessages bool) (*ChatInfo, error) {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, err
	}
	if id == "" {
		id = chatID
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tenants[tenantID]
	if !ok {
		return nil, nil
	}
	chat, ok := t.chats[id]
	if !ok {
		return nil, errors.New("chat not found")
	}

	res := chat.Clone()
	if withMessages {
		res.Messages = chat.Messages
	}
	return res, nil
}

func NewMemoryStoreManager(store MessageStore) MessageStoreManager {
	if mgr, ok := store.(MessageStoreManager); ok {
		return mgr
	}
	return nil
}

func (m *inMemory) ListTenants(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tenants []string
	for tenantID := range m.tenants {
		tenants = append(tenants, tenantID)
	}
	return tenants, nil
}

func (m *inMemory) Cleanup(ctx context.Context, tenantID string, olderThan time.Duration) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tenants[tenantID]
	if !ok {
		return 0, nil
	}

	deleted := uint32(0)
	cutoff := time.Now().UTC().Add(-olderThan)
	for chatID, chat := range t.chats {
		if chat.UpdatedAt.Before(cutoff) {
			delete(t.chats, chatID)
			deleted++
		}
	}
	return deleted, nil
}
