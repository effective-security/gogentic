package llms_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextParts(t *testing.T) {
	t.Parallel()
	type args struct {
		role  llms.Role
		parts []string
	}
	tests := []struct {
		name string
		args args
		want llms.Message
	}{
		{
			"basics",
			args{
				llms.RoleHuman,
				[]string{"a", "b", "c"},
			},
			llms.MessageFromTextParts(llms.RoleHuman, "a", "b", "c"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mc := llms.MessageFromTextParts(tt.args.role, tt.args.parts...)
			assert.Equal(t, tt.want, mc)
		})
	}
}

func Test_Message_JSON(t *testing.T) {
	t.Parallel()
	source := &llms.MessageSource{
		Name:     "test",
		RunID:    "1234",
		ActionID: "action1",
	}
	m1 := llms.MessageFromTextParts(llms.RoleHuman, "a").WithSource(source)
	js := llmutils.ToJSON(m1)
	exp := `{"role":"human","text":"a","source":{"name":"test","run_id":"1234","action_id":"action1"}}`
	assert.Equal(t, exp, js)

	m2 := llms.Message{}
	err := json.Unmarshal([]byte(js), &m2)
	assert.NoError(t, err)
	assert.Equal(t, m1, m2)
}

func Test_MessageContent_JSON(t *testing.T) {
	t.Parallel()

	source := &llms.MessageSource{
		Name:     "test",
		RunID:    "1234",
		ActionID: "action1",
	}

	tests := []struct {
		name    string
		msg     llms.Message
		js      string
		content string
	}{
		{
			"text",
			llms.MessageFromTextParts(llms.RoleHuman, "a", "b", "c").WithSource(source),
			`{"role":"human","parts":[{"text":"a","type":"text"},{"text":"b","type":"text"},{"text":"c","type":"text"}],"source":{"name":"test","run_id":"1234","action_id":"action1"}}`,
			`a
b
c
`,
		},
		{
			"binary",
			llms.MessageFromParts(llms.RoleHuman, llms.BinaryPart("image/png", []byte{0x00, 0x01, 0x02})).WithSource(source),
			`{"role":"human","parts":[{"type":"binary","binary":{"data":"AAEC","mime_type":"image/png"}}],"source":{"name":"test","run_id":"1234","action_id":"action1"}}`,
			`Binary: image/png
AAEC
`,
		},
		{
			"image",
			llms.MessageFromParts(llms.RoleHuman, llms.ImageURLPart("https://example.com/image.png")).WithSource(source),
			`{"role":"human","parts":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}],"source":{"name":"test","run_id":"1234","action_id":"action1"}}`,
			`URL: https://example.com/image.png
`,
		},
		{
			"image_with_detail",
			llms.MessageFromParts(llms.RoleHuman, llms.ImageURLWithDetailPart("https://example.com/image.png", "low")).WithSource(source),
			`{"role":"human","parts":[{"type":"image_url","image_url":{"url":"https://example.com/image.png","detail":"low"}}],"source":{"name":"test","run_id":"1234","action_id":"action1"}}`,
			`URL: https://example.com/image.png
`,
		},
		{
			"tool_call",
			llms.MessageFromParts(llms.RoleAI, llms.ToolCall{ID: "123", Type: "function", FunctionCall: &llms.FunctionCall{Name: "add", Arguments: `{"a":1,"b":2}`}}).WithSource(source),
			`{"role":"ai","parts":[{"type":"tool_call","tool_call":{"function":{"name":"add","arguments":"{\"a\":1,\"b\":2}"},"id":"123","type":"function"}}],"source":{"name":"test","run_id":"1234","action_id":"action1"}}`,
			`Tool Call: {"type":"tool_call","tool_call":{"function":{"name":"add","arguments":"{\"a\":1,\"b\":2}"},"id":"123","type":"function"}}
`,
		},
		{
			"tool_response",
			llms.MessageFromParts(llms.RoleAI, llms.ToolCallResponse{ToolCallID: "123", Name: "add", Content: "42"}).WithSource(source),
			`{"role":"ai","parts":[{"type":"tool_response","tool_response":{"tool_call_id":"123","name":"add","content":"42"}}],"source":{"name":"test","run_id":"1234","action_id":"action1"}}`,
			`Response: {"type":"tool_response","tool_response":{"tool_call_id":"123","name":"add","content":"42"}}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			js := llmutils.ToJSON(tt.msg)
			assert.Equal(t, tt.js, js)

			var buf strings.Builder
			tt.msg.Print(&buf)
			content := buf.String()

			assert.Equal(t, tt.content, content)
		})
	}
}

func TestContentResponse(t *testing.T) {
	t.Parallel()

	cr := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Hello.\nWorld.\n\n\n\n\n\n",
			},
			{
				Content: "How can I help you?\nI'm here to help you.",
			},
		},
	}

	exp := `Hello.
World.

How can I help you?
I'm here to help you.
`
	assert.Equal(t, exp, cr.String())
}

func TestContentResponseUsageStats(t *testing.T) {
	t.Parallel()
	cr := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Hello.\nWorld.\n\n\n\n\n\n",
				Usage: llms.Usage{
					InputTokens:      10,
					OutputTokens:     20,
					CacheWriteTokens: 30,
					CacheReadTokens:  40,
					ReasoningTokens:  50,
					TotalTokens:      60,
				},
			},
			{
				Content: "Hello.\nWorld.\n\n\n\n\n\n",
				Usage: llms.Usage{
					InputTokens:      11,
					OutputTokens:     21,
					CacheWriteTokens: 31,
					CacheReadTokens:  41,
					TotalTokens:      61,
				},
			},
		},
	}
	st := cr.Usage()
	assert.Equal(t, uint64(21), st.InputTokens)
	assert.Equal(t, uint64(41), st.OutputTokens)
	assert.Equal(t, uint64(61), st.CacheWriteTokens)
	assert.Equal(t, uint64(81), st.CacheReadTokens)
	assert.Equal(t, uint64(50), st.ReasoningTokens)
	assert.Equal(t, uint64(121), st.TotalTokens)
}

func TestContentResponseContentSize(t *testing.T) {
	t.Parallel()
	cr := &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		Content:          "Hello",
		ReasoningContent: "R",
		FuncCall:         &llms.FunctionCall{Name: "fn", Arguments: "{}"},
		ToolCalls: []llms.ToolCall{{
			ID:           "1",
			Type:         "function",
			FunctionCall: &llms.FunctionCall{Name: "t", Arguments: "a"},
		}},
	}}}
	assert.Equal(t, uint64(21), cr.ContentSize())
}

func TestContentParts_TypeAndLength(t *testing.T) {
	t.Parallel()

	text := llms.TextContent{Text: "hello"}
	assert.Equal(t, llms.ContentTypeText, text.ContentType())
	assert.Equal(t, "hello", text.String())
	assert.Equal(t, 5, text.ContentLength())

	img := llms.ImageURLContent{
		URL:    "https://example.com/a.png",
		Detail: "low",
	}
	assert.Equal(t, llms.ContentTypeImageURL, img.ContentType())
	assert.Equal(t, "https://example.com/a.png", img.String())
	assert.Equal(t, len("https://example.com/a.png")+len("low"), img.ContentLength())

	bin := llms.BinaryContent{
		MIMEType: "image/png",
		Data:     []byte("abc"),
	}
	assert.Equal(t, llms.ContentTypeBinary, bin.ContentType())
	assert.Equal(t, "data:image/png;base64,YWJj", bin.String())
	assert.Equal(t, len("image/png")+3, bin.ContentLength())

	call := llms.ToolCall{
		ID:   "call-1",
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      "lookup",
			Arguments: `{"q":1}`,
		},
	}
	assert.Equal(t, llms.ContentTypeToolCall, call.ContentType())
	assert.Equal(t, "lookup", call.GetFunctionCallName())
	assert.Equal(t, `{"q":1}`, call.GetFunctionCallArguments())
	assert.Equal(t, `ToolCall: call-1 (lookup), input: {"q":1}`, call.String())

	resp := llms.ToolCallResponse{
		ToolCallID: "call-1",
		Name:       "lookup",
		Content:    "result",
	}
	assert.Equal(t, llms.ContentTypeToolResponse, resp.ContentType())
	assert.Equal(t, "ToolCallResponse: call-1 (lookup), response size: 6", resp.String())
	assert.Equal(t, len("call-1")+len("lookup")+len("result"), resp.ContentLength())
}

func TestToolCall_ProviderNative(t *testing.T) {
	t.Parallel()

	// Provider-native tools such as web_search have no function call: the
	// name falls back to the tool type and there are no arguments.
	webSearch := llms.ToolCall{
		ID:   "ws-1",
		Type: "web_search",
	}
	assert.Equal(t, "web_search", webSearch.GetFunctionCallName())
	assert.Empty(t, webSearch.GetFunctionCallArguments())
	assert.Equal(t, len("ws-1")+len("web_search"), webSearch.ContentLength())

	// A non-function type ignores a function call even when present.
	custom := llms.ToolCall{
		Type: "custom",
		FunctionCall: &llms.FunctionCall{
			Name:      "ignored",
			Arguments: "{}",
		},
	}
	assert.Equal(t, "custom", custom.GetFunctionCallName())
	assert.Empty(t, custom.GetFunctionCallArguments())
}

func TestContentResponse_NilAndSingle(t *testing.T) {
	t.Parallel()

	var nilResp *llms.ContentResponse
	assert.Equal(t, &llms.Usage{}, nilResp.Usage())
	assert.Equal(t, uint64(0), nilResp.ContentSize())
	assert.Empty(t, nilResp.String())

	var nilChoice *llms.ContentChoice
	assert.Equal(t, uint64(0), nilChoice.ContentSize())

	single := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "  only choice\n",
				Usage: llms.Usage{
					InputTokens: 2,
				},
			},
			nil,
		},
	}
	// A nil choice is skipped when summing usage and counts zero bytes.
	assert.Equal(t, &llms.Usage{InputTokens: 2}, single.Usage())
	assert.Equal(t, uint64(len("  only choice\n")), single.ContentSize())

	single.Choices = single.Choices[:1]
	// A single choice is returned verbatim, without trimming.
	assert.Equal(t, "  only choice\n", single.String())
}

func TestMessageSource_String(t *testing.T) {
	t.Parallel()

	var nilSource *llms.MessageSource
	assert.Empty(t, nilSource.String())

	assert.Equal(t, "run.agent", (&llms.MessageSource{
		Name:  "agent",
		RunID: "run",
	}).String())
	assert.Equal(t, "run.action.agent", (&llms.MessageSource{
		Name:     "agent",
		RunID:    "run",
		ActionID: "action",
	}).String())
}

func TestUsageStats_Add(t *testing.T) {
	t.Parallel()

	const (
		modelA = "model-a"
		modelB = "model-b"
	)

	var total llms.UsageStats
	total.AddModelUsage(modelA, &llms.Usage{
		InputTokens:  1,
		OutputTokens: 2,
	})
	// nil usage is ignored and does not create an entry.
	total.AddModelUsage(modelB, nil)
	_, hasB := total.ModelUsage[modelB]
	assert.False(t, hasB)

	total.Add(&llms.UsageStats{
		ModelUsage: map[string]*llms.Usage{
			modelA: {
				InputTokens: 10,
			},
			modelB: {
				OutputTokens: 5,
			},
		},
		BytesOut:     100,
		BytesIn:      50,
		LlmCallCount: 2,
	})
	total.Add(nil)

	assert.Equal(t, llms.UsageStats{
		ModelUsage: map[string]*llms.Usage{
			modelA: {
				InputTokens:  11,
				OutputTokens: 2,
			},
			modelB: {
				OutputTokens: 5,
			},
		},
		BytesOut:     100,
		BytesIn:      50,
		LlmCallCount: 2,
	}, total)

	// A nil receiver is a no-op rather than a panic.
	var nilStats *llms.UsageStats
	nilStats.Add(&total)
	nilStats.AddModelUsage(modelA, &llms.Usage{})
	assert.Nil(t, nilStats)
}

func TestMessageFromToolCallsAndResponse(t *testing.T) {
	t.Parallel()

	call := llms.ToolCall{
		ID:   "call-1",
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      "lookup",
			Arguments: "{}",
		},
	}
	msg := llms.MessageFromToolCalls(llms.RoleAI, call)
	assert.Equal(t, llms.RoleAI, msg.Role)
	require.Len(t, msg.Parts, 1)
	got, ok := msg.Parts[0].(llms.ToolCall)
	require.True(t, ok)
	assert.Equal(t, call, got)
	// The function call is copied, not shared with the input.
	assert.NotSame(t, call.FunctionCall, got.FunctionCall)
	assert.Equal(t, len("call-1")+len("function")+len("lookup")+len("{}"), got.ContentLength())

	resp := llms.ToolCallResponse{
		ToolCallID: "call-1",
		Name:       "lookup",
		Content:    "ok",
	}
	msg = llms.MessageFromToolResponse(llms.RoleTool, resp)
	assert.Equal(t, llms.Message{
		Role: llms.RoleTool,
		Parts: []llms.ContentPart{
			resp,
		},
	}, msg)
}
