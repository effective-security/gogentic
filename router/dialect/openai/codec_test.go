package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
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

// sdkChatReplay is what the OpenAI SDKs send when a prior assistant message is
// appended to the history verbatim.
const sdkChatReplay = `{"model":"m","messages":[
 {"role":"user","content":"lookup"},
 {"role":"assistant","content":null,"refusal":null,"annotations":[],"audio":null,
  "tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
 {"role":"tool","tool_call_id":"call_1","content":"result"}]}`

const sdkResponsesReplay = `{"model":"m","input":[
 {"role":"user","content":"lookup"},
 {"id":"msg_1","type":"message","role":"assistant","status":"completed",
  "content":[{"type":"output_text","text":"calling","annotations":[],"logprobs":[]}]},
 {"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}","status":"completed"},
 {"type":"function_call_output","call_id":"call_1","output":"result"}]}`

func TestSDKShapedReplay(t *testing.T) {
	req, err := DecodeChat([]byte(sdkChatReplay), lenient)
	require.NoError(t, err)
	require.Len(t, req.Input.Messages, 3)
	assistant := req.Input.Messages[1]
	assert.Equal(t, llms.InferenceRoleAssistant, assistant.Role)
	require.Len(t, assistant.Content, 1)
	assert.Equal(t, llms.BlockFunctionCall, assistant.Content[0].Type)
	assert.Equal(t, "call_1", assistant.Content[0].ID)
	assert.Equal(t, llms.InferenceBlock{
		Type: llms.BlockFunctionOutput,
		ID:   "call_1",
		Text: "result",
	}, req.Input.Messages[2].Content[0])

	req, err = DecodeResponses([]byte(sdkResponsesReplay), lenient)
	require.NoError(t, err)
	require.Len(t, req.Input.Messages, 3)
	assistant = req.Input.Messages[1]
	require.Len(t, assistant.Content, 2, "message text and function call merge into one assistant turn")
	assert.Equal(t, "calling", assistant.Content[0].Text)
	assert.Equal(t, llms.BlockFunctionCall, assistant.Content[1].Type)
	assert.Equal(t, llms.InferenceRoleTool, req.Input.Messages[2].Role)

	// Strict mode rejects nulls and unknown fields, naming the field.
	_, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"assistant","content":"x","audio":null}]}`), strict)
	kind, param := kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Equal(t, "messages[0].audio", param)
	_, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"assistant","content":"x","refusal":null}]}`), strict)
	require.NoError(t, err, "null refusal is part of the dialect")
	_, err = DecodeResponses([]byte(`{"model":"m","input":[{"role":"assistant","content":[{"type":"output_text","text":"x","logprobs":[]}]}]}`), strict)
	kind, param = kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Equal(t, "input[0].content[0].logprobs", param)
	_, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"foo":1}`), strict)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "foo", param)
	_, err = DecodeChat([]byte(`{"model":"m","model":"n","messages":[]}`), strict)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "request", param)
}

func TestChatRejectedAndAcceptedControls(t *testing.T) {
	base := `"model":"m","messages":[{"role":"user","content":"hi"}]`
	rejected := map[string]string{
		"n":                    `2`,
		"stream":               `true`,
		"stream_options":       `{"include_usage":true}`,
		"store":                `true`,
		"logprobs":             `true`,
		"top_logprobs":         `3`,
		"logit_bias":           `{"50256":-100}`,
		"frequency_penalty":    `0.5`,
		"presence_penalty":     `-0.5`,
		"modalities":           `["text","audio"]`,
		"audio":                `{"voice":"alloy","format":"wav"}`,
		"prediction":           `{"type":"content","content":"x"}`,
		"functions":            `[{"name":"f"}]`,
		"function_call":        `"auto"`,
		"web_search_options":   `{}`,
		"service_tier":         `"flex"`,
		"verbosity":            `"high"`,
		"reasoning_effort":     `"extreme"`,
		"tool_choice":          `"any"`,
		"response_format":      `{"type":"grammar"}`,
		"tools":                `[{"type":"custom","custom":{"name":"x"}}]`,
		"messages":             `[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x"}}]}]`,
		"messages.tool_choice": `[{"role":"assistant","content":"x","tool_calls":[{"id":"1","type":"custom","custom":{}}]}]`,
	}
	for key, value := range rejected {
		t.Run(key, func(t *testing.T) {
			field := strings.SplitN(key, ".", 2)[0]
			body := `{` + base + `,"` + field + `":` + value + `}`
			if field == "messages" {
				body = `{"model":"m","messages":` + value + `}`
			}
			for _, opts := range []dialect.DecodeOptions{lenient, strict} {
				_, err := DecodeChat([]byte(body), opts)
				kind, param := kindAndParam(t, err)
				assert.Equal(t, router.KindUnsupported, kind, param)
				assert.True(t, strings.HasPrefix(param, field), param)
			}
		})
	}
	accepted := `{` + base + `,"n":1,"stream":false,"store":false,"logprobs":false,"top_logprobs":0,
	  "logit_bias":{},"frequency_penalty":0,"presence_penalty":0,"modalities":["text"],
	  "service_tier":"auto","verbosity":"medium","user":"u1","seed":7,"metadata":{"k":"v"},
	  "prompt_cache_key":"x","max_completion_tokens":10,"reasoning_effort":"low","stop":"END",
	  "response_format":{"type":"json_schema","json_schema":{"name":"a","strict":true,"schema":{"type":"object"}}},
	  "tools":[{"type":"function","function":{"name":"lookup"}}],
	  "tool_choice":{"type":"function","function":{"name":"lookup"}},"parallel_tool_calls":false}`
	req, err := DecodeChat([]byte(accepted), lenient)
	require.NoError(t, err)
	assert.Equal(t, "m", req.Model)
	assert.Equal(t, map[string]string{"k": "v", dialect.MetadataUser: "u1"}, req.Metadata)
	require.NotNil(t, req.Input.Seed)
	assert.EqualValues(t, 7, *req.Input.Seed)
	require.NotNil(t, req.Input.MaxTokens)
	assert.Equal(t, 10, *req.Input.MaxTokens)
	assert.Equal(t, []string{"END"}, req.Input.Stop)
	assert.Equal(t, &llms.InferenceReasoning{Effort: llms.EffortLow}, req.Input.Reasoning)
	require.NotNil(t, req.Input.Format)
	assert.Equal(t, llms.FormatJSONSchema, req.Input.Format.Type)
	assert.True(t, req.Input.Format.Strict)
	assert.JSONEq(t, `{"type":"object"}`, string(req.Input.Format.Schema))
	require.Len(t, req.Input.Tools, 1)
	assert.JSONEq(t, string(emptyParameters), string(req.Input.Tools[0].Parameters))
	assert.Equal(t, &llms.InferenceToolChoice{Mode: llms.ToolChoiceFunction, Name: "lookup"}, req.Input.ToolChoice)
	require.NotNil(t, req.Input.ParallelToolCalls)
	assert.False(t, *req.Input.ParallelToolCalls)
	_, err = DecodeChat([]byte(accepted), strict)
	_, param := kindAndParam(t, err)
	assert.Equal(t, "prompt_cache_key", param, "strict mode rejects the ignored field")

	_, err = DecodeChat([]byte(`{`+base+`,"max_tokens":1,"max_completion_tokens":1}`), lenient)
	kind, param := kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Equal(t, "max_tokens", param)
	_, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"tool","content":"x"}]}`), lenient)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "messages[0].tool_call_id", param)
	_, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"user","content":"x","tool_call_id":"1"}]}`), lenient)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "messages[0].tool_call_id", param)
	_, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"critic","content":"x"}]}`), lenient)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "messages[0].role", param)
	_, err = DecodeChat([]byte(`[]`), lenient)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "request", param)
}

func TestResponsesRejectedAndAcceptedControls(t *testing.T) {
	base := `"model":"m","input":"hi"`
	rejected := map[string]string{
		"stream":               `true`,
		"store":                `true`,
		"background":           `true`,
		"truncation":           `"auto"`,
		"previous_response_id": `"resp_1"`,
		"conversation":         `"conv_1"`,
		"prompt":               `{"id":"p"}`,
		"max_tool_calls":       `3`,
		"top_logprobs":         `2`,
		"service_tier":         `"priority"`,
		"include":              `["message.output_text.logprobs"]`,
		"text":                 `{"verbosity":"low"}`,
		"reasoning":            `{"effort":"ultra"}`,
		"tools":                `[{"type":"web_search"}]`,
		"tool_choice":          `{"type":"file_search"}`,
		"input":                `[{"role":"user","content":[{"type":"input_image","image_url":"https://x"}]}]`,
		"input.reference":      `[{"type":"item_reference","id":"msg_1"}]`,
	}
	for key, value := range rejected {
		t.Run(key, func(t *testing.T) {
			field := strings.SplitN(key, ".", 2)[0]
			body := `{` + base + `,"` + field + `":` + value + `}`
			if field == "input" {
				body = `{"model":"m","input":` + value + `}`
			}
			_, err := DecodeResponses([]byte(body), lenient)
			kind, param := kindAndParam(t, err)
			assert.Equal(t, router.KindUnsupported, kind, param)
			assert.True(t, strings.HasPrefix(param, field), param)
		})
	}
	accepted := `{"model":"m","instructions":"be brief","input":[
	  {"role":"developer","content":[{"type":"input_text","text":"dev"}]},
	  {"role":"user","content":"hi"}],
	  "store":false,"stream":false,"background":false,"truncation":"disabled",
	  "include":["reasoning.encrypted_content"],"safety_identifier":"u2","max_output_tokens":5,
	  "text":{"format":{"type":"json_object"},"verbosity":"medium"},
	  "reasoning":{"effort":"high","summary":"auto"},
	  "tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}},"strict":true}],
	  "tool_choice":"required","temperature":0,"top_p":null}`
	req, err := DecodeResponses([]byte(accepted), lenient)
	require.NoError(t, err)
	require.Len(t, req.Input.Messages, 3)
	assert.Equal(t, llms.InferenceRoleSystem, req.Input.Messages[0].Role)
	assert.Equal(t, "be brief", req.Input.Messages[0].Content[0].Text)
	assert.Equal(t, llms.InferenceRoleDeveloper, req.Input.Messages[1].Role)
	assert.Equal(t, "u2", req.Metadata[dialect.MetadataUser])
	assert.Equal(t, 5, *req.Input.MaxTokens)
	assert.Equal(t, llms.FormatJSONObject, req.Input.Format.Type)
	assert.Equal(t, llms.EffortHigh, req.Input.Reasoning.Effort)
	require.Len(t, req.Input.Tools, 1)
	assert.True(t, *req.Input.Tools[0].Strict)
	assert.Equal(t, llms.ToolChoiceRequired, req.Input.ToolChoice.Mode)
	require.NotNil(t, req.Input.Temperature)
	assert.Zero(t, *req.Input.Temperature)
	assert.Nil(t, req.Input.TopP)

	_, err = DecodeResponses([]byte(`{"model":"m","input":[{"role":"tool","content":"x"}]}`), lenient)
	kind, param := kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Equal(t, "input[0].role", param)
	_, err = DecodeResponses([]byte(`{"model":"m"}`), lenient)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "input", param)
	_, err = DecodeResponses([]byte(`{"model":"m","input":[{"type":"function_call_output","call_id":"c","output":[{"type":"input_text","text":"a"},{"type":"input_text","text":"b"}]}]}`), lenient)
	require.NoError(t, err)
}

func TestNullHandling(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],"stop":null,"tool_choice":null,"temperature":null}`
	req, err := DecodeChat([]byte(body), lenient)
	require.NoError(t, err)
	assert.Nil(t, req.Input.Stop)
	assert.Nil(t, req.Input.ToolChoice)
	assert.Nil(t, req.Input.Temperature)
	_, err = DecodeChat([]byte(body), strict)
	kind, param := kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
	assert.Contains(t, []string{"stop", "tool_choice", "temperature"}, param)

	// Null assistant content is part of the dialect in both modes.
	nullContent := `{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":null,"tool_calls":[{"id":"1","type":"function","function":{"name":"f","arguments":"{}"}}]},{"role":"tool","tool_call_id":"1","content":"r"}]}`
	for _, opts := range []dialect.DecodeOptions{lenient, strict} {
		_, err := DecodeChat([]byte(nullContent), opts)
		require.NoError(t, err)
	}
	_, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"user","content":null}]}`), lenient)
	_, param = kindAndParam(t, err)
	assert.Equal(t, "messages[0].content", param)
}

func TestExplicitZeroTemperature(t *testing.T) {
	req, err := DecodeChat([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"temperature":0}`), lenient)
	require.NoError(t, err)
	require.NotNil(t, req.Input.Temperature)
	assert.Zero(t, *req.Input.Temperature)
	req, err = DecodeChat([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`), lenient)
	require.NoError(t, err)
	assert.Nil(t, req.Input.Temperature)
}

func reasoningResult() *router.Result {
	return &router.Result{
		ID:        "resp_abc",
		CreatedAt: 1700000000,
		Model:     "public",
		Response: llms.InferenceResponse{
			FinishReason: llms.FinishToolCalls,
			Content: []llms.InferenceBlock{{
				Type:   llms.BlockReasoning,
				Text:   "thinking about it",
				Opaque: json.RawMessage(`{"signature":"sig-1"}`),
			}, {
				Type: llms.BlockText,
				Text: "calling",
			}, {
				Type:      llms.BlockFunctionCall,
				ID:        "call_1",
				Name:      "lookup",
				Arguments: `{"q":1}`,
			}},
			UsageKnown: true,
			Usage: llms.Usage{
				InputTokens:     10,
				OutputTokens:    4,
				TotalTokens:     14,
				CacheReadTokens: 2,
				ReasoningTokens: 3,
			},
		},
	}
}

func TestReasoningRoundTripChat(t *testing.T) {
	res := reasoningResult()
	body, err := EncodeChat(res, dialect.EncodeOptions{})
	require.NoError(t, err)
	var completion openaisdk.ChatCompletion
	require.NoError(t, json.Unmarshal(body, &completion))
	assert.Equal(t, "chatcmpl_abc", completion.ID)
	assert.Equal(t, "public", completion.Model)
	require.Len(t, completion.Choices, 1)
	assert.Equal(t, "calling", completion.Choices[0].Message.Content)
	assert.Equal(t, "tool_calls", string(completion.Choices[0].FinishReason))
	assert.EqualValues(t, 14, completion.Usage.TotalTokens)
	assert.EqualValues(t, 2, completion.Usage.PromptTokensDetails.CachedTokens)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	message := raw["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	details := message["reasoning_details"].([]any)
	require.Len(t, details, 2)

	// Replay the assistant message verbatim in a new request.
	message["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"] = `{"q":1}`
	replay := map[string]any{
		"model": "public",
		"messages": []any{
			map[string]any{"role": "user", "content": "lookup"},
			message,
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "r"},
		},
	}
	replayBody, err := json.Marshal(replay)
	require.NoError(t, err)
	req, err := DecodeChat(replayBody, strict)
	require.NoError(t, err, "encoded output must decode in strict mode")
	assistant := req.Input.Messages[1]
	require.Len(t, assistant.Content, 4, "encrypted + summary reasoning, text, call")
	assert.Equal(t, llms.BlockReasoning, assistant.Content[0].Type)
	assert.Equal(t, "public", assistant.Content[0].Source)
	assert.JSONEq(t, `{"signature":"sig-1"}`, string(assistant.Content[0].Opaque))
	assert.Equal(t, "thinking about it", assistant.Content[1].Text)
	assert.Equal(t, llms.BlockFunctionCall, assistant.Content[3].Type)

	body, err = EncodeChat(res, dialect.EncodeOptions{OmitReasoning: true})
	require.NoError(t, err)
	assert.NotContains(t, string(body), "reasoning_details")
}

func TestReasoningRoundTripResponses(t *testing.T) {
	res := reasoningResult()
	req := router.Request{
		Model:    "public",
		Metadata: map[string]string{"k": "v", dialect.MetadataUser: "hidden"},
		Input: llms.InferenceRequest{
			Tools: []llms.InferenceTool{{Name: "lookup", Parameters: emptyParameters}},
		},
	}
	body, err := EncodeResponses(res, req, dialect.EncodeOptions{})
	require.NoError(t, err)
	var response responses.Response
	require.NoError(t, json.Unmarshal(body, &response))
	assert.Equal(t, "resp_abc", response.ID)
	assert.Equal(t, "public", response.Model)
	assert.Equal(t, "completed", string(response.Status))
	require.Len(t, response.Output, 3)
	assert.Equal(t, "reasoning", string(response.Output[0].Type))
	assert.Equal(t, "message", string(response.Output[1].Type))
	assert.Equal(t, "call_1", response.Output[2].CallID)
	assert.Equal(t, "calling", response.OutputText())
	assert.EqualValues(t, 14, response.Usage.TotalTokens)
	assert.Equal(t, map[string]string{"k": "v"}, map[string]string(response.Metadata))

	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	output := raw["output"].([]any)
	output = append([]any{map[string]any{"role": "user", "content": "lookup"}}, output...)
	output = append(output, map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "r"})
	replayBody, err := json.Marshal(map[string]any{"model": "public", "input": output})
	require.NoError(t, err)
	decoded, err := DecodeResponses(replayBody, strict)
	require.NoError(t, err, "encoded output must decode in strict mode")
	require.Len(t, decoded.Input.Messages, 3)
	assistant := decoded.Input.Messages[1]
	require.Len(t, assistant.Content, 3)
	assert.Equal(t, llms.BlockReasoning, assistant.Content[0].Type)
	assert.Equal(t, "public", assistant.Content[0].Source)
	assert.JSONEq(t, `{"signature":"sig-1"}`, string(assistant.Content[0].Opaque))
	assert.Equal(t, "thinking about it", assistant.Content[0].Text)
	assert.Equal(t, "calling", assistant.Content[1].Text)
	assert.Equal(t, `{"q":1}`, assistant.Content[2].Arguments)

	body, err = EncodeResponses(res, req, dialect.EncodeOptions{OmitReasoning: true})
	require.NoError(t, err)
	assert.NotContains(t, string(body), "encrypted_content")

	res.Response.FinishReason = llms.FinishLength
	res.Response.UsageKnown = false
	body, err = EncodeResponses(res, req, dialect.EncodeOptions{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &raw))
	assert.Equal(t, "incomplete", raw["status"])
	assert.Equal(t, "max_output_tokens", raw["incomplete_details"].(map[string]any)["reason"])
	assert.Contains(t, raw, "usage")
	assert.Nil(t, raw["usage"])

	tampered := strings.Replace(string(replayBody), `"encrypted_content":"`, `"encrypted_content":"x`, 1)
	_, err = DecodeResponses([]byte(tampered), lenient)
	kind, _ := kindAndParam(t, err)
	assert.Equal(t, router.KindInvalidRequest, kind)
}

type fakeModel struct{ calls int }

func (*fakeModel) ValidateInference(llms.InferenceRequest) error { return nil }
func (m *fakeModel) Infer(_ context.Context, r llms.InferenceRequest) (*llms.InferenceResponse, error) {
	m.calls++
	return &llms.InferenceResponse{
		Content: []llms.InferenceBlock{{
			Type: llms.BlockText,
			Text: "ok:" + r.Messages[len(r.Messages)-1].Content[0].Text,
		}},
		FinishReason: llms.FinishStop,
	}, nil
}

// codecHandler is the minimal host: decode, generate, encode.
func codecHandler(t *testing.T, r *router.Router) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var body []byte
		switch req.URL.Path {
		case "/v1/chat/completions":
			decoded, err := DecodeChat(raw, lenient)
			if err == nil {
				var res *router.Result
				if res, err = r.Generate(req.Context(), decoded); err == nil {
					body, err = EncodeChat(res, dialect.EncodeOptions{})
				}
			}
			if err != nil {
				status, envelope := EncodeError(err)
				w.WriteHeader(status)
				_, _ = w.Write(envelope)
				return
			}
		case "/v1/responses":
			decoded, err := DecodeResponses(raw, lenient)
			if err == nil {
				var res *router.Result
				if res, err = r.Generate(req.Context(), decoded); err == nil {
					body, err = EncodeResponses(res, decoded, dialect.EncodeOptions{})
				}
			}
			if err != nil {
				status, envelope := EncodeError(err)
				w.WriteHeader(status)
				_, _ = w.Write(envelope)
				return
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAISDKClientRoundTrip(t *testing.T) {
	m := &fakeModel{}
	r, err := router.New(router.Config{
		Targets: []router.Target{{
			ID:           "public",
			BackendModel: "fixture",
			Connector:    m,
		}},
	})
	require.NoError(t, err)
	handler := codecHandler(t, r)
	client := openaisdk.NewClient(option.WithAPIKey("local"), option.WithBaseURL("http://router/v1/"), option.WithMaxRetries(0),
		option.WithHTTPClient(&http.Client{Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
			rec := newRecorder()
			handler.ServeHTTP(rec, req)
			return rec.result(req), nil
		})}))
	chat, err := client.Chat.Completions.New(context.Background(), openaisdk.ChatCompletionNewParams{
		Model:    "public",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage("hi")},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok:hi", chat.Choices[0].Message.Content)
	response, err := client.Responses.New(context.Background(), responses.ResponseNewParams{
		Model: "public",
		Input: responses.ResponseNewParamsInputUnion{OfString: openaisdk.String("hi")},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok:hi", response.OutputText())
	assert.Equal(t, 2, m.calls)

	_, err = client.Chat.Completions.New(context.Background(), openaisdk.ChatCompletionNewParams{
		Model:    "missing",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage("hi")},
	})
	var apiErr *openaisdk.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, 2, m.calls)
}

func TestEncodeError(t *testing.T) {
	cases := []struct {
		kind   router.Kind
		status int
		typ    string
	}{
		{router.KindInvalidRequest, 400, errorTypeInvalid},
		{router.KindUnsupported, 400, errorTypeInvalid},
		{router.KindModelNotFound, 404, errorTypeInvalid},
		{router.KindRateLimited, 429, errorTypeInvalid},
		{router.KindSelectionUnavailable, 503, errorTypeServer},
		{router.KindUpstream, 502, errorTypeServer},
		{router.KindTimeout, 504, errorTypeServer},
		{router.KindInternal, 500, errorTypeServer},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			err := errors.WithStack(&router.Error{
				Kind:    tc.kind,
				Param:   "p",
				Message: "safe message",
				Cause:   errors.New("provider secret"),
			})
			status, body := EncodeError(err)
			assert.Equal(t, tc.status, status)
			var envelope struct {
				Error struct {
					Message string  `json:"message"`
					Type    string  `json:"type"`
					Param   *string `json:"param"`
					Code    string  `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(body, &envelope))
			assert.Equal(t, "safe message", envelope.Error.Message)
			assert.Equal(t, tc.typ, envelope.Error.Type)
			assert.Equal(t, string(tc.kind), envelope.Error.Code)
			require.NotNil(t, envelope.Error.Param)
			assert.Equal(t, "p", *envelope.Error.Param)
			assert.NotContains(t, string(body), "secret")
		})
	}
	status, body := EncodeError(errors.New("raw failure text"))
	assert.Equal(t, 500, status)
	assert.NotContains(t, string(body), "raw failure text")
	assert.Contains(t, string(body), `"param":null`)
}

func FuzzDecodeChat(f *testing.F) {
	f.Add(sdkChatReplay)
	f.Add(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"x"}]}],"tools":[{"type":"function","function":{"name":"f"}}]}`)
	f.Fuzz(func(t *testing.T, body string) {
		for _, opts := range []dialect.DecodeOptions{lenient, strict} {
			_, _ = DecodeChat([]byte(body), opts)
		}
	})
}

func FuzzDecodeResponses(f *testing.F) {
	f.Add(sdkResponsesReplay)
	f.Add(`{"model":"m","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"x"}],"encrypted_content":"e30"}]}`)
	f.Fuzz(func(t *testing.T, body string) {
		for _, opts := range []dialect.DecodeOptions{lenient, strict} {
			_, _ = DecodeResponses([]byte(body), opts)
		}
	})
}
