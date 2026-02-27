# Anthropic LLM Integration

This package provides integration with Anthropic's Claude models using the official [Anthropic SDK for Go](https://github.com/anthropics/anthropic-sdk-go).

## Features

- **Official SDK Integration**: Uses the official Anthropic SDK v1.4.0
- **Complete API Support**: Text generation, multimodal inputs, function calling, streaming
- **Comprehensive Testing**: Unit tests, integration tests, and benchmarks
- **Type Safety**: Full Go type safety with proper error handling
- **Configurable**: Flexible configuration options with sensible defaults

## Installation

```bash
go get github.com/effective-security/gogentic/pkg/llms/anthropic
```

## Quick Start

### Basic Setup

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/effective-security/gogentic/pkg/llms"
    "github.com/effective-security/gogentic/pkg/llms/anthropic"
)

func main() {
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
```

### Environment Variable Setup

Set your API key as an environment variable:

```bash
export ANTHROPIC_API_KEY="your-api-key-here"
```

Then you can omit the token from the configuration:

```go
llm, err := anthropic.New(
    anthropic.WithModel("claude-3-5-sonnet-20241022"),
)
```

## Configuration Options

### Available Options

```go
// API Configuration
anthropic.WithToken("your-api-key")                    // API key
anthropic.WithModel("claude-3-5-sonnet-20241022")      // Model selection
anthropic.WithBaseURL("https://api.anthropic.com")     // Custom base URL

// HTTP Configuration  
anthropic.WithHTTPClient(&http.Client{})               // Custom HTTP client
anthropic.WithAnthropicBetaHeader("beta-feature-1")    // Beta features
```

### Supported Models

- `claude-3-5-sonnet-20241022` (Latest and most capable)
- `claude-3-5-haiku-20241022` (Fast and cost-effective)
- `claude-3-opus-20240229` (Most powerful for complex tasks)
- `claude-3-sonnet-20240229` (Balanced performance)
- `claude-3-haiku-20240307` (Fastest response times)

Structured JSON outputs (see [Structured JSON Outputs](#structured-json-outputs)) are supported on: `claude-opus-4-6`, `claude-sonnet-4-5`, `claude-opus-4-5`, `claude-haiku-4-5`.

## Advanced Usage

### System Messages and Conversation

```go
messages := []llms.MessageContent{
    {
        Role:  llms.ChatMessageTypeSystem,
        Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
    },
    {
        Role:  llms.ChatMessageTypeHuman,
        Parts: []llms.ContentPart{llms.TextPart("What's the weather like?")},
    },
    {
        Role:  llms.ChatMessageTypeAI,
        Parts: []llms.ContentPart{llms.TextPart("I'd be happy to help! However, I don't have access to real-time weather data.")},
    },
    {
        Role:  llms.ChatMessageTypeHuman,
        Parts: []llms.ContentPart{llms.TextPart("Can you tell me a joke instead?")},
    },
}

resp, err := llm.GenerateContent(ctx, messages)
```

### Multimodal (Image) Support

```go
// Read an image file
imageData, err := os.ReadFile("image.jpg")
if err != nil {
    log.Fatal(err)
}

messages := []llms.MessageContent{
    {
        Role: llms.ChatMessageTypeHuman,
        Parts: []llms.ContentPart{
            llms.TextPart("What do you see in this image?"),
            llms.BinaryPart("image/jpeg", imageData),
        },
    },
}

resp, err := llm.GenerateContent(ctx, messages)
```

### Streaming Responses

```go
messages := []llms.MessageContent{
    {
        Role:  llms.ChatMessageTypeHuman,
        Parts: []llms.ContentPart{llms.TextPart("Write a short story")},
    },
}

resp, err := llm.GenerateContent(ctx, messages,
    llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
        fmt.Print(string(chunk)) // Print each chunk as it arrives
        return nil
    }),
)
```

### Function/Tool Calling

```go
package main

import (
    "context"
    "reflect"

    "github.com/effective-security/gogentic/pkg/llms"
    "github.com/effective-security/gogentic/pkg/llms/anthropic"
    "github.com/effective-security/gogentic/pkg/schema"
)

// Define function parameters
type WeatherParams struct {
    Location string `json:"location" description:"The city and state, e.g. San Francisco, CA"`
    Unit     string `json:"unit" description:"Temperature unit" enum:"celsius,fahrenheit"`
}

func main() {
    llm, err := anthropic.New(
        anthropic.WithModel("claude-3-5-sonnet-20241022"),
    )
    if err != nil {
        log.Fatal(err)
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
            Description: "Get current weather information",
            Parameters:  schema.Parameters,
        },
    }

    messages := []llms.MessageContent{
        {
            Role:  llms.ChatMessageTypeHuman,
            Parts: []llms.ContentPart{llms.TextPart("What's the weather in Tokyo?")},
        },
    }

    // Make the request with functions
    resp, err := llm.GenerateContent(ctx, messages, llms.WithFunctions(functions))
    if err != nil {
        log.Fatal(err)
    }

    // Check if the model wants to call a function
    if len(resp.Choices[0].ToolCalls) > 0 {
        toolCall := resp.Choices[0].ToolCalls[0]
        fmt.Printf("Function call: %s\n", toolCall.FunctionCall.Name)
        fmt.Printf("Arguments: %s\n", toolCall.FunctionCall.Arguments)
        
        // You would implement the actual function call here
        // Then send the result back to continue the conversation
    }
}
```

### Structured JSON Outputs

Claude can generate JSON responses that strictly follow a specified schema using [structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs). Use `llms.WithResponseFormat()` with a schema from `schema.NewResponseFormat()`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "reflect"

    "github.com/effective-security/gogentic/pkg/llms"
    "github.com/effective-security/gogentic/pkg/llms/anthropic"
    "github.com/effective-security/gogentic/pkg/schema"
)

// Define your expected output structure
type InvoiceData struct {
    InvoiceNumber string  `json:"invoice_number" description:"The invoice number"`
    Date          string  `json:"date" description:"Invoice date"`
    TotalAmount   float64 `json:"total_amount" description:"Total amount"`
    CustomerName  string  `json:"customer_name" description:"Customer name"`
}

func main() {
    llm, err := anthropic.New(
        anthropic.WithModel("claude-sonnet-4-5-20241022"), // or claude-opus-4-6 for structured outputs
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create schema from struct
    responseFormat, err := schema.NewResponseFormat(reflect.TypeOf(InvoiceData{}), true)
    if err != nil {
        log.Fatal(err)
    }

    messages := []llms.Message{
        llms.MessageFromTextParts(llms.RoleHuman,
            "Extract invoice data: Invoice #INV-2024-001 dated Jan 15, 2024 for $1,234.56 to John Doe"),
    }

    resp, err := llm.GenerateContent(context.Background(), messages,
        llms.WithResponseFormat(responseFormat),
        llms.WithMaxTokens(1024),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Response is guaranteed to be valid JSON matching the schema
    fmt.Println(resp.Choices[0].Content)
}
```

**Features:**

- Guaranteed valid JSON output
- Type-safe schema validation
- No parsing errors
- Consistent field types
- All required fields present

**Supported models for structured outputs:** `claude-opus-4-6`, `claude-sonnet-4-5`, `claude-opus-4-5`, `claude-haiku-4-5`

### Configuration Parameters

```go
resp, err := llm.GenerateContent(ctx, messages,
    llms.WithTemperature(0.7),        // Creativity (0.0-1.0)
    llms.WithMaxTokens(1000),         // Maximum response length
    llms.WithTopP(0.9),               // Nucleus sampling
    llms.WithStopWords([]string{"END"}), // Stop sequences
)
```

## Error Handling

The package provides comprehensive error handling:

```go
resp, err := llm.GenerateContent(ctx, messages)
if err != nil {
    // Errors are wrapped with context information
    if strings.Contains(err.Error(), "authentication_error") {
        log.Fatal("Invalid API key")
    }
    if strings.Contains(err.Error(), "rate_limit_error") {
        log.Fatal("Rate limit exceeded")
    }
    log.Fatal("Other error:", err)
}
```

## Testing

### Unit Tests

Run the unit tests:

```bash
go test ./pkg/llms/anthropic/ -v -short
```

### Integration Tests

For integration tests with real API calls, set your API key and run:

```bash
export ANTHROPIC_API_KEY="your-api-key"
go test ./pkg/llms/anthropic/ -v
```

### Benchmarks

Run performance benchmarks:

```bash
go test ./pkg/llms/anthropic/ -bench=. -short
```

## Migration from Custom Client

This package replaces the previous custom Anthropic client implementation with the official SDK. Key changes:

1. **Official SDK**: Now uses `github.com/anthropics/anthropic-sdk-go`
2. **Better Type Safety**: Improved type definitions and error handling
3. **Complete API Coverage**: Full support for all Anthropic API features
4. **Performance**: Better streaming and connection handling
5. **Maintenance**: Official support and updates from Anthropic

### Migration Guide

If migrating from the old custom client:

1. Update imports to use the new package
2. Update function calls to use the new `anthropic.New()` constructor
3. API remains largely the same for basic usage
4. Tool calling syntax updated to use new schema format

## Best Practices

### 1. API Key Security

- Never hardcode API keys in source code
- Use environment variables or secure configuration management
- Rotate keys regularly

### 2. Error Handling

- Always check for errors from API calls
- Implement appropriate retry logic for transient failures
- Log errors with sufficient context for debugging

### 3. Rate Limiting

- Implement backoff strategies for rate limit errors
- Monitor your usage to stay within limits
- Consider using multiple API keys for high-volume applications

### 4. Cost Management

- Use appropriate models for your use case (Haiku for simple tasks, Sonnet/Opus for complex ones)
- Set reasonable max_tokens limits
- Monitor token usage and costs

### 5. Content Safety

- Implement content filtering for user inputs
- Review Anthropic's usage policies
- Consider implementing safety measures for generated content

## Performance Considerations

### Model Selection

| Model | Speed | Cost | Capability | Best For |
|-------|-------|------|------------|----------|
| Claude 3.5 Haiku | Fastest | Lowest | Good | Simple tasks, high volume |
| Claude 3.5 Sonnet | Balanced | Medium | Excellent | General purpose |
| Claude 3 Opus | Slower | Highest | Best | Complex reasoning, analysis |

### Streaming vs Non-Streaming

- Use streaming for real-time user interfaces
- Use non-streaming for batch processing
- Streaming reduces perceived latency

### Connection Management

- Reuse the client instance across requests
- Configure appropriate timeouts for your use case
- Consider connection pooling for high-volume applications

## Contributing

1. Ensure all tests pass: `go test ./...`
2. Run linting: `make lint`
3. Add tests for new functionality
4. Update documentation for API changes

## License

This package is part of the gogentic project. See the main project license for details.