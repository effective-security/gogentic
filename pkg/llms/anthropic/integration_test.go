package anthropic_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests that require a real API key
// These tests are skipped when ANTHROPIC_API_KEY is not set

func checkAnthropicAPIKeyOrSkip(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
}

func TestIntegrationTextGeneration(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Say 'Hello, World!' in exactly those words.")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	assert.Contains(t, choice.Content, "Hello, World!")
	assert.NotEmpty(t, choice.GenerationInfo)

	// Verify token usage information
	info := choice.GenerationInfo
	assert.Contains(t, info, "InputTokens")
	assert.Contains(t, info, "OutputTokens")
	assert.Greater(t, info["InputTokens"], int64(0))
	assert.Greater(t, info["OutputTokens"], int64(0))
}

func TestIntegrationChatSequence(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextPart("You are a helpful math tutor.")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("What is 2 + 2?")},
		},
		{
			Role:  llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{llms.TextPart("2 + 2 equals 4.")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("What about 3 + 3?")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	assert.Contains(t, strings.ToLower(choice.Content), "6")
}

func TestIntegrationStreaming(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Count from 1 to 5, each number on a new line.")},
		},
	}

	var streamedContent strings.Builder
	resp, err := llm.GenerateContent(context.Background(), content,
		llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			streamedContent.Write(chunk)
			return nil
		}))

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]

	// Verify both streamed and final content contain the numbers
	finalContent := choice.Content
	streamed := streamedContent.String()

	for i := 1; i <= 5; i++ {
		numStr := string(rune('0' + i))
		assert.Contains(t, finalContent, numStr, "Final content should contain number %d", i)
		assert.Contains(t, streamed, numStr, "Streamed content should contain number %d", i)
	}

	// Verify streaming worked (streamed content should not be empty)
	assert.NotEmpty(t, streamed)
}

func TestIntegrationStreamingError(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)

	llm := newTestClient(t)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Say hello")},
		},
	}

	// Streaming function that returns an error
	_, err := llm.GenerateContent(context.Background(), content,
		llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			return assert.AnError
		}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "streaming function error")
}

func TestIntegrationToolCalling(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	// Define weather function
	type WeatherParams struct {
		Location string `json:"location" description:"The city and state, e.g. San Francisco, CA"`
		Unit     string `json:"unit" description:"Temperature unit" enum:"celsius,fahrenheit"`
	}

	sc, err := schema.New(reflect.TypeOf(WeatherParams{}))
	require.NoError(t, err)

	tools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "get_current_weather",
				Description: "Get the current weather in a given location",
				Parameters:  sc.Parameters,
			},
		},
	}

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("What's the weather like in Boston?")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content, llms.WithTools(tools))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	require.NotEmpty(t, choice.ToolCalls)

	toolCall := choice.ToolCalls[0]
	assert.Equal(t, "get_current_weather", toolCall.FunctionCall.Name)
	assert.NotEmpty(t, toolCall.ID)
	assert.Contains(t, strings.ToLower(toolCall.FunctionCall.Arguments), "boston")
}

func TestIntegrationToolCallAndResponse(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	// Define a simple calculation function
	type CalcParams struct {
		Expression string `json:"expression" description:"Mathematical expression to evaluate"`
	}

	sc, err := schema.New(reflect.TypeOf(CalcParams{}))
	require.NoError(t, err)

	tools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "calculate",
				Description: "Perform mathematical calculations",
				Parameters:  sc.Parameters,
			},
		},
	}

	// First request: ask for calculation
	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Calculate 15 * 23")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content, llms.WithTools(tools))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Choices)
	require.NotEmpty(t, resp.Choices[0].ToolCalls)

	toolCall := resp.Choices[0].ToolCalls[0]

	// Add the tool call to conversation and provide result
	content = append(content, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:           toolCall.ID,
				FunctionCall: toolCall.FunctionCall,
			},
		},
	})

	content = append(content, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: toolCall.ID,
				Content:    "345",
			},
		},
	})

	content = append(content, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart("What was the result?")},
	})

	// Second request: get final answer
	resp2, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)
	assert.NotEmpty(t, resp2.Choices)

	finalChoice := resp2.Choices[0]
	assert.Contains(t, finalChoice.Content, "345")
}

func TestIntegrationMultimodalImage(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t, anthropic.WithModel("claude-3-5-sonnet-20241022"))

	// Create a simple red pixel as a test image (1x1 PNG)
	redPixelPNG := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, 0x00, 0x00, 0x00,
		0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0x99, 0x01, 0x01, 0x00, 0x00, 0x00,
		0xFF, 0xFF, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0xE2, 0x21, 0xBC, 0x33,
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}

	content := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextPart("What do you see in this image? Be very brief."),
				llms.BinaryPart("image/png", redPixelPNG),
			},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content, llms.WithMaxTokens(50))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	// Should mention it's a small/tiny image or single pixel
	content_lower := strings.ToLower(choice.Content)
	assert.True(t,
		strings.Contains(content_lower, "pixel") ||
			strings.Contains(content_lower, "small") ||
			strings.Contains(content_lower, "tiny") ||
			strings.Contains(content_lower, "1x1"),
		"Response should mention the image is small/tiny/pixel: %s", choice.Content)
}

func TestIntegrationErrorHandling(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)

	// Test with invalid model
	llm, err := anthropic.New(
		anthropic.WithToken(os.Getenv("ANTHROPIC_API_KEY")),
		anthropic.WithModel("invalid-model-name"),
	)
	require.NoError(t, err) // Client creation should succeed

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Hello")},
		},
	}

	_, err = llm.GenerateContent(context.Background(), content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anthropic:")
}

func TestIntegrationModelParameters(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Generate a creative story starter in exactly 10 words.")},
		},
	}

	// Test with different temperature settings
	resp1, err := llm.GenerateContent(context.Background(), content,
		llms.WithTemperature(0.1), // Low creativity
		llms.WithMaxTokens(50),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, resp1.Choices)

	resp2, err := llm.GenerateContent(context.Background(), content,
		llms.WithTemperature(0.9), // High creativity
		llms.WithMaxTokens(50),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, resp2.Choices)

	// Both should be valid responses
	assert.NotEmpty(t, resp1.Choices[0].Content)
	assert.NotEmpty(t, resp2.Choices[0].Content)
}

func TestIntegrationStopSequences(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Count from 1 to 10: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content,
		llms.WithStopWords([]string{"5"}),
		llms.WithMaxTokens(100),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	// Should stop before or at "5"
	assert.NotContains(t, choice.Content, "6")
	assert.NotContains(t, choice.Content, "7")
}

func TestIntegrationMaxTokens(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Write a very long story about a dragon.")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content,
		llms.WithMaxTokens(10), // Very limited tokens
	)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	// Response should be quite short due to token limit
	assert.True(t, len(choice.Content) < 200, "Response should be short due to token limit: %s", choice.Content)

	// Check generation info
	info := choice.GenerationInfo
	outputTokens := info["OutputTokens"].(int64)
	assert.LessOrEqual(t, outputTokens, int64(15)) // Should be close to or at the limit
}

// Benchmark integration tests
func BenchmarkIntegrationSimpleGeneration(b *testing.B) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" || apiKey == "fakekey" {
		b.Skip("ANTHROPIC_API_KEY not set")
	}

	llm, err := anthropic.New(anthropic.WithModel("claude-3-5-sonnet-20241022"))
	if err != nil {
		b.Fatal(err)
	}

	content := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Say hello")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := llm.GenerateContent(context.Background(), content, llms.WithMaxTokens(10))
		if err != nil {
			b.Fatal(err)
		}
	}
}
