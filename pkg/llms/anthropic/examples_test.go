package anthropic_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"reflect"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/schema"
)

// Example_basicUsage demonstrates basic text generation
func Example_basicUsage() {
	// Initialize the client
	llm, err := anthropic.New(
		anthropic.WithToken("your-api-key"), // or set ANTHROPIC_API_KEY env var
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create a simple message
	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Hello, how are you?")},
		},
	}

	// Generate content
	resp, err := llm.GenerateContent(context.Background(), messages)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Choices[0].Content)
}

// Example_conversationWithSystem demonstrates system messages and multi-turn conversation
func Example_conversationWithSystem() {
	llm, err := anthropic.New(
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	)
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextPart("You are a helpful math tutor. Always explain your reasoning step by step.")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("What is 25 * 4?")},
		},
		{
			Role:  llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{llms.TextPart("Let me solve this step by step:\n25 × 4 = 100\n\nThis is because 25 × 4 is the same as 25 × 4 = (20 + 5) × 4 = 20 × 4 + 5 × 4 = 80 + 20 = 100.")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Now what about 25 * 8?")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), messages)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Choices[0].Content)
}

// Example_multimodalWithImage demonstrates image analysis
func Example_multimodalWithImage() {
	llm, err := anthropic.New(
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Read an image file (you would replace this with actual image data)
	imageData, err := os.ReadFile("example.jpg")
	if err != nil {
		// For this example, we'll use a placeholder
		imageData = []byte("placeholder-image-data")
	}

	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextPart("What do you see in this image? Please describe it in detail."),
				llms.BinaryPart("image/jpeg", imageData),
			},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), messages,
		llms.WithMaxTokens(500),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Choices[0].Content)
}

// Example_streamingResponse demonstrates real-time streaming
func Example_streamingResponse() {
	llm, err := anthropic.New(
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	)
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Write a short poem about programming")},
		},
	}

	fmt.Print("Streaming response: ")
	resp, err := llm.GenerateContent(context.Background(), messages,
		llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			fmt.Print(string(chunk)) // Print each chunk as it arrives
			return nil
		}),
		llms.WithMaxTokens(200),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n\nFinal response: %s\n", resp.Choices[0].Content)
}

// Example_functionCalling demonstrates tool/function calling
func Example_functionCalling() {
	llm, err := anthropic.New(
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Define function parameters
	type WeatherParams struct {
		Location string `json:"location" description:"The city and state, e.g. San Francisco, CA"`
		Unit     string `json:"unit" description:"Temperature unit" enum:"celsius,fahrenheit"`
	}

	// Create schema for function parameters
	schema, err := schema.New(reflect.TypeOf(WeatherParams{}))
	if err != nil {
		log.Fatal(err)
	}

	// Define available functions
	functions := []llms.FunctionDefinition{
		{
			Name:        "get_weather",
			Description: "Get current weather information for a location",
			Parameters:  schema.Parameters,
		},
	}

	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("What's the weather like in Tokyo, Japan?")},
		},
	}

	// Make the request with functions
	resp, err := llm.GenerateContent(context.Background(), messages,
		llms.WithFunctions(functions),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Check if the model wants to call a function
	if len(resp.Choices[0].ToolCalls) > 0 {
		toolCall := resp.Choices[0].ToolCalls[0]
		fmt.Printf("Function call requested:\n")
		fmt.Printf("  Function: %s\n", toolCall.FunctionCall.Name)
		fmt.Printf("  Arguments: %s\n", toolCall.FunctionCall.Arguments)

		// Simulate function execution (you would implement the actual function here)
		functionResult := "Temperature: 22°C, Condition: Partly cloudy, Humidity: 65%"

		// Continue the conversation with the function result
		messages = append(messages, llms.MessageContent{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					ID:           toolCall.ID,
					FunctionCall: toolCall.FunctionCall,
				},
			},
		})

		messages = append(messages, llms.MessageContent{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: toolCall.ID,
					Content:    functionResult,
				},
			},
		})

		// Get the final response
		finalResp, err := llm.GenerateContent(context.Background(), messages)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Final response: %s\n", finalResp.Choices[0].Content)
	} else {
		fmt.Printf("Direct response: %s\n", resp.Choices[0].Content)
	}
}

// Example_advancedConfiguration demonstrates various configuration options
func Example_advancedConfiguration() {
	llm, err := anthropic.New(
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
		anthropic.WithBaseURL("https://api.anthropic.com"),
		// anthropic.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
		// anthropic.WithAnthropicBetaHeader("beta-feature-1"),
	)
	if err != nil {
		log.Fatal(err)
	}

	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextPart("You are a creative writer.")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Write a very short story about a robot learning to paint.")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), messages,
		llms.WithTemperature(0.8),           // Higher creativity
		llms.WithMaxTokens(300),             // Limit response length
		llms.WithTopP(0.9),                  // Nucleus sampling
		llms.WithStopWords([]string{"END"}), // Stop if this sequence appears
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Generated story:\n%s\n", resp.Choices[0].Content)

	// Access generation metadata
	info := resp.Choices[0].GenerationInfo
	if inputTokens, ok := info["InputTokens"]; ok {
		fmt.Printf("Input tokens used: %v\n", inputTokens)
	}
	if outputTokens, ok := info["OutputTokens"]; ok {
		fmt.Printf("Output tokens used: %v\n", outputTokens)
	}
}

// Example_errorHandling demonstrates proper error handling
func Example_errorHandling() {
	// Example with invalid configuration to show error handling
	llm, err := anthropic.New(
		anthropic.WithToken("invalid-token"),
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	)
	if err != nil {
		fmt.Printf("Configuration error: %v\n", err)
		return
	}

	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Hello")},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), messages)
	if err != nil {
		// Handle different types of errors
		switch {
		case contains(err.Error(), "authentication_error"):
			fmt.Println("Authentication failed: check your API key")
		case contains(err.Error(), "rate_limit_error"):
			fmt.Println("Rate limit exceeded: please wait and retry")
		case contains(err.Error(), "invalid_request_error"):
			fmt.Println("Invalid request: check your parameters")
		default:
			fmt.Printf("Other error: %v\n", err)
		}
		return
	}

	fmt.Printf("Success: %s\n", resp.Choices[0].Content)
}

// Example_differentModels demonstrates using different Claude models
func Example_differentModels() {
	models := []string{
		"claude-3-5-haiku-20241022",  // Fast and cost-effective
		"claude-3-5-sonnet-20241022", // Balanced performance
		"claude-3-opus-20240229",     // Most capable for complex tasks
	}

	message := "Explain quantum computing in one sentence."

	for _, model := range models {
		llm, err := anthropic.New(
			anthropic.WithModel(model),
		)
		if err != nil {
			log.Printf("Failed to initialize %s: %v", model, err)
			continue
		}

		messages := []llms.MessageContent{
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextPart(message)},
			},
		}

		resp, err := llm.GenerateContent(context.Background(), messages,
			llms.WithMaxTokens(100),
		)
		if err != nil {
			log.Printf("Error with %s: %v", model, err)
			continue
		}

		fmt.Printf("%s: %s\n\n", model, resp.Choices[0].Content)
	}
}

// Helper function for error handling example
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}
