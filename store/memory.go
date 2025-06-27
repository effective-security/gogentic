package store

import (
	"context"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/chatmodel"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/x/values"
)

type tenant struct {
	mu    sync.RWMutex
	id    string
	chats map[string]*ChatInfo
}

func (t *tenant) messages(chatID string) []llms.ChatMessage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if chat, ok := t.chats[chatID]; ok {
		return chat.Messages
	}
	return nil
}

func (t *tenant) add(chatID string, msg llms.ChatMessage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	chat, ok := t.chats[chatID]
	if !ok {
		chat = &ChatInfo{
			TenantID:  t.id,
			ChatID:    chatID,
			Title:     "New Chat",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		t.chats[chatID] = chat
	}
	chat.UpdatedAt = time.Now()
	chat.Messages = append(chat.Messages, msg)
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

func (m *inMemory) Messages(ctx context.Context) []llms.ChatMessage {
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

func (m *inMemory) Add(ctx context.Context, msg llms.ChatMessage) error {
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
	t.add(chatID, msg)

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
func (m *inMemory) UpdateChat(ctx context.Context, title string, metadata map[string]any) error {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tenants[tenantID]
	if !ok {
		t = &tenant{
			chats: make(map[string]*ChatInfo),
		}
		m.tenants[tenantID] = t
	}

	chat, ok := t.chats[chatID]
	if !ok {
		chat = &ChatInfo{
			TenantID:  tenantID,
			ChatID:    chatID,
			CreatedAt: time.Now(),
			Title:     values.StringsCoalesce(title, "New Chat"),
			Metadata:  make(map[string]any),
		}
		t.chats[chatID] = chat
	}
	if title != "" {
		chat.Title = title
	}
	for k, v := range metadata {
		chat.Metadata[k] = v
	}

	chat.UpdatedAt = time.Now()
	return nil
}

func (m *inMemory) ListChats(ctx context.Context) ([]string, error) {
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

func (m *inMemory) GetChatInfo(ctx context.Context, id string) (*ChatInfo, error) {
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
	return chat, nil
}

// GetChatTitle returns the title for a tenant and chat ID from context.
// If the chat does not exist or not persisted, it returns an empty string.
func (m *inMemory) GetChatTitle(ctx context.Context, id string) (string, error) {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return "", err
	}
	if id == "" {
		id = chatID
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tenants[tenantID]
	if !ok {
		return "", nil
	}
	chat, ok := t.chats[id]
	if !ok {
		return "", nil
	}
	return chat.Title, nil
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
	cutoff := time.Now().Add(-olderThan)
	for chatID, chat := range t.chats {
		if chat.UpdatedAt.Before(cutoff) {
			delete(t.chats, chatID)
			deleted++
		}
	}
	return deleted, nil
}
