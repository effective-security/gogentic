package llms

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type unknownContent struct{}

func (unknownContent) isPart() {}

func TestUnmarshalYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    Message
		wantErr bool
	}{
		{
			name: "single text part",
			input: `role: user
text: Hello, world!
`,
			want: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
				},
			},
			wantErr: false,
		},
		{
			name: "multiple parts",
			input: `role: user
parts:
- type: text
  text: Hello!, world!
- type: image_url
  image_url:
    url: http://example.com/image.png
- type: image_url
  image_url:
    url: http://example.com/image.png
    detail: high
- type: binary
  binary:
    mime_type: application/octet-stream
    data: SGVsbG8sIHdvcmxkIQ==
- tool_response:
    tool_call_id: "123"
    name: hammer
    content: hit
  type: tool_response
`,
			want: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello!, world!"},
					ImageURLContent{URL: "http://example.com/image.png"},
					ImageURLContent{URL: "http://example.com/image.png", Detail: "high"},
					BinaryContent{
						MIMEType: "application/octet-stream",
						Data:     []byte("Hello, world!"),
					},
					ToolCallResponse{ToolCallID: "123", Name: "hammer", Content: "hit"},
				},
			},
			wantErr: false,
		},
		{
			name: "Unknown content type",
			input: `
role: user
parts:
  - type: unknown
    data: some data
`,
			want: Message{
				Role: "user",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mc Message
			err := yaml.Unmarshal([]byte(tt.input), &mc)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, mc)
		})
	}
}

func TestMarshalYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   Message
		want    string
		wantErr bool
	}{
		{
			name: "single text part",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
				},
			},
			want: `role: user
text: Hello, world!
`,
			wantErr: false,
		},
		{
			name: "multiple parts",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
					ImageURLContent{URL: "http://example.com/image.png"},
					BinaryContent{
						MIMEType: "application/octet-stream",
						Data:     []byte("Hello, world!"),
					},
					ToolCallResponse{
						ToolCallID: "123",
						Name:       "hammer",
						Content:    "hit",
					},
				},
			},
			want: `parts:
- text: Hello, world!
  type: text
- image_url:
    url: http://example.com/image.png
  type: image_url
- binary:
    data: SGVsbG8sIHdvcmxkIQ==
    mime_type: application/octet-stream
  type: binary
- tool_response:
    content: hit
    name: hammer
    tool_call_id: "123"
  type: tool_response
role: user
`,
			wantErr: false,
		},
		{
			name: "unknown content type",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					unknownContent{},
				},
			},
			want: "parts:\n- {}\nrole: user\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := yaml.Marshal(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestUnmarshalJSONMessageContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    Message
		wantErr bool
	}{
		{
			name:  "single text part",
			input: `{"role":"user","text":"Hello, world!"}`,
			want: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
				},
			},

			wantErr: false,
		},
		{
			name:  "multiple parts",
			input: `{"role":"user","parts":[{"text":"Hello, world!","type":"text"},{"type":"image_url","image_url":{"url":"http://example.com/image.png"}},{"type":"binary","binary":{"data":"SGVsbG8sIHdvcmxkIQ==","mime_type":"application/octet-stream"}}]}`,
			want: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
					ImageURLContent{URL: "http://example.com/image.png"},
					BinaryContent{
						MIMEType: "application/octet-stream",
						Data:     []byte("Hello, world!"),
					},
				},
			},
			wantErr: false,
		},
		{
			name:  "Unknown content type",
			input: `{"role":"user","parts":[{"type":"unknown","data":"some data"}]}`,
			want: Message{
				Role: "user",
			},
			wantErr: true,
		},
		{
			name:  "tool use",
			input: `{"role":"assistant","parts":[{"type":"text","text":"Hello there!"},{"type":"tool_call","tool_call":{"id":"t42","type":"function","function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}}]}`,
			want: Message{
				Role: "assistant",
				Parts: []ContentPart{
					TextContent{Text: "Hello there!"},
					ToolCall{
						ID:           "t42",
						Type:         "function",
						FunctionCall: &FunctionCall{Name: "get_current_weather", Arguments: `{ "location": "New York" }`},
					},
				},
			},
			wantErr: false,
		},
		{
			name:  "tool response",
			input: `{"role":"user","parts":[{"type":"tool_response","tool_response":{"tool_call_id":"123","name":"hammer","content":"hit"}}]}`,
			want: Message{
				Role: "user",
				Parts: []ContentPart{
					ToolCallResponse{ToolCallID: "123", Name: "hammer", Content: "hit"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mc Message
			err := mc.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, mc)
		})
	}
}

func TestMarshalJSONMessageContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   Message
		want    string
		wantErr bool
	}{
		{
			name: "single text part",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
				},
			},
			want:    `{"role":"user","text":"Hello, world!"}`,
			wantErr: false,
		},
		{
			name: "multiple parts",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
					ImageURLContent{URL: "http://example.com/image.png"},
					BinaryContent{
						MIMEType: "application/octet-stream",
						Data:     []byte("Hello, world!"),
					},
				},
			},
			want:    `{"role":"user","parts":[{"text":"Hello, world!","type":"text"},{"type":"image_url","image_url":{"url":"http://example.com/image.png"}},{"type":"binary","binary":{"data":"SGVsbG8sIHdvcmxkIQ==","mime_type":"application/octet-stream"}}]}`,
			wantErr: false,
		},
		{
			name: "Unknown content type",
			input: Message{
				Role: "user",
				Parts: []ContentPart{
					unknownContent{},
				},
			},
			want:    `{"role":"user","parts":[{}]}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			gotStr := string(got)
			assert.Equal(t, tt.want, gotStr)
		})
	}
}

// Test roundtripping for both JSON and YAML representations.
func TestRoundtripping(t *testing.T) { // nolint:funlen // We make an exception given the number of test cases.
	t.Parallel()
	tests := []struct {
		name         string
		in           Message
		assertedJSON string
		assertedYAML string
	}{
		{
			name: "single text part",
			in: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello, world!"},
				},
			},
			assertedJSON: `{"role":"user","text":"Hello, world!"}`,
			assertedYAML: "role: user\ntext: Hello, world!\n",
		},
		{
			name: "multiple parts",
			in: Message{
				Role: "user",
				Parts: []ContentPart{
					TextContent{Text: "Hello!, world!"},
					ImageURLContent{URL: "http://example.com/image.png", Detail: "low"},
					BinaryContent{
						MIMEType: "application/octet-stream",
						Data:     []byte("Hello, world!"),
					},
				},
			},
			assertedYAML: `parts:
- text: Hello!, world!
  type: text
- image_url:
    detail: low
    url: http://example.com/image.png
  type: image_url
- binary:
    data: SGVsbG8sIHdvcmxkIQ==
    mime_type: application/octet-stream
  type: binary
role: user
`,
		},
		{
			name: "tool use",
			in: Message{
				Role: "assistant",
				Parts: []ContentPart{
					ToolCall{Type: "function", ID: "t01", FunctionCall: &FunctionCall{Name: "get_current_weather", Arguments: `{ "location": "New York" }`}},
				},
			},
		},
		{
			name: "multiple tool uses",
			in: Message{
				Role: "assistant",
				Parts: []ContentPart{
					ToolCall{Type: "function", ID: "tc01", FunctionCall: &FunctionCall{Name: "get_current_weather", Arguments: `{ "location": "New York" }`}},
					ToolCall{Type: "function", ID: "tc02", FunctionCall: &FunctionCall{Name: "get_current_weather", Arguments: `{ "location": "Berlin" }`}},
				},
			},
			assertedJSON: `{"role":"assistant","parts":[{"type":"tool_call","tool_call":{"function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"},"id":"tc01","type":"function"}},{"type":"tool_call","tool_call":{"function":{"name":"get_current_weather","arguments":"{ \"location\": \"Berlin\" }"},"id":"tc02","type":"function"}}]}`,
			assertedYAML: `parts:
- tool_call:
    function:
      arguments: '{ "location": "New York" }'
      name: get_current_weather
    id: tc01
    type: function
  type: tool_call
- tool_call:
    function:
      arguments: '{ "location": "Berlin" }'
      name: get_current_weather
    id: tc02
    type: function
  type: tool_call
role: assistant
`,
		},
		{
			name: "tool use with arguments",
			in: Message{
				Role: "assistant",
				Parts: []ContentPart{
					ToolCall{Type: "hammer", FunctionCall: &FunctionCall{Name: "hit", Arguments: `{ "force": 10 }`}},
				},
			},
		},
		{
			name: "tool use with multiple arguments",
			in: Message{
				Role: "assistant",
				Parts: []ContentPart{
					ToolCall{Type: "hammer", FunctionCall: &FunctionCall{Name: "hit", Arguments: `{ "force": 10, "direction": "down" }`}},
				},
			},
		},
		{
			name: "tool response",
			in: Message{
				Role: "user",
				Parts: []ContentPart{
					ToolCallResponse{ToolCallID: "123", Name: "hammer", Content: "hit"},
				},
			},
		},
		{
			name: "multi-tool response",
			in: Message{
				Role: "user",
				Parts: []ContentPart{
					ToolCallResponse{ToolCallID: "123", Name: "hammer", Content: "hit"},
					ToolCallResponse{ToolCallID: "456", Name: "screwdriver", Content: "turn"},
				},
			},
		},
		{
			name: "tool response with arguments",
			in: Message{
				Role: "user",
				Parts: []ContentPart{
					ToolCallResponse{ToolCallID: "123", Name: "hammer", Content: "hit"},
				},
			},
		},
		{
			name: "multi-tool response with arguments",
			in: Message{
				Role: "user",
				Parts: []ContentPart{
					ToolCallResponse{ToolCallID: "123", Name: "hammer", Content: "hit"},
					ToolCallResponse{ToolCallID: "456", Name: "screwdriver", Content: "turn"},
				},
			},
		},
	}

	// Round-trip both JSON and YAML:
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// JSON
			jsonBytes, err := json.Marshal(tt.in)
			require.NoError(t, err)
			if tt.assertedJSON != "" {
				assert.Equal(t, tt.assertedJSON, string(jsonBytes))
			}
			var mc Message
			err = mc.UnmarshalJSON(jsonBytes)
			require.NoError(t, err)
			assert.Equal(t, tt.in, mc)

			// YAML
			yamlBytes, err := yaml.Marshal(tt.in)
			require.NoError(t, err)
			if tt.assertedYAML != "" {
				assert.Equal(t, tt.assertedYAML, string(yamlBytes))
			}
			mc = Message{}
			err = yaml.Unmarshal(yamlBytes, &mc)
			require.NoError(t, err)
			assert.Equal(t, tt.in, mc)
		})
	}
}

func TestUnmarshalJSONTextContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    TextContent
		wantErr bool
	}{
		{
			name:    "valid text content",
			input:   `{"type":"text","text":"Hello, world!"}`,
			want:    TextContent{Text: "Hello, world!"},
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   `{"type":"image_url","text":"Hello, world!"}`,
			want:    TextContent{},
			wantErr: true,
		},
		{
			name:    "missing type field",
			input:   `{"text":"Hello, world!"}`,
			want:    TextContent{},
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{"type":"text","text":"Hello, world!"`,
			want:    TextContent{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tc TextContent
			err := tc.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, tc)
		})
	}
}

func TestUnmarshalJSONImageURLContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    ImageURLContent
		wantErr bool
	}{
		{
			name:    "valid image URL content",
			input:   `{"type":"image_url","image_url":{"url":"http://example.com/image.png"}}`,
			want:    ImageURLContent{URL: "http://example.com/image.png"},
			wantErr: false,
		},
		{
			name:    "image URL with detail",
			input:   `{"type":"image_url","image_url":{"url":"http://example.com/image.png","detail":"high"}}`,
			want:    ImageURLContent{URL: "http://example.com/image.png", Detail: "high"},
			wantErr: false,
		},
		{
			name:    "missing type field",
			input:   `{"image_url":{"url":"http://example.com/image.png"}}`,
			want:    ImageURLContent{},
			wantErr: true,
		},
		{
			name:    "missing image_url field",
			input:   `{"type":"image_url"}`,
			want:    ImageURLContent{},
			wantErr: true,
		},
		{
			name:    "invalid image_url field type",
			input:   `{"type":"image_url","image_url":"not an object"}`,
			want:    ImageURLContent{},
			wantErr: true,
		},
		{
			name:    "missing url field",
			input:   `{"type":"image_url","image_url":{"detail":"high"}}`,
			want:    ImageURLContent{},
			wantErr: true,
		},
		{
			name:    "invalid url field type",
			input:   `{"type":"image_url","image_url":{"url":123}}`,
			want:    ImageURLContent{},
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{"type":"image_url","image_url":{"url":"http://example.com/image.png"}`,
			want:    ImageURLContent{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var iuc ImageURLContent
			err := iuc.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, iuc)
		})
	}
}

func TestUnmarshalJSONBinaryContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    BinaryContent
		wantErr bool
	}{
		{
			name:    "valid binary content",
			input:   `{"type":"binary","binary":{"mime_type":"application/octet-stream","data":"SGVsbG8sIHdvcmxkIQ=="}}`,
			want:    BinaryContent{MIMEType: "application/octet-stream", Data: []byte("Hello, world!")},
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   `{"type":"text","binary":{"mime_type":"application/octet-stream","data":"SGVsbG8sIHdvcmxkIQ=="}}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "missing binary field",
			input:   `{"type":"binary"}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "invalid binary field type",
			input:   `{"type":"binary","binary":"not an object"}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "missing mime_type field",
			input:   `{"type":"binary","binary":{"data":"SGVsbG8sIHdvcmxkIQ=="}}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "missing data field",
			input:   `{"type":"binary","binary":{"mime_type":"application/octet-stream"}}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "invalid mime_type field type",
			input:   `{"type":"binary","binary":{"mime_type":123,"data":"SGVsbG8sIHdvcmxkIQ=="}}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "invalid data field type",
			input:   `{"type":"binary","binary":{"mime_type":"application/octet-stream","data":123}}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "invalid base64 data",
			input:   `{"type":"binary","binary":{"mime_type":"application/octet-stream","data":"invalid-base64!"}}`,
			want:    BinaryContent{},
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{"type":"binary","binary":{"mime_type":"application/octet-stream","data":"SGVsbG8sIHdvcmxkIQ=="}`,
			want:    BinaryContent{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var bc BinaryContent
			err := bc.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, bc)
		})
	}
}

func TestUnmarshalJSONToolCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    ToolCall
		wantErr bool
	}{
		{
			name:    "valid tool call with function",
			input:   `{"type":"tool_call","tool_call":{"id":"t42","type":"function","function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}}`,
			want:    ToolCall{ID: "t42", Type: "function", FunctionCall: &FunctionCall{Name: "get_current_weather", Arguments: `{ "location": "New York" }`}},
			wantErr: false,
		},
		{
			name:    "tool call without function",
			input:   `{"type":"tool_call","tool_call":{"id":"t42","type":"function"}}`,
			want:    ToolCall{ID: "t42", Type: "function", FunctionCall: &FunctionCall{}},
			wantErr: false,
		},
		{
			name:    "missing type field",
			input:   `{"tool_call":{"id":"t42","type":"function","function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}}`,
			want:    ToolCall{},
			wantErr: true,
		},
		{
			name:    "missing tool_call field",
			input:   `{"type":"tool_call"}`,
			want:    ToolCall{},
			wantErr: true,
		},
		{
			name:    "invalid tool_call field type",
			input:   `{"type":"tool_call","tool_call":"not an object"}`,
			want:    ToolCall{},
			wantErr: true,
		},
		{
			name:    "missing id field",
			input:   `{"type":"tool_call","tool_call":{"type":"function","function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}}`,
			want:    ToolCall{},
			wantErr: true,
		},
		{
			name:    "missing type field in tool_call",
			input:   `{"type":"tool_call","tool_call":{"id":"t42","function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}}`,
			want:    ToolCall{},
			wantErr: true,
		},
		{
			name:    "invalid id field type",
			input:   `{"type":"tool_call","tool_call":{"id":123,"type":"function","function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}}`,
			want:    ToolCall{},
			wantErr: true,
		},
		{
			name:    "invalid type field type in tool_call",
			input:   `{"type":"tool_call","tool_call":{"id":"t42","type":123,"function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}}`,
			want:    ToolCall{},
			wantErr: true,
		},
		{
			name:    "invalid function field - not json raw message",
			input:   `{"type":"tool_call","tool_call":{"id":"t42","type":"function","function":"invalid function"}}`,
			want:    ToolCall{ID: "t42", Type: "function", FunctionCall: &FunctionCall{}},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{"type":"tool_call","tool_call":{"id":"t42","type":"function","function":{"name":"get_current_weather","arguments":"{ \"location\": \"New York\" }"}}`,
			want:    ToolCall{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tc ToolCall
			err := tc.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, tc)
		})
	}
}

func TestUnmarshalJSONToolCallResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    ToolCallResponse
		wantErr bool
	}{
		{
			name:    "valid tool call response",
			input:   `{"type":"tool_response","tool_response":{"tool_call_id":"123","name":"hammer","content":"hit"}}`,
			want:    ToolCallResponse{ToolCallID: "123", Name: "hammer", Content: "hit"},
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   `{"type":"tool_call","tool_response":{"tool_call_id":"123","name":"hammer","content":"hit"}}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "missing tool_response field",
			input:   `{"type":"tool_response"}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "invalid tool_response field type",
			input:   `{"type":"tool_response","tool_response":"not an object"}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "missing tool_call_id field",
			input:   `{"type":"tool_response","tool_response":{"name":"hammer","content":"hit"}}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "missing name field",
			input:   `{"type":"tool_response","tool_response":{"tool_call_id":"123","content":"hit"}}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "missing content field",
			input:   `{"type":"tool_response","tool_response":{"tool_call_id":"123","name":"hammer"}}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "invalid tool_call_id field type",
			input:   `{"type":"tool_response","tool_response":{"tool_call_id":123,"name":"hammer","content":"hit"}}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "invalid name field type",
			input:   `{"type":"tool_response","tool_response":{"tool_call_id":"123","name":123,"content":"hit"}}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "invalid content field type",
			input:   `{"type":"tool_response","tool_response":{"tool_call_id":"123","name":"hammer","content":123}}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{"type":"tool_response","tool_response":{"tool_call_id":"123","name":"hammer","content":"hit"}`,
			want:    ToolCallResponse{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tcr ToolCallResponse
			err := tcr.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, tcr)
		})
	}
}
