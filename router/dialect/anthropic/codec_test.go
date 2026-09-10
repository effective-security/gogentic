package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	lenient = dialect.DecodeOptions{}
	strict  = dialect.DecodeOptions{Strict: true}
)

func kindAndParam(t *testing.T, err error) (router.Kind, string) {
	t.Helper()
	require.Error(t, err)
	re := router.AsError(err)
	return re.Kind, re.Param
}

func TestDecodeFullRequest(t *testing.T) {
	body := `{
		"model":"public","max_tokens":256,"temperature":0,"top_p":0.5,"stop_sequences":["END"],
		"system":[{"type":"text","text":"Be brief.","cache_control":{"type":"ephemeral"}}],
		"messages":[
			{"role":"user","content":"lookup two things"},
			{"role":"assistant","content":[
				{"type":"text","text":"Looking up."},
				{"type":"tool_use","id":"call_1","name":"lookup","input":{"n":9007199254740993}},
				{"type":"tool_use","id":"call_2","name":"lookup","input":{"n":2}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"call_1","content":"one"},
				{"type":"tool_result","tool_use_id":"call_2","content":[{"type":"text","text":"tw"},{"type":"text","text":"o"}],"is_error":true},
				{"type":"text","text":"thanks"}
			]}
		],
		"tools":[{"name":"lookup","description":"d","input_schema":{"type":"object","properties":{"n":{"type":"integer"}}},"strict":true},{"type":"custom","name":"noargs"}],
		"tool_choice":{"type":"tool","name":"lookup","disable_parallel_tool_use":true},
		"metadata":{"user_id":"u1"},
		"stream":false,"service_tier":"auto","mcp_servers":[],"betas":[]
	}`
	req, err := DecodeMessages([]byte(body), lenient)
	require.NoError(t, err)
	assert.Equal(t, "public", req.Model)
	require.NotNil(t, req.Input.MaxTokens)
	assert.Equal(t, 256, *req.Input.MaxTokens)
	require.NotNil(t, req.Input.Temperature)
	assert.Equal(t, float64(0), *req.Input.Temperature)
	assert.Equal(t, 0.5, *req.Input.TopP)
	assert.Equal(t, []string{"END"}, req.Input.Stop)
	assert.Equal(t, map[string]string{dialect.MetadataUser: "u1"}, req.Metadata)

	msgs := req.Input.Messages
	require.Len(t, msgs, 5)
	assert.Equal(t, llms.InferenceRoleSystem, msgs[0].Role)
	assert.Equal(t, "Be brief.", msgs[0].Content[0].Text)
	assert.Equal(t, llms.InferenceRoleUser, msgs[1].Role)
	assert.Equal(t, llms.InferenceRoleAssistant, msgs[2].Role)
	require.Len(t, msgs[2].Content, 3)
	assert.Equal(t, llms.BlockText, msgs[2].Content[0].Type)
	assert.Equal(t, llms.BlockFunctionCall, msgs[2].Content[1].Type)
	assert.Equal(t, "call_1", msgs[2].Content[1].ID)
	assert.Equal(t, "lookup", msgs[2].Content[1].Name)
	assert.Equal(t, `{"n":9007199254740993}`, msgs[2].Content[1].Arguments, "raw argument bytes preserved")
	assert.Equal(t, llms.InferenceRoleTool, msgs[3].Role)
	require.Len(t, msgs[3].Content, 2)
	assert.Equal(t, llms.BlockFunctionOutput, msgs[3].Content[0].Type)
	assert.Equal(t, "call_1", msgs[3].Content[0].ID)
	assert.Equal(t, "one", msgs[3].Content[0].Text)
	assert.Equal(t, "call_2", msgs[3].Content[1].ID)
	assert.Equal(t, "two", msgs[3].Content[1].Text)
	assert.Equal(t, llms.InferenceRoleUser, msgs[4].Role)
	assert.Equal(t, "thanks", msgs[4].Content[0].Text)

	require.Len(t, req.Input.Tools, 2)
	assert.Equal(t, "lookup", req.Input.Tools[0].Name)
	assert.Equal(t, "d", req.Input.Tools[0].Description)
	assert.JSONEq(t, `{"type":"object","properties":{"n":{"type":"integer"}}}`, string(req.Input.Tools[0].Parameters))
	require.NotNil(t, req.Input.Tools[0].Strict)
	assert.True(t, *req.Input.Tools[0].Strict)
	assert.JSONEq(t, emptyObjectSchema, string(req.Input.Tools[1].Parameters))
	assert.Nil(t, req.Input.Tools[1].Strict)

	require.NotNil(t, req.Input.ToolChoice)
	assert.Equal(t, llms.ToolChoiceFunction, req.Input.ToolChoice.Mode)
	assert.Equal(t, "lookup", req.Input.ToolChoice.Name)
	require.NotNil(t, req.Input.ParallelToolCalls)
	assert.False(t, *req.Input.ParallelToolCalls)
	assert.Nil(t, req.Input.Reasoning)
	assert.Nil(t, req.Input.Format)
	assert.Nil(t, req.Input.Seed)
}

func TestDecodeThinkingAndOutputConfig(t *testing.T) {
	req, err := DecodeMessages([]byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":2048},"output_config":{"format":{"type":"json_schema","schema":{"type":"object","properties":{}}}}}`), strict)
	require.NoError(t, err)
	require.NotNil(t, req.Input.Reasoning)
	require.NotNil(t, req.Input.Reasoning.BudgetTokens)
	assert.Equal(t, 2048, *req.Input.Reasoning.BudgetTokens)
	assert.Empty(t, req.Input.Reasoning.Effort)
	require.NotNil(t, req.Input.Format)
	assert.Equal(t, llms.FormatJSONSchema, req.Input.Format.Type)
	assert.Equal(t, defaultFormatName, req.Input.Format.Name)
	assert.True(t, req.Input.Format.Strict)
	assert.JSONEq(t, `{"type":"object","properties":{}}`, string(req.Input.Format.Schema))

	req, err = DecodeMessages([]byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high","format":{"type":"json_schema","name":"answer","schema":{"type":"object"}}}}`), strict)
	require.NoError(t, err)
	assert.Equal(t, llms.EffortMedium, req.Input.Reasoning.Effort, "explicit thinking wins over output_config.effort")
	assert.Equal(t, "answer", req.Input.Format.Name)

	req, err = DecodeMessages([]byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"},"output_config":{"effort":"low"}}`), strict)
	require.NoError(t, err)
	require.NotNil(t, req.Input.Reasoning)
	assert.Equal(t, llms.EffortLow, req.Input.Reasoning.Effort)

	_, err = DecodeMessages([]byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"text"}}}`), lenient)
	kind, param := kindAndParam(t, err)
	assert.Equal(t, router.KindUnsupported, kind)
	assert.Equal(t, "output_config.format.type", param)
}

func TestReasoningRoundTrip(t *testing.T) {
	res := &router.Result{
		ID:    "resp_123",
		Model: "public",
		Response: llms.InferenceResponse{
			FinishReason: llms.FinishToolCalls,
			Content: []llms.InferenceBlock{
				{Type: llms.BlockReasoning, Text: "thinking hard", Opaque: json.RawMessage(`{"signature":"sig1"}`)},
				{Type: llms.BlockReasoning, Opaque: json.RawMessage(`{"data":"redacted"}`)},
				{Type: llms.BlockReasoning, Text: "summary only"},
				{Type: llms.BlockText, Text: "Calling."},
				{Type: llms.BlockFunctionCall, ID: "call_1", Name: "lookup", Arguments: `{"n":1}`},
			},
			UsageKnown: true,
			Usage:      llms.Usage{InputTokens: 10, CacheReadTokens: 2, CacheWriteTokens: 1, OutputTokens: 4, TotalTokens: 14},
		},
	}
	out, err := EncodeMessages(res, dialect.EncodeOptions{})
	require.NoError(t, err)
	var msg map[string]any
	require.NoError(t, json.Unmarshal(out, &msg))
	assert.Equal(t, "msg_123", msg["id"])
	assert.Equal(t, "tool_use", msg["stop_reason"])
	assert.Nil(t, msg["stop_sequence"])
	content := msg["content"].([]any)
	require.Len(t, content, 5)
	assert.Equal(t, blockThinking, content[0].(map[string]any)["type"])
	assert.Equal(t, blockRedactedThinking, content[1].(map[string]any)["type"])
	assert.Equal(t, blockThinking, content[2].(map[string]any)["type"])
	assert.Equal(t, "", content[2].(map[string]any)["signature"])
	usage := msg["usage"].(map[string]any)
	assert.Equal(t, float64(7), usage["input_tokens"], "native input excludes cache counters")
	assert.Equal(t, float64(2), usage["cache_read_input_tokens"])
	assert.Equal(t, float64(1), usage["cache_creation_input_tokens"])

	// Replay the assistant content verbatim in a new request.
	contentJSON, err := json.Marshal(content)
	require.NoError(t, err)
	body := `{"model":"public","max_tokens":10,"messages":[{"role":"user","content":"go"},{"role":"assistant","content":` + string(contentJSON) + `},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"ok"}]}]}`
	req, err := DecodeMessages([]byte(body), strict)
	require.NoError(t, err)
	assistant := req.Input.Messages[1]
	require.Len(t, assistant.Content, 5)
	assert.Equal(t, llms.BlockReasoning, assistant.Content[0].Type)
	assert.Equal(t, "thinking hard", assistant.Content[0].Text)
	assert.Equal(t, "public", assistant.Content[0].Source)
	assert.JSONEq(t, `{"signature":"sig1"}`, string(assistant.Content[0].Opaque))
	assert.Equal(t, "public", assistant.Content[1].Source)
	assert.JSONEq(t, `{"data":"redacted"}`, string(assistant.Content[1].Opaque))
	assert.Empty(t, assistant.Content[1].Text)
	assert.Equal(t, "summary only", assistant.Content[2].Text)
	assert.Empty(t, assistant.Content[2].Source)
	assert.Nil(t, assistant.Content[2].Opaque)
	assert.Equal(t, llms.BlockFunctionCall, assistant.Content[4].Type)
	assert.Equal(t, `{"n":1}`, assistant.Content[4].Arguments)

	out, err = EncodeMessages(res, dialect.EncodeOptions{OmitReasoning: true})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(out, &msg))
	content = msg["content"].([]any)
	require.Len(t, content, 2)
	assert.Equal(t, blockText, content[0].(map[string]any)["type"])

	_, err = DecodeMessages([]byte(`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"forged"}]},{"role":"user","content":"b"}]}`), lenient)
	kind, param := kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Equal(t, "messages[1].content[0].signature", param)
}

func TestEncodeRefusalBlockAsRefusalStop(t *testing.T) {
	res := &router.Result{
		ID:    "resp_1",
		Model: "m",
		Response: llms.InferenceResponse{
			FinishReason: llms.FinishStop,
			Content:      []llms.InferenceBlock{{Type: llms.BlockRefusal, Text: "no"}},
		},
	}
	out, err := EncodeMessages(res, dialect.EncodeOptions{})
	require.NoError(t, err)
	var msg sdk.Message
	require.NoError(t, json.Unmarshal(out, &msg))
	assert.Equal(t, sdk.StopReason(stopRefusal), msg.StopReason, "a refusal block is reported as Anthropic's refusal stop reason")
}

func TestEncodeStopReasonsAndUnknownUsage(t *testing.T) {
	for finish, want := range map[llms.FinishReason]string{
		llms.FinishStop:          stopEndTurn,
		llms.FinishLength:        stopMaxTokens,
		llms.FinishToolCalls:     stopToolUse,
		llms.FinishContentFilter: stopRefusal,
	} {
		res := &router.Result{
			ID:    "resp_1",
			Model: "m",
			Response: llms.InferenceResponse{
				FinishReason: finish,
				Content:      []llms.InferenceBlock{{Type: llms.BlockText, Text: "no"}},
			},
		}
		out, err := EncodeMessages(res, dialect.EncodeOptions{})
		require.NoError(t, err)
		var msg sdk.Message
		require.NoError(t, json.Unmarshal(out, &msg))
		assert.Equal(t, want, string(msg.StopReason))
		assert.Equal(t, "no", msg.Content[0].Text)
		assert.EqualValues(t, 0, msg.Usage.InputTokens)
		assert.EqualValues(t, 0, msg.Usage.OutputTokens)
		assert.Contains(t, string(out), `"usage"`)
	}
	_, err := EncodeMessages(nil, dialect.EncodeOptions{})
	require.Error(t, err)
}

func TestUnsupportedControls(t *testing.T) {
	base := `"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]`
	cases := map[string]string{
		`{` + base + `,"top_k":3}`:                                                    "top_k",
		`{` + base + `,"stream":true}`:                                                "stream",
		`{` + base + `,"service_tier":"gold"}`:                                        "service_tier",
		`{` + base + `,"mcp_servers":[{"url":"x"}]}`:                                  "mcp_servers",
		`{` + base + `,"betas":["x"]}`:                                                "betas",
		`{` + base + `,"container":"c1"}`:                                             "container",
		`{` + base + `,"context_management":{"edits":[]}}`:                            "context_management",
		`{` + base + `,"tools":[{"type":"web_search_20250305","name":"web_search"}]}`: "tools[0].type",
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{}}]}]}`:                                                                                                        "messages[0].content[0].type",
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":[{"type":"server_tool_use","id":"x","name":"web_search","input":{}}]},{"role":"user","content":"b"}]}`: "messages[1].content[0].type",
	}
	for body, param := range cases {
		t.Run(param, func(t *testing.T) {
			_, err := DecodeMessages([]byte(body), lenient)
			kind, got := kindAndParam(t, err)
			assert.Equal(t, router.KindUnsupported, kind)
			assert.Equal(t, param, got)
		})
	}
}

func TestInvalidRequests(t *testing.T) {
	cases := map[string]string{
		`{"model":"m","messages":[{"role":"user","content":"hi"}]}`:                                                "max_tokens",
		`{"model":"m","max_tokens":10}`:                                                                            "messages",
		`{"model":"m","max_tokens":10,"messages":[{"role":"system","content":"hi"}]}`:                              "messages[0].role",
		`{"model":"m","max_tokens":10,"messages":[{"role":"user"}]}`:                                               "messages[0].content",
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"weird"}}`: "tool_choice.type",
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"maybe"}}`:    "thinking.type",
		`{"model":"m","max_tokens":"ten","messages":[{"role":"user","content":"hi"}]}`:                             "max_tokens",
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"a"},{"role":"assistant","content":[{"type":"tool_use","id":"1","name":"f","input":[]}]},{"role":"user","content":"b"}]}`: "messages[1].content[0].input",
	}
	for body, param := range cases {
		t.Run(param, func(t *testing.T) {
			_, err := DecodeMessages([]byte(body), lenient)
			kind, got := kindAndParam(t, err)
			assert.Equal(t, router.KindInvalidRequest, kind)
			assert.Equal(t, param, got)
		})
	}
}

func TestLenientVersusStrict(t *testing.T) {
	body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}],"unknown_field":1,"top_p":null}`
	_, err := DecodeMessages([]byte(body), lenient)
	require.NoError(t, err)

	_, err = DecodeMessages([]byte(body), strict)
	kind, param := kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Equal(t, "top_p", param, "strict rejects null")

	body = `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}]}`
	_, err = DecodeMessages([]byte(body), strict)
	kind, param = kindAndParam(t, err)
	assert.Equal(t, router.KindUnsupported, kind)
	assert.Equal(t, "messages[0].content[0].cache_control", param)

	body = `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"unknown_field":1}`
	_, err = DecodeMessages([]byte(body), strict)
	kind, param = kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Equal(t, "unknown_field", param)

	body = `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"1","content":"x","is_error":true}]}]}`
	_, err = DecodeMessages([]byte(body), lenient)
	require.NoError(t, err)
	_, err = DecodeMessages([]byte(body), strict)
	kind, param = kindAndParam(t, err)
	assert.Equal(t, router.KindUnsupported, kind)
	assert.Equal(t, "messages[0].content[0].is_error", param)

	_, err = DecodeMessages([]byte(`{"a":1,"a":2}`), strict)
	require.Error(t, err)
}

func TestToolChoiceMapping(t *testing.T) {
	cases := map[string]llms.ToolChoiceMode{
		`{"type":"auto"}`:                 llms.ToolChoiceAuto,
		`{"type":"any"}`:                  llms.ToolChoiceRequired,
		`{"type":"none"}`:                 llms.ToolChoiceNone,
		`{"type":"tool","name":"lookup"}`: llms.ToolChoiceFunction,
	}
	for choice, mode := range cases {
		req, err := DecodeMessages([]byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"tool_choice":`+choice+`}`), strict)
		require.NoError(t, err)
		require.NotNil(t, req.Input.ToolChoice)
		assert.Equal(t, mode, req.Input.ToolChoice.Mode)
		assert.Nil(t, req.Input.ParallelToolCalls)
		if mode == llms.ToolChoiceFunction {
			assert.Equal(t, "lookup", req.Input.ToolChoice.Name)
		}
	}
	req, err := DecodeMessages([]byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"auto","disable_parallel_tool_use":false}}`), strict)
	require.NoError(t, err)
	require.NotNil(t, req.Input.ParallelToolCalls)
	assert.True(t, *req.Input.ParallelToolCalls)
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSDKClientRoundTrip(t *testing.T) {
	var decoded router.Request
	client := sdk.NewClient(
		option.WithAPIKey("local"),
		option.WithBaseURL("http://router/"),
		option.WithMaxRetries(0),
		option.WithHTTPClient(&http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, "/v1/messages", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			decoded, err = DecodeMessages(body, lenient)
			var out []byte
			status := http.StatusOK
			if err != nil {
				status, out = EncodeError(err)
			} else {
				res := &router.Result{
					ID:    "resp_abc",
					Model: decoded.Model,
					Response: llms.InferenceResponse{
						FinishReason: llms.FinishToolCalls,
						Content: []llms.InferenceBlock{
							{Type: llms.BlockText, Text: "Let me look."},
							{Type: llms.BlockFunctionCall, ID: "toolu_1", Name: "lookup", Arguments: `{"q":"x"}`},
						},
						UsageKnown: true,
						Usage:      llms.Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
					},
				}
				out, err = EncodeMessages(res, dialect.EncodeOptions{})
				require.NoError(t, err)
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(out))),
				Request:    r,
			}, nil
		})}),
	)
	msg, err := client.Messages.New(context.Background(), sdk.MessageNewParams{
		Model:     "public",
		MaxTokens: 100,
		System:    []sdk.TextBlockParam{{Text: "Be brief."}},
		Messages:  []sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock("hi"))},
		Tools: []sdk.ToolUnionParam{{OfTool: &sdk.ToolParam{
			Name: "lookup",
			InputSchema: sdk.ToolInputSchemaParam{
				Properties: map[string]any{"q": map[string]any{"type": "string"}},
			},
		}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "public", decoded.Model)
	assert.Equal(t, 100, *decoded.Input.MaxTokens)
	require.Len(t, decoded.Input.Messages, 2)
	assert.Equal(t, llms.InferenceRoleSystem, decoded.Input.Messages[0].Role)
	require.Len(t, decoded.Input.Tools, 1)
	assert.Equal(t, "lookup", decoded.Input.Tools[0].Name)

	assert.Equal(t, "msg_abc", msg.ID)
	assert.Equal(t, "public", msg.Model)
	assert.Equal(t, "tool_use", string(msg.StopReason))
	require.Len(t, msg.Content, 2)
	assert.Equal(t, "text", msg.Content[0].Type)
	assert.Equal(t, "Let me look.", msg.Content[0].Text)
	assert.Equal(t, "tool_use", msg.Content[1].Type)
	assert.Equal(t, "toolu_1", msg.Content[1].ID)
	assert.Equal(t, "lookup", msg.Content[1].Name)
	assert.JSONEq(t, `{"q":"x"}`, string(msg.Content[1].Input))
	assert.EqualValues(t, 5, msg.Usage.InputTokens)
	assert.EqualValues(t, 3, msg.Usage.OutputTokens)

	// A rejected request surfaces as an SDK API error with the Anthropic envelope.
	_, err = client.Messages.New(context.Background(), sdk.MessageNewParams{
		Model:     "public",
		MaxTokens: 100,
		TopK:      sdk.Int(4),
		Messages:  []sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock("hi"))},
	})
	var apiErr *sdk.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}

func TestEncodeError(t *testing.T) {
	cases := map[router.Kind]struct {
		status int
		typ    string
	}{
		router.KindInvalidRequest:       {400, errorTypeInvalidRequest},
		router.KindUnsupported:          {400, errorTypeInvalidRequest},
		router.KindModelNotFound:        {404, errorTypeNotFound},
		router.KindRateLimited:          {429, errorTypeRateLimit},
		router.KindSelectionUnavailable: {503, errorTypeAPI},
		router.KindUpstream:             {502, errorTypeAPI},
		router.KindTimeout:              {504, errorTypeAPI},
		router.KindInternal:             {500, errorTypeAPI},
	}
	for kind, want := range cases {
		err := &router.Error{
			Kind:    kind,
			Param:   "p",
			Message: "safe message",
			Cause:   assert.AnError,
		}
		status, body := EncodeError(err)
		assert.Equal(t, want.status, status, kind)
		var env struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(body, &env))
		assert.Equal(t, typeError, env.Type)
		assert.Equal(t, want.typ, env.Error.Type, kind)
		assert.Equal(t, "safe message", env.Error.Message)
		assert.NotContains(t, string(body), assert.AnError.Error())
	}
	status, body := EncodeError(assert.AnError)
	assert.Equal(t, 500, status)
	assert.NotContains(t, string(body), assert.AnError.Error())
	assert.Contains(t, string(body), "internal router error")
}

func FuzzDecodeMessages(f *testing.F) {
	f.Add(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
	f.Add(`{"model":"m","max_tokens":10,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"e30"}]}]}`)
	f.Add(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"x","content":[{"type":"text","text":"ok"}]}]}]}`)
	f.Fuzz(func(t *testing.T, body string) {
		for _, opts := range []dialect.DecodeOptions{lenient, strict} {
			_, _ = DecodeMessages([]byte(body), opts)
		}
	})
}
