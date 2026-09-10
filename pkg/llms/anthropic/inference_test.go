package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fixture captures the upstream request body and serves a canned reply.
type fixture struct {
	path   string
	body   map[string]any
	raw    string
	status int
	reply  string
	calls  int
}

func newModel(t *testing.T, f *fixture) llms.InferenceModel {
	t.Helper()
	client := &http.Client{
		Transport: transport(func(r *http.Request) (*http.Response, error) {
			f.calls++
			f.path = r.URL.Path
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			f.raw = string(raw)
			f.body = nil
			require.NoError(t, json.Unmarshal(raw, &f.body))
			status := f.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(f.reply)),
				Request: r,
			}, nil
		}),
	}
	m, err := anthropic.New(anthropic.WithModel("fixture"), anthropic.WithToken("fixture"), anthropic.WithHTTPClient(client))
	require.NoError(t, err)
	return m
}

func userRequest(text string) llms.InferenceRequest {
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

const textReply = `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":7,"cache_read_input_tokens":2,"cache_creation_input_tokens":1,"output_tokens":2}}`

func assertUnsupported(t *testing.T, err error, param string) {
	t.Helper()
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadRequest, ie.Status)
	assert.Equal(t, param, ie.Param)
}

func TestTextSamplingAndUsage(t *testing.T) {
	f := &fixture{reply: textReply}
	m := newModel(t, f)
	req := userRequest("hi")
	req.Messages = append([]llms.InferenceMessage{{
		Role:    llms.InferenceRoleSystem,
		Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "be brief"}},
	}}, req.Messages...)
	zero := 0.0
	req.Temperature = &zero
	req.Stop = []string{"END"}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "/v1/messages", f.path)
	assert.Equal(t, 1, f.calls)
	assert.Equal(t, float64(0), f.body["temperature"])
	assert.NotContains(t, f.body, "top_p")
	assert.Equal(t, float64(anthropic.DefaultMaxTokens), f.body["max_tokens"])
	assert.Equal(t, []any{"END"}, f.body["stop_sequences"])
	assert.Equal(t, "be brief", f.body["system"].([]any)[0].(map[string]any)["text"])
	assert.Len(t, f.body["messages"], 1)
	require.Len(t, out.Content, 1)
	assert.Equal(t, "hello", out.Content[0].Text)
	assert.Equal(t, llms.FinishStop, out.FinishReason)
	assert.Equal(t, "msg_1", out.UpstreamID)
	assert.True(t, out.UsageKnown)
	assert.Equal(t, llms.Usage{
		InputTokens:      10,
		OutputTokens:     2,
		CacheReadTokens:  2,
		CacheWriteTokens: 1,
		TotalTokens:      12,
	}, out.Usage)

	limit := 99
	req = userRequest("hi")
	req.MaxTokens = &limit
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.NotContains(t, f.body, "temperature")
	assert.NotContains(t, f.body, "system")
	assert.Equal(t, float64(99), f.body["max_tokens"])

	f.reply = `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"x"}],"stop_reason":"end_turn"}`
	out, err = m.Infer(context.Background(), userRequest("hi"))
	require.NoError(t, err)
	assert.False(t, out.UsageKnown)
}

func TestToolCallsAndMergedContinuation(t *testing.T) {
	f := &fixture{reply: `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"looking"},{"type":"tool_use","id":"call_1","name":"lookup","input":{"n":9007199254740993}},{"type":"tool_use","id":"call_2","name":"lookup","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`}
	m := newModel(t, f)
	strict := true
	req := userRequest("lookup twice")
	req.Tools = []llms.InferenceTool{{
		Name:        "lookup",
		Description: "look",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`),
		Strict:      &strict,
	}}
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceRequired}
	no := false
	req.ParallelToolCalls = &no
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	tool := f.body["tools"].([]any)[0].(map[string]any)
	assert.Equal(t, "lookup", tool["name"])
	assert.Equal(t, true, tool["strict"])
	assert.Equal(t, "object", tool["input_schema"].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "any", "disable_parallel_tool_use": true}, f.body["tool_choice"])
	assert.Equal(t, llms.FinishToolCalls, out.FinishReason)
	require.Len(t, out.Content, 3)
	assert.Equal(t, llms.BlockText, out.Content[0].Type)
	assert.Equal(t, "call_1", out.Content[1].ID)
	assert.Equal(t, `{"n":9007199254740993}`, out.Content[1].Arguments)

	f.reply = textReply
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: out.Content},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call_1", Text: "one"}}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call_2", Text: "two"}}},
	)
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceFunction, Name: "lookup"}
	req.ParallelToolCalls = nil
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"type": "tool", "name": "lookup"}, f.body["tool_choice"])
	msgs := f.body["messages"].([]any)
	require.Len(t, msgs, 3, "consecutive tool results merge into one user turn")
	assistant := msgs[1].(map[string]any)
	assert.Equal(t, "assistant", assistant["role"])
	assert.Equal(t, "tool_use", assistant["content"].([]any)[1].(map[string]any)["type"])
	assert.Contains(t, f.raw, `"input":{"n":9007199254740993}`)
	results := msgs[2].(map[string]any)
	assert.Equal(t, "user", results["role"])
	content := results["content"].([]any)
	require.Len(t, content, 2)
	assert.Equal(t, "call_1", content[0].(map[string]any)["tool_use_id"])
	assert.Equal(t, "two", content[1].(map[string]any)["content"])

	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceNone}
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"type": "none"}, f.body["tool_choice"])
}

func TestJSONSchemaOutput(t *testing.T) {
	f := &fixture{reply: strings.ReplaceAll(textReply, `"hello"`, `"{\"ok\":true}"`)}
	m := newModel(t, f)
	req := userRequest("answer")
	req.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "answer",
		Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`),
		Strict: true,
	}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	format := f.body["output_config"].(map[string]any)["format"].(map[string]any)
	assert.Equal(t, "json_schema", format["type"])
	assert.Equal(t, "object", format["schema"].(map[string]any)["type"])
	assert.Equal(t, `{"ok":true}`, out.Content[0].Text)
}

func TestThinkingRoundTrip(t *testing.T) {
	thinking := `{"type":"thinking","thinking":"let me see","signature":"sig-1"}`
	redacted := `{"type":"redacted_thinking","data":"blob"}`
	f := &fixture{reply: `{"id":"m","type":"message","role":"assistant","content":[` + thinking + `,` + redacted + `,{"type":"text","text":"42"}],"stop_reason":"end_turn"}`}
	m := newModel(t, f)
	req := userRequest("think")
	req.Reasoning = &llms.InferenceReasoning{Effort: llms.EffortMedium}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	budget, _ := llms.ReasoningBudget(llms.EffortMedium)
	assert.Equal(t, map[string]any{"type": "enabled", "budget_tokens": float64(budget)}, f.body["thinking"])
	assert.Equal(t, float64(budget+anthropic.DefaultMaxTokens), f.body["max_tokens"], "default max_tokens grows past the budget")
	require.Len(t, out.Content, 3)
	assert.Equal(t, llms.BlockReasoning, out.Content[0].Type)
	assert.Equal(t, "let me see", out.Content[0].Text)
	assert.JSONEq(t, thinking, string(out.Content[0].Opaque))
	assert.Equal(t, llms.BlockReasoning, out.Content[1].Type)
	assert.Empty(t, out.Content[1].Text)
	assert.JSONEq(t, redacted, string(out.Content[1].Opaque))

	// Replay both blocks verbatim; a reasoning block without state is dropped.
	f.reply = textReply
	explicit := 3000
	limit := 8000
	req.Reasoning = &llms.InferenceReasoning{BudgetTokens: &explicit}
	req.MaxTokens = &limit
	history := append(out.Content, llms.InferenceBlock{Type: llms.BlockReasoning, Text: "summary only"})
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: history},
		llms.InferenceMessage{Role: llms.InferenceRoleUser, Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "and?"}}},
	)
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, float64(3000), f.body["thinking"].(map[string]any)["budget_tokens"])
	assert.Equal(t, float64(8000), f.body["max_tokens"])
	msgs := f.body["messages"].([]any)
	require.Len(t, msgs, 3)
	blocks := msgs[1].(map[string]any)["content"].([]any)
	require.Len(t, blocks, 3)
	var wantThinking, wantRedacted map[string]any
	require.NoError(t, json.Unmarshal([]byte(thinking), &wantThinking))
	require.NoError(t, json.Unmarshal([]byte(redacted), &wantRedacted))
	assert.Equal(t, wantThinking, blocks[0])
	assert.Equal(t, wantRedacted, blocks[1])
	assert.Equal(t, "text", blocks[2].(map[string]any)["type"])
}

func TestThinkingRestrictions(t *testing.T) {
	m := newModel(t, &fixture{reply: textReply})
	one := 1.0
	small := 100

	req := userRequest("x")
	req.Reasoning = &llms.InferenceReasoning{}
	assertUnsupported(t, m.ValidateInference(req), "reasoning")

	req.Reasoning = &llms.InferenceReasoning{Effort: llms.EffortLow}
	req.Temperature = &one
	assertUnsupported(t, m.ValidateInference(req), "temperature")
	req.Temperature = nil
	req.TopP = &one
	assertUnsupported(t, m.ValidateInference(req), "top_p")
	req.TopP = nil
	req.Tools = []llms.InferenceTool{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceRequired}
	assertUnsupported(t, m.ValidateInference(req), "tool_choice")
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceAuto}
	req.MaxTokens = &small
	assertUnsupported(t, m.ValidateInference(req), "max_tokens")
	req.MaxTokens = nil
	require.NoError(t, m.ValidateInference(req))
}

func TestRejectedControls(t *testing.T) {
	m := newModel(t, &fixture{reply: textReply})
	seed := int64(1)
	req := userRequest("x")
	req.Seed = &seed
	assertUnsupported(t, m.ValidateInference(req), "seed")

	req = userRequest("x")
	hot := 1.5
	req.Temperature = &hot
	assertUnsupported(t, m.ValidateInference(req), "temperature")

	req = userRequest("x")
	half := 0.5
	req.Temperature = &half
	req.TopP = &half
	assertUnsupported(t, m.ValidateInference(req), "temperature+top_p")

	req = userRequest("x")
	req.Format = &llms.InferenceFormat{Type: llms.FormatJSONObject}
	assertUnsupported(t, m.ValidateInference(req), "response_format.json_object")

	req = userRequest("x")
	req.Messages[0].Role = llms.InferenceRoleDeveloper
	assertUnsupported(t, m.ValidateInference(req), "messages.role.developer")
}

func TestStopReasonsAndErrors(t *testing.T) {
	f := &fixture{reply: `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"partial"}],"stop_reason":"max_tokens"}`}
	m := newModel(t, f)
	out, err := m.Infer(context.Background(), userRequest("long"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishLength, out.FinishReason)

	f.reply = `{"id":"m","type":"message","role":"assistant","content":[],"stop_reason":"refusal"}`
	out, err = m.Infer(context.Background(), userRequest("bad"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishContentFilter, out.FinishReason)
	assert.Empty(t, out.Content)

	f.reply = `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"x"}],"stop_reason":"stop_sequence","stop_sequence":"END"}`
	out, err = m.Infer(context.Background(), userRequest("x"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishStop, out.FinishReason)

	f.reply = `{"id":"m","type":"message","role":"assistant","content":[{"type":"server_tool_use","id":"x"}],"stop_reason":"end_turn"}`
	_, err = m.Infer(context.Background(), userRequest("x"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)

	f.status = http.StatusTooManyRequests
	f.reply = `{"type":"error","error":{"type":"rate_limit_error","message":"internal secret"}}`
	f.calls = 0
	_, err = m.Infer(context.Background(), userRequest("x"))
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusTooManyRequests, ie.Status)
	assert.Equal(t, 1, f.calls, "exactly one attempt")
}
