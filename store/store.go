package store

import (
	"github.com/tmc/langchaingo/llms"
)

type MessageStore interface {
	Messages(chatID string) []llms.ChatMessage
	Add(chatID string, msg llms.ChatMessage) error
	Reset(chatID string) error
}
