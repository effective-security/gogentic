package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	msgString      = "expected a string"
	msgBoolean     = "expected a boolean"
	msgNumber      = "expected a number"
	msgArray       = "expected an array"
	msgObject      = "expected an object"
	msgUnknown     = "unknown field"
	msgUnsupported = "unsupported value for this field"
	msgTextOnly    = "only text blocks are supported here"
	msgContent     = "expected a string or an array of content blocks"
	userHi         = `{"role":"user","content":"hi"}`
	userMore       = `{"role":"user","content":"more"}`
)

// body builds a valid request around extra top-level members.
func body(extra string) string {
	out := `{"model":"m","max_tokens":10,"messages":[` + userHi + `]`
	if extra != "" {
		out += `,` + extra
	}
	return out + `}`
}

// withMessages builds a valid request with the given messages array members.
func withMessages(messages ...string) string {
	return `{"model":"m","max_tokens":10,"messages":[` + strings.Join(messages, ",") + `]}`
}

// assistantTurn wraps assistant content between two user turns.
func assistantTurn(content string) string {
	return withMessages(userHi, `{"role":"assistant","content":`+content+`}`, userMore)
}

// userContent builds a single user message with the given content.
func userContent(content string) string {
	return withMessages(`{"role":"user","content":` + content + `}`)
}

func TestDecodeMessagesRejections(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		strict bool
		kind   router.Kind
		param  string
		msg    string
	}{
		{
			name:  "model not a string",
			body:  `{"model":1,"max_tokens":10,"messages":[` + userHi + `]}`,
			kind:  router.KindInvalidRequest,
			param: fieldModel,
			msg:   msgString,
		},
		{
			name:  "temperature not a number",
			body:  body(`"temperature":"hot"`),
			kind:  router.KindInvalidRequest,
			param: fieldTemperature,
			msg:   msgNumber,
		},
		{
			name:  "top_p not a number",
			body:  body(`"top_p":"x"`),
			kind:  router.KindInvalidRequest,
			param: fieldTopP,
			msg:   msgNumber,
		},
		{
			name:  "stop sequences not an array",
			body:  body(`"stop_sequences":"END"`),
			kind:  router.KindInvalidRequest,
			param: fieldStopSequences,
			msg:   msgArray,
		},
		{
			name:  "stop sequence not a string",
			body:  body(`"stop_sequences":["END",1]`),
			kind:  router.KindInvalidRequest,
			param: "stop_sequences[1]",
			msg:   msgString,
		},
		{
			name:  "stream not a boolean",
			body:  body(`"stream":"yes"`),
			kind:  router.KindInvalidRequest,
			param: fieldStream,
			msg:   msgBoolean,
		},
		{
			name:  "service tier not a string",
			body:  body(`"service_tier":1`),
			kind:  router.KindInvalidRequest,
			param: fieldServiceTier,
			msg:   msgString,
		},
		{
			name:  "mcp servers not an array",
			body:  body(`"mcp_servers":{}`),
			kind:  router.KindUnsupported,
			param: fieldMCPServers,
			msg:   msgUnsupported,
		},
		{
			name:  "system not text",
			body:  body(`"system":5`),
			kind:  router.KindInvalidRequest,
			param: fieldSystem,
			msg:   "expected a string or an array of text blocks",
		},
		{
			name:  "system block not an object",
			body:  body(`"system":[1]`),
			kind:  router.KindInvalidRequest,
			param: "system[0]",
			msg:   msgObject,
		},
		{
			name:  "system block type not a string",
			body:  body(`"system":[{"type":1}]`),
			kind:  router.KindInvalidRequest,
			param: "system[0].type",
			msg:   msgString,
		},
		{
			name:  "system block not text",
			body:  body(`"system":[{"type":"image"}]`),
			kind:  router.KindUnsupported,
			param: "system[0].type",
			msg:   msgTextOnly,
		},
		{
			name:  "system text not a string",
			body:  body(`"system":[{"type":"text","text":1}]`),
			kind:  router.KindInvalidRequest,
			param: "system[0].text",
			msg:   msgString,
		},
		{
			name:   "system cache control in strict mode",
			body:   body(`"system":[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]`),
			strict: true,
			kind:   router.KindUnsupported,
			param:  "system[0].cache_control",
			msg:    msgUnsupported,
		},
		{
			name:   "system block unknown field in strict mode",
			body:   body(`"system":[{"type":"text","text":"x","extra":1}]`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "system[0].extra",
			msg:    msgUnknown,
		},
		{
			name:  "messages not an array",
			body:  `{"model":"m","max_tokens":10,"messages":{}}`,
			kind:  router.KindInvalidRequest,
			param: fieldMessages,
			msg:   msgArray,
		},
		{
			name:  "message not an object",
			body:  withMessages(`1`),
			kind:  router.KindInvalidRequest,
			param: "messages[0]",
			msg:   msgObject,
		},
		{
			name:  "role not a string",
			body:  withMessages(`{"role":1,"content":"x"}`),
			kind:  router.KindInvalidRequest,
			param: "messages[0].role",
			msg:   msgString,
		},
		{
			name:   "message unknown field in strict mode",
			body:   withMessages(`{"role":"user","content":"x","extra":1}`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "messages[0].extra",
			msg:    msgUnknown,
		},
		{
			name:  "user content not text or blocks",
			body:  userContent(`5`),
			kind:  router.KindInvalidRequest,
			param: "messages[0].content",
			msg:   msgContent,
		},
		{
			name:  "user block not an object",
			body:  userContent(`[1]`),
			kind:  router.KindInvalidRequest,
			param: "messages[0].content[0]",
			msg:   msgObject,
		},
		{
			name:  "user block type not a string",
			body:  userContent(`[{"type":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[0].content[0].type",
			msg:   msgString,
		},
		{
			name:  "user text not a string",
			body:  userContent(`[{"type":"text","text":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[0].content[0].text",
			msg:   msgString,
		},
		{
			name:   "user block unknown field in strict mode",
			body:   userContent(`[{"type":"text","text":"x","extra":1}]`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "messages[0].content[0].extra",
			msg:    msgUnknown,
		},
		{
			name:  "tool result ID not a string",
			body:  userContent(`[{"type":"tool_result","tool_use_id":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[0].content[0].tool_use_id",
			msg:   msgString,
		},
		{
			name:  "tool result error flag not a boolean",
			body:  userContent(`[{"type":"tool_result","tool_use_id":"c","is_error":"yes"}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[0].content[0].is_error",
			msg:   msgBoolean,
		},
		{
			name:  "tool result content not text",
			body:  userContent(`[{"type":"tool_result","tool_use_id":"c","content":[{"type":"image"}]}]`),
			kind:  router.KindUnsupported,
			param: "messages[0].content[0].content[0].type",
			msg:   msgTextOnly,
		},
		{
			name:  "assistant content not text or blocks",
			body:  assistantTurn(`5`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content",
			msg:   msgContent,
		},
		{
			name:  "assistant block not an object",
			body:  assistantTurn(`[1]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0]",
			msg:   msgObject,
		},
		{
			name:  "assistant block type not a string",
			body:  assistantTurn(`[{"type":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].type",
			msg:   msgString,
		},
		{
			name:  "assistant text not a string",
			body:  assistantTurn(`[{"type":"text","text":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].text",
			msg:   msgString,
		},
		{
			name:   "assistant cache control in strict mode",
			body:   assistantTurn(`[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]`),
			strict: true,
			kind:   router.KindUnsupported,
			param:  "messages[1].content[0].cache_control",
			msg:    msgUnsupported,
		},
		{
			name:   "assistant block unknown field in strict mode",
			body:   assistantTurn(`[{"type":"text","text":"x","extra":1}]`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "messages[1].content[0].extra",
			msg:    msgUnknown,
		},
		{
			name:  "tool use ID not a string",
			body:  assistantTurn(`[{"type":"tool_use","id":1,"name":"f"}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].id",
			msg:   msgString,
		},
		{
			name:  "tool use name not a string",
			body:  assistantTurn(`[{"type":"tool_use","id":"c","name":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].name",
			msg:   msgString,
		},
		{
			name:  "thinking text not a string",
			body:  assistantTurn(`[{"type":"thinking","thinking":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].thinking",
			msg:   msgString,
		},
		{
			name:  "thinking signature not a string",
			body:  assistantTurn(`[{"type":"thinking","thinking":"x","signature":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].signature",
			msg:   msgString,
		},
		{
			name:  "redacted thinking data not a string",
			body:  assistantTurn(`[{"type":"redacted_thinking","data":1}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].data",
			msg:   msgString,
		},
		{
			name:  "redacted thinking data forged",
			body:  assistantTurn(`[{"type":"redacted_thinking","data":"forged"}]`),
			kind:  router.KindInvalidRequest,
			param: "messages[1].content[0].data",
			msg:   "data is not a router-issued value",
		},
		{
			name:  "tools not an array",
			body:  body(`"tools":{}`),
			kind:  router.KindInvalidRequest,
			param: fieldTools,
			msg:   msgArray,
		},
		{
			name:  "tool not an object",
			body:  body(`"tools":[1]`),
			kind:  router.KindInvalidRequest,
			param: "tools[0]",
			msg:   msgObject,
		},
		{
			name:  "tool type not a string",
			body:  body(`"tools":[{"type":1,"name":"f"}]`),
			kind:  router.KindInvalidRequest,
			param: "tools[0].type",
			msg:   msgString,
		},
		{
			name:  "tool name not a string",
			body:  body(`"tools":[{"name":1}]`),
			kind:  router.KindInvalidRequest,
			param: "tools[0].name",
			msg:   msgString,
		},
		{
			name:  "tool description not a string",
			body:  body(`"tools":[{"name":"f","description":1}]`),
			kind:  router.KindInvalidRequest,
			param: "tools[0].description",
			msg:   msgString,
		},
		{
			name:  "tool input schema not an object",
			body:  body(`"tools":[{"name":"f","input_schema":[]}]`),
			kind:  router.KindInvalidRequest,
			param: "tools[0].input_schema",
			msg:   msgObject,
		},
		{
			name:  "tool strict not a boolean",
			body:  body(`"tools":[{"name":"f","strict":"yes"}]`),
			kind:  router.KindInvalidRequest,
			param: "tools[0].strict",
			msg:   msgBoolean,
		},
		{
			name:   "tool cache control in strict mode",
			body:   body(`"tools":[{"name":"f","cache_control":{"type":"ephemeral"}}]`),
			strict: true,
			kind:   router.KindUnsupported,
			param:  "tools[0].cache_control",
			msg:    msgUnsupported,
		},
		{
			name:   "tool unknown field in strict mode",
			body:   body(`"tools":[{"name":"f","extra":1}]`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "tools[0].extra",
			msg:    msgUnknown,
		},
		{
			name:  "tool choice not an object",
			body:  body(`"tool_choice":"auto"`),
			kind:  router.KindInvalidRequest,
			param: fieldToolChoice,
			msg:   msgObject,
		},
		{
			name:  "tool choice type not a string",
			body:  body(`"tool_choice":{"type":1}`),
			kind:  router.KindInvalidRequest,
			param: "tool_choice.type",
			msg:   msgString,
		},
		{
			name:  "tool choice name not a string",
			body:  body(`"tool_choice":{"type":"tool","name":1}`),
			kind:  router.KindInvalidRequest,
			param: "tool_choice.name",
			msg:   msgString,
		},
		{
			name:  "disable parallel not a boolean",
			body:  body(`"tool_choice":{"type":"auto","disable_parallel_tool_use":"yes"}`),
			kind:  router.KindInvalidRequest,
			param: "tool_choice.disable_parallel_tool_use",
			msg:   msgBoolean,
		},
		{
			name:   "tool choice unknown field in strict mode",
			body:   body(`"tool_choice":{"type":"auto","extra":1}`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "tool_choice.extra",
			msg:    msgUnknown,
		},
		{
			name:  "thinking type not a string",
			body:  body(`"thinking":{"type":1}`),
			kind:  router.KindInvalidRequest,
			param: "thinking.type",
			msg:   msgString,
		},
		{
			name:  "thinking budget not an integer",
			body:  body(`"thinking":{"type":"enabled","budget_tokens":"x"}`),
			kind:  router.KindInvalidRequest,
			param: "thinking.budget_tokens",
			msg:   "expected an integer",
		},
		{
			name:  "output format not an object",
			body:  body(`"output_config":{"format":[]}`),
			kind:  router.KindInvalidRequest,
			param: "output_config.format",
			msg:   msgObject,
		},
		{
			name:  "output format type not a string",
			body:  body(`"output_config":{"format":{"type":1}}`),
			kind:  router.KindInvalidRequest,
			param: "output_config.format.type",
			msg:   msgString,
		},
		{
			name:  "output format schema missing",
			body:  body(`"output_config":{"format":{"type":"json_schema"}}`),
			kind:  router.KindInvalidRequest,
			param: "output_config.format.schema",
			msg:   "expected a schema object",
		},
		{
			name:  "output format name not a string",
			body:  body(`"output_config":{"format":{"type":"json_schema","schema":{"type":"object"},"name":1}}`),
			kind:  router.KindInvalidRequest,
			param: "output_config.format.name",
			msg:   msgString,
		},
		{
			name:   "output format unknown field in strict mode",
			body:   body(`"output_config":{"format":{"type":"json_schema","schema":{"type":"object"},"extra":1}}`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "output_config.format.extra",
			msg:    msgUnknown,
		},
		{
			name:  "effort not a string",
			body:  body(`"output_config":{"effort":1}`),
			kind:  router.KindInvalidRequest,
			param: "output_config.effort",
			msg:   msgString,
		},
		{
			name:  "effort out of range",
			body:  body(`"output_config":{"effort":"max"}`),
			kind:  router.KindInvalidRequest,
			param: "output_config.effort",
			msg:   "effort must be low, medium or high",
		},
		{
			name:   "output config unknown field in strict mode",
			body:   body(`"output_config":{"effort":"low","extra":1}`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "output_config.extra",
			msg:    msgUnknown,
		},
		{
			name:  "metadata not an object",
			body:  body(`"metadata":[]`),
			kind:  router.KindInvalidRequest,
			param: fieldMetadata,
			msg:   msgObject,
		},
		{
			name:  "metadata user not a string",
			body:  body(`"metadata":{"user_id":1}`),
			kind:  router.KindInvalidRequest,
			param: "metadata.user_id",
			msg:   msgString,
		},
		{
			name:   "metadata unknown field in strict mode",
			body:   body(`"metadata":{"user_id":"u","extra":1}`),
			strict: true,
			kind:   router.KindInvalidRequest,
			param:  "metadata.extra",
			msg:    msgUnknown,
		},
		{
			name:  "root not an object",
			body:  `[]`,
			kind:  router.KindInvalidRequest,
			param: "",
			msg:   msgObject,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := lenient
			if tc.strict {
				opts = strict
			}
			_, err := DecodeMessages([]byte(tc.body), opts)
			require.Error(t, err)
			re := router.AsError(err)
			assert.Equal(t, tc.kind, re.Kind)
			assert.Equal(t, tc.param, re.Param)
			assert.Equal(t, tc.msg, re.Message)
		})
	}
}

func TestDecodeMessagesAlternateShapes(t *testing.T) {
	sealed, err := dialect.SealOpaque("public", json.RawMessage(`{"data":"r"}`))
	require.NoError(t, err)
	raw := `{"model":"m","max_tokens":10,"system":[],
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"plain"},
			{"role":"user","content":"go"},
			{"role":"assistant","content":[
				{"type":"redacted_thinking","data":"` + sealed + `"},
				{"type":"tool_use","id":"c1","name":"noargs"}
			]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1"}]}
		],
		"output_config":{"effort":"medium"},
		"metadata":{}}`
	req, err := DecodeMessages([]byte(raw), strict)
	require.NoError(t, err)
	assert.Nil(t, req.Metadata, "metadata without user_id adds nothing")
	require.NotNil(t, req.Input.Reasoning)
	assert.Equal(t, llms.EffortMedium, req.Input.Reasoning.Effort)
	assert.Nil(t, req.Input.Reasoning.BudgetTokens)

	msgs := req.Input.Messages
	require.Len(t, msgs, 5, "an empty system array adds no instruction message")
	assert.Equal(t, llms.InferenceRoleUser, msgs[0].Role)
	assert.Equal(t, llms.InferenceMessage{
		Role: llms.InferenceRoleAssistant,
		Content: []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: "plain",
		}},
	}, msgs[1])
	assistant := msgs[3]
	require.Len(t, assistant.Content, 2)
	assert.Equal(t, llms.BlockReasoning, assistant.Content[0].Type)
	assert.Empty(t, assistant.Content[0].Text)
	assert.Equal(t, "public", assistant.Content[0].Source)
	assert.JSONEq(t, `{"data":"r"}`, string(assistant.Content[0].Opaque))
	assert.Equal(t, llms.InferenceBlock{
		Type:      llms.BlockFunctionCall,
		ID:        "c1",
		Name:      "noargs",
		Arguments: emptyArguments,
	}, assistant.Content[1], "missing input becomes an empty object")
	assert.Equal(t, llms.InferenceMessage{
		Role: llms.InferenceRoleTool,
		Content: []llms.InferenceBlock{{
			Type: llms.BlockFunctionOutput,
			ID:   "c1",
		}},
	}, msgs[4], "a tool_result without text-only blocks has no user message")

	req, err = DecodeMessages([]byte(body(`"system":"Be brief.","output_config":{"format":{"type":"json_schema","name":"","schema":{"type":"object"}}}`)), strict)
	require.NoError(t, err)
	assert.Equal(t, llms.InferenceMessage{
		Role: llms.InferenceRoleSystem,
		Content: []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: "Be brief.",
		}},
	}, req.Input.Messages[0])
	require.NotNil(t, req.Input.Format)
	assert.Equal(t, defaultFormatName, req.Input.Format.Name, "empty name falls back to the default")
	assert.Nil(t, req.Input.Reasoning, "format without effort does not enable reasoning")
}

func TestDecodeMessagesUserContentWithoutText(t *testing.T) {
	req, err := DecodeMessages([]byte(userContent(`[]`)), strict)
	require.NoError(t, err)
	assert.Equal(t, []llms.InferenceMessage{{
		Role: llms.InferenceRoleUser,
	}}, req.Input.Messages, "an empty block list still yields a user turn for router validation")
}

func TestMapEffort(t *testing.T) {
	for in, want := range map[string]llms.Effort{
		effortLow:    llms.EffortLow,
		effortMedium: llms.EffortMedium,
		effortHigh:   llms.EffortHigh,
	} {
		got, ok := mapEffort(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got)
	}
	got, ok := mapEffort("minimal")
	assert.False(t, ok)
	assert.Empty(t, got)
}
