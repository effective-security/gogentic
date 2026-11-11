package googleai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const (
	// generatePath is the path of a GenerateContent request for the fixture
	// model.
	generatePath = "/v1beta/models/fixture:generateContent"
	// toolName is the function the model is offered and calls.
	toolName = "lookup"
	// pngType is the MIME type of the image fixtures.
	pngType = "image/png"
	// headerContentType names the content type header.
	headerContentType = "Content-Type"

	// richResponse holds two text parts, a function call and every usage
	// counter.
	richResponse = `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [
					{"text": "hello "},
					{"text": "world"},
					{"functionCall": {"name": "lookup", "args": {"q": "x"}}}
				]
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"cachedContentTokenCount": 1,
			"candidatesTokenCount": 3,
			"toolUsePromptTokenCount": 2,
			"thoughtsTokenCount": 4,
			"totalTokenCount": 20
		}
	}`
)

// humanText is a conversation of one user message.
func humanText(text string) []llms.Message {
	return []llms.Message{{
		Role: llms.RoleHuman,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: text},
		},
	}}
}

// sentParts returns the parts of one content of a recorded request.
func sentParts(t *testing.T, content any) []any {
	t.Helper()
	c, ok := content.(map[string]any)
	require.True(t, ok, "content %v", content)
	parts, ok := c["parts"].([]any)
	require.True(t, ok, "content %v", content)
	return parts
}

// The client options become the generation controls and safety settings of
// every request, which goes to the client's model.
func TestGenerateContent_ClientOptionsReachTheWire(t *testing.T) {
	f := &fixture{
		t:        t,
		response: textResponse,
	}
	m, err := New(context.Background(),
		WithDefaultModel("fixture"),
		WithAPIKey("fixture"),
		WithHTTPClient(f.client()),
		WithDefaultCandidateCount(2),
		WithDefaultMaxTokens(128),
		WithDefaultTemperature(0.25),
		WithDefaultTopK(5),
		WithDefaultTopP(0.5),
		WithHarmThreshold(genai.HarmBlockThresholdBlockOnlyHigh),
	)
	require.NoError(t, err)
	assert.Equal(t, "fixture", m.GetName())
	assert.Equal(t, llms.ProviderGoogleAI, m.GetProviderType())

	_, err = m.GenerateContent(context.Background(), humanText("hi"),
		llms.WithStopWords([]string{"END"}),
		llms.WithSeed(9),
	)
	require.NoError(t, err)
	require.Len(t, f.requests, 1)
	assert.Equal(t, []string{generatePath}, f.paths)

	want := map[string]any{
		"candidateCount":  float64(2),
		"maxOutputTokens": float64(128),
		"temperature":     0.25,
		"topK":            float64(5),
		"topP":            0.5,
		"seed":            float64(9),
		"stopSequences": []any{
			"END",
		},
	}
	assert.Equal(t, want, generation(f.requests[0]))

	categories := []string{
		string(genai.HarmCategoryDangerousContent),
		string(genai.HarmCategoryHarassment),
		string(genai.HarmCategoryHateSpeech),
		string(genai.HarmCategorySexuallyExplicit),
	}
	settings, ok := f.requests[0]["safetySettings"].([]any)
	require.True(t, ok)
	require.Len(t, settings, len(categories))
	for i, category := range categories {
		setting := map[string]any{
			"category":  category,
			"threshold": string(genai.HarmBlockThresholdBlockOnlyHigh),
		}
		assert.Equal(t, setting, settings[i], "setting %d", i)
	}
}

// Every role and part type of a conversation is converted; the system message
// becomes the system instruction.
func TestGenerateContent_ConvertsTheConversation(t *testing.T) {
	f := &fixture{
		t:        t,
		response: richResponse,
	}
	m := f.model()
	image := []byte{0x89, 'P', 'N', 'G'}
	messages := []llms.Message{
		{
			Role: llms.RoleSystem,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "be brief"},
			},
		},
		{
			Role: llms.RoleHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "look at this"},
				llms.BinaryContent{
					MIMEType: pngType,
					Data:     image,
				},
			},
		},
		{
			Role: llms.RoleAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					ID:   "call-1",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      toolName,
						Arguments: `{"q":"x"}`,
					},
				},
			},
		},
		{
			Role: llms.RoleTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: "call-1",
					Name:       toolName,
					Content:    "found",
				},
			},
		},
		{
			Role: llms.RoleGeneric,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "and then?"},
			},
		},
	}

	resp, err := m.GenerateContent(context.Background(), messages)
	require.NoError(t, err)

	require.Len(t, f.requests, 1)
	req := f.requests[0]
	system := map[string]any{
		"parts": []any{
			map[string]any{
				"text": "be brief",
			},
		},
		"role": RoleSystem,
	}
	assert.Equal(t, system, req["systemInstruction"])

	contents, ok := req["contents"].([]any)
	require.True(t, ok)
	require.Len(t, contents, 4)
	roles := make([]string, 0, len(contents))
	for _, c := range contents {
		roles = append(roles, c.(map[string]any)["role"].(string))
	}
	assert.Equal(t, []string{RoleUser, RoleModel, RoleTool, RoleUser}, roles)

	human := sentParts(t, contents[0])
	require.Len(t, human, 2)
	assert.Equal(t, map[string]any{"text": "look at this"}, human[0])
	inline := map[string]any{
		"inlineData": map[string]any{
			"mimeType": pngType,
			"data":     base64.StdEncoding.EncodeToString(image),
		},
	}
	assert.Equal(t, inline, human[1])

	call := map[string]any{
		"functionCall": map[string]any{
			"name": toolName,
			"args": map[string]any{
				"q": "x",
			},
		},
	}
	assert.Equal(t, []any{call}, sentParts(t, contents[1]))

	result := map[string]any{
		"functionResponse": map[string]any{
			"name": toolName,
			"response": map[string]any{
				"response": "found",
			},
		},
	}
	assert.Equal(t, []any{result}, sentParts(t, contents[2]))

	require.Len(t, resp.Choices, 1)
	choice := resp.Choices[0]
	assert.Equal(t, "hello world", choice.Content)
	assert.Equal(t, string(genai.FinishReasonStop), choice.StopReason)
	wantCalls := []llms.ToolCall{{
		Type: llms.ToolTypeFunction,
		FunctionCall: &llms.FunctionCall{
			Name:      toolName,
			Arguments: `{"q":"x"}`,
		},
	}}
	assert.Equal(t, wantCalls, choice.ToolCalls)
	assert.EqualValues(t, 10, choice.Usage.InputTokens)
	assert.EqualValues(t, 1, choice.Usage.CacheReadTokens)
	assert.EqualValues(t, 3+2+4, choice.Usage.OutputTokens)
	assert.EqualValues(t, 20, choice.Usage.TotalTokens)
	assert.Contains(t, choice.GenerationInfo, CITATIONS)
	assert.Contains(t, choice.GenerationInfo, SAFETY)
}

// An image named by URL is downloaded and sent inline.
func TestGenerateContent_ImageURL(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentType, pngType)
		_, _ = w.Write(image)
	}))
	t.Cleanup(server.Close)

	f := &fixture{
		t:        t,
		response: textResponse,
	}
	messages := []llms.Message{{
		Role: llms.RoleHuman,
		Parts: []llms.ContentPart{
			llms.ImageURLContent{URL: server.URL + "/cat.png"},
		},
	}}
	_, err := f.model().GenerateContent(context.Background(), messages)
	require.NoError(t, err)

	require.Len(t, f.requests, 1)
	contents := f.requests[0]["contents"].([]any)
	require.Len(t, contents, 1)
	parts := sentParts(t, contents[0])
	require.Len(t, parts, 1)
	inline, ok := parts[0].(map[string]any)["inlineData"].(map[string]any)
	require.True(t, ok, "part %v", parts[0])
	assert.Equal(t, base64.StdEncoding.EncodeToString(image), inline["data"])
}

// A conversation that cannot be converted fails before a request is sent.
func TestGenerateContent_ConversionErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentType, "nonsense")
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name    string
		message llms.Message
		wantErr string
	}{
		{
			name: "unsupported role",
			message: llms.Message{
				Role: llms.Role("narrator"),
				Parts: []llms.ContentPart{
					llms.TextContent{Text: "once upon a time"},
				},
			},
			wantErr: "role narrator not supported",
		},
		{
			name: "tool call arguments are not JSON",
			message: llms.Message{
				Role: llms.RoleAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						FunctionCall: &llms.FunctionCall{
							Name:      toolName,
							Arguments: "{",
						},
					},
				},
			},
			wantErr: "googleai: failed to unmarshal tool call arguments: unexpected end of JSON input",
		},
		{
			// F-038: used to panic on the nil FunctionCall.
			name: "tool call without function call",
			message: llms.Message{
				Role: llms.RoleAI,
				Parts: []llms.ContentPart{
					llms.ToolCall{
						ID:   "ws_1",
						Type: "web_search",
					},
				},
			},
			wantErr: `googleai: unsupported tool call in history: tool call "ws_1" of type "web_search": tool call has no function call`,
		},
		{
			name: "image without a MIME type",
			message: llms.Message{
				Role: llms.RoleHuman,
				Parts: []llms.ContentPart{
					llms.ImageURLContent{URL: server.URL},
				},
			},
			wantErr: "invalid mime type nonsense",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{
				t:        t,
				response: textResponse,
			}
			resp, err := f.model().GenerateContent(context.Background(), []llms.Message{tt.message})
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, resp)
			assert.Empty(t, f.requests)
		})
	}
}

// A tool the provider cannot offer fails before a request is sent.
func TestGenerateContent_UnsupportedTool(t *testing.T) {
	f := &fixture{
		t:        t,
		response: textResponse,
	}
	tools := []llms.Tool{{
		Type: "retrieval",
	}}
	resp, err := f.model().GenerateContent(context.Background(), humanText("hi"), llms.WithTools(tools))
	require.EqualError(t, err, `tool [0]: unsupported type "retrieval", want 'function'`)
	assert.Nil(t, resp)
	assert.Empty(t, f.requests)

	// F-022: a function tool without a definition used to panic.
	resp, err = f.model().GenerateContent(context.Background(), humanText("hi"), llms.WithTools([]llms.Tool{{
		Type: llms.ToolTypeFunction,
	}}))
	require.ErrorIs(t, err, llms.ErrMissingFunctionDefinition)
	require.EqualError(t, err, `tool [0]: tool of type "function": tool has no function definition`)
	assert.Nil(t, resp)
	assert.Empty(t, f.requests)
}

// A JSON response format asks for JSON, with the schema when one is given,
// unless function tools are offered.
func TestGenerateContent_ResponseFormat(t *testing.T) {
	answer := &schema.ResponseFormat{
		Type: "json_object",
		JSONSchema: &schema.ResponseFormatJSONSchema{
			Name: "answer",
			Schema: &schema.ResponseFormatJSONSchemaProperty{
				Type: "object",
				Properties: map[string]*schema.ResponseFormatJSONSchemaProperty{
					"ok": {
						Type: "boolean",
					},
				},
				Required: []string{
					"ok",
				},
			},
		},
	}
	answerSchema := map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"ok": map[string]any{
				"type": "BOOLEAN",
			},
		},
		"required": []any{
			"ok",
		},
	}
	function := llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        toolName,
			Description: "looks things up",
		},
	}
	search := llms.Tool{
		Type: "google_search",
	}

	tests := []struct {
		name       string
		format     *schema.ResponseFormat
		tools      []llms.Tool
		wantMIME   bool
		wantSchema map[string]any
	}{
		{
			name:       "schema",
			format:     answer,
			wantMIME:   true,
			wantSchema: answerSchema,
		},
		{
			name: "JSON without a schema",
			format: &schema.ResponseFormat{
				Type: "json_object",
			},
			wantMIME: true,
		},
		{
			name:   "function tools win over the format",
			format: answer,
			tools: []llms.Tool{
				search,
				function,
			},
		},
		{
			name:   "a search tool does not",
			format: answer,
			tools: []llms.Tool{
				search,
			},
			wantMIME:   true,
			wantSchema: answerSchema,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{
				t:        t,
				response: textResponse,
			}
			opts := []llms.CallOption{
				llms.WithResponseFormat(tt.format),
			}
			if tt.tools != nil {
				opts = append(opts, llms.WithTools(tt.tools))
			}
			_, err := f.model().GenerateContent(context.Background(), humanText("answer"), opts...)
			require.NoError(t, err)
			require.Len(t, f.requests, 1)

			cfg := generation(f.requests[0])
			if tt.wantMIME {
				assert.Equal(t, ResponseMIMETypeJson, cfg["responseMimeType"])
			} else {
				assert.NotContains(t, cfg, "responseMimeType")
			}
			if tt.wantSchema != nil {
				assert.Equal(t, tt.wantSchema, cfg["responseSchema"])
			} else {
				assert.NotContains(t, cfg, "responseSchema")
			}
		})
	}
}

// A reply that is an error, holds no candidate or holds a part that is
// neither text nor a function call is an error.
func TestGenerateContent_ReplyErrors(t *testing.T) {
	t.Run("API error", func(t *testing.T) {
		f := &fixture{
			t:        t,
			status:   http.StatusBadRequest,
			response: `{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`,
		}
		resp, err := f.model().GenerateContent(context.Background(), humanText("hi"))
		var apiErr genai.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusBadRequest, apiErr.Code)
		assert.Equal(t, "bad request", apiErr.Message)
		assert.Nil(t, resp)
		assert.Len(t, f.requests, 1)
	})

	t.Run("no candidate", func(t *testing.T) {
		f := &fixture{
			t:        t,
			response: `{"candidates":[]}`,
		}
		resp, err := f.model().GenerateContent(context.Background(), humanText("hi"))
		require.ErrorIs(t, err, ErrNoContentInResponse)
		assert.Nil(t, resp)
	})

	t.Run("unknown part", func(t *testing.T) {
		f := &fixture{
			t:        t,
			response: `{"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]},"finishReason":"STOP"}]}`,
		}
		resp, err := f.model().GenerateContent(context.Background(), humanText("hi"))
		require.ErrorIs(t, err, ErrUnknownPartInResponse)
		assert.Nil(t, resp)
	})
}

// Without a configuration the request carries the conversation alone.
func TestGenerateFromMessages_NilConfig(t *testing.T) {
	f := &fixture{
		t:        t,
		response: textResponse,
	}
	resp, err := f.model().generateFromMessages(context.Background(), humanText("hi"), nil)
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	assert.Equal(t, "hello", resp.Choices[0].Content)

	require.Len(t, f.requests, 1)
	assert.Equal(t, map[string]any{}, generation(f.requests[0]))
	assert.NotContains(t, f.requests[0], "safetySettings")
	assert.NotContains(t, f.requests[0], "systemInstruction")
}

func TestConvertCandidates(t *testing.T) {
	t.Run("arguments that cannot be encoded", func(t *testing.T) {
		candidates := []*genai.Candidate{{
			Content: &genai.Content{
				Parts: []*genai.Part{{
					FunctionCall: &genai.FunctionCall{
						Name: toolName,
						Args: map[string]any{
							"c": make(chan int),
						},
					},
				}},
			},
		}}
		resp, err := convertCandidates(candidates, nil)
		require.EqualError(t, err, "json: unsupported type: chan int")
		assert.Nil(t, resp)
	})

	t.Run("candidate without content and usage", func(t *testing.T) {
		candidates := []*genai.Candidate{{
			FinishReason: genai.FinishReasonSafety,
		}}
		resp, err := convertCandidates(candidates, nil)
		require.NoError(t, err)
		require.Len(t, resp.Choices, 1)
		choice := resp.Choices[0]
		assert.Empty(t, choice.Content)
		assert.Nil(t, choice.ToolCalls)
		assert.Equal(t, string(genai.FinishReasonSafety), choice.StopReason)
		assert.Equal(t, llms.Usage{}, choice.Usage)
	})
}
