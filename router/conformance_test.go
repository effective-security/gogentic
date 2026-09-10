package router_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/llms/anthropic"
	"github.com/effective-security/gogentic/pkg/llms/bedrock"
	"github.com/effective-security/gogentic/pkg/llms/googleai"
	"github.com/effective-security/gogentic/pkg/llms/openai"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	anthropicdialect "github.com/effective-security/gogentic/router/dialect/anthropic"
	openaidialect "github.com/effective-security/gogentic/router/dialect/openai"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The conformance matrix drives every public dialect through the router into
// every connector over a mocked transport. It proves wire contracts, not live
// provider behaviour; see Documentation/router.md for the live smoke test.

const (
	familyOpenAI     = "openai"
	familyOpenAIChat = "openai-chat"
	familyAnthropic  = "anthropic"
	familyGoogleAI   = "googleai"
	familyBedrock    = "bedrock"

	dialectChat      = "chat"
	dialectResponses = "responses"
	dialectMessages  = "messages"

	modeText      = "text"
	modeTool      = "tool"
	modeParallel  = "parallel"
	modeSchema    = "schema"
	modeCombined  = "combined"
	modeReasoning = "reasoning"
	modeRefusal   = "refusal"
	modeLength    = "length"
	modeNoUsage   = "nousage"

	targetID      = "public"
	fixtureModel  = "fixture"
	opaqueOpenAI  = "ENCRYPTED-STATE"
	opaqueSig     = "SIGNATURE-STATE"
	opaqueGoogle  = "U0lHTkFUVVJF" // base64 of SIGNATURE
	upstreamRate  = `{"error":{"code":429,"type":"rate_limit_error","message":"internal secret"}}`
	schemaJSON    = `{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`
	toolSchema    = `{"type":"object","properties":{}}`
	answerText    = "hello"
	answerJSON    = `{"ok":true}`
	reasoningText = "thinking hard"
)

var families = []string{familyOpenAI, familyOpenAIChat, familyAnthropic, familyGoogleAI, familyBedrock}
var dialects = []string{dialectChat, dialectResponses, dialectMessages}
var modes = []string{modeText, modeTool, modeParallel, modeSchema, modeCombined, modeReasoning, modeRefusal, modeLength, modeNoUsage}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// upstream records every request body and answers with a canned provider response.
type upstream struct {
	t      *testing.T
	family string
	mode   string
	bodies []map[string]any
	raw    []string
	fail   bool
}

func (u *upstream) last() map[string]any {
	require.NotEmpty(u.t, u.bodies)
	return u.bodies[len(u.bodies)-1]
}

func (u *upstream) lastRaw() string {
	require.NotEmpty(u.t, u.raw)
	return u.raw[len(u.raw)-1]
}

func (u *upstream) serve(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	require.NoError(u.t, err)
	var v map[string]any
	require.NoError(u.t, json.Unmarshal(body, &v), string(body))
	u.bodies = append(u.bodies, v)
	u.raw = append(u.raw, string(body))
	status := http.StatusOK
	output := u.response(r)
	if u.fail {
		status = http.StatusTooManyRequests
		output = upstreamRate
	}
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(output)),
		Request: r,
	}, nil
}

func (u *upstream) calls() int {
	switch u.mode {
	case modeTool, modeCombined:
		return 1
	case modeParallel:
		return 2
	}
	return 0
}

func (u *upstream) text() string {
	if u.mode == modeSchema || u.mode == modeCombined {
		return answerJSON
	}
	return answerText
}

func quoted(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (u *upstream) response(r *http.Request) string {
	t := u.t
	usage := true
	if u.mode == modeNoUsage {
		usage = false
	}
	switch u.family {
	case familyOpenAI:
		assert.Equal(t, "/v1/responses", r.URL.Path)
		var items []string
		if u.mode == modeReasoning {
			items = append(items, `{"type":"reasoning","id":"rs_up","summary":[{"type":"summary_text","text":`+quoted(reasoningText)+`}],"encrypted_content":`+quoted(opaqueOpenAI)+`}`)
		}
		for i := range u.calls() {
			items = append(items, `{"type":"function_call","id":"fc_up`+itoa(i)+`","call_id":"call_`+itoa(i)+`","name":"lookup","arguments":"{}","status":"completed"}`)
		}
		status := `"status":"completed"`
		switch u.mode {
		case modeRefusal:
			items = append(items, `{"type":"message","id":"msg_up","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"no"}]}`)
		case modeLength:
			status = `"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}`
			items = append(items, `{"type":"message","id":"msg_up","role":"assistant","status":"incomplete","content":[{"type":"output_text","text":"partial","annotations":[]}]}`)
		default:
			if u.calls() == 0 {
				items = append(items, `{"type":"message","id":"msg_up","role":"assistant","status":"completed","content":[{"type":"output_text","text":`+quoted(u.text())+`,"annotations":[],"logprobs":[]}]}`)
			}
		}
		out := `{"id":"resp_up","object":"response","model":"fixture",` + status + `,"output":[` + strings.Join(items, ",") + `]`
		if usage {
			out += `,"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cached_tokens":1},"output_tokens_details":{"reasoning_tokens":1}}`
		}
		return out + `}`
	case familyOpenAIChat:
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		message := `{"role":"assistant","content":` + quoted(u.text()) + `,"refusal":null}`
		finish := "stop"
		switch {
		case u.calls() > 0:
			var calls []string
			for i := range u.calls() {
				calls = append(calls, `{"id":"call_`+itoa(i)+`","type":"function","function":{"name":"lookup","arguments":"{}"}}`)
			}
			message = `{"role":"assistant","content":null,"tool_calls":[` + strings.Join(calls, ",") + `]}`
			finish = "tool_calls"
		case u.mode == modeRefusal:
			message = `{"role":"assistant","content":null,"refusal":"no"}`
		case u.mode == modeLength:
			message = `{"role":"assistant","content":"partial"}`
			finish = "length"
		}
		out := `{"id":"chatcmpl_up","object":"chat.completion","choices":[{"index":0,"message":` + message + `,"finish_reason":"` + finish + `","logprobs":null}]`
		if usage {
			out += `,"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}`
		}
		return out + `}`
	case familyAnthropic:
		assert.Equal(t, "/v1/messages", r.URL.Path)
		var blocks []string
		if u.mode == modeReasoning {
			blocks = append(blocks, `{"type":"thinking","thinking":`+quoted(reasoningText)+`,"signature":`+quoted(opaqueSig)+`}`)
		}
		for i := range u.calls() {
			blocks = append(blocks, `{"type":"tool_use","id":"call_`+itoa(i)+`","name":"lookup","input":{}}`)
		}
		stop := "end_turn"
		switch {
		case u.calls() > 0:
			stop = "tool_use"
		case u.mode == modeRefusal:
			stop = "refusal"
		case u.mode == modeLength:
			stop = "max_tokens"
			blocks = append(blocks, `{"type":"text","text":"partial"}`)
		default:
			blocks = append(blocks, `{"type":"text","text":`+quoted(u.text())+`}`)
		}
		out := `{"id":"msg_up","type":"message","role":"assistant","model":"fixture","content":[` + strings.Join(blocks, ",") + `],"stop_reason":"` + stop + `","stop_sequence":null`
		if usage {
			out += `,"usage":{"input_tokens":7,"cache_read_input_tokens":2,"cache_creation_input_tokens":1,"output_tokens":2}`
		}
		return out + `}`
	case familyGoogleAI:
		assert.Contains(t, r.URL.Path, ":generateContent")
		var parts []string
		if u.mode == modeReasoning {
			parts = append(parts, `{"text":`+quoted(reasoningText)+`,"thought":true,"thoughtSignature":`+quoted(opaqueGoogle)+`}`)
		}
		for i := range u.calls() {
			sig := ""
			if i == 0 && u.mode == modeReasoning {
				sig = `,"thoughtSignature":` + quoted(opaqueGoogle)
			}
			parts = append(parts, `{"functionCall":{"name":"lookup","args":{}}`+sig+`}`)
		}
		finish := "STOP"
		switch {
		case u.calls() > 0:
		case u.mode == modeRefusal:
			finish = "SAFETY"
		case u.mode == modeLength:
			finish = "MAX_TOKENS"
			parts = append(parts, `{"text":"partial"}`)
		default:
			parts = append(parts, `{"text":`+quoted(u.text())+`}`)
		}
		content := `"content":{"role":"model","parts":[` + strings.Join(parts, ",") + `]},`
		if len(parts) == 0 {
			content = ""
		}
		out := `{"responseId":"resp_up","candidates":[{` + content + `"finishReason":"` + finish + `"}]`
		if usage {
			out += `,"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1,"thoughtsTokenCount":1,"totalTokenCount":12}`
		}
		return out + `}`
	case familyBedrock:
		assert.Contains(t, r.URL.Path, "/converse")
		var blocks []string
		if u.mode == modeReasoning {
			blocks = append(blocks, `{"reasoningContent":{"reasoningText":{"text":`+quoted(reasoningText)+`,"signature":`+quoted(opaqueSig)+`}}}`)
		}
		for i := range u.calls() {
			blocks = append(blocks, `{"toolUse":{"toolUseId":"call_`+itoa(i)+`","name":"lookup","input":{}}}`)
		}
		stop := "end_turn"
		switch {
		case u.calls() > 0:
			stop = "tool_use"
		case u.mode == modeRefusal:
			stop = "content_filtered"
		case u.mode == modeLength:
			stop = "max_tokens"
			blocks = append(blocks, `{"text":"partial"}`)
		default:
			blocks = append(blocks, `{"text":`+quoted(u.text())+`}`)
		}
		out := `{"output":{"message":{"role":"assistant","content":[` + strings.Join(blocks, ",") + `]}},"stopReason":"` + stop + `","metrics":{"latencyMs":1}`
		if usage {
			out += `,"usage":{"inputTokens":7,"cacheReadInputTokens":2,"cacheWriteInputTokens":1,"outputTokens":2,"totalTokens":12}`
		}
		return out + `}`
	}
	t.Fatalf("unknown family %s", u.family)
	return ""
}

func itoa(i int) string {
	return string(rune('0' + i))
}

func connector(t *testing.T, u *upstream) llms.InferenceModel {
	t.Helper()
	client := &http.Client{Transport: roundTrip(u.serve)}
	var m llms.Model
	var err error
	switch u.family {
	case familyOpenAI:
		m, err = openai.New(openai.WithModel(fixtureModel), openai.WithToken(fixtureModel), openai.WithHTTPClient(client))
	case familyOpenAIChat:
		m, err = openai.New(openai.WithModel(fixtureModel), openai.WithToken(fixtureModel), openai.WithHTTPClient(client), openai.WithInferenceAPI(openai.InferenceAPIChat))
	case familyAnthropic:
		m, err = anthropic.New(anthropic.WithModel(fixtureModel), anthropic.WithToken(fixtureModel), anthropic.WithHTTPClient(client))
	case familyGoogleAI:
		m, err = googleai.New(context.Background(), googleai.WithDefaultModel(fixtureModel), googleai.WithAPIKey(fixtureModel), googleai.WithHTTPClient(client))
	case familyBedrock:
		c := bedrockruntime.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider(fixtureModel, fixtureModel, ""),
			HTTPClient:  client,
		})
		m, err = bedrock.New(bedrock.WithClient(c), bedrock.WithModel(fixtureModel), bedrock.WithConverse())
	default:
		t.Fatalf("unknown family %s", u.family)
	}
	require.NoError(t, err)
	c, err := router.Connector(m)
	require.NoError(t, err)
	return c
}

func newRouter(t *testing.T, id string, c llms.InferenceModel) *router.Router {
	t.Helper()
	r, err := router.New(router.Config{
		Targets: []router.Target{{
			ID:           id,
			BackendModel: fixtureModel,
			Connector:    c,
			Features: router.Features{
				Tools:           true,
				JSON:            true,
				JSONSchema:      true,
				StrictSchema:    true,
				StrictTools:     true,
				SchemaWithTools: true,
				Reasoning:       true,
				Seed:            true,
			},
		}},
	})
	require.NoError(t, err)
	return r
}

// codec bundles the three dialect entry points behind one shape for the matrix.
type codec struct {
	decode func([]byte) (router.Request, error)
	encode func(*router.Result, router.Request) ([]byte, error)
	status func(error) (int, []byte)
}

func codecFor(name string) codec {
	opts := dialect.DecodeOptions{}
	enc := dialect.EncodeOptions{}
	switch name {
	case dialectChat:
		return codec{
			decode: func(b []byte) (router.Request, error) { return openaidialect.DecodeChat(b, opts) },
			encode: func(r *router.Result, _ router.Request) ([]byte, error) { return openaidialect.EncodeChat(r, enc) },
			status: openaidialect.EncodeError,
		}
	case dialectResponses:
		return codec{
			decode: func(b []byte) (router.Request, error) { return openaidialect.DecodeResponses(b, opts) },
			encode: func(r *router.Result, q router.Request) ([]byte, error) {
				return openaidialect.EncodeResponses(r, q, enc)
			},
			status: openaidialect.EncodeError,
		}
	default:
		return codec{
			decode: func(b []byte) (router.Request, error) { return anthropicdialect.DecodeMessages(b, opts) },
			encode: func(r *router.Result, _ router.Request) ([]byte, error) {
				return anthropicdialect.EncodeMessages(r, enc)
			},
			status: anthropicdialect.EncodeError,
		}
	}
}

// initialBody builds the first request of a conversation in the given dialect.
func initialBody(name, mode string) string {
	tools := ""
	schema := ""
	reasoning := ""
	switch name {
	case dialectChat:
		if mode == modeTool || mode == modeParallel || mode == modeCombined {
			tools = `,"tools":[{"type":"function","function":{"name":"lookup","parameters":` + toolSchema + `}}],"tool_choice":"auto"`
		}
		if mode == modeSchema || mode == modeCombined {
			schema = `,"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":` + schemaJSON + `,"strict":true}}`
		}
		temperature := `"temperature":0,`
		if mode == modeReasoning {
			reasoning = `,"reasoning_effort":"low"`
			temperature = ""
		}
		return `{"model":"public",` + temperature + `"messages":[{"role":"user","content":"hello"}]` + tools + schema + reasoning + `}`
	case dialectResponses:
		if mode == modeTool || mode == modeParallel || mode == modeCombined {
			tools = `,"tools":[{"type":"function","name":"lookup","parameters":` + toolSchema + `}],"tool_choice":"auto"`
		}
		if mode == modeSchema || mode == modeCombined {
			schema = `,"text":{"format":{"type":"json_schema","name":"answer","schema":` + schemaJSON + `,"strict":true}}`
		}
		temperature := `"temperature":0,`
		if mode == modeReasoning {
			reasoning = `,"reasoning":{"effort":"low"}`
			temperature = ""
		}
		return `{"model":"public",` + temperature + `"input":"hello"` + tools + schema + reasoning + `}`
	default:
		if mode == modeTool || mode == modeParallel || mode == modeCombined {
			tools = `,"tools":[{"name":"lookup","input_schema":` + toolSchema + `}],"tool_choice":{"type":"auto"}`
		}
		if mode == modeSchema || mode == modeCombined {
			schema = `,"output_config":{"format":{"type":"json_schema","schema":` + schemaJSON + `}}`
		}
		temperature := `"temperature":0,`
		if mode == modeReasoning {
			reasoning = `,"thinking":{"type":"enabled","budget_tokens":1024}`
			temperature = ""
		}
		return `{"model":"public","max_tokens":4096,` + temperature + `"messages":[{"role":"user","content":"hello"}]` + tools + schema + reasoning + `}`
	}
}

// continuationBody replays the encoded public response the way an SDK client does
// and answers every tool call.
func continuationBody(t *testing.T, name string, encoded []byte, initial string) string {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(encoded, &out))
	var init map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(initial), &init))
	delete(init, "reasoning_effort")
	delete(init, "reasoning")
	switch name {
	case dialectChat:
		choice := out["choices"].([]any)[0].(map[string]any)
		message := choice["message"].(map[string]any)
		messages := []any{
			map[string]any{"role": "user", "content": "hello"},
			message,
		}
		calls, _ := message["tool_calls"].([]any)
		for _, c := range calls {
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": c.(map[string]any)["id"],
				"content":      "result",
			})
		}
		if len(calls) == 0 {
			messages = append(messages, map[string]any{"role": "user", "content": "next"})
		}
		init["messages"] = mustJSON(t, messages)
	case dialectResponses:
		items := []any{map[string]any{"role": "user", "content": "hello"}}
		output := out["output"].([]any)
		items = append(items, output...)
		calls := 0
		for _, item := range output {
			m := item.(map[string]any)
			if m["type"] == "function_call" {
				calls++
				items = append(items, map[string]any{
					"type":    "function_call_output",
					"call_id": m["call_id"],
					"output":  "result",
				})
			}
		}
		if calls == 0 {
			items = append(items, map[string]any{"role": "user", "content": "next"})
		}
		init["input"] = mustJSON(t, items)
	default:
		content := out["content"].([]any)
		var results []any
		for _, b := range content {
			m := b.(map[string]any)
			if m["type"] == "tool_use" {
				results = append(results, map[string]any{
					"type":        "tool_result",
					"tool_use_id": m["id"],
					"content":     "result",
				})
			}
		}
		var last any = map[string]any{"role": "user", "content": "next"}
		if len(results) > 0 {
			last = map[string]any{"role": "user", "content": results}
		}
		init["messages"] = mustJSON(t, []any{
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": content},
			last,
		})
	}
	return string(mustJSON(t, init))
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func decodePublic(t *testing.T, name string, encoded []byte) (text string, calls int, reasoning int, refusal bool) {
	t.Helper()
	switch name {
	case dialectChat:
		var out openaisdk.ChatCompletion
		require.NoError(t, json.Unmarshal(encoded, &out))
		require.Len(t, out.Choices, 1)
		assert.Equal(t, targetID, out.Model)
		var raw map[string]any
		require.NoError(t, json.Unmarshal(encoded, &raw))
		message := raw["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
		details, _ := message["reasoning_details"].([]any)
		for _, d := range details {
			if d.(map[string]any)["type"] == "reasoning.encrypted" {
				reasoning++
			}
		}
		return out.Choices[0].Message.Content, len(out.Choices[0].Message.ToolCalls), reasoning, out.Choices[0].Message.Refusal != ""
	case dialectResponses:
		var out responses.Response
		require.NoError(t, json.Unmarshal(encoded, &out))
		assert.Equal(t, targetID, out.Model)
		for _, item := range out.Output {
			switch item.Type {
			case "function_call":
				calls++
			case "reasoning":
				reasoning++
			case "message":
				for _, part := range item.Content {
					if part.Type == "refusal" {
						refusal = true
					}
				}
			}
		}
		return out.OutputText(), calls, reasoning, refusal
	default:
		var out sdk.Message
		require.NoError(t, json.Unmarshal(encoded, &out))
		assert.Equal(t, sdk.Model(targetID), out.Model)
		for _, b := range out.Content {
			switch b.Type {
			case "tool_use":
				calls++
			case "thinking", "redacted_thinking":
				reasoning++
			case "text":
				text += b.Text
			}
		}
		return text, calls, reasoning, out.StopReason == "refusal"
	}
}

func TestConformanceMatrix(t *testing.T) {
	for _, family := range families {
		for _, name := range dialects {
			for _, mode := range modes {
				if mode == modeReasoning && family == familyOpenAIChat {
					continue // Chat Completions upstream has no reasoning slot.
				}
				t.Run(family+"/"+name+"/"+mode, func(t *testing.T) {
					up := &upstream{t: t, family: family, mode: mode}
					c := connector(t, up)
					rt := newRouter(t, targetID, c)
					cd := codecFor(name)

					initial := initialBody(name, mode)
					req, err := cd.decode([]byte(initial))
					require.NoError(t, err, initial)
					res, err := rt.Generate(context.Background(), req)
					require.NoError(t, err)
					require.Len(t, up.bodies, 1)
					assertUpstreamRequest(t, up, name, mode)

					encoded, err := cd.encode(res, req)
					require.NoError(t, err)
					text, calls, reasoning, refusal := decodePublic(t, name, encoded)
					switch mode {
					case modeRefusal:
						assert.True(t, refusal || res.Response.FinishReason == llms.FinishContentFilter)
						return
					case modeLength:
						assert.Equal(t, llms.FinishLength, res.Response.FinishReason)
						return
					case modeNoUsage:
						assert.False(t, res.Response.UsageKnown)
						assert.Equal(t, answerText, text)
						return
					case modeReasoning:
						assert.Equal(t, 1, reasoning, string(encoded))
						var opaque int
						for _, b := range res.Response.Content {
							if b.Type == llms.BlockReasoning && len(b.Opaque) > 0 {
								opaque++
							}
						}
						assert.Equal(t, 1, opaque, "the Go result must carry the opaque state")
					}
					assert.True(t, res.Response.UsageKnown)
					assert.EqualValues(t, 12, res.Response.Usage.TotalTokens)
					assert.Equal(t, up.calls(), calls)
					if up.calls() == 0 {
						assert.Equal(t, up.text(), text)
					}

					// Second turn: replay the public response as an SDK client would.
					next := continuationBody(t, name, encoded, initial)
					req, err = cd.decode([]byte(next))
					require.NoError(t, err, next)
					_, err = rt.Generate(context.Background(), req)
					require.NoError(t, err, next)
					require.Len(t, up.bodies, 2)
					assertContinuation(t, up, mode)
				})
			}
		}
	}
}

func assertUpstreamRequest(t *testing.T, up *upstream, name, mode string) {
	t.Helper()
	body := up.last()
	if mode != modeReasoning || name == dialectMessages {
		switch up.family {
		case familyOpenAI, familyOpenAIChat, familyAnthropic:
			if mode != modeReasoning {
				assert.Equal(t, float64(0), body["temperature"], "explicit zero temperature must survive")
			}
			assert.NotContains(t, body, "top_p")
		case familyGoogleAI:
			cfg, _ := body["generationConfig"].(map[string]any)
			if mode != modeReasoning {
				assert.Equal(t, float64(0), cfg["temperature"])
			}
		case familyBedrock:
			cfg, _ := body["inferenceConfig"].(map[string]any)
			if mode != modeReasoning {
				assert.Equal(t, float64(0), cfg["temperature"])
			}
		}
	}
	if mode == modeSchema || mode == modeCombined {
		switch up.family {
		case familyOpenAI:
			assert.Contains(t, body, "text")
		case familyOpenAIChat:
			assert.Contains(t, body, "response_format")
		case familyAnthropic:
			assert.Contains(t, body, "output_config")
		case familyGoogleAI:
			assert.Contains(t, body["generationConfig"], "responseJsonSchema")
		case familyBedrock:
			assert.Contains(t, body, "outputConfig")
		}
	}
	if mode == modeReasoning {
		switch up.family {
		case familyOpenAI:
			assert.Contains(t, body, "reasoning")
			assert.Contains(t, up.lastRaw(), "reasoning.encrypted_content")
		case familyAnthropic:
			assert.Contains(t, body, "thinking")
		case familyGoogleAI:
			assert.Contains(t, body["generationConfig"], "thinkingConfig")
		case familyBedrock:
			assert.Contains(t, body, "additionalModelRequestFields")
		}
	}
}

func assertContinuation(t *testing.T, up *upstream, mode string) {
	t.Helper()
	body := up.last()
	raw := up.lastRaw()
	want := up.calls()
	switch up.family {
	case familyOpenAI:
		input := body["input"].([]any)
		outputs := 0
		for _, item := range input {
			m := item.(map[string]any)
			if m["type"] == "function_call_output" {
				outputs++
				assert.Equal(t, "result", m["output"])
			}
		}
		assert.Equal(t, want, outputs)
		if mode == modeReasoning {
			assert.Contains(t, raw, opaqueOpenAI, "reasoning state must be replayed verbatim")
		}
	case familyOpenAIChat:
		messages := body["messages"].([]any)
		outputs := 0
		for _, item := range messages {
			m := item.(map[string]any)
			if m["role"] == "tool" {
				outputs++
				assert.Equal(t, "result", m["content"])
			}
		}
		assert.Equal(t, want, outputs)
	case familyAnthropic:
		messages := body["messages"].([]any)
		if want > 0 {
			last := messages[len(messages)-1].(map[string]any)
			content := last["content"].([]any)
			assert.Len(t, content, want, "all results in one user turn")
			assert.Equal(t, "tool_result", content[0].(map[string]any)["type"])
		}
		if mode == modeReasoning {
			assert.Contains(t, raw, opaqueSig)
		}
	case familyGoogleAI:
		contents := body["contents"].([]any)
		if want > 0 {
			last := contents[len(contents)-1].(map[string]any)
			parts := last["parts"].([]any)
			assert.Len(t, parts, want, "all function responses in one content")
			assert.Contains(t, parts[0].(map[string]any), "functionResponse")
		}
		if mode == modeReasoning {
			assert.Contains(t, raw, opaqueGoogle)
		}
	case familyBedrock:
		messages := body["messages"].([]any)
		if want > 0 {
			last := messages[len(messages)-1].(map[string]any)
			content := last["content"].([]any)
			assert.Len(t, content, want, "all tool results in one user turn")
			assert.Contains(t, content[0].(map[string]any), "toolResult")
		}
		if mode == modeReasoning {
			assert.Contains(t, raw, opaqueSig)
		}
	}
}

func TestForeignReasoningStateIsRefused(t *testing.T) {
	for _, family := range []string{familyOpenAI, familyAnthropic, familyGoogleAI, familyBedrock} {
		t.Run(family, func(t *testing.T) {
			up := &upstream{t: t, family: family, mode: modeReasoning}
			c := connector(t, up)
			cd := codecFor(dialectResponses)
			rt := newRouter(t, targetID, c)
			initial := initialBody(dialectResponses, modeReasoning)
			req, err := cd.decode([]byte(initial))
			require.NoError(t, err)
			res, err := rt.Generate(context.Background(), req)
			require.NoError(t, err)
			encoded, err := cd.encode(res, req)
			require.NoError(t, err)
			next := continuationBody(t, dialectResponses, encoded, initial)
			next = strings.Replace(next, `"model":"public"`, `"model":"other"`, 1)

			other := newRouter(t, "other", connector(t, &upstream{t: t, family: family, mode: modeText}))
			req, err = cd.decode([]byte(next))
			require.NoError(t, err)
			_, err = other.Generate(context.Background(), req)
			re := router.AsError(err)
			require.NotNil(t, re)
			assert.Equal(t, router.KindUnsupported, re.Kind, next)
			assert.Equal(t, "messages.reasoning", re.Param)
		})
	}
}

func TestUpstreamFailureIsSanitized(t *testing.T) {
	for _, family := range families {
		t.Run(family, func(t *testing.T) {
			up := &upstream{t: t, family: family, mode: modeText, fail: true}
			rt := newRouter(t, targetID, connector(t, up))
			for _, name := range dialects {
				cd := codecFor(name)
				req, err := cd.decode([]byte(initialBody(name, modeText)))
				require.NoError(t, err)
				_, err = rt.Generate(context.Background(), req)
				require.Error(t, err)
				status, body := cd.status(err)
				assert.Equal(t, http.StatusTooManyRequests, status)
				assert.NotContains(t, string(body), "secret")
			}
		})
	}
}
