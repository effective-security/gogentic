package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/effective-security/gogentic/chatmodel"
	"github.com/tmc/langchaingo/llms"
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
			Title:     fmt.Sprintf("Chat %s", chatID),
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

func PopulateMemoryStore(ctx context.Context, store MessageStore) (MessageStore, error) {
	s := NewMemoryStore()
	if store != nil {
		for _, msg := range store.Messages(ctx) {
			err := s.Add(ctx, msg)
			if err != nil {
				return nil, err
			}
		}
	}
	return s, nil
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
func (m *inMemory) UpdateChat(ctx context.Context, title string, metadata map[string]any) (*ChatInfo, error) {
	tenantID, chatID, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, err
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
			Title:     fmt.Sprintf("Chat %s", chatID),
			Metadata:  make(map[string]any),
		}
		t.chats[chatID] = chat
	}
	updated := false
	if title != "" {
		chat.Title = title
		updated = true
	}
	if metadata != nil {
		for k, v := range metadata {
			chat.Metadata[k] = v
		}
		updated = true
	}
	if updated {
		chat.UpdatedAt = time.Now()
	}

	return chat, nil
}

func (m *inMemory) ListChats(ctx context.Context) ([]string, error) {
	tenantID, _, err := chatmodel.GetTenantAndChatID(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

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

	m.mu.Lock()
	defer m.mu.Unlock()

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
