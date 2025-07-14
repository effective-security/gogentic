package llms_test

import (
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

func Test_MessageContent_JSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		msg     llms.Message
		js      string
		content string
	}{
		{
			"text",
			llms.MessageFromTextParts(llms.RoleHuman, "a", "b", "c"),
			`{"role":"human","parts":[{"text":"a","type":"text"},{"text":"b","type":"text"},{"text":"c","type":"text"}]}`,
			`a
b
c
`,
		},
		{
			"binary",
			llms.MessageFromParts(llms.RoleHuman, llms.BinaryPart("image/png", []byte{0x00, 0x01, 0x02})),
			`{"role":"human","parts":[{"type":"binary","binary":{"data":"AAEC","mime_type":"image/png"}}]}`,
			`Binary: image/png
AAEC
`,
		},
		{
			"image",
			llms.MessageFromParts(llms.RoleHuman, llms.ImageURLPart("https://example.com/image.png")),
			`{"role":"human","parts":[{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}`,
			`URL: https://example.com/image.png
`,
		},
		{
			"image_with_detail",
			llms.MessageFromParts(llms.RoleHuman, llms.ImageURLWithDetailPart("https://example.com/image.png", "low")),
			`{"role":"human","parts":[{"type":"image_url","image_url":{"url":"https://example.com/image.png","detail":"low"}}]}`,
			`URL: https://example.com/image.png
`,
		},
		{
			"tool_call",
			llms.MessageFromParts(llms.RoleAI, llms.ToolCall{ID: "123", Type: "function", FunctionCall: &llms.FunctionCall{Name: "add", Arguments: `{"a":1,"b":2}`}}),
			`{"role":"ai","parts":[{"type":"tool_call","tool_call":{"function":{"name":"add","arguments":"{\"a\":1,\"b\":2}"},"id":"123","type":"function"}}]}`,
			`Tool Call: {"type":"tool_call","tool_call":{"function":{"name":"add","arguments":"{\"a\":1,\"b\":2}"},"id":"123","type":"function"}}
`,
		},
		{
			"tool_response",
			llms.MessageFromParts(llms.RoleAI, llms.ToolCallResponse{ToolCallID: "123", Name: "add", Content: "42"}),
			`{"role":"ai","parts":[{"type":"tool_response","tool_response":{"tool_call_id":"123","name":"add","content":"42"}}]}`,
			`Response: {"type":"tool_response","tool_response":{"tool_call_id":"123","name":"add","content":"42"}}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			js := llmutils.ToJSON(tt.msg)
			assert.Equal(t, tt.js, js)
			content := tt.msg.GetContent()
			assert.Equal(t, tt.content, content)
		})
	}
}
