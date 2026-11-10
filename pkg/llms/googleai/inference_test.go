package googleai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	t        *testing.T
	status   int
	response string
	requests []map[string]any
	bodies   []string
	// paths holds the URL path of each request.
	paths []string
}

// client returns an HTTP client that records each request and serves the
// fixture's canned response.
func (f *fixture) client() *http.Client {
	return &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			f.paths = append(f.paths, r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(f.t, err)
			f.bodies = append(f.bodies, string(body))
			var v map[string]any
			require.NoError(f.t, json.Unmarshal(body, &v))
			f.requests = append(f.requests, v)
			status := f.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(f.response)),
				Request: r,
			}, nil
		}),
	}
}

func (f *fixture) model() *GoogleAI {
	m, err := New(context.Background(), WithDefaultModel("fixture"), WithAPIKey("fixture"), WithHTTPClient(f.client()))
	require.NoError(f.t, err)
	return m
}

func textRequest(text string) llms.InferenceRequest {
	return llms.InferenceRequest{
		Model: "fixture",
		Messages: []llms.InferenceMessage{{
			Role: llms.InferenceRoleUser,
			Content: []llms.InferenceBlock{{
				Type: llms.BlockText,
				Text: text,
			}},
		}},
	}
}

const textResponse = `{"responseId":"up-1","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`

func generation(req map[string]any) map[string]any {
	cfg, _ := req["generationConfig"].(map[string]any)
	return cfg
}

func TestInferTextPreservesExplicitZeroAndSeed(t *testing.T) {
	f := &fixture{t: t, response: textResponse}
	m := f.model()
	zero := 0.0
	seed := int64(7)
	req := textRequest("hi")
	req.Temperature = &zero
	req.Seed = &seed
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, llms.FinishStop, out.FinishReason)
	assert.Equal(t, "hello", out.Content[0].Text)
	assert.Equal(t, "up-1", out.UpstreamID)
	assert.True(t, out.UsageKnown)
	assert.EqualValues(t, 10, out.Usage.InputTokens)
	assert.EqualValues(t, 12, out.Usage.TotalTokens)
	cfg := generation(f.requests[0])
	assert.Equal(t, float64(0), cfg["temperature"])
	assert.Equal(t, float64(7), cfg["seed"])
	assert.NotContains(t, cfg, "topP")

	// Omitted sampling controls are absent from the wire.
	_, err = m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	cfg = generation(f.requests[1])
	assert.NotContains(t, cfg, "temperature")
	assert.NotContains(t, cfg, "seed")
}

func TestInferReusesOneClient(t *testing.T) {
	f := &fixture{t: t, response: textResponse}
	m := f.model()
	client := m.client
	for range 2 {
		_, err := m.Infer(context.Background(), textRequest("hi"))
		require.NoError(t, err)
	}
	assert.Same(t, client, m.client)
	assert.Len(t, f.requests, 2)
}

func TestInferToolCallSyntheticIDStrippedOnReplay(t *testing.T) {
	f := &fixture{t: t, response: `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"n":9007199254740993}}}]},"finishReason":"STOP"}]}`}
	m := f.model()
	req := textRequest("lookup")
	req.Tools = []llms.InferenceTool{{
		Name:       "lookup",
		Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`),
	}}
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceFunction, Name: "lookup"}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, llms.FinishToolCalls, out.FinishReason)
	require.Len(t, out.Content, 1)
	call := out.Content[0]
	assert.Equal(t, llms.BlockFunctionCall, call.Type)
	assert.True(t, strings.HasPrefix(call.ID, syntheticCallPrefix))
	assert.Equal(t, `{"n":9007199254740993}`, call.Arguments)
	assert.False(t, out.UsageKnown)
	toolConfig := f.requests[0]["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
	assert.Equal(t, "ANY", toolConfig["mode"])
	assert.Equal(t, []any{"lookup"}, toolConfig["allowedFunctionNames"])

	f.response = textResponse
	req.ToolChoice = nil
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: []llms.InferenceBlock{call}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{
			Type: llms.BlockFunctionOutput,
			ID:   call.ID,
			Name: "lookup",
			Text: "ok",
		}}},
	)
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, f.bodies[1], "9007199254740993")
	contents := f.requests[1]["contents"].([]any)
	require.Len(t, contents, 3)
	callPart := contents[1].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	assert.NotContains(t, callPart, "id")
	resultPart := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	assert.NotContains(t, resultPart, "id")
	assert.Equal(t, "lookup", resultPart["name"])
	assert.Equal(t, map[string]any{"output": "ok"}, resultPart["response"])
}

func TestInferParallelToolResultsShareOneTurn(t *testing.T) {
	f := &fixture{t: t, response: textResponse}
	m := f.model()
	req := textRequest("lookup twice")
	req.Tools = []llms.InferenceTool{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: []llms.InferenceBlock{
			{Type: llms.BlockFunctionCall, ID: "call-a", Name: "lookup", Arguments: `{}`},
			{Type: llms.BlockFunctionCall, ID: "call-b", Name: "lookup", Arguments: `{}`},
		}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call-a", Name: "lookup", Text: "a"}}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call-b", Name: "lookup", Text: "b"}}},
	)
	_, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	contents := f.requests[0]["contents"].([]any)
	require.Len(t, contents, 3, "two tool messages merge into one user turn")
	last := contents[2].(map[string]any)
	assert.Equal(t, "user", last["role"])
	parts := last["parts"].([]any)
	require.Len(t, parts, 2)
	first := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	assert.Equal(t, "call-a", first["id"], "non-synthetic IDs pass through")
}

func TestInferJSONSchemaAndSystemInstruction(t *testing.T) {
	f := &fixture{t: t, response: `{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"ok\":true}"}]},"finishReason":"STOP"}]}`}
	m := f.model()
	req := textRequest("answer")
	req.Messages = append([]llms.InferenceMessage{{
		Role:    llms.InferenceRoleSystem,
		Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "be brief"}},
	}}, req.Messages...)
	req.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "answer",
		Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`),
	}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, out.Content[0].Text)
	cfg := generation(f.requests[0])
	assert.Equal(t, "application/json", cfg["responseMimeType"])
	assert.Contains(t, cfg, "responseJsonSchema")
	assert.Contains(t, f.requests[0], "systemInstruction")
	assert.Len(t, f.requests[0]["contents"].([]any), 1)
}

func TestInferThoughtSignatureRoundTrip(t *testing.T) {
	signature := "c2lnbmF0dXJl" // base64("signature")
	f := &fixture{t: t, response: `{"candidates":[{"content":{"role":"model","parts":[{"text":"thinking...","thought":true},{"functionCall":{"name":"lookup","args":{}},"thoughtSignature":"` + signature + `"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":5,"totalTokenCount":17}}`}
	m := f.model()
	req := textRequest("lookup")
	req.Tools = []llms.InferenceTool{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}
	req.Reasoning = &llms.InferenceReasoning{Effort: llms.EffortLow}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, out.Content, 3)
	assert.Equal(t, llms.BlockReasoning, out.Content[0].Type)
	assert.Equal(t, "thinking...", out.Content[0].Text)
	assert.Empty(t, out.Content[0].Opaque)
	assert.Equal(t, llms.BlockReasoning, out.Content[1].Type)
	assert.JSONEq(t, `{"thoughtSignature":"`+signature+`"}`, string(out.Content[1].Opaque))
	assert.Equal(t, llms.BlockFunctionCall, out.Content[2].Type)
	assert.EqualValues(t, 7, out.Usage.OutputTokens)
	assert.EqualValues(t, 5, out.Usage.ReasoningTokens)
	thinking := generation(f.requests[0])["thinkingConfig"].(map[string]any)
	assert.Equal(t, true, thinking["includeThoughts"])
	assert.Equal(t, float64(2048), thinking["thinkingBudget"])

	f.response = textResponse
	for i := range out.Content {
		out.Content[i].Source = "target"
	}
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: out.Content},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: out.Content[2].ID, Name: "lookup", Text: "ok"}}},
	)
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	model := f.requests[1]["contents"].([]any)[1].(map[string]any)
	parts := model["parts"].([]any)
	require.Len(t, parts, 1, "reasoning blocks produce no parts of their own")
	part := parts[0].(map[string]any)
	assert.Contains(t, part, "functionCall")
	assert.Equal(t, signature, part["thoughtSignature"])
}

func TestInferFinishReasonsAndErrors(t *testing.T) {
	f := &fixture{t: t, response: `{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"SAFETY"}]}`}
	m := f.model()
	out, err := m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishContentFilter, out.FinishReason)
	assert.Empty(t, out.Content)

	f.response = `{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]},"finishReason":"MAX_TOKENS"}]}`
	out, err = m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishLength, out.FinishReason)

	f.response = `{"promptFeedback":{"blockReason":"SAFETY"}}`
	out, err = m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishContentFilter, out.FinishReason)

	f.response = `{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"MALFORMED_FUNCTION_CALL"}]}`
	_, err = m.Infer(context.Background(), textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)

	f.status = http.StatusTooManyRequests
	f.response = `{"error":{"code":429,"message":"secret quota","status":"RESOURCE_EXHAUSTED"}}`
	_, err = m.Infer(context.Background(), textRequest("hi"))
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusTooManyRequests, ie.Status)
	assert.Len(t, f.requests, 5, "exactly one attempt per call")
}

func TestValidateInferenceRejections(t *testing.T) {
	m := (&fixture{t: t, response: textResponse}).model()
	yes := true
	cases := map[string]llms.InferenceRequest{
		"parallel_tool_calls":     {ParallelToolCalls: &yes},
		"tools.strict":            {Tools: []llms.InferenceTool{{Name: "x", Strict: &yes}}},
		"messages.role.developer": {Messages: []llms.InferenceMessage{{Role: llms.InferenceRoleDeveloper}}},
		"reasoning":               {Reasoning: &llms.InferenceReasoning{Effort: "extreme"}},
	}
	for param, req := range cases {
		t.Run(param, func(t *testing.T) {
			err := m.ValidateInference(req)
			var ie *llms.InferenceError
			require.ErrorAs(t, err, &ie)
			assert.Equal(t, param, ie.Param)
		})
	}
	budget := 512
	require.NoError(t, m.ValidateInference(llms.InferenceRequest{Reasoning: &llms.InferenceReasoning{BudgetTokens: &budget}}))
}

// stubBody is a response body whose read and close fail as a test says.
type stubBody struct {
	io.Reader
	closeErr error
}

func (b *stubBody) Close() error { return b.closeErr }

// failingReader fails every read.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// zeros is an endless stream of zero bytes.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestCaptureTransport(t *testing.T) {
	errUpstream := errors.New("connection refused")
	errRead := errors.New("connection reset")
	errClose := errors.New("close failed")
	payload := []byte(textResponse)

	tests := []struct {
		name    string
		capture bool
		err     error
		body    io.ReadCloser
		wantErr string
	}{
		{
			name:    "upstream failure",
			capture: true,
			err:     errUpstream,
			wantErr: "send googleai request: connection refused",
		},
		{
			name:    "read failure",
			capture: true,
			body: &stubBody{
				Reader: failingReader{err: errRead},
			},
			wantErr: "read googleai inference: connection reset",
		},
		{
			name:    "close failure",
			capture: true,
			body: &stubBody{
				Reader:   bytes.NewReader(payload),
				closeErr: errClose,
			},
			wantErr: "close googleai response: close failed",
		},
		{
			name:    "response too large",
			capture: true,
			body: &stubBody{
				Reader: io.LimitReader(zeros{}, maxInferenceResponseBytes+1),
			},
			wantErr: "googleai response exceeds 16777216 bytes",
		},
		{
			name:    "captured",
			capture: true,
			body: &stubBody{
				Reader: bytes.NewReader(payload),
			},
		},
		{
			name: "not captured",
			body: &stubBody{
				Reader:   bytes.NewReader(payload),
				closeErr: errClose,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &http.Response{
				StatusCode: http.StatusOK,
				Body:       tt.body,
			}
			transport := &captureTransport{
				base: roundTrip(func(*http.Request) (*http.Response, error) {
					if tt.err != nil {
						return nil, tt.err
					}
					return upstream, nil
				}),
			}
			ctx := context.Background()
			holder := &rawCapture{}
			if tt.capture {
				ctx = context.WithValue(ctx, captureKey{}, holder)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://gemini.test/", http.NoBody)
			require.NoError(t, err)

			res, err := transport.RoundTrip(req)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, res)
				assert.Nil(t, holder.body)
				return
			}
			require.NoError(t, err)
			require.Same(t, upstream, res)
			if !tt.capture {
				assert.Same(t, tt.body, res.Body, "a request without a capture passes through")
				assert.Nil(t, holder.body)
				return
			}
			assert.Equal(t, payload, holder.body)
			replayed, err := io.ReadAll(res.Body)
			require.NoError(t, err)
			assert.Equal(t, payload, replayed, "the caller still reads the whole body")
		})
	}
}

// A client without an HTTP client of its own is used as it is.
func TestWrapInferenceTransport_WithoutHTTPClient(t *testing.T) {
	client := &genai.Client{}
	wrapped, err := wrapInferenceTransport(context.Background(), client)
	require.NoError(t, err)
	assert.Same(t, client, wrapped)
}

// The sampling, length, stop and format controls reach the wire, and every
// tool choice selects its function calling mode.
func TestInferControls(t *testing.T) {
	topP := 0.5
	maxTokens := 64
	tools := []llms.InferenceTool{{
		Name:        "lookup",
		Description: "looks things up",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
	}}

	f := &fixture{
		t:        t,
		response: textResponse,
	}
	m := f.model()
	req := textRequest("hi")
	req.TopP = &topP
	req.MaxTokens = &maxTokens
	req.Stop = []string{"END"}
	req.Format = &llms.InferenceFormat{
		Type: llms.FormatJSONObject,
	}
	_, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, f.requests, 1)
	want := map[string]any{
		"topP":             0.5,
		"maxOutputTokens":  float64(64),
		"responseMimeType": ResponseMIMETypeJson,
		"stopSequences": []any{
			"END",
		},
	}
	assert.Equal(t, want, generation(f.requests[0]))
	assert.Equal(t, []string{"/v1beta/models/fixture:generateContent"}, f.paths)

	modes := map[llms.ToolChoiceMode]string{
		llms.ToolChoiceNone:     "NONE",
		llms.ToolChoiceRequired: "ANY",
		llms.ToolChoiceAuto:     "AUTO",
	}
	for mode, wire := range modes {
		t.Run(string(mode), func(t *testing.T) {
			f := &fixture{
				t:        t,
				response: textResponse,
			}
			req := textRequest("hi")
			req.Tools = tools
			req.ToolChoice = &llms.InferenceToolChoice{
				Mode: mode,
			}
			_, err := f.model().Infer(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, f.requests, 1)
			toolConfig := f.requests[0]["toolConfig"].(map[string]any)
			calling := map[string]any{
				"mode": wire,
			}
			assert.Equal(t, calling, toolConfig["functionCallingConfig"])
		})
	}
}

// A request that cannot be translated is rejected before anything is sent.
func TestInferRejectsBeforeSending(t *testing.T) {
	yes := true
	badReasoning := llms.InferenceBlock{
		Type: llms.BlockReasoning,
	}
	tests := []struct {
		name       string
		edit       func(*llms.InferenceRequest)
		wantStatus int
		wantParam  string
		wantErr    string
	}{
		{
			name: "unsupported control",
			edit: func(r *llms.InferenceRequest) {
				r.ParallelToolCalls = &yes
			},
			wantStatus: http.StatusBadRequest,
			wantParam:  paramParallelToolCalls,
			wantErr:    "unsupported inference parameter: parallel_tool_calls",
		},
		{
			name: "function arguments are not JSON",
			edit: func(r *llms.InferenceRequest) {
				r.Messages = append(r.Messages, llms.InferenceMessage{
					Role: llms.InferenceRoleAssistant,
					Content: []llms.InferenceBlock{{
						Type:      llms.BlockFunctionCall,
						ID:        "call-1",
						Name:      "lookup",
						Arguments: "{",
					}},
				})
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    "decode function arguments: unexpected EOF",
		},
		{
			name: "reasoning state is not JSON",
			edit: func(r *llms.InferenceRequest) {
				block := badReasoning
				block.Opaque = json.RawMessage(`{`)
				r.Messages[0].Content = append([]llms.InferenceBlock{block}, r.Messages[0].Content...)
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    "decode reasoning state: unexpected end of JSON input",
		},
		{
			name: "thought signature is not base64",
			edit: func(r *llms.InferenceRequest) {
				block := badReasoning
				block.Opaque = json.RawMessage(`{"thoughtSignature":"!!"}`)
				r.Messages[0].Content = append([]llms.InferenceBlock{block}, r.Messages[0].Content...)
			},
			wantStatus: http.StatusBadRequest,
			wantErr:    "decode thought signature: illegal base64 data at input byte 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{
				t:        t,
				response: textResponse,
			}
			req := textRequest("hi")
			tt.edit(&req)
			out, err := f.model().Infer(context.Background(), req)
			var ie *llms.InferenceError
			require.ErrorAs(t, err, &ie)
			assert.Equal(t, tt.wantStatus, ie.Status)
			assert.Equal(t, tt.wantParam, ie.Param)
			assert.EqualError(t, err, tt.wantErr)
			assert.Nil(t, out)
			assert.Empty(t, f.requests)
		})
	}
}

// The configuration checks the reasoning budget on its own too.
func TestInferenceConfig_InvalidReasoning(t *testing.T) {
	m := (&fixture{t: t, response: textResponse}).model()
	cfg, err := m.inferenceConfig(llms.InferenceRequest{
		Reasoning: &llms.InferenceReasoning{
			Effort: "extreme",
		},
	})
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, paramReasoning, ie.Param)
	assert.Nil(t, cfg)
}

// Reasoning without a signature, refusals and blocks Gemini has no part for
// are translated without a signature or dropped.
func TestInferPartsWithoutSignature(t *testing.T) {
	f := &fixture{
		t:        t,
		response: textResponse,
	}
	req := textRequest("hi")
	req.Messages = append(req.Messages, llms.InferenceMessage{
		Role: llms.InferenceRoleAssistant,
		Content: []llms.InferenceBlock{
			{
				Type:   llms.BlockReasoning,
				Opaque: json.RawMessage(`{"other":"state"}`),
			},
			{
				Type: llms.BlockType("image"),
			},
			{
				Type: llms.BlockRefusal,
				Text: "no",
			},
		},
	})
	_, err := f.model().Infer(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, f.requests, 1)
	contents := f.requests[0]["contents"].([]any)
	require.Len(t, contents, 2)
	model := contents[1].(map[string]any)
	assert.Equal(t, RoleModel, model["role"])
	parts := []any{
		map[string]any{
			"text": "no",
		},
	}
	assert.Equal(t, parts, model["parts"])
}

// A transport failure is a bad gateway.
func TestInferTransportFailure(t *testing.T) {
	client := &http.Client{
		Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}),
	}
	m, err := New(context.Background(), WithDefaultModel("fixture"), WithAPIKey("fixture"), WithHTTPClient(client))
	require.NoError(t, err)

	out, err := m.Infer(context.Background(), textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)
	assert.ErrorContains(t, err, "send googleai request: connection refused")
	assert.Nil(t, out)
}

// A successful reply without a body is a bad gateway.
func TestInferEmptyReply(t *testing.T) {
	f := &fixture{
		t: t,
	}
	out, err := f.model().Infer(context.Background(), textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)
	assert.EqualError(t, err, "decode googleai inference: empty response")
	assert.Nil(t, out)
	assert.Len(t, f.requests, 1)
}

func TestDecodeInferenceResponse_Errors(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "not JSON",
			raw:     `{`,
			wantErr: "decode googleai inference: unexpected end of JSON input",
		},
		{
			name:    "no candidate",
			raw:     `{"usageMetadata":{"promptTokenCount":1}}`,
			wantErr: "decode googleai inference: expected one candidate",
		},
		{
			name:    "two candidates",
			raw:     `{"candidates":[{"finishReason":"STOP"},{"finishReason":"STOP"}]}`,
			wantErr: "decode googleai inference: expected one candidate",
		},
		{
			name:    "function call without arguments",
			raw:     `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup"}}]},"finishReason":"STOP"}]}`,
			wantErr: "decode googleai inference: missing function arguments",
		},
		{
			name:    "part that is neither text nor a call",
			raw:     `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"AA=="}}]},"finishReason":"STOP"}]}`,
			wantErr: "decode googleai inference: unsupported content part",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := decodeInferenceResponse([]byte(tt.raw))
			var ie *llms.InferenceError
			require.ErrorAs(t, err, &ie)
			assert.Equal(t, http.StatusBadGateway, ie.Status)
			assert.EqualError(t, err, tt.wantErr)
			assert.Nil(t, out)
		})
	}
}

// A function call Gemini identified keeps its ID, and a call that ends with
// another finish reason keeps it.
func TestDecodeInferenceResponse_CallWithID(t *testing.T) {
	raw := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"fc-1","name":"lookup","args":{"q":1}}}]},"finishReason":"MAX_TOKENS"}]}`
	out, err := decodeInferenceResponse([]byte(raw))
	require.NoError(t, err)
	want := &llms.InferenceResponse{
		FinishReason: llms.FinishLength,
		Content: []llms.InferenceBlock{{
			Type:      llms.BlockFunctionCall,
			ID:        "fc-1",
			Name:      "lookup",
			Arguments: `{"q":1}`,
		}},
	}
	assert.Equal(t, want, out)
}

// Without an HTTP client of the caller's, the capture wraps the default
// transport. Nothing is sent.
func TestWrapInferenceTransport_DefaultTransport(t *testing.T) {
	m, err := New(context.Background(), WithDefaultModel("fixture"), WithAPIKey("fixture"))
	require.NoError(t, err)
	httpClient := m.client.ClientConfig().HTTPClient
	require.NotNil(t, httpClient)
	capture, ok := httpClient.Transport.(*captureTransport)
	require.True(t, ok, "transport %T", httpClient.Transport)
	assert.Equal(t, http.DefaultTransport, capture.base)
}
