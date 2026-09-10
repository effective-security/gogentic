package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
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
	paths    []string
}

func (f *fixture) model() *LLM {
	client := &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			require.NoError(f.t, err)
			f.bodies = append(f.bodies, string(body))
			f.paths = append(f.paths, r.URL.Path)
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
	runtime := bedrockruntime.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("fixture", "fixture", ""),
		HTTPClient:  client,
	})
	m, err := New(WithClient(runtime), WithModel("fixture"), WithConverse())
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

const textResponse = `{"output":{"message":{"role":"assistant","content":[{"text":"hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":7,"cacheReadInputTokens":2,"cacheWriteInputTokens":1,"outputTokens":2,"totalTokens":12},"metrics":{"latencyMs":1}}`

func TestInferTextAndUsage(t *testing.T) {
	f := &fixture{t: t, response: textResponse}
	m := f.model()
	zero := 0.0
	req := textRequest("hi")
	req.Temperature = &zero
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, llms.FinishStop, out.FinishReason)
	assert.Equal(t, "hello", out.Content[0].Text)
	require.True(t, out.UsageKnown)
	assert.EqualValues(t, 10, out.Usage.InputTokens, "input includes cache reads and writes")
	assert.EqualValues(t, 2, out.Usage.CacheReadTokens)
	assert.EqualValues(t, 1, out.Usage.CacheWriteTokens)
	assert.EqualValues(t, 12, out.Usage.TotalTokens)
	assert.Contains(t, f.paths[0], "/converse")
	cfg := f.requests[0]["inferenceConfig"].(map[string]any)
	assert.Equal(t, float64(0), cfg["temperature"])
	assert.NotContains(t, cfg, "topP")
}

func TestInferToolCallAndParallelContinuation(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"call_1","name":"lookup","input":{"n":9007199254740993}}}]}},"stopReason":"tool_use"}`}
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
	assert.Equal(t, "call_1", out.Content[0].ID)
	assert.Equal(t, `{"n":9007199254740993}`, out.Content[0].Arguments)
	assert.False(t, out.UsageKnown)
	choice := f.requests[0]["toolConfig"].(map[string]any)["toolChoice"].(map[string]any)
	assert.Equal(t, "lookup", choice["tool"].(map[string]any)["name"])

	f.response = textResponse
	req.ToolChoice = nil
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: []llms.InferenceBlock{
			out.Content[0],
			{Type: llms.BlockFunctionCall, ID: "call_2", Name: "lookup", Arguments: `{"n":2}`},
		}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call_1", Name: "lookup", Text: "a"}}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call_2", Name: "lookup", Text: "b"}}},
	)
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, f.bodies[1], "9007199254740993")
	messages := f.requests[1]["messages"].([]any)
	require.Len(t, messages, 3, "tool results share one user turn")
	last := messages[2].(map[string]any)
	assert.Equal(t, "user", last["role"])
	content := last["content"].([]any)
	require.Len(t, content, 2)
	assert.Equal(t, "call_1", content[0].(map[string]any)["toolResult"].(map[string]any)["toolUseId"])
	assert.Equal(t, "call_2", content[1].(map[string]any)["toolResult"].(map[string]any)["toolUseId"])
}

func TestInferJSONSchemaAndSystem(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"text":"{\"ok\":true}"}]}},"stopReason":"end_turn"}`}
	m := f.model()
	req := textRequest("answer")
	req.Messages = append([]llms.InferenceMessage{{
		Role:    llms.InferenceRoleSystem,
		Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "be brief"}},
	}}, req.Messages...)
	req.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "answer",
		Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
	}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, out.Content[0].Text)
	format := f.requests[0]["outputConfig"].(map[string]any)["textFormat"].(map[string]any)
	assert.Equal(t, "json_schema", format["type"])
	assert.Equal(t, "answer", format["structure"].(map[string]any)["jsonSchema"].(map[string]any)["name"])
	assert.Len(t, f.requests[0]["system"].([]any), 1)
	assert.Len(t, f.requests[0]["messages"].([]any), 1)
}

func TestInferReasoningRoundTripAndThinkingConfig(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"reasoningContent":{"reasoningText":{"text":"thinking...","signature":"sig"}}},{"reasoningContent":{"redactedContent":"AQID"}},{"text":"hello"}]}},"stopReason":"end_turn"}`}
	m := f.model()
	req := textRequest("think")
	req.Reasoning = &llms.InferenceReasoning{Effort: llms.EffortMedium}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, out.Content, 3)
	assert.Equal(t, llms.BlockReasoning, out.Content[0].Type)
	assert.Equal(t, "thinking...", out.Content[0].Text)
	assert.JSONEq(t, `{"reasoningText":{"text":"thinking...","signature":"sig"}}`, string(out.Content[0].Opaque))
	assert.Equal(t, llms.BlockReasoning, out.Content[1].Type)
	assert.JSONEq(t, `{"redactedContent":"AQID"}`, string(out.Content[1].Opaque))
	assert.Equal(t, llms.BlockText, out.Content[2].Type)
	thinking := f.requests[0]["additionalModelRequestFields"].(map[string]any)["thinking"].(map[string]any)
	assert.Equal(t, "enabled", thinking["type"])
	assert.Equal(t, float64(8192), thinking["budget_tokens"])

	// Replay with a follow-up user turn; reasoning blocks return at their position.
	for i := range out.Content {
		out.Content[i].Source = "target"
	}
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: out.Content},
		llms.InferenceMessage{Role: llms.InferenceRoleUser, Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "more"}}},
	)
	f.response = textResponse
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assistant := f.requests[1]["messages"].([]any)[1].(map[string]any)
	content := assistant["content"].([]any)
	require.Len(t, content, 3)
	reasoning := content[0].(map[string]any)["reasoningContent"].(map[string]any)["reasoningText"].(map[string]any)
	assert.Equal(t, "thinking...", reasoning["text"])
	assert.Equal(t, "sig", reasoning["signature"])
	assert.Equal(t, "AQID", content[1].(map[string]any)["reasoningContent"].(map[string]any)["redactedContent"])
	assert.Contains(t, content[2].(map[string]any), "text")
}

func TestInferFinishReasonsAndErrors(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"text":"partial"}]}},"stopReason":"max_tokens"}`}
	m := f.model()
	out, err := m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishLength, out.FinishReason)

	f.response = `{"output":{"message":{"role":"assistant","content":[]}},"stopReason":"guardrail_intervened"}`
	out, err = m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishContentFilter, out.FinishReason)

	f.response = `{"output":{"message":{"role":"assistant","content":[]}},"stopReason":"mystery"}`
	_, err = m.Infer(context.Background(), textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)

	f.status = http.StatusTooManyRequests
	f.response = `{"message":"secret throttle"}`
	_, err = m.Infer(context.Background(), textRequest("hi"))
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusTooManyRequests, ie.Status)
	assert.Len(t, f.requests, 4, "exactly one attempt per call")
}

func TestValidateInferenceRejections(t *testing.T) {
	m := (&fixture{t: t, response: textResponse}).model()
	yes := true
	high := 1.5
	half := 0.5
	seed := int64(1)
	small := 100
	cases := map[string]llms.InferenceRequest{
		"temperature":                 {Temperature: &high},
		"parallel_tool_calls":         {ParallelToolCalls: &yes},
		"tool_choice.none":            {ToolChoice: &llms.InferenceToolChoice{Mode: llms.ToolChoiceNone}},
		"response_format.json_object": {Format: &llms.InferenceFormat{Type: llms.FormatJSONObject}},
		"seed":                        {Seed: &seed},
		"messages.role.developer":     {Messages: []llms.InferenceMessage{{Role: llms.InferenceRoleDeveloper}}},
		"reasoning":                   {Reasoning: &llms.InferenceReasoning{Effort: "extreme"}},
		"top_p":                       {Reasoning: &llms.InferenceReasoning{Effort: llms.EffortLow}, TopP: &half},
		"max_tokens":                  {Reasoning: &llms.InferenceReasoning{Effort: llms.EffortLow}, MaxTokens: &small},
	}
	for param, req := range cases {
		t.Run(param, func(t *testing.T) {
			err := m.ValidateInference(req)
			var ie *llms.InferenceError
			require.ErrorAs(t, err, &ie)
			assert.Equal(t, param, ie.Param)
		})
	}
	legacy, err := New(WithClient(bedrockruntime.NewFromConfig(aws.Config{Region: "us-east-1"})), WithModel("fixture"))
	require.NoError(t, err)
	err = legacy.ValidateInference(textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, "provider.converse", ie.Param)
}
