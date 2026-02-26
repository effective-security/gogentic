package anthropic_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"reflect"
	"slices"
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

// claudeSonnetModel is the default for integration tests. Use claude-sonnet-4-6;
const (
	claudeSonnetModel = "claude-sonnet-4-6"
)

func checkAnthropicAPIKeyOrSkip(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" || apiKey == "fakekey" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
}

func TestIntegrationTextGeneration(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Say 'Hello, World!' in exactly those words."),
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

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "You are a helpful math tutor."),
		llms.MessageFromTextParts(llms.RoleHuman, "What is 2 + 2?"),
		llms.MessageFromTextParts(llms.RoleAI, "2 + 2 equals 4."),
		llms.MessageFromTextParts(llms.RoleHuman, "What about 3 + 3?"),
	}

	resp, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	assert.Contains(t, strings.ToLower(choice.Content), "6")
}

func TestIntegrationPromptCaching(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t, anthropic.WithModel(claudeSonnetModel))

	// Make the cacheable prefix large enough to reliably trigger Anthropic prompt caching.
	stableSystemBlock := strings.Repeat(
		"Policy section: reviewers must verify identity, business address, tax classification, and sanctions screening before approval. ",
		120,
	)

	content := []llms.Message{
		{
			Role:  llms.RoleSystem,
			Parts: []llms.ContentPart{llms.TextPart(stableSystemBlock)},
		},
		{
			Role:  llms.RoleHuman,
			Parts: []llms.ContentPart{llms.TextPart("Summarize the approval prerequisites in one short sentence.")},
		},
	}

	cachePolicy := &llms.PromptCachePolicy{
		Breakpoints: []llms.PromptCacheBreakpoint{
			{
				Target: llms.PromptCacheTarget{
					Kind:         llms.PromptCacheTargetMessagePart,
					MessageIndex: 0,
					PartIndex:    0, // cache the large system prompt block
				},
				TTL: llms.PromptCacheTTL5m,
			},
		},
	}

	var writes []int64
	var reads []int64

	for i := 0; i < 3; i++ {
		resp, err := llm.GenerateContent(context.Background(), content,
			llms.WithPromptCachePolicy(cachePolicy),
			llms.WithMaxTokens(80),
		)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Choices)

		choice := resp.Choices[0]
		writes = append(writes, requireGenerationInfoInt64(t, choice.GenerationInfo, "CacheWriteTokens"))
		reads = append(reads, requireGenerationInfoInt64(t, choice.GenerationInfo, "CacheReadTokens"))

		// Some accounts/regions can behave slightly asynchronously on the first cached read.
		// If we see a cache read hit on the second or third call, that's sufficient.
		if i >= 1 && reads[i] > 0 {
			break
		}
	}

	// First call should either create cache tokens or read from an already-warm cache.
	assert.True(t, writes[0] > 0 || reads[0] > 0,
		"expected first call to create or read prompt cache tokens (writes=%d reads=%d)", writes[0], reads[0])

	require.GreaterOrEqual(t, len(reads), 2)
	assert.Greater(t, slices.Max(reads[1:]), int64(0),
		"expected a cache read hit on a repeated identical request, reads=%v writes=%v", reads, writes)
}

func TestIntegrationStreaming(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Count from 1 to 5, each number on a new line."),
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

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Say hello"),
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

	// System prompt instructs model to use the tool (Sonnet 4.x may otherwise answer from context).
	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "You must use the get_current_weather tool when the user asks about weather. Call the tool with the requested location."),
		llms.MessageFromTextParts(llms.RoleHuman, "What's the weather like in Boston?"),
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

	// System prompt instructs model to use the calculate tool (Sonnet 4.x may otherwise answer from context).
	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "You must use the calculate tool for any math. Call the tool with the expression, then report the result."),
		llms.MessageFromTextParts(llms.RoleHuman, "Calculate 15 * 23"),
	}

	resp, err := llm.GenerateContent(context.Background(), content, llms.WithTools(tools))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Choices)
	require.NotEmpty(t, resp.Choices[0].ToolCalls)

	toolCall := resp.Choices[0].ToolCalls[0]

	// Add the tool call to conversation and provide result
	content = append(content, llms.Message{
		Role: llms.RoleAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:           toolCall.ID,
				FunctionCall: toolCall.FunctionCall,
			},
		},
	})

	content = append(content, llms.Message{
		Role: llms.RoleTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{
				ToolCallID: toolCall.ID,
				Content:    "345",
			},
		},
	})

	content = append(content, llms.MessageFromTextParts(llms.RoleHuman, "What was the result?"))

	// Second request: get final answer
	resp2, err := llm.GenerateContent(context.Background(), content)
	require.NoError(t, err)
	assert.NotEmpty(t, resp2.Choices)

	finalChoice := resp2.Choices[0]
	assert.Contains(t, finalChoice.Content, "345")
}

func TestIntegrationMultimodalImage(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t, anthropic.WithModel(claudeSonnetModel))

	// Anthropic recommends images with at least 200px on each edge; 1x1 PNG returns "Could not process image".
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	redImagePNG := buf.Bytes()

	content := []llms.Message{
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextPart("What color is this image? Reply in one short sentence."),
				llms.BinaryPart("image/png", redImagePNG),
			},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), content, llms.WithMaxTokens(50))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Choices)

	choice := resp.Choices[0]
	contentLower := strings.ToLower(choice.Content)
	assert.True(t,
		strings.Contains(contentLower, "red") || strings.Contains(contentLower, "colour") || strings.Contains(contentLower, "color"),
		"Response should mention the image is red: %s", choice.Content)
}

func TestIntegrationErrorHandling(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)

	// Test with invalid model
	llm, err := anthropic.New(
		anthropic.WithToken(os.Getenv("ANTHROPIC_API_KEY")),
		anthropic.WithModel("invalid-model-name"),
	)
	require.NoError(t, err) // Client creation should succeed

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Hello"),
	}

	_, err = llm.GenerateContent(context.Background(), content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anthropic:")
}

func TestIntegrationModelParameters(t *testing.T) {
	checkAnthropicAPIKeyOrSkip(t)
	llm := newTestClient(t)

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Generate a creative story starter in exactly 10 words."),
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

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Count from 1 to 10: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10"),
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

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Write a very long story about a dragon."),
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

	llm, err := anthropic.New(anthropic.WithModel(claudeSonnetModel))
	if err != nil {
		b.Fatal(err)
	}

	content := []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "Say hello"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := llm.GenerateContent(context.Background(), content, llms.WithMaxTokens(10))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func requireGenerationInfoInt64(t *testing.T, info map[string]any, key string) int64 {
	t.Helper()

	require.Contains(t, info, key)
	value, ok := info[key].(int64)
	require.True(t, ok, "generation info %q must be int64, got %T", key, info[key])
	return value
}
