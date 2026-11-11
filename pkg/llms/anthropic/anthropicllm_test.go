package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/invopop/jsonschema"
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
				_ = os.Unsetenv("ANTHROPIC_API_KEY")
				defer func() {
					if originalToken != "" {
						_ = os.Setenv("ANTHROPIC_API_KEY", originalToken)
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
			_ = os.Setenv("ANTHROPIC_API_KEY", originalToken)
		} else {
			_ = os.Unsetenv("ANTHROPIC_API_KEY")
		}
	}()

	_ = os.Setenv("ANTHROPIC_API_KEY", "env-token")

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
		messages     []llms.Message
		wantMessages int
		wantSystem   string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "empty messages",
			messages:     []llms.Message{},
			wantMessages: 0,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "system message only",
			messages: []llms.Message{
				{
					Role:  llms.RoleSystem,
					Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
				},
			},
			wantMessages: 0,
			wantSystem:   "You are a helpful assistant.",
			wantErr:      false,
		},
		{
			name: "multiple system messages",
			messages: []llms.Message{
				{
					Role:  llms.RoleSystem,
					Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
				},
				{
					Role:  llms.RoleSystem,
					Parts: []llms.ContentPart{llms.TextPart("Always be polite and respectful.")},
				},
			},
			wantMessages: 0,
			wantSystem:   "You are a helpful assistant.\nAlways be polite and respectful.",
			wantErr:      false,
		},
		{
			name: "human message with text",
			messages: []llms.Message{
				{
					Role:  llms.RoleHuman,
					Parts: []llms.ContentPart{llms.TextPart("Hello, how are you?")},
				},
			},
			wantMessages: 1,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "human message with image",
			messages: []llms.Message{
				{
					Role: llms.RoleHuman,
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
			messages: []llms.Message{
				{
					Role: llms.RoleAI,
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
			messages: []llms.Message{
				{
					Role: llms.RoleTool,
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
			messages: []llms.Message{
				{
					Role:  llms.RoleGeneric,
					Parts: []llms.ContentPart{llms.TextPart("Generic message")},
				},
			},
			wantMessages: 1,
			wantSystem:   "",
			wantErr:      false,
		},
		{
			name: "human message with unsupported binary content",
			messages: []llms.Message{
				{
					Role: llms.RoleHuman,
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
		model     string
		wantTools int
	}{
		{
			name:      "empty tools",
			tools:     []llms.Tool{},
			model:     "claude-sonnet-4-5",
			wantTools: 0,
		},
		{
			name:      "nil tools",
			tools:     nil,
			model:     "claude-sonnet-4-5",
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
			model:     "claude-sonnet-4-5",
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
			model:     "claude-sonnet-4-5",
			wantTools: 2,
		},
		{
			name: "web_search non-legacy model",
			tools: []llms.Tool{
				{
					Type: "web_search",
					WebSearchOptions: &llms.WebSearchOptions{
						AllowedDomains:  []string{"example.com"},
						ExcludedDomains: []string{"excluded.com"},
						MaxUses:         10,
					},
				},
			},
			model:     "claude-sonnet-4-6",
			wantTools: 1,
		},
		{
			name: "web_search legacy model",
			tools: []llms.Tool{
				{
					Type: "web_search",
					WebSearchOptions: &llms.WebSearchOptions{
						AllowedDomains:  []string{"example.com"},
						ExcludedDomains: []string{"excluded.com"},
						MaxUses:         10,
					},
				},
			},
			model:     "claude-sonnet-4-0",
			wantTools: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := anthropic.ToTools(tt.tools, tt.model)
			require.NoError(t, err)
			if tt.wantTools == 0 {
				assert.Nil(t, result)
			} else {
				require.Len(t, result, tt.wantTools)

				// Verify first tool structure if any
				if len(result) > 0 {
					tool := result[0]
					switch {
					case tt.name == "web_search non-legacy model":
						require.NotNil(t, tool.OfWebFetchTool20260209)
						assert.Equal(t, []string{"example.com"}, tool.OfWebFetchTool20260209.AllowedDomains)
						assert.Equal(t, []string{"excluded.com"}, tool.OfWebFetchTool20260209.BlockedDomains)
						assert.Equal(t, int64(10), tool.OfWebFetchTool20260209.MaxUses.Value)
					case tt.name == "web_search legacy model":
						require.NotNil(t, tool.OfWebFetchTool20250910)
						assert.Equal(t, []string{"example.com"}, tool.OfWebFetchTool20250910.AllowedDomains)
						assert.Equal(t, []string{"excluded.com"}, tool.OfWebFetchTool20250910.BlockedDomains)
						assert.Equal(t, int64(10), tool.OfWebFetchTool20250910.MaxUses.Value)
					default:
						require.NotNil(t, tool.OfTool)
						assert.Equal(t, tt.tools[0].Function.Name, tool.OfTool.Name)
						assert.NotNil(t, tool.OfTool.Description)
						assert.Equal(t, "object", string(tool.OfTool.InputSchema.Type))
					}
				}
			}
		})
	}
}

func TestToTools_FunctionDefinition(t *testing.T) {
	t.Parallel()

	t.Run("missing definition", func(t *testing.T) {
		t.Parallel()
		// F-022: a tool other than web_search without Function used to panic.
		for _, toolType := range []string{"", llms.ToolTypeFunction, "code_execution"} {
			result, err := anthropic.ToTools([]llms.Tool{
				{
					Type: llms.ToolTypeFunction,
					Function: &llms.FunctionDefinition{
						Name: "ok",
					},
				},
				{
					Type: toolType,
				},
			}, "claude-sonnet-4-5")
			require.ErrorIs(t, err, llms.ErrMissingFunctionDefinition)
			assert.EqualError(t, err, `anthropic: tool [1]: tool of type "`+toolType+`": tool has no function definition`)
			assert.Nil(t, result)
		}
	})

	t.Run("no parameters", func(t *testing.T) {
		t.Parallel()
		result, err := anthropic.ToTools([]llms.Tool{
			{
				Type: llms.ToolTypeFunction,
				Function: &llms.FunctionDefinition{
					Name:        "now",
					Description: "Current time",
				},
			},
		}, "claude-sonnet-4-5")
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.NotNil(t, result[0].OfTool)
		assert.Equal(t, "now", result[0].OfTool.Name)
		b, err := json.Marshal(result[0].OfTool.InputSchema)
		require.NoError(t, err)
		assert.JSONEq(t, `{"type":"object"}`, string(b))
	})
}

func TestHandleAIMessage_ToolCallWithoutFunctionCall(t *testing.T) {
	t.Parallel()

	// F-038: a provider-native tool call has no FunctionCall.
	_, err := anthropic.HandleAIMessage(llms.Message{
		Role: llms.RoleAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{
				ID:   "srv_1",
				Type: "web_search",
			},
		},
	})
	require.ErrorIs(t, err, llms.ErrMissingFunctionCall)
	assert.EqualError(t, err, `anthropic: unsupported tool call in history: tool call "srv_1" of type "web_search": tool call has no function call`)
}

func TestHandleSystemMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		msg         llms.Message
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid text content",
			msg: llms.Message{
				Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
			},
			want:    "You are a helpful assistant.",
			wantErr: false,
		},
		{
			name: "invalid content type",
			msg: llms.Message{
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
		msg         llms.Message
		wantErr     bool
		errContains string
	}{
		{
			name: "text only",
			msg: llms.Message{
				Parts: []llms.ContentPart{llms.TextPart("Hello!")},
			},
			wantErr: false,
		},
		{
			name: "text and image",
			msg: llms.Message{
				Parts: []llms.ContentPart{
					llms.TextPart("What's in this image?"),
					llms.BinaryPart("image/jpeg", []byte("fake-image-data")),
				},
			},
			wantErr: false,
		},
		{
			name: "unsupported binary type",
			msg: llms.Message{
				Parts: []llms.ContentPart{
					llms.BinaryPart("application/pdf", []byte("pdf-data")),
				},
			},
			wantErr:     true,
			errContains: "unsupported binary content type",
		},
		{
			name: "empty parts",
			msg: llms.Message{
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
		msg         llms.Message
		wantErr     bool
		errContains string
	}{
		{
			name: "text content",
			msg: llms.Message{
				Parts: []llms.ContentPart{llms.TextPart("I'm doing well, thank you!")},
			},
			wantErr: false,
		},
		{
			name: "tool call",
			msg: llms.Message{
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
			msg: llms.Message{
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
			msg: llms.Message{
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
		msg         llms.Message
		wantErr     bool
		errContains string
	}{
		{
			name: "valid tool response",
			msg: llms.Message{
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
			msg: llms.Message{
				Parts: []llms.ContentPart{llms.TextPart("Not a tool response")},
			},
			wantErr:     true,
			errContains: "invalid content type",
		},
		{
			name: "empty parts",
			msg: llms.Message{
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
		anthropic.WithModel(claudeSonnetModel),
	}
	defaultOpts = append(defaultOpts, opts...)

	llm, err := anthropic.New(defaultOpts...)
	require.NoError(t, err)
	return llm
}

// Benchmark tests
func BenchmarkProcessMessages(b *testing.B) {
	messages := []llms.Message{
		{
			Role:  llms.RoleSystem,
			Parts: []llms.ContentPart{llms.TextPart("You are a helpful assistant.")},
		},
		{
			Role:  llms.RoleHuman,
			Parts: []llms.ContentPart{llms.TextPart("Hello, how are you?")},
		},
		{
			Role:  llms.RoleAI,
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
		result, err := anthropic.ToTools(tools, "claude-sonnet-4-5")
		if err != nil || len(result) != 1 {
			b.Fatal("unexpected result length")
		}
	}
}

const (
	testToken      = "test-token"
	testModel      = "claude-sonnet-4-5"
	testBetaHeader = "beta-feature-1"
	testRegion     = "us-west-2"

	pathMessages      = "/v1/messages"
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
	contentTypeSSE    = "text/event-stream"

	weatherToolName = "get_weather"

	invalidRequestReply = `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is too large"}}`
	hiRequestBody       = `{"model":"claude-sonnet-4-5","max_tokens":8192,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
)

var errStop = errors.New("stop streaming")

// messagesRequest is what the fake Messages API received.
type messagesRequest struct {
	Method string
	Path   string
	APIKey string
	Beta   string
	Body   string
}

// messagesAPI is an httptest handler playing the Anthropic Messages API: it
// records every request and serves one canned reply.
type messagesAPI struct {
	status      int
	contentType string
	reply       string

	mu       sync.Mutex
	requests []messagesRequest
}

func (m *messagesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m.mu.Lock()
	m.requests = append(m.requests, messagesRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		APIKey: r.Header.Get("X-Api-Key"),
		Beta:   r.Header.Get("Anthropic-Beta"),
		Body:   string(body),
	})
	m.mu.Unlock()
	w.Header().Set(headerContentType, m.contentType)
	w.WriteHeader(m.status)
	_, _ = io.WriteString(w, m.reply)
}

// received returns a copy of the requests recorded so far.
func (m *messagesAPI) received() []messagesRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

// newMessagesAPI starts a fake Messages API and returns a model pointed at it.
func newMessagesAPI(t *testing.T, status int, contentType, reply string, opts ...anthropic.Option) (*messagesAPI, *anthropic.LLM) {
	t.Helper()
	api := &messagesAPI{
		status:      status,
		contentType: contentType,
		reply:       reply,
	}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	opts = append([]anthropic.Option{
		anthropic.WithToken(testToken),
		anthropic.WithModel(testModel),
		anthropic.WithBaseURL(srv.URL),
		anthropic.WithHTTPClient(srv.Client()),
	}, opts...)
	llm, err := anthropic.New(opts...)
	require.NoError(t, err)
	return api, llm
}

// sentBody asserts that exactly one Messages request was sent with the test
// token and returns its body.
func sentBody(t *testing.T, api *messagesAPI) string {
	t.Helper()
	requests := api.received()
	require.Len(t, requests, 1)
	assert.Equal(t, http.MethodPost, requests[0].Method)
	assert.Equal(t, pathMessages, requests[0].Path)
	assert.Equal(t, testToken, requests[0].APIKey)
	return requests[0].Body
}

// sseEvent formats one server-sent event of the streaming Messages API.
func sseEvent(name, data string) string {
	return "event: " + name + "\ndata: " + data + "\n\n"
}

var (
	streamStart = sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_s","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,"usage":{"input_tokens":11,"output_tokens":1}}}`)

	streamText = sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		sseEvent("ping", `{"type":"ping"}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`) +
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)

	streamToolUse = sseEvent("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Paris\"}"}}`) +
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":1}`)

	streamEnd = sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":42,"cache_creation_input_tokens":3,"cache_read_input_tokens":5}}`) +
		sseEvent("message_stop", `{"type":"message_stop"}`)
)

func hiMessages() []llms.Message {
	return []llms.Message{
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	}
}

func weatherTool() llms.Tool {
	props := jsonschema.NewProperties()
	props.Set("location", &jsonschema.Schema{
		Type:        "string",
		Description: "City name",
	})
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        weatherToolName,
			Description: "Get the current weather",
			Parameters: &jsonschema.Schema{
				Type:       "object",
				Properties: props,
				Required:   []string{"location"},
			},
		},
	}
}

func TestGetName(t *testing.T) {
	t.Parallel()

	llm, err := anthropic.New(
		anthropic.WithToken(testToken),
		anthropic.WithModel(testModel),
	)
	require.NoError(t, err)
	assert.Equal(t, testModel, llm.GetName())
}

func TestNewBedrock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        []anthropic.Option
		wantErr     string
		wantBaseURL string
	}{
		{
			name: "missing model",
			opts: []anthropic.Option{
				anthropic.WithAWSConfig(&aws.Config{
					Region: testRegion,
				}),
			},
			wantErr: "anthropic: model is required",
		},
		{
			name: "missing AWS config",
			opts: []anthropic.Option{
				anthropic.WithModel(testModel),
			},
			wantErr: "anthropic: AWS config is required",
		},
		{
			name: "missing region",
			opts: []anthropic.Option{
				anthropic.WithModel(testModel),
				anthropic.WithAWSConfig(&aws.Config{}),
			},
			wantErr: "anthropic: AWS region is required",
		},
		{
			name: "regional endpoint by default",
			opts: []anthropic.Option{
				anthropic.WithModel(testModel),
				anthropic.WithAWSConfig(&aws.Config{
					Region: testRegion,
				}),
			},
			wantBaseURL: "https://bedrock-runtime.us-west-2.amazonaws.com",
		},
		{
			name: "custom endpoint",
			opts: []anthropic.Option{
				anthropic.WithModel(testModel),
				anthropic.WithAWSConfig(&aws.Config{
					Region: testRegion,
				}),
				anthropic.WithBaseURL("https://bedrock.example.test"),
			},
			wantBaseURL: "https://bedrock.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			llm, err := anthropic.NewBedrock(tt.opts...)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, llm)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, llm.Client)
			assert.Equal(t, tt.wantBaseURL, llm.Options.BaseURL)
			assert.Equal(t, testModel, llm.GetName())
		})
	}
}

func TestProcessMessages_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []llms.Message
		wantErr  string
		wantIs   error
	}{
		{
			name: "system message with binary part",
			messages: []llms.Message{
				{
					Role: llms.RoleSystem,
					Parts: []llms.ContentPart{
						llms.BinaryPart("image/png", []byte("png")),
					},
				},
			},
			wantErr: "anthropic: failed to handle system message: anthropic: for system message: anthropic: invalid content type",
			wantIs:  anthropic.ErrInvalidContentType,
		},
		{
			name: "human message with tool call",
			messages: []llms.Message{
				{
					Role: llms.RoleHuman,
					Parts: []llms.ContentPart{
						llms.ToolCall{
							ID: "call_1",
						},
					},
				},
			},
			wantErr: "anthropic: failed to handle human message: anthropic: unsupported human message part type: llms.ToolCall",
		},
		{
			name: "AI message with binary part",
			messages: []llms.Message{
				{
					Role: llms.RoleAI,
					Parts: []llms.ContentPart{
						llms.BinaryPart("image/png", []byte("png")),
					},
				},
			},
			wantErr: "anthropic: failed to handle AI message: anthropic: unsupported AI message part type: llms.BinaryContent",
		},
		{
			name: "tool message with text part",
			messages: []llms.Message{
				llms.MessageFromTextParts(llms.RoleTool, "not a tool response"),
			},
			wantErr: "anthropic: failed to handle tool message: anthropic: for tool message part type: llms.TextContent: anthropic: invalid content type",
			wantIs:  anthropic.ErrInvalidContentType,
		},
		{
			name: "unsupported role",
			messages: []llms.Message{
				llms.MessageFromTextParts(llms.Role("narrator"), "once upon a time"),
			},
			wantErr: "anthropic: narrator: anthropic: unsupported message type",
			wantIs:  anthropic.ErrUnsupportedMessageType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			messages, system, err := anthropic.ProcessMessages(tt.messages)
			require.EqualError(t, err, tt.wantErr)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
			assert.Nil(t, messages)
			assert.Empty(t, system)
		})
	}
}

func TestProcessMessages_SkipsEmptyMessages(t *testing.T) {
	t.Parallel()

	messages, system, err := anthropic.ProcessMessages([]llms.Message{
		{
			Role: llms.RoleSystem,
		},
		{
			Role: llms.RoleHuman,
		},
		llms.MessageFromTextParts(llms.RoleHuman, "hi"),
	})
	require.NoError(t, err)
	assert.Empty(t, system)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 1)
	assert.Equal(t, "hi", messages[0].Content[0].OfText.Text)
}

func TestGenerateContent_Request(t *testing.T) {
	t.Parallel()

	const reply = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5",` +
		`"content":[` +
		`{"type":"text","text":"Let me check. "},` +
		`{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"location":"Paris"}},` +
		`{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"weather"}},` +
		`{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[]},` +
		`{"type":"web_fetch_tool_result","tool_use_id":"srvtoolu_2","content":{"type":"web_fetch_tool_result_error","error_code":"url_not_accessible"}},` +
		`{"type":"code_execution_tool_result","tool_use_id":"srvtoolu_3","content":{"type":"code_execution_tool_result_error","error_code":"unavailable"}},` +
		`{"type":"text","text":"Done."}` +
		`],"stop_reason":"tool_use",` +
		`"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":3,"cache_read_input_tokens":2}}`

	api, llm := newMessagesAPI(t, http.StatusOK, contentTypeJSON, reply, anthropic.WithAnthropicBetaHeader(testBetaHeader))
	messages := []llms.Message{
		llms.MessageFromTextParts(llms.RoleSystem, "Be brief."),
		llms.MessageFromTextParts(llms.RoleHuman, "Weather in Paris?"),
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				llms.TextPart("Checking."),
				llms.ToolCall{
					ID: "call_1",
					FunctionCall: &llms.FunctionCall{
						Name:      weatherToolName,
						Arguments: `{"location":"Paris"}`,
					},
				},
			},
		},
		{
			Role: llms.RoleTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: "call_1",
					Content:    "sunny",
				},
			},
		},
	}

	resp, err := llm.GenerateContent(context.Background(), messages,
		llms.WithMaxTokens(256),
		llms.WithTemperature(0.5),
		llms.WithTopP(0.9),
		llms.WithStopWords([]string{"END"}),
		llms.WithTools([]llms.Tool{weatherTool()}),
		llms.WithPromptCachePolicy(&llms.PromptCachePolicy{
			Breakpoints: []llms.PromptCacheBreakpoint{
				{
					Target: llms.PromptCacheTarget{
						Kind:         llms.PromptCacheTargetMessagePart,
						MessageIndex: 0,
						PartIndex:    0,
					},
					TTL: llms.PromptCacheTTL1h,
				},
			},
		}),
	)
	require.NoError(t, err)

	assert.Equal(t, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Let me check. Done.",
				ToolCalls: []llms.ToolCall{
					{
						ID:   "toolu_1",
						Type: llms.ToolTypeFunction,
						FunctionCall: &llms.FunctionCall{
							Name:      weatherToolName,
							Arguments: `{"location":"Paris"}`,
						},
					},
				},
				StopReason: "tool_use",
				Usage: llms.Usage{
					InputTokens:      10,
					OutputTokens:     5,
					CacheWriteTokens: 3,
					CacheReadTokens:  2,
					TotalTokens:      20,
				},
				GenerationInfo: map[string]any{
					"ID": "msg_1",
				},
			},
		},
	}, resp)

	requests := api.received()
	require.Len(t, requests, 1)
	// The 1h cache TTL adds its beta to the one the client is configured with.
	assert.Equal(t, testBetaHeader+",extended-cache-ttl-2025-04-11", requests[0].Beta)
	assert.JSONEq(t, `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 256,
		"temperature": 0.5,
		"top_p": 0.9,
		"stop_sequences": ["END"],
		"system": [
			{"type": "text", "text": "Be brief.", "cache_control": {"type": "ephemeral", "ttl": "1h"}}
		],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Weather in Paris?"}]},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Checking."},
				{"type": "tool_use", "id": "call_1", "name": "get_weather", "input": {"location": "Paris"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "call_1", "is_error": false, "content": [{"type": "text", "text": "sunny"}]}
			]}
		],
		"tools": [
			{
				"name": "get_weather",
				"description": "Get the current weather",
				"input_schema": {
					"type": "object",
					"properties": {"location": {"type": "string", "description": "City name"}},
					"required": ["location"]
				}
			}
		]
	}`, sentBody(t, api))
}

// F-007: thinking is on whenever reasoning effort enables it (and always on
// for models where it cannot be disabled), so thinking blocks and the results
// of server-side tools must not fail a non-streaming call.
func TestGenerateContent_ThinkingAndServerToolBlocks(t *testing.T) {
	t.Parallel()

	const reply = `{"id":"msg_1","type":"message","role":"assistant","content":[
		{"type":"thinking","thinking":"Paris is in France.","signature":"sig_1"},
		{"type":"redacted_thinking","data":"opaque"},
		{"type":"thinking","thinking":"","signature":"sig_2"},
		{"type":"bash_code_execution_tool_result","tool_use_id":"srvtoolu_1","content":{"type":"bash_code_execution_result","stdout":"","stderr":"","return_code":0,"content":[]}},
		{"type":"text_editor_code_execution_tool_result","tool_use_id":"srvtoolu_2","content":{"type":"text_editor_code_execution_view_result","content":"","file_type":"text","num_lines":0,"start_line":1,"total_lines":0}},
		{"type":"tool_search_tool_result","tool_use_id":"srvtoolu_3","content":{"type":"tool_search_tool_search_result","tool_references":[]}},
		{"type":"container_upload","file_id":"file_1"},
		{"type":"text","text":"Paris."}
	],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":6}}`
	_, llm := newMessagesAPI(t, http.StatusOK, contentTypeJSON, reply)

	resp, err := llm.GenerateContent(context.Background(), hiMessages())
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	choice := resp.Choices[0]
	assert.Equal(t, "Paris.", choice.Content)
	assert.Equal(t, "Paris is in France.", choice.ReasoningContent)
	assert.Empty(t, choice.ToolCalls)
	assert.Equal(t, "end_turn", choice.StopReason)
}

// Streamed thinking deltas land in ReasoningContent, as in the
// non-streaming path, and are not passed to the streaming function.
func TestGenerateContent_StreamingThinking(t *testing.T) {
	t.Parallel()

	streamThinking := sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Paris is "}}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"in France."}}`) +
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_1"}}`) +
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)
	_, llm := newMessagesAPI(t, http.StatusOK, contentTypeSSE, streamStart+streamThinking+streamText+streamEnd)
	var chunks []string

	resp, err := llm.GenerateContent(context.Background(), hiMessages(), llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
		chunks = append(chunks, string(chunk))
		return nil
	}))
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "Hello", resp.Choices[0].Content)
	assert.Equal(t, "Paris is in France.", resp.Choices[0].ReasoningContent)
	assert.Equal(t, []string{"Hel", "lo"}, chunks)
}

func TestGenerateContent_UnknownContentBlock(t *testing.T) {
	t.Parallel()

	const reply = `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"x"},{"type":"future_block"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	api, llm := newMessagesAPI(t, http.StatusOK, contentTypeJSON, reply)

	resp, err := llm.GenerateContent(context.Background(), hiMessages())
	require.EqualError(t, err, "response content type: <nil>: anthropic: unsupported content type")
	assert.ErrorIs(t, err, anthropic.ErrUnsupportedContentType)
	assert.Nil(t, resp)
	assert.JSONEq(t, hiRequestBody, sentBody(t, api))
}

func TestGenerateContent_APIError(t *testing.T) {
	t.Parallel()

	api, llm := newMessagesAPI(t, http.StatusBadRequest, contentTypeJSON, invalidRequestReply)

	resp, err := llm.GenerateContent(context.Background(), hiMessages())
	var apiErr *sdk.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "invalid_request_error", string(apiErr.Type()))
	assert.True(t, strings.HasPrefix(err.Error(), "anthropic: failed to create message: "), err.Error())
	assert.Nil(t, resp)
	assert.JSONEq(t, hiRequestBody, sentBody(t, api), "a bad request is not retried")
}

func TestGenerateContent_RejectedBeforeSending(t *testing.T) {
	t.Parallel()

	breakpoint := llms.PromptCacheBreakpoint{
		Target: llms.PromptCacheTarget{
			Kind:         llms.PromptCacheTargetMessagePart,
			MessageIndex: 0,
			PartIndex:    0,
		},
	}
	tests := []struct {
		name     string
		messages []llms.Message
		opts     []llms.CallOption
		wantErr  string
		wantIs   error
	}{
		{
			name: "invalid message",
			messages: []llms.Message{
				llms.MessageFromTextParts(llms.Role("narrator"), "once upon a time"),
			},
			wantErr: "anthropic: failed to process messages: anthropic: narrator: anthropic: unsupported message type",
			wantIs:  anthropic.ErrUnsupportedMessageType,
		},
		{
			name:     "invalid prompt cache policy",
			messages: hiMessages(),
			opts: []llms.CallOption{
				llms.WithPromptCachePolicy(&llms.PromptCachePolicy{
					Breakpoints: []llms.PromptCacheBreakpoint{
						breakpoint,
						breakpoint,
					},
				}),
			},
			wantErr: "anthropic: duplicate prompt cache breakpoint for message[0].part[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api, llm := newMessagesAPI(t, http.StatusOK, contentTypeJSON, textReply)
			resp, err := llm.GenerateContent(context.Background(), tt.messages, tt.opts...)
			require.EqualError(t, err, tt.wantErr)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
			assert.Nil(t, resp)
			assert.Empty(t, api.received())
		})
	}
}

func TestGenerateContent_Streaming(t *testing.T) {
	t.Parallel()

	api, llm := newMessagesAPI(t, http.StatusOK, contentTypeSSE, streamStart+streamText+streamToolUse+streamEnd)
	var chunks []string

	resp, err := llm.GenerateContent(context.Background(), hiMessages(), llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
		chunks = append(chunks, string(chunk))
		return nil
	}))
	require.NoError(t, err)

	assert.Equal(t, []string{"Hel", "lo"}, chunks)
	assert.Equal(t, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Hello",
				ToolCalls: []llms.ToolCall{
					{
						ID:   "toolu_1",
						Type: llms.ToolTypeFunction,
						FunctionCall: &llms.FunctionCall{
							Name:      weatherToolName,
							Arguments: `{"location":"Paris"}`,
						},
					},
				},
				StopReason: "tool_use",
				Usage: llms.Usage{
					InputTokens:      11,
					OutputTokens:     42,
					CacheWriteTokens: 3,
					CacheReadTokens:  5,
					TotalTokens:      61,
				},
			},
		},
	}, resp)
	assert.JSONEq(t, `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 8192,
		"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}],
		"stream": true
	}`, sentBody(t, api))
}

func TestGenerateContent_StreamingErrors(t *testing.T) {
	t.Parallel()

	t.Run("streaming func stops the stream", func(t *testing.T) {
		t.Parallel()

		_, llm := newMessagesAPI(t, http.StatusOK, contentTypeSSE, streamStart+streamText+streamEnd)
		var chunks []string

		resp, err := llm.GenerateContent(context.Background(), hiMessages(), llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			chunks = append(chunks, string(chunk))
			return errStop
		}))
		require.EqualError(t, err, "anthropic: streaming function error: stop streaming")
		assert.ErrorIs(t, err, errStop)
		assert.Nil(t, resp)
		assert.Equal(t, []string{"Hel"}, chunks)
	})

	t.Run("error event", func(t *testing.T) {
		t.Parallel()

		reply := streamStart + sseEvent("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
		_, llm := newMessagesAPI(t, http.StatusOK, contentTypeSSE, reply)

		resp, err := llm.GenerateContent(context.Background(), hiMessages(), llms.WithStreamingFunc(func(context.Context, []byte) error {
			return nil
		}))
		var apiErr *sdk.Error
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusOK, apiErr.StatusCode)
		assert.Equal(t, "overloaded_error", string(apiErr.Type()))
		assert.True(t, strings.HasPrefix(err.Error(), "anthropic: streaming error: "), err.Error())
		assert.Nil(t, resp)
	})

	t.Run("error status", func(t *testing.T) {
		t.Parallel()

		api, llm := newMessagesAPI(t, http.StatusBadRequest, contentTypeJSON, invalidRequestReply)

		resp, err := llm.GenerateContent(context.Background(), hiMessages(), llms.WithStreamingFunc(func(context.Context, []byte) error {
			return nil
		}))
		var apiErr *sdk.Error
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		assert.Equal(t, "invalid_request_error", string(apiErr.Type()))
		assert.True(t, strings.HasPrefix(err.Error(), "anthropic: streaming error: "), err.Error())
		assert.Nil(t, resp)
		assert.Len(t, api.received(), 1)
	})
}
