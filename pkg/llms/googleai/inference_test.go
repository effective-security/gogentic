package googleai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	t        *testing.T
	status   int
	response string
	requests []map[string]any
	bodies   []string
}

func (f *fixture) model() *GoogleAI {
	client := &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
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
	m, err := New(context.Background(), WithDefaultModel("fixture"), WithAPIKey("fixture"), WithHTTPClient(client))
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
