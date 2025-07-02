package bedrockclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetProvider(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		expected string
	}{
		{
			name:     "Direct Anthropic model ID",
			modelID:  "anthropic.claude-3-sonnet-20240229-v1:0",
			expected: "anthropic",
		},
		{
			name:     "Inference Profile with US region",
			modelID:  "us.anthropic.claude-3-5-sonnet-20241022-v2:0",
			expected: "anthropic",
		},
		{
			name:     "Inference Profile with EU region",
			modelID:  "eu.anthropic.claude-3-haiku-20240307-v1:0",
			expected: "anthropic",
		},
		{
			name:     "Direct Amazon model ID",
			modelID:  "amazon.titan-text-premier-v1:0",
			expected: "amazon",
		},
		{
			name:     "Inference Profile with Amazon",
			modelID:  "us.amazon.nova-micro-v1:0",
			expected: "amazon",
		},
		{
			name:     "Direct Meta model ID",
			modelID:  "meta.llama3-2-1b-instruct-v1:0",
			expected: "meta",
		},
		{
			name:     "Inference Profile with Meta",
			modelID:  "us.meta.llama3-2-11b-instruct-v1:0",
			expected: "meta",
		},
		{
			name:     "Single part model ID",
			modelID:  "anthropic",
			expected: "anthropic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getProvider(tt.modelID)
			assert.Equal(t, tt.expected, result)
		})
	}
}
