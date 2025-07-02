package anthropic_test

import (
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        []anthropic.Option
		wantErr     bool
		errContains string
	}{
		{
			name:        "missing token",
			opts:        []anthropic.Option{anthropic.WithModel("claude-3-5-sonnet-20241022")},
			wantErr:     true,
			errContains: "missing API key",
		},
		{
			name:        "missing model",
			opts:        []anthropic.Option{anthropic.WithToken("fake-token")},
			wantErr:     true,
			errContains: "model is required",
		},
		{
			name: "valid configuration",
			opts: []anthropic.Option{
				anthropic.WithToken("fake-token"),
				anthropic.WithModel("claude-3-5-sonnet-20241022"),
			},
			wantErr: false,
		},
		{
			name: "with custom base URL",
			opts: []anthropic.Option{
				anthropic.WithToken("fake-token"),
				anthropic.WithModel("claude-3-5-sonnet-20241022"),
				anthropic.WithBaseURL("https://custom.anthropic.com"),
			},
			wantErr: false,
		},
		{
			name: "with custom HTTP client",
			opts: []anthropic.Option{
				anthropic.WithToken("fake-token"),
				anthropic.WithModel("claude-3-5-sonnet-20241022"),
				anthropic.WithHTTPClient(&http.Client{}),
			},
			wantErr: false,
		},
		{
			name: "with beta header",
			opts: []anthropic.Option{
				anthropic.WithToken("fake-token"),
				anthropic.WithModel("claude-3-5-sonnet-20241022"),
				anthropic.WithAnthropicBetaHeader("beta-feature-1"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// For missing token test, temporarily unset environment variable
			if tt.name == "missing token" {
				originalToken := os.Getenv("ANTHROPIC_API_KEY")
				os.Unsetenv("ANTHROPIC_API_KEY")
				defer func() {
					if originalToken != "" {
						os.Setenv("ANTHROPIC_API_KEY", originalToken)
					}
				}()
			}

			allm, err := anthropic.New(tt.opts...)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, allm)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, allm)
				assert.NotNil(t, allm.Client)
				assert.NotNil(t, allm.Options)
			}
		})
	}
}

func TestNewWithEnvironmentVariable(t *testing.T) {
	t.Parallel()

	// Set environment variable for this test
	originalToken := os.Getenv("ANTHROPIC_API_KEY")
	defer func() {
		if originalToken != "" {
			os.Setenv("ANTHROPIC_API_KEY", originalToken)
		} else {
			os.Unsetenv("ANTHROPIC_API_KEY")
		}
	}()

	os.Setenv("ANTHROPIC_API_KEY", "env-token")

	llm, err := anthropic.New(anthropic.WithModel("claude-3-5-sonnet-20241022"))
	require.NoError(t, err)
	assert.NotNil(t, llm)
	assert.Equal(t, "env-token", llm.Options.Token)
}

func TestGetProviderType(t *testing.T) {
	t.Parallel()

	llm, err := anthropic.New(
		anthropic.WithToken("fake-token"),
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	)
	require.NoError(t, err)

	providerType := llm.GetProviderType()
	assert.Equal(t, llms.ProviderAnthropic, providerType)
}

func TestProcessMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		messages     []llms.MessageContent
		wantMessages int
		wantSystem   string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "empty messages",
			messages:     []llms.MessageContent{},
			wantMessages: 0,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "system message only",
			messages: []llms.MessageContent{
				{
					Role:  llms.ChatMessageTypeSystem,
					Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
				},
			},
			wantMessages: 0,
			wantSystem:   "You are a helpful assistant.",
			wantErr:      false,
		},
		{
			name: "multiple system messages",
			messages: []llms.MessageContent{
				{
					Role:  llms.ChatMessageTypeSystem,
					Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
				},
				{
					Role:  llms.ChatMessageTypeSystem,
					Parts: []llms.ContentPart{llms.TextPart("Always be polite and respectful.")},
				},
			},
			wantMessages: 0,
			wantSystem:   "You are a helpful assistant.\nAlways be polite and respectful.",
			wantErr:      false,
		},
		{
			name: "human message with text",
			messages: []llms.MessageContent{
				{
					Role:  llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{llms.TextPart("Hello, how are you?")},
				},
			},
			wantMessages: 1,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "human message with image",
			messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{
						llms.TextPart("What's in this image?"),
						llms.BinaryPart("image/jpeg", []byte("fake-image-data")),
					},
				},
			},
			wantMessages: 1,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "AI message with tool call",
			messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeAI,
					Parts: []llms.ContentPart{
						llms.ToolCall{
							ID: "call_123",
							FunctionCall: &llms.FunctionCall{
								Name:      "get_weather",
								Arguments: `{"location": "Boston"}`,
							},
						},
					},
				},
			},
			wantMessages: 1,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "tool message",
			messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeTool,
					Parts: []llms.ContentPart{
						llms.ToolCallResponse{
							ToolCallID: "call_123",
							Content:    "The weather in Boston is sunny, 22°C",
						},
					},
				},
			},
			wantMessages: 1,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "generic message",
			messages: []llms.MessageContent{
				{
					Role:  llms.ChatMessageTypeGeneric,
					Parts: []llms.ContentPart{llms.TextPart("Generic message")},
				},
			},
			wantMessages: 1,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "human message with unsupported binary content",
			messages: []llms.MessageContent{
				{
					Role: llms.ChatMessageTypeHuman,
					Parts: []llms.ContentPart{
						llms.BinaryPart("application/pdf", []byte("fake-pdf-data")),
					},
				},
			},
			wantErr:     true,
			errContains: "unsupported binary content type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			messages, system, err := anthropic.ProcessMessages(tt.messages)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.Len(t, messages, tt.wantMessages)
				assert.Equal(t, tt.wantSystem, system)
			}
		})
	}
}

func TestToolsToTools(t *testing.T) {
	t.Parallel()

	// Create schemas using the schema package
	type WeatherParams struct {
		Location string `json:"location" description:"The city name"`
	}
	weatherSchema, err := schema.New(reflect.TypeOf(WeatherParams{}))
	require.NoError(t, err)

	type CalcParams struct {
		Expression string `json:"expression" description:"Mathematical expression"`
	}
	calcSchema, err := schema.New(reflect.TypeOf(CalcParams{}))
	require.NoError(t, err)

	tests := []struct {
		name      string
		tools     []llms.Tool
		wantTools int
	}{
		{
			name:      "empty tools",
			tools:     []llms.Tool{},
			wantTools: 0,
		},
		{
			name:      "nil tools",
			tools:     nil,
			wantTools: 0,
		},
		{
			name: "single tool",
			tools: []llms.Tool{
				{
					Function: &llms.FunctionDefinition{
						Name:        "get_weather",
						Description: "Get current weather",
						Parameters:  weatherSchema.Parameters,
					},
				},
			},
			wantTools: 1,
		},
		{
			name: "multiple tools",
			tools: []llms.Tool{
				{
					Function: &llms.FunctionDefinition{
						Name:        "get_weather",
						Description: "Get current weather",
						Parameters:  weatherSchema.Parameters,
					},
				},
				{
					Function: &llms.FunctionDefinition{
						Name:        "calculate",
						Description: "Perform calculation",
						Parameters:  calcSchema.Parameters,
					},
				},
			},
			wantTools: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := anthropic.ToTools(tt.tools)
			if tt.wantTools == 0 {
				assert.Nil(t, result)
			} else {
				require.Len(t, result, tt.wantTools)

				// Verify first tool structure if any
				if len(result) > 0 {
					tool := result[0]
					assert.NotNil(t, tool.OfTool)
					assert.Equal(t, tt.tools[0].Function.Name, tool.OfTool.Name)
					assert.NotNil(t, tool.OfTool.Description)
					assert.Equal(t, "object", string(tool.OfTool.InputSchema.Type))
				}
			}
		})
	}
}

func TestHandleSystemMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         llms.MessageContent
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid text content",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
			},
			want:    "You are a helpful assistant.",
			wantErr: false,
		},
		{
			name: "invalid content type",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{llms.BinaryPart("image/jpeg", []byte("data"))},
			},
			wantErr:     true,
			errContains: "invalid content type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := anthropic.HandleSystemMessage(tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}
		})
	}
}

func TestHandleHumanMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         llms.MessageContent
		wantErr     bool
		errContains string
	}{
		{
			name: "text only",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{llms.TextPart("Hello!")},
			},
			wantErr: false,
		},
		{
			name: "text and image",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{
					llms.TextPart("What's in this image?"),
					llms.BinaryPart("image/jpeg", []byte("fake-image-data")),
				},
			},
			wantErr: false,
		},
		{
			name: "unsupported binary type",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{
					llms.BinaryPart("application/pdf", []byte("pdf-data")),
				},
			},
			wantErr:     true,
			errContains: "unsupported binary content type",
		},
		{
			name: "empty parts",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{},
			},
			wantErr:     true,
			errContains: "no valid content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := anthropic.HandleHumanMessage(tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestHandleAIMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         llms.MessageContent
		wantErr     bool
		errContains string
	}{
		{
			name: "text content",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{llms.TextPart("I'm doing well, thank you!")},
			},
			wantErr: false,
		},
		{
			name: "tool call",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID: "call_123",
						FunctionCall: &llms.FunctionCall{
							Name:      "get_weather",
							Arguments: `{"location": "Boston"}`,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid JSON in tool call",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID: "call_123",
						FunctionCall: &llms.FunctionCall{
							Name:      "get_weather",
							Arguments: `{invalid-json}`,
						},
					},
				},
			},
			wantErr:     true,
			errContains: "failed to unmarshal tool call arguments",
		},
		{
			name: "empty parts",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{},
			},
			wantErr:     true,
			errContains: "no valid content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := anthropic.HandleAIMessage(tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestHandleToolMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         llms.MessageContent
		wantErr     bool
		errContains string
	}{
		{
			name: "valid tool response",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: "call_123",
						Content:    "The weather is sunny, 22°C",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid content type",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{llms.TextPart("Not a tool response")},
			},
			wantErr:     true,
			errContains: "invalid content type",
		},
		{
			name: "empty parts",
			msg: llms.MessageContent{
				Parts: []llms.ContentPart{},
			},
			wantErr:     true,
			errContains: "no valid content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := anthropic.HandleToolMessage(tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// newTestClient creates a test client for integration tests
func newTestClient(t *testing.T, opts ...anthropic.Option) llms.Model {
	t.Helper()
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey == "" || apiKey == "fakekey" {
		t.Skip("ANTHROPIC_API_KEY not set")
		return nil
	}

	defaultOpts := []anthropic.Option{
		anthropic.WithModel("claude-3-5-sonnet-20241022"),
	}
	defaultOpts = append(defaultOpts, opts...)

	llm, err := anthropic.New(defaultOpts...)
	require.NoError(t, err)
	return llm
}

// Benchmark tests
func BenchmarkProcessMessages(b *testing.B) {
	messages := []llms.MessageContent{
		{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
		},
		{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextPart("Hello, how are you?")},
		},
		{
			Role:  llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{llms.TextPart("I'm doing well, thank you!")},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := anthropic.ProcessMessages(messages)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToolsToTools(b *testing.B) {
	type WeatherParams struct {
		Location string `json:"location"`
	}
	weatherSchema, err := schema.New(reflect.TypeOf(WeatherParams{}))
	if err != nil {
		b.Fatal(err)
	}

	tools := []llms.Tool{
		{
			Function: &llms.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get current weather",
				Parameters:  weatherSchema.Parameters,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := anthropic.ToTools(tools)
		if len(result) != 1 {
			b.Fatal("unexpected result length")
		}
	}
}
