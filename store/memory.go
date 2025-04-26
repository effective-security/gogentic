package store

import (
	"sync"

	"github.com/tmc/langchaingo/llms"
)

type inMemory struct {
	mu      sync.RWMutex
	storage map[string][]llms.ChatMessage
}

func NewMemoryStore() MessageStore {
	return &inMemory{}
}

func (m *inMemory) Messages(threadID string) []llms.ChatMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.storage == nil {
		return nil
	}
	return m.storage[threadID]
}

func (m *inMemory) Add(threadID string, msg llms.ChatMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storage == nil {
		// create on first use
		m.storage = make(map[string][]llms.ChatMessage)
	}
	m.storage[threadID] = append(m.storage[threadID], msg)
	return nil
}

func (m *inMemory) Reset(threadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storage != nil {
		delete(m.storage, threadID)
	}
	return nil
}
