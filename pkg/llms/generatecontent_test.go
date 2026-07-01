package llms_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llmutils"
	"github.com/stretchr/testify/assert"
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
