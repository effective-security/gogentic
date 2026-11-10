package openai

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
	msgInteger     = "expected an integer"
	msgNumber      = "expected a number"
	msgArray       = "expected an array"
	msgObject      = "expected an object"
	msgUnknown     = "unknown field"
	msgUnsupported = "unsupported value for this field"
	msgSchemaObj   = "expected a JSON Schema object"
	msgParts       = "expected a string or an array of content parts"
	msgRequired    = "content is required"
	msgNotIssued   = "reasoning state is not a router-issued value"
	chatUser       = `{"role":"user","content":"hi"}`
)

// decodeCase is one rejected request body with the exact expected error.
type decodeCase struct {
	name   string
	body   string
	strict bool
	kind   router.Kind
	param  string
	msg    string
}

func runDecodeCases(t *testing.T, decode func([]byte, dialect.DecodeOptions) (router.Request, error), cases []decodeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := lenient
			if tc.strict {
				opts = strict
			}
			_, err := decode([]byte(tc.body), opts)
			require.Error(t, err)
			re := router.AsError(err)
			assert.Equal(t, tc.kind, re.Kind)
			assert.Equal(t, tc.param, re.Param)
			assert.Equal(t, tc.msg, re.Message)
		})
	}
}

func invalid(name, body, param, msg string) decodeCase {
	return decodeCase{
		name:  name,
		body:  body,
		kind:  router.KindInvalidRequest,
		param: param,
		msg:   msg,
	}
}

func invalidStrict(name, body, param, msg string) decodeCase {
	c := invalid(name, body, param, msg)
	c.strict = true
	return c
}

func unsupported(name, body, param string) decodeCase {
	return decodeCase{
		name:  name,
		body:  body,
		kind:  router.KindUnsupported,
		param: param,
		msg:   msgUnsupported,
	}
}

// chatBody builds a valid Chat request around extra top-level members.
func chatBody(extra string) string {
	return `{"model":"m","messages":[` + chatUser + `],` + extra + `}`
}

// chatMessages builds a Chat request from raw message objects.
func chatMessages(messages ...string) string {
	return `{"model":"m","messages":[` + strings.Join(messages, ",") + `]}`
}

// chatAssistant builds a Chat request with one assistant message carrying extra members.
func chatAssistant(members string) string {
	return chatMessages(chatUser, `{"role":"assistant",`+members+`}`)
}

// chatCall builds a Chat request with one assistant tool call object.
func chatCall(call string) string {
	return chatMessages(chatUser, `{"role":"assistant","tool_calls":[`+call+`]}`)
}

// chatDetail builds a Chat request with one assistant reasoning detail.
func chatDetail(detail string) string {
	return chatAssistant(`"content":"x","reasoning_details":[` + detail + `]`)
}

// responsesBody builds a valid Responses request around extra top-level members.
func responsesBody(extra string) string {
	return `{"model":"m","input":"hi",` + extra + `}`
}

// responsesInput builds a Responses request from raw input items.
func responsesInput(items ...string) string {
	return `{"model":"m","input":[` + strings.Join(items, ",") + `]}`
}

func TestDecodeChatRejections(t *testing.T) {
	runDecodeCases(t, DecodeChat, []decodeCase{
		invalid("malformed body", `not json`, paramRequest, "malformed JSON"),
		invalid("model not a string", `{"model":1,"messages":[]}`, keyModel, msgString),
		invalid("temperature not a number", chatBody(`"temperature":"hot"`), keyTemperature, msgNumber),
		invalid("top_p not a number", chatBody(`"top_p":"x"`), keyTopP, msgNumber),
		invalid("parallel tool calls not a boolean", chatBody(`"parallel_tool_calls":"yes"`), keyParallelToolCalls, msgBoolean),
		invalid("metadata values not strings", chatBody(`"metadata":{"k":1}`), keyMetadata, "expected an object of strings"),
		invalid("user not a string", chatBody(`"user":1`), keyUser, msgString),
		invalid("safety identifier not a string", chatBody(`"safety_identifier":true`), keySafetyIdentifier, msgString),
		invalid("stream not a boolean", chatBody(`"stream":"yes"`), keyStream, msgBoolean),
		invalid("top logprobs not an integer", chatBody(`"top_logprobs":"x"`), keyTopLogprobs, msgInteger),
		invalid("service tier not a string", chatBody(`"service_tier":1`), keyServiceTier, msgString),
		invalid("messages not an array", `{"model":"m","messages":{}}`, keyMessages, msgArray),
		invalid("messages missing", `{"model":"m"}`, keyMessages, "expected an array of messages"),
		invalid("message not an object", chatMessages(`1`), "messages[0]", msgObject),
		invalid("role not a string", chatMessages(`{"role":1,"content":"x"}`), "messages[0].role", msgString),
		invalid("tool call ID not a string", chatMessages(`{"role":"tool","tool_call_id":1,"content":"x"}`), "messages[0].tool_call_id", msgString),
		invalid("user content not text", chatMessages(`{"role":"user","content":5}`), "messages[0].content", msgParts),
		invalid("user part not an object", chatMessages(`{"role":"user","content":[1]}`), "messages[0].content[0]", msgObject),
		invalid("user part type not a string", chatMessages(`{"role":"user","content":[{"type":1}]}`), "messages[0].content[0].type", msgString),
		invalid("user part text not a string", chatMessages(`{"role":"user","content":[{"type":"text","text":1}]}`), "messages[0].content[0].text", msgString),
		invalidStrict("user part unknown field", chatMessages(`{"role":"user","content":[{"type":"text","text":"x","extra":1}]}`), "messages[0].content[0].extra", msgUnknown),
		invalid("tool message with tool calls", chatMessages(chatUser, `{"role":"tool","tool_call_id":"c","content":"x","tool_calls":[]}`), "messages[1].tool_calls", "tool messages cannot carry tool calls"),
		invalid("tool content not text", chatMessages(`{"role":"tool","tool_call_id":"c","content":5}`), "messages[0].content", msgParts),
		invalid("tool content null", chatMessages(`{"role":"tool","tool_call_id":"c","content":null}`), "messages[0].content", msgRequired),
		invalidStrict("message unknown field", chatMessages(`{"role":"user","content":"x","extra":1}`), "messages[0].extra", msgUnknown),
		invalid("reasoning details not an array", chatAssistant(`"content":"x","reasoning_details":{}`), "messages[1].reasoning_details", msgArray),
		invalid("reasoning detail not an object", chatDetail(`1`), "messages[1].reasoning_details[0]", msgObject),
		invalid("reasoning detail type not a string", chatDetail(`{"type":1}`), "messages[1].reasoning_details[0].type", msgString),
		invalid("encrypted detail data not a string", chatDetail(`{"type":"reasoning.encrypted","data":1}`), "messages[1].reasoning_details[0].data", msgString),
		invalid("encrypted detail data forged", chatDetail(`{"type":"reasoning.encrypted","data":"forged"}`), keyReasoning, msgNotIssued),
		invalid("summary detail not a string", chatDetail(`{"type":"reasoning.summary","summary":1}`), "messages[1].reasoning_details[0].summary", msgString),
		invalid("text detail not a string", chatDetail(`{"type":"reasoning.text","text":1}`), "messages[1].reasoning_details[0].text", msgString),
		unsupported("unknown detail type", chatDetail(`{"type":"reasoning.other"}`), "messages[1].reasoning_details[0].type"),
		invalidStrict("detail unknown field", chatDetail(`{"type":"reasoning.text","text":"x","extra":1}`), "messages[1].reasoning_details[0].extra", msgUnknown),
		invalid("assistant content not text", chatAssistant(`"content":5`), "messages[1].content", msgParts),
		unsupported("assistant part not text", chatAssistant(`"content":[{"type":"image_url"}]`), "messages[1].content[0].type"),
		invalid("assistant refusal not a string", chatAssistant(`"content":"x","refusal":1`), "messages[1].refusal", msgString),
		unsupported("assistant audio", chatAssistant(`"content":"x","audio":{"id":"a"}`), "messages[1].audio"),
		unsupported("assistant function call", chatAssistant(`"content":"x","function_call":{"name":"f"}`), "messages[1].function_call"),
		invalid("tool calls not an array", chatAssistant(`"tool_calls":{}`), "messages[1].tool_calls", msgArray),
		invalid("assistant without content", chatAssistant(`"content":null`), "messages[1].content", "assistant messages require content or tool calls"),
		invalid("tool call not an object", chatCall(`1`), "messages[1].tool_calls[0]", msgObject),
		invalid("tool call type not a string", chatCall(`{"type":1}`), "messages[1].tool_calls[0].type", msgString),
		invalid("assistant tool call ID not a string", chatCall(`{"type":"function","id":1}`), "messages[1].tool_calls[0].id", msgString),
		invalid("tool call function not an object", chatCall(`{"type":"function","id":"c","function":[]}`), "messages[1].tool_calls[0].function", msgObject),
		invalid("tool call function missing", chatCall(`{"type":"function","id":"c"}`), "messages[1].tool_calls[0].function", "function call is required"),
		invalid("tool call name not a string", chatCall(`{"type":"function","id":"c","function":{"name":1}}`), "messages[1].tool_calls[0].function.name", msgString),
		invalid("tool call arguments not a string", chatCall(`{"type":"function","id":"c","function":{"name":"f","arguments":{}}}`), "messages[1].tool_calls[0].function.arguments", msgString),
		invalidStrict("tool call function unknown field", chatCall(`{"type":"function","id":"c","function":{"name":"f","arguments":"{}","extra":1}}`), "messages[1].tool_calls[0].function.extra", msgUnknown),
		invalidStrict("tool call unknown field", chatCall(`{"type":"function","id":"c","function":{"name":"f","arguments":"{}"},"extra":1}`), "messages[1].tool_calls[0].extra", msgUnknown),
		invalid("tools not an array", chatBody(`"tools":{}`), keyTools, msgArray),
		invalid("tool not an object", chatBody(`"tools":[1]`), "tools[0]", msgObject),
		invalid("tool type not a string", chatBody(`"tools":[{"type":1}]`), "tools[0].type", msgString),
		invalid("tool function not an object", chatBody(`"tools":[{"type":"function","function":[]}]`), "tools[0].function", msgObject),
		invalid("tool function missing", chatBody(`"tools":[{"type":"function"}]`), "tools[0].function", "function definition is required"),
		invalid("tool name not a string", chatBody(`"tools":[{"type":"function","function":{"name":1}}]`), "tools[0].function.name", msgString),
		invalid("tool description not a string", chatBody(`"tools":[{"type":"function","function":{"name":"f","description":1}}]`), "tools[0].function.description", msgString),
		invalid("tool parameters not an object", chatBody(`"tools":[{"type":"function","function":{"name":"f","parameters":[]}}]`), "tools[0].function.parameters", msgSchemaObj),
		invalid("tool strict not a boolean", chatBody(`"tools":[{"type":"function","function":{"name":"f","strict":"yes"}}]`), "tools[0].function.strict", msgBoolean),
		invalidStrict("tool function unknown field", chatBody(`"tools":[{"type":"function","function":{"name":"f","extra":1}}]`), "tools[0].function.extra", msgUnknown),
		invalidStrict("tool unknown field", chatBody(`"tools":[{"type":"function","function":{"name":"f"},"extra":1}]`), "tools[0].extra", msgUnknown),
		invalid("tool choice not an object", chatBody(`"tool_choice":[]`), keyToolChoice, msgObject),
		invalid("tool choice type not a string", chatBody(`"tool_choice":{"type":1}`), "tool_choice.type", msgString),
		invalid("tool choice function not an object", chatBody(`"tool_choice":{"type":"function","function":[]}`), "tool_choice.function", msgObject),
		invalid("tool choice function missing", chatBody(`"tool_choice":{"type":"function"}`), "tool_choice.function", "function reference is required"),
		invalid("tool choice name not a string", chatBody(`"tool_choice":{"type":"function","function":{"name":1}}`), "tool_choice.function.name", msgString),
		invalid("tool choice name missing", chatBody(`"tool_choice":{"type":"function","function":{}}`), "tool_choice.function.name", "function name is required"),
		invalidStrict("tool choice function unknown field", chatBody(`"tool_choice":{"type":"function","function":{"name":"f","extra":1}}`), "tool_choice.function.extra", msgUnknown),
		invalidStrict("tool choice unknown field", chatBody(`"tool_choice":{"type":"function","function":{"name":"f"},"extra":1}`), "tool_choice.extra", msgUnknown),
		invalid("max tokens not an integer", chatBody(`"max_tokens":"x"`), keyMaxTokens, msgInteger),
		invalid("max completion tokens not an integer", chatBody(`"max_completion_tokens":1.5`), keyMaxCompletion, msgInteger),
		invalid("stop not strings", chatBody(`"stop":5`), keyStop, "expected a string or an array of strings"),
		invalid("stop list not strings", chatBody(`"stop":["a",1]`), keyStop, "expected a string or an array of strings"),
		invalid("seed not an integer", chatBody(`"seed":"x"`), keySeed, msgInteger),
		invalid("response format not an object", chatBody(`"response_format":[]`), keyResponseFormat, msgObject),
		invalid("response format type not a string", chatBody(`"response_format":{"type":1}`), "response_format.type", msgString),
		invalid("json schema not an object", chatBody(`"response_format":{"type":"json_schema","json_schema":[]}`), "response_format.json_schema", msgObject),
		invalid("json schema with json object type", chatBody(`"response_format":{"type":"json_object","json_schema":{}}`), "response_format.json_schema", "json_schema requires type json_schema"),
		invalid("json schema definition missing", chatBody(`"response_format":{"type":"json_schema"}`), "response_format.json_schema", "json_schema definition is required"),
		invalid("json schema name not a string", chatBody(`"response_format":{"type":"json_schema","json_schema":{"name":1}}`), "response_format.json_schema.name", msgString),
		invalid("json schema description not a string", chatBody(`"response_format":{"type":"json_schema","json_schema":{"name":"a","description":1}}`), "response_format.json_schema.description", msgString),
		invalid("json schema missing schema", chatBody(`"response_format":{"type":"json_schema","json_schema":{"name":"a"}}`), "response_format.json_schema.schema", msgSchemaObj),
		invalid("json schema strict not a boolean", chatBody(`"response_format":{"type":"json_schema","json_schema":{"name":"a","schema":{},"strict":"yes"}}`), "response_format.json_schema.strict", msgBoolean),
		invalidStrict("json schema unknown field", chatBody(`"response_format":{"type":"json_schema","json_schema":{"name":"a","schema":{},"extra":1}}`), "response_format.json_schema.extra", msgUnknown),
		invalidStrict("response format unknown field", chatBody(`"response_format":{"type":"text","extra":1}`), "response_format.extra", msgUnknown),
		invalid("reasoning effort not a string", chatBody(`"reasoning_effort":1`), keyReasoningEffort, msgString),
		invalid("n not an integer", chatBody(`"n":"x"`), keyN, msgInteger),
		invalid("logit bias not an object", chatBody(`"logit_bias":[]`), keyLogitBias, msgObject),
		invalid("frequency penalty not a number", chatBody(`"frequency_penalty":"x"`), keyFrequencyPenalty, msgNumber),
		invalid("modalities not an array", chatBody(`"modalities":"text"`), keyModalities, msgArray),
		unsupported("modalities not text", chatBody(`"modalities":[1]`), keyModalities),
		invalid("verbosity not a string", chatBody(`"verbosity":1`), keyVerbosity, msgString),
	})
}

func TestDecodeChatAlternateShapes(t *testing.T) {
	raw := `{"model":"m","max_tokens":5,"stop":["a","b"],"top_p":0.5,"parallel_tool_calls":true,
		"metadata":{"k":"v"},"user":"","safety_identifier":"sid",
		"tools":[{"type":"function","function":{"name":"f","description":"d","parameters":{"type":"object"},"strict":false}}],
		"tool_choice":"none","response_format":{"type":"text"},"reasoning_effort":"minimal",
		"messages":[
			{"role":"system","content":[{"type":"text","text":"sys"}],"name":"ops"},
			{"role":"user","content":"hi"}
		]}`
	req, err := DecodeChat([]byte(raw), strict)
	require.NoError(t, err)
	require.NotNil(t, req.Input.MaxTokens)
	assert.Equal(t, 5, *req.Input.MaxTokens)
	assert.Equal(t, []string{"a", "b"}, req.Input.Stop)
	require.NotNil(t, req.Input.TopP)
	assert.Equal(t, 0.5, *req.Input.TopP)
	require.NotNil(t, req.Input.ParallelToolCalls)
	assert.True(t, *req.Input.ParallelToolCalls)
	assert.Equal(t, map[string]string{
		"k":                  "v",
		dialect.MetadataUser: "sid",
	}, req.Metadata, "empty user is ignored and safety_identifier is kept")
	require.Len(t, req.Input.Tools, 1)
	require.NotNil(t, req.Input.Tools[0].Strict)
	assert.False(t, *req.Input.Tools[0].Strict)
	assert.Equal(t, "d", req.Input.Tools[0].Description)
	assert.JSONEq(t, `{"type":"object"}`, string(req.Input.Tools[0].Parameters))
	assert.Equal(t, &llms.InferenceToolChoice{Mode: llms.ToolChoiceNone}, req.Input.ToolChoice)
	assert.Equal(t, &llms.InferenceFormat{Type: llms.FormatText}, req.Input.Format)
	assert.Equal(t, &llms.InferenceReasoning{Effort: llms.EffortMinimal}, req.Input.Reasoning)
	assert.Equal(t, []llms.InferenceMessage{
		{
			Role: llms.InferenceRoleSystem,
			Content: []llms.InferenceBlock{{
				Type: llms.BlockText,
				Text: "sys",
			}},
		},
		{
			Role: llms.InferenceRoleUser,
			Content: []llms.InferenceBlock{{
				Type: llms.BlockText,
				Text: "hi",
			}},
		},
	}, req.Input.Messages, "participant names carry no content")
}

func TestDecodeChatAssistantBlockOrder(t *testing.T) {
	sealed, err := dialect.SealOpaque("public", json.RawMessage(`{"sig":"s"}`))
	require.NoError(t, err)
	raw := chatMessages(chatUser, `{"role":"assistant","content":[{"type":"text","text":"a"},{"type":"refusal","refusal":"r"}],"refusal":"no",
		"reasoning_details":[
			{"type":"reasoning.encrypted","data":"`+sealed+`"},
			{"type":"reasoning.encrypted","data":""},
			{"type":"reasoning.text","text":"raw"}
		]}`, `{"role":"user","content":"more"}`)
	req, err := DecodeChat([]byte(raw), lenient)
	require.NoError(t, err)
	assert.Equal(t, []llms.InferenceBlock{
		{
			Type:   llms.BlockReasoning,
			Source: "public",
			Opaque: json.RawMessage(`{"sig":"s"}`),
		},
		{
			Type: llms.BlockReasoning,
		},
		{
			Type: llms.BlockReasoning,
			Text: "raw",
		},
		{
			Type: llms.BlockText,
			Text: "a",
		},
		{
			Type: llms.BlockRefusal,
			Text: "r",
		},
		{
			Type: llms.BlockRefusal,
			Text: "no",
		},
	}, req.Input.Messages[1].Content)
}

func TestDecodeResponsesRejections(t *testing.T) {
	runDecodeCases(t, DecodeResponses, []decodeCase{
		invalid("malformed body", `not json`, paramRequest, "malformed JSON"),
		invalid("root not an object", `"hi"`, paramRequest, "expected a JSON object"),
		invalid("model not a string", `{"model":1,"input":"hi"}`, keyModel, msgString),
		invalid("store not a boolean", responsesBody(`"store":1`), keyStore, msgBoolean),
		invalid("instructions not a string", responsesBody(`"instructions":1`), keyInstructions, msgString),
		invalid("input not text or items", `{"model":"m","input":5}`, keyInput, "expected a string or an array of input items"),
		invalid("item not an object", responsesInput(`1`), "input[0]", msgObject),
		invalid("item type not a string", responsesInput(`{"type":1}`), "input[0].type", msgString),
		invalid("message role not a string", responsesInput(`{"role":1,"content":"x"}`), "input[0].role", msgString),
		invalid("message content missing", responsesInput(`{"role":"user"}`), "input[0].content", msgRequired),
		unsupported("user message with output text", responsesInput(`{"role":"user","content":[{"type":"output_text","text":"x"}]}`), "input[0].content[0].type"),
		invalidStrict("message unknown field", responsesInput(`{"role":"user","content":"x","extra":1}`), "input[0].extra", msgUnknown),
		invalid("call ID not a string", responsesInput(`{"type":"function_call","call_id":1}`), "input[0].call_id", msgString),
		invalid("call name not a string", responsesInput(`{"type":"function_call","call_id":"c","name":1}`), "input[0].name", msgString),
		invalid("call arguments not a string", responsesInput(`{"type":"function_call","call_id":"c","name":"f","arguments":{}}`), "input[0].arguments", msgString),
		invalidStrict("call unknown field", responsesInput(`{"type":"function_call","call_id":"c","name":"f","arguments":"{}","extra":1}`), "input[0].extra", msgUnknown),
		invalid("output call ID not a string", responsesInput(`{"type":"function_call_output","call_id":1}`), "input[0].call_id", msgString),
		invalid("output not text", responsesInput(`{"type":"function_call_output","call_id":"c","output":5}`), "input[0].output", msgParts),
		invalid("output missing", responsesInput(`{"type":"function_call_output","call_id":"c"}`), "input[0].output", "output is required"),
		invalidStrict("output unknown field", responsesInput(`{"type":"function_call_output","call_id":"c","output":"r","extra":1}`), "input[0].extra", msgUnknown),
		invalid("reasoning summary not an array", responsesInput(`{"type":"reasoning","summary":"x"}`), "input[0].summary", msgArray),
		invalid("reasoning summary part not an object", responsesInput(`{"type":"reasoning","summary":[1]}`), "input[0].summary[0]", msgObject),
		invalid("reasoning summary type not a string", responsesInput(`{"type":"reasoning","summary":[{"type":1}]}`), "input[0].summary[0].type", msgString),
		unsupported("reasoning summary type unknown", responsesInput(`{"type":"reasoning","summary":[{"type":"reasoning_text","text":"x"}]}`), "input[0].summary[0].type"),
		invalid("reasoning summary text not a string", responsesInput(`{"type":"reasoning","summary":[{"type":"summary_text","text":1}]}`), "input[0].summary[0].text", msgString),
		invalidStrict("reasoning summary unknown field", responsesInput(`{"type":"reasoning","summary":[{"type":"summary_text","text":"x","extra":1}]}`), "input[0].summary[0].extra", msgUnknown),
		invalid("encrypted content not a string", responsesInput(`{"type":"reasoning","encrypted_content":1}`), "input[0].encrypted_content", msgString),
		invalidStrict("reasoning item unknown field", responsesInput(`{"type":"reasoning","extra":1}`), "input[0].extra", msgUnknown),
		invalid("tools not an array", responsesBody(`"tools":{}`), keyTools, msgArray),
		invalid("tool parameters not an object", responsesBody(`"tools":[{"type":"function","name":"f","parameters":"x"}]`), "tools[0].parameters", msgSchemaObj),
		invalid("tool choice name missing", responsesBody(`"tool_choice":{"type":"function"}`), "tool_choice.name", "function name is required"),
		invalid("max output tokens not an integer", responsesBody(`"max_output_tokens":"x"`), keyMaxOutputTokens, msgInteger),
		invalid("text not an object", responsesBody(`"text":[]`), keyText, msgObject),
		unsupported("text format unknown", responsesBody(`"text":{"format":{"type":"grammar"}}`), "text.format.type"),
		invalid("text verbosity not a string", responsesBody(`"text":{"verbosity":1}`), "text.verbosity", msgString),
		invalidStrict("text unknown field", responsesBody(`"text":{"extra":1}`), "text.extra", msgUnknown),
		invalid("inline schema missing", responsesBody(`"text":{"format":{"type":"json_schema","name":"a"}}`), "text.format.schema", msgSchemaObj),
		invalid("reasoning not an object", responsesBody(`"reasoning":[]`), keyReasoning, msgObject),
		invalid("reasoning effort not a string", responsesBody(`"reasoning":{"effort":1}`), "reasoning.effort", msgString),
		invalidStrict("reasoning unknown field", responsesBody(`"reasoning":{"effort":"low","extra":1}`), "reasoning.extra", msgUnknown),
		invalid("include not an array", responsesBody(`"include":"x"`), keyInclude, msgArray),
		unsupported("include not strings", responsesBody(`"include":[1]`), keyInclude),
		invalid("background not a boolean", responsesBody(`"background":"no"`), keyBackground, msgBoolean),
		invalid("truncation not a string", responsesBody(`"truncation":1`), keyTruncation, msgString),
	})
}

func TestDecodeResponsesAlternateShapes(t *testing.T) {
	raw := `{"model":"m","input":[
		{"role":"user","content":"hi"},
		{"type":"reasoning","summary":[{"type":"summary_text","text":"s1"},{"type":"summary_text","text":"s2"}],"content":[{"type":"reasoning_text","text":"raw"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a"},{"type":"refusal","refusal":"r"}]},
		{"type":"function_call","call_id":"c1","name":"f","arguments":"{}"},
		{"type":"function_call","call_id":"c2","name":"f","arguments":"{}"},
		{"type":"function_call_output","call_id":"c1","output":"one"},
		{"type":"function_call_output","call_id":"c2","output":[{"type":"input_text","text":"t"},{"type":"input_text","text":"wo"}]}
	],
	"text":{"format":{"type":"json_schema","name":"a","description":"d","schema":{"type":"object"},"strict":true}},
	"reasoning":{"summary":"auto"},
	"tool_choice":{"type":"function","name":"f"},
	"tools":[{"type":"function","name":"f"}]}`
	req, err := DecodeResponses([]byte(raw), strict)
	require.NoError(t, err)
	assert.Nil(t, req.Input.Reasoning, "summary preference alone does not enable reasoning controls")
	assert.Equal(t, &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "a",
		Schema: json.RawMessage(`{"type":"object"}`),
		Strict: true,
	}, req.Input.Format)
	assert.Equal(t, &llms.InferenceToolChoice{
		Mode: llms.ToolChoiceFunction,
		Name: "f",
	}, req.Input.ToolChoice)
	require.Len(t, req.Input.Tools, 1)
	assert.JSONEq(t, string(emptyParameters), string(req.Input.Tools[0].Parameters))
	assert.Nil(t, req.Input.Tools[0].Strict)

	assert.Equal(t, []llms.InferenceMessage{
		{
			Role: llms.InferenceRoleUser,
			Content: []llms.InferenceBlock{{
				Type: llms.BlockText,
				Text: "hi",
			}},
		},
		{
			Role: llms.InferenceRoleAssistant,
			Content: []llms.InferenceBlock{
				{
					Type: llms.BlockReasoning,
					Text: "s1" + summarySeparator + "s2",
				},
				{
					Type: llms.BlockText,
					Text: "a",
				},
				{
					Type: llms.BlockRefusal,
					Text: "r",
				},
				{
					Type:      llms.BlockFunctionCall,
					ID:        "c1",
					Name:      "f",
					Arguments: "{}",
				},
				{
					Type:      llms.BlockFunctionCall,
					ID:        "c2",
					Name:      "f",
					Arguments: "{}",
				},
			},
		},
		{
			Role: llms.InferenceRoleTool,
			Content: []llms.InferenceBlock{
				{
					Type: llms.BlockFunctionOutput,
					ID:   "c1",
					Text: "one",
				},
				{
					Type: llms.BlockFunctionOutput,
					ID:   "c2",
					Text: "two",
				},
			},
		},
	}, req.Input.Messages)
}

func TestDecodeEffort(t *testing.T) {
	for wire, want := range map[string]llms.Effort{
		effortMinimal: llms.EffortMinimal,
		effortLow:     llms.EffortLow,
		effortMedium:  llms.EffortMedium,
		effortHigh:    llms.EffortHigh,
	} {
		req, err := DecodeResponses([]byte(responsesBody(`"reasoning":{"effort":"`+wire+`"}`)), strict)
		require.NoError(t, err, wire)
		assert.Equal(t, &llms.InferenceReasoning{Effort: want}, req.Input.Reasoning)
	}
}
