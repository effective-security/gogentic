package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/openai"
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

func (f *fixture) client(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{
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
}

func newModel(t *testing.T, f *fixture, opts ...openai.Option) llms.InferenceModel {
	t.Helper()
	opts = append([]openai.Option{
		openai.WithModel("fixture"),
		openai.WithToken("fixture"),
		openai.WithHTTPClient(f.client(t)),
	}, opts...)
	m, err := openai.New(opts...)
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

func lookupTool() llms.InferenceTool {
	return llms.InferenceTool{
		Name:       "lookup",
		Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

const responsesTextReply = `{"id":"resp_1","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":1}}}`

const chatTextReply = `{"id":"chatcmpl_1","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":1}}}`

func TestUpstreamSelectionAndStore(t *testing.T) {
	cases := []struct {
		name     string
		opts     []openai.Option
		path     string
		hasStore bool
	}{
		{"openai default responses", nil, "/v1/responses", true},
		{"openai explicit chat", []openai.Option{openai.WithInferenceAPI(openai.InferenceAPIChat)}, "/v1/chat/completions", false},
		{"azure default chat", []openai.Option{openai.WithProvider(llms.ProviderAzure), openai.WithBaseURL("https://azure.example/v1"), openai.WithAPIVersion("2024-10-21")}, "/v1/chat/completions", false},
		{"openrouter default chat", []openai.Option{openai.WithProvider(llms.ProviderOpenRouter), openai.WithBaseURL("https://router.example/v1")}, "/v1/chat/completions", false},
		{"openrouter explicit responses no store", []openai.Option{openai.WithProvider(llms.ProviderOpenRouter), openai.WithBaseURL("https://router.example/v1"), openai.WithInferenceAPI(openai.InferenceAPIResponses)}, "/v1/responses", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fixture{reply: chatTextReply}
			if strings.HasSuffix(tc.path, "responses") {
				f.reply = responsesTextReply
			}
			m := newModel(t, f, tc.opts...)
			out, err := m.Infer(context.Background(), userRequest("hi"))
			require.NoError(t, err)
			assert.Equal(t, tc.path, f.path)
			assert.Equal(t, 1, f.calls)
			assert.Equal(t, false, f.body["stream"])
			_, hasStore := f.body["store"]
			assert.Equal(t, tc.hasStore, hasStore)
			if hasStore {
				assert.Equal(t, false, f.body["store"])
			}
			require.Len(t, out.Content, 1)
			assert.Equal(t, llms.BlockText, out.Content[0].Type)
			assert.Equal(t, "hello", out.Content[0].Text)
			assert.Equal(t, llms.FinishStop, out.FinishReason)
			assert.True(t, out.UsageKnown)
			assert.Equal(t, llms.Usage{
				InputTokens:     10,
				OutputTokens:    2,
				TotalTokens:     12,
				CacheReadTokens: 3,
				ReasoningTokens: 1,
			}, out.Usage)
		})
	}
}

func TestSamplingPresence(t *testing.T) {
	for _, api := range []openai.InferenceAPI{openai.InferenceAPIResponses, openai.InferenceAPIChat} {
		t.Run(string(api), func(t *testing.T) {
			f := &fixture{reply: responsesTextReply}
			if api == openai.InferenceAPIChat {
				f.reply = chatTextReply
			}
			m := newModel(t, f, openai.WithInferenceAPI(api))
			req := userRequest("hi")
			zero := 0.0
			limit := 77
			req.Temperature = &zero
			req.MaxTokens = &limit
			_, err := m.Infer(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, float64(0), f.body["temperature"])
			assert.NotContains(t, f.body, "top_p")
			if api == openai.InferenceAPIChat {
				assert.Equal(t, float64(77), f.body["max_completion_tokens"])
				assert.NotContains(t, f.body, "max_tokens")
			} else {
				assert.Equal(t, float64(77), f.body["max_output_tokens"])
			}

			_, err = m.Infer(context.Background(), userRequest("hi"))
			require.NoError(t, err)
			assert.NotContains(t, f.body, "temperature")
			assert.NotContains(t, f.body, "max_output_tokens")
			assert.NotContains(t, f.body, "max_completion_tokens")
		})
	}
}

func TestResponsesToolCallAndContinuation(t *testing.T) {
	f := &fixture{reply: `{"id":"resp_2","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"n\":9007199254740993}","status":"completed"},{"type":"function_call","id":"fc_2","call_id":"call_2","name":"lookup","arguments":"{}","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`}
	m := newModel(t, f)
	req := userRequest("lookup")
	req.Tools = []llms.InferenceTool{lookupTool()}
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceFunction, Name: "lookup"}
	yes := true
	req.ParallelToolCalls = &yes
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	tools := f.body["tools"].([]any)
	require.Len(t, tools, 1)
	assert.Equal(t, "function", tools[0].(map[string]any)["type"])
	assert.Equal(t, "lookup", tools[0].(map[string]any)["name"])
	assert.Equal(t, map[string]any{"type": "function", "name": "lookup"}, f.body["tool_choice"])
	assert.Equal(t, true, f.body["parallel_tool_calls"])
	assert.Equal(t, llms.FinishToolCalls, out.FinishReason, "completed with calls normalizes to tool_calls")
	require.Len(t, out.Content, 2)
	assert.Equal(t, llms.BlockFunctionCall, out.Content[0].Type)
	assert.Equal(t, "call_1", out.Content[0].ID)
	assert.Equal(t, `{"n":9007199254740993}`, out.Content[0].Arguments, "arguments are never re-encoded")

	f.reply = responsesTextReply
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: out.Content},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{
			{Type: llms.BlockFunctionOutput, ID: "call_1", Name: "lookup", Text: "one"},
			{Type: llms.BlockFunctionOutput, ID: "call_2", Name: "lookup", Text: "two"},
		}},
	)
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceAuto}
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "auto", f.body["tool_choice"])
	items := f.body["input"].([]any)
	require.Len(t, items, 5)
	assert.Equal(t, "message", items[0].(map[string]any)["type"])
	assert.Equal(t, "input_text", items[0].(map[string]any)["content"].([]any)[0].(map[string]any)["type"])
	assert.Equal(t, "function_call", items[1].(map[string]any)["type"])
	assert.Equal(t, "call_1", items[1].(map[string]any)["call_id"])
	assert.Contains(t, f.raw, `9007199254740993`)
	assert.Equal(t, "function_call_output", items[3].(map[string]any)["type"])
	assert.Equal(t, "call_1", items[3].(map[string]any)["call_id"])
	assert.Equal(t, "one", items[3].(map[string]any)["output"])
	assert.Equal(t, "two", items[4].(map[string]any)["output"])
}

func TestChatToolCallAndContinuation(t *testing.T) {
	f := &fixture{reply: `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}},{"id":"call_2","type":"function","function":{"name":"lookup","arguments":"{\"a\":1}"}}]},"finish_reason":"tool_calls"}]}`}
	m := newModel(t, f, openai.WithInferenceAPI(openai.InferenceAPIChat))
	req := userRequest("lookup")
	req.Tools = []llms.InferenceTool{lookupTool()}
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceFunction, Name: "lookup"}
	seed := int64(42)
	req.Seed = &seed
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, float64(42), f.body["seed"])
	assert.Equal(t, "lookup", f.body["tool_choice"].(map[string]any)["function"].(map[string]any)["name"])
	assert.Equal(t, "lookup", f.body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"])
	assert.False(t, out.UsageKnown)
	assert.Equal(t, llms.FinishToolCalls, out.FinishReason)
	require.Len(t, out.Content, 2)
	assert.Equal(t, "call_2", out.Content[1].ID)
	assert.Equal(t, `{"a":1}`, out.Content[1].Arguments)

	f.reply = chatTextReply
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: out.Content},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{
			{Type: llms.BlockFunctionOutput, ID: "call_1", Text: "one"},
			{Type: llms.BlockFunctionOutput, ID: "call_2", Text: "two"},
		}},
	)
	req.ToolChoice = nil
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	msgs := f.body["messages"].([]any)
	require.Len(t, msgs, 4)
	assistant := msgs[1].(map[string]any)
	assert.Nil(t, assistant["content"])
	assert.Len(t, assistant["tool_calls"], 2)
	assert.Equal(t, "call_1", msgs[2].(map[string]any)["tool_call_id"])
	assert.Equal(t, "two", msgs[3].(map[string]any)["content"])
	assert.NotContains(t, f.body, "tool_choice")
}

func TestJSONSchemaFormat(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`)
	for _, api := range []openai.InferenceAPI{openai.InferenceAPIResponses, openai.InferenceAPIChat} {
		t.Run(string(api), func(t *testing.T) {
			f := &fixture{reply: strings.ReplaceAll(responsesTextReply, `"hello"`, `"{\"ok\":true}"`)}
			if api == openai.InferenceAPIChat {
				f.reply = strings.ReplaceAll(chatTextReply, `"hello"`, `"{\"ok\":true}"`)
			}
			m := newModel(t, f, openai.WithInferenceAPI(api))
			req := userRequest("answer")
			req.Format = &llms.InferenceFormat{
				Type:   llms.FormatJSONSchema,
				Name:   "answer",
				Schema: schema,
				Strict: true,
			}
			out, err := m.Infer(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, `{"ok":true}`, out.Content[0].Text)
			var format map[string]any
			if api == openai.InferenceAPIChat {
				rf := f.body["response_format"].(map[string]any)
				assert.Equal(t, "json_schema", rf["type"])
				format = rf["json_schema"].(map[string]any)
			} else {
				format = f.body["text"].(map[string]any)["format"].(map[string]any)
				assert.Equal(t, "json_schema", format["type"])
			}
			assert.Equal(t, "answer", format["name"])
			assert.Equal(t, true, format["strict"])
			assert.Equal(t, "object", format["schema"].(map[string]any)["type"])
		})
	}
}

func TestResponsesReasoningRoundTrip(t *testing.T) {
	reasoningItem := `{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"thinking "},{"type":"summary_text","text":"hard"}],"encrypted_content":"opaque-bytes"}`
	f := &fixture{reply: `{"id":"resp_3","status":"completed","output":[` + reasoningItem + `,{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"42","annotations":[]}]}]}`}
	m := newModel(t, f)
	req := userRequest("think")
	req.Reasoning = &llms.InferenceReasoning{Effort: llms.EffortHigh}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"effort": "high", "summary": "auto"}, f.body["reasoning"])
	assert.Equal(t, []any{"reasoning.encrypted_content"}, f.body["include"])
	require.Len(t, out.Content, 2)
	assert.Equal(t, llms.BlockReasoning, out.Content[0].Type)
	assert.Equal(t, "thinking hard", out.Content[0].Text)
	assert.JSONEq(t, reasoningItem, string(out.Content[0].Opaque))
	assert.False(t, out.UsageKnown)

	// Replay: the raw item goes back verbatim even without reasoning controls.
	req.Reasoning = nil
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: out.Content},
		llms.InferenceMessage{Role: llms.InferenceRoleUser, Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "and?"}}},
	)
	f.reply = responsesTextReply
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.NotContains(t, f.body, "reasoning")
	assert.Equal(t, []any{"reasoning.encrypted_content"}, f.body["include"])
	items := f.body["input"].([]any)
	require.Len(t, items, 4)
	var replayed map[string]any
	require.NoError(t, json.Unmarshal([]byte(reasoningItem), &replayed))
	assert.Equal(t, replayed, items[1])
	assert.Equal(t, "output_text", items[2].(map[string]any)["content"].([]any)[0].(map[string]any)["type"])

	// Chat upstream has no reasoning slot: effort is sent, history dropped, a budget maps to effort.
	f.reply = chatTextReply
	chat := newModel(t, f, openai.WithInferenceAPI(openai.InferenceAPIChat))
	req.Reasoning = &llms.InferenceReasoning{Effort: llms.EffortLow}
	out, err = chat.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "low", f.body["reasoning_effort"])
	assert.Len(t, f.body["messages"], 3)
	assert.Equal(t, "42", f.body["messages"].([]any)[1].(map[string]any)["content"])
	for _, b := range out.Content {
		assert.NotEqual(t, llms.BlockReasoning, b.Type)
	}
	budget := 8192
	req.Reasoning = &llms.InferenceReasoning{BudgetTokens: &budget}
	_, err = chat.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, string(llms.EffortMedium), f.body["reasoning_effort"], "a budget without effort maps to the closest effort")
	req.Reasoning = &llms.InferenceReasoning{}
	_, err = chat.Infer(context.Background(), req)
	assertUnsupported(t, err, "reasoning.budget_tokens")
}

func TestRefusalLengthAndStatuses(t *testing.T) {
	f := &fixture{reply: `{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","status":"incomplete","content":[{"type":"output_text","text":"partial","annotations":[]}]}]}`}
	m := newModel(t, f)
	out, err := m.Infer(context.Background(), userRequest("long"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishLength, out.FinishReason)

	f.reply = `{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"no"}]}]}`
	out, err = m.Infer(context.Background(), userRequest("bad"))
	require.NoError(t, err)
	require.Len(t, out.Content, 1)
	assert.Equal(t, llms.BlockRefusal, out.Content[0].Type)
	assert.Equal(t, "no", out.Content[0].Text)

	f.reply = `{"id":"r","status":"failed","error":{"code":"server_error","message":"internal secret"},"output":[]}`
	_, err = m.Infer(context.Background(), userRequest("x"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)

	chat := newModel(t, f, openai.WithInferenceAPI(openai.InferenceAPIChat))
	f.reply = `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"","refusal":"no"},"finish_reason":"length"}]}`
	out, err = chat.Infer(context.Background(), userRequest("x"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishLength, out.FinishReason)
	require.Len(t, out.Content, 1)
	assert.Equal(t, llms.BlockRefusal, out.Content[0].Type)
	f.reply = `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"content_filter"}]}`
	out, err = chat.Infer(context.Background(), userRequest("x"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishContentFilter, out.FinishReason)
}

func TestUpstreamErrorStatus(t *testing.T) {
	for _, api := range []openai.InferenceAPI{openai.InferenceAPIResponses, openai.InferenceAPIChat} {
		t.Run(string(api), func(t *testing.T) {
			f := &fixture{
				status: http.StatusTooManyRequests,
				reply:  `{"error":{"type":"rate_limit_error","message":"internal secret"}}`,
			}
			m := newModel(t, f, openai.WithInferenceAPI(api))
			_, err := m.Infer(context.Background(), userRequest("hi"))
			var ie *llms.InferenceError
			require.ErrorAs(t, err, &ie)
			assert.Equal(t, http.StatusTooManyRequests, ie.Status)
			assert.Equal(t, 1, f.calls, "exactly one attempt")
		})
	}
}

func TestValidateInferenceRejections(t *testing.T) {
	responsesModel := newModel(t, &fixture{reply: responsesTextReply})
	chatModel := newModel(t, &fixture{reply: chatTextReply}, openai.WithInferenceAPI(openai.InferenceAPIChat))

	req := userRequest("hi")
	req.Stop = []string{"END"}
	assertUnsupported(t, responsesModel.ValidateInference(req), "stop")
	require.NoError(t, chatModel.ValidateInference(req))

	req = userRequest("hi")
	seed := int64(1)
	req.Seed = &seed
	assertUnsupported(t, responsesModel.ValidateInference(req), "seed")
	require.NoError(t, chatModel.ValidateInference(req))

	req = userRequest("hi")
	req.Messages = append(req.Messages, llms.InferenceMessage{
		Role: llms.InferenceRoleAssistant,
		Content: []llms.InferenceBlock{
			{Type: llms.BlockFunctionCall, ID: "c", Name: "lookup", Arguments: "{}"},
			{Type: llms.BlockText, Text: "after"},
		},
	})
	assertUnsupported(t, chatModel.ValidateInference(req), "messages.content.order")
	require.NoError(t, responsesModel.ValidateInference(req))

	for _, provider := range []llms.ProviderType{llms.ProviderOpenAI, llms.ProviderAzure, llms.ProviderAzureAD, llms.ProviderOpenRouter, llms.ProviderPerplexity, llms.ProviderOpenAIBedrock} {
		opts := []openai.Option{openai.WithProvider(provider), openai.WithBaseURL("https://example.test/v1")}
		switch provider {
		case llms.ProviderAzure, llms.ProviderAzureAD:
			opts = append(opts, openai.WithAPIVersion("2024-10-21"))
		case llms.ProviderOpenAIBedrock:
			opts = append(opts, openai.WithAWSConfig(&aws.Config{Region: "us-east-1"}))
		}
		m := newModel(t, &fixture{}, opts...)
		assert.NoError(t, m.ValidateInference(userRequest("hi")), string(provider))
	}
}

func assertUnsupported(t *testing.T, err error, param string) {
	t.Helper()
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadRequest, ie.Status)
	assert.Equal(t, param, ie.Param)
	assert.True(t, errors.Is(err, ie))
}
