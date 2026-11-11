package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	t        *testing.T
	status   int
	response string
	requests []map[string]any
	bodies   []string
	paths    []string
}

func (f *fixture) model() *LLM {
	client := &http.Client{
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			require.NoError(f.t, err)
			f.bodies = append(f.bodies, string(body))
			f.paths = append(f.paths, r.URL.Path)
			var v map[string]any
			require.NoError(f.t, json.Unmarshal(body, &v))
			f.requests = append(f.requests, v)
			status := f.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{
				StatusCode: status,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(f.response)),
				Request: r,
			}, nil
		}),
	}
	runtime := bedrockruntime.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("fixture", "fixture", ""),
		HTTPClient:  client,
	})
	m, err := New(WithClient(runtime), WithModel("fixture"), WithConverse())
	require.NoError(f.t, err)
	return m
}

func textRequest(text string) llms.InferenceRequest {
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

const textResponse = `{"output":{"message":{"role":"assistant","content":[{"text":"hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":7,"cacheReadInputTokens":2,"cacheWriteInputTokens":1,"outputTokens":2,"totalTokens":12},"metrics":{"latencyMs":1}}`

func TestInferTextAndUsage(t *testing.T) {
	f := &fixture{t: t, response: textResponse}
	m := f.model()
	zero := 0.0
	req := textRequest("hi")
	req.Temperature = &zero
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, llms.FinishStop, out.FinishReason)
	assert.Equal(t, "hello", out.Content[0].Text)
	require.True(t, out.UsageKnown)
	assert.EqualValues(t, 10, out.Usage.InputTokens, "input includes cache reads and writes")
	assert.EqualValues(t, 2, out.Usage.CacheReadTokens)
	assert.EqualValues(t, 1, out.Usage.CacheWriteTokens)
	assert.EqualValues(t, 12, out.Usage.TotalTokens)
	assert.Contains(t, f.paths[0], "/converse")
	cfg := f.requests[0]["inferenceConfig"].(map[string]any)
	assert.Equal(t, float64(0), cfg["temperature"])
	assert.NotContains(t, cfg, "topP")
}

func TestInferToolCallAndParallelContinuation(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"call_1","name":"lookup","input":{"n":9007199254740993}}}]}},"stopReason":"tool_use"}`}
	m := f.model()
	req := textRequest("lookup")
	req.Tools = []llms.InferenceTool{{
		Name:       "lookup",
		Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`),
	}}
	req.ToolChoice = &llms.InferenceToolChoice{Mode: llms.ToolChoiceFunction, Name: "lookup"}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, llms.FinishToolCalls, out.FinishReason)
	require.Len(t, out.Content, 1)
	assert.Equal(t, "call_1", out.Content[0].ID)
	assert.Equal(t, `{"n":9007199254740993}`, out.Content[0].Arguments)
	assert.False(t, out.UsageKnown)
	choice := f.requests[0]["toolConfig"].(map[string]any)["toolChoice"].(map[string]any)
	assert.Equal(t, "lookup", choice["tool"].(map[string]any)["name"])

	f.response = textResponse
	req.ToolChoice = nil
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: []llms.InferenceBlock{
			out.Content[0],
			{Type: llms.BlockFunctionCall, ID: "call_2", Name: "lookup", Arguments: `{"n":2}`},
		}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call_1", Name: "lookup", Text: "a"}}},
		llms.InferenceMessage{Role: llms.InferenceRoleTool, Content: []llms.InferenceBlock{{Type: llms.BlockFunctionOutput, ID: "call_2", Name: "lookup", Text: "b"}}},
	)
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, f.bodies[1], "9007199254740993")
	messages := f.requests[1]["messages"].([]any)
	require.Len(t, messages, 3, "tool results share one user turn")
	last := messages[2].(map[string]any)
	assert.Equal(t, "user", last["role"])
	content := last["content"].([]any)
	require.Len(t, content, 2)
	assert.Equal(t, "call_1", content[0].(map[string]any)["toolResult"].(map[string]any)["toolUseId"])
	assert.Equal(t, "call_2", content[1].(map[string]any)["toolResult"].(map[string]any)["toolUseId"])
}

func TestInferJSONSchemaAndSystem(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"text":"{\"ok\":true}"}]}},"stopReason":"end_turn"}`}
	m := f.model()
	req := textRequest("answer")
	req.Messages = append([]llms.InferenceMessage{{
		Role:    llms.InferenceRoleSystem,
		Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "be brief"}},
	}}, req.Messages...)
	req.Format = &llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "answer",
		Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
	}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, out.Content[0].Text)
	format := f.requests[0]["outputConfig"].(map[string]any)["textFormat"].(map[string]any)
	assert.Equal(t, "json_schema", format["type"])
	assert.Equal(t, "answer", format["structure"].(map[string]any)["jsonSchema"].(map[string]any)["name"])
	assert.Len(t, f.requests[0]["system"].([]any), 1)
	assert.Len(t, f.requests[0]["messages"].([]any), 1)
}

func TestInferReasoningRoundTripAndThinkingConfig(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"reasoningContent":{"reasoningText":{"text":"thinking...","signature":"sig"}}},{"reasoningContent":{"redactedContent":"AQID"}},{"text":"hello"}]}},"stopReason":"end_turn"}`}
	m := f.model()
	req := textRequest("think")
	req.Reasoning = &llms.InferenceReasoning{Effort: llms.EffortMedium}
	out, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, out.Content, 3)
	assert.Equal(t, llms.BlockReasoning, out.Content[0].Type)
	assert.Equal(t, "thinking...", out.Content[0].Text)
	assert.JSONEq(t, `{"reasoningText":{"text":"thinking...","signature":"sig"}}`, string(out.Content[0].Opaque))
	assert.Equal(t, llms.BlockReasoning, out.Content[1].Type)
	assert.JSONEq(t, `{"redactedContent":"AQID"}`, string(out.Content[1].Opaque))
	assert.Equal(t, llms.BlockText, out.Content[2].Type)
	thinking := f.requests[0]["additionalModelRequestFields"].(map[string]any)["thinking"].(map[string]any)
	assert.Equal(t, "enabled", thinking["type"])
	assert.Equal(t, float64(8192), thinking["budget_tokens"])

	// Replay with a follow-up user turn; reasoning blocks return at their position.
	for i := range out.Content {
		out.Content[i].Source = "target"
	}
	req.Messages = append(req.Messages,
		llms.InferenceMessage{Role: llms.InferenceRoleAssistant, Content: out.Content},
		llms.InferenceMessage{Role: llms.InferenceRoleUser, Content: []llms.InferenceBlock{{Type: llms.BlockText, Text: "more"}}},
	)
	f.response = textResponse
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assistant := f.requests[1]["messages"].([]any)[1].(map[string]any)
	content := assistant["content"].([]any)
	require.Len(t, content, 3)
	reasoning := content[0].(map[string]any)["reasoningContent"].(map[string]any)["reasoningText"].(map[string]any)
	assert.Equal(t, "thinking...", reasoning["text"])
	assert.Equal(t, "sig", reasoning["signature"])
	assert.Equal(t, "AQID", content[1].(map[string]any)["reasoningContent"].(map[string]any)["redactedContent"])
	assert.Contains(t, content[2].(map[string]any), "text")
}

func TestInferFinishReasonsAndErrors(t *testing.T) {
	f := &fixture{t: t, response: `{"output":{"message":{"role":"assistant","content":[{"text":"partial"}]}},"stopReason":"max_tokens"}`}
	m := f.model()
	out, err := m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishLength, out.FinishReason)

	f.response = `{"output":{"message":{"role":"assistant","content":[]}},"stopReason":"guardrail_intervened"}`
	out, err = m.Infer(context.Background(), textRequest("hi"))
	require.NoError(t, err)
	assert.Equal(t, llms.FinishContentFilter, out.FinishReason)

	f.response = `{"output":{"message":{"role":"assistant","content":[]}},"stopReason":"mystery"}`
	_, err = m.Infer(context.Background(), textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusBadGateway, ie.Status)

	f.status = http.StatusTooManyRequests
	f.response = `{"message":"secret throttle"}`
	_, err = m.Infer(context.Background(), textRequest("hi"))
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, http.StatusTooManyRequests, ie.Status)
	assert.Len(t, f.requests, 4, "exactly one attempt per call")
}

func TestValidateInferenceRejections(t *testing.T) {
	m := (&fixture{t: t, response: textResponse}).model()
	yes := true
	high := 1.5
	half := 0.5
	seed := int64(1)
	small := 100
	cases := map[string]llms.InferenceRequest{
		"temperature":                 {Temperature: &high},
		"parallel_tool_calls":         {ParallelToolCalls: &yes},
		"tool_choice.none":            {ToolChoice: &llms.InferenceToolChoice{Mode: llms.ToolChoiceNone}},
		"response_format.json_object": {Format: &llms.InferenceFormat{Type: llms.FormatJSONObject}},
		"seed":                        {Seed: &seed},
		"messages.role.developer":     {Messages: []llms.InferenceMessage{{Role: llms.InferenceRoleDeveloper}}},
		"reasoning":                   {Reasoning: &llms.InferenceReasoning{Effort: "extreme"}},
		"top_p":                       {Reasoning: &llms.InferenceReasoning{Effort: llms.EffortLow}, TopP: &half},
		"max_tokens":                  {Reasoning: &llms.InferenceReasoning{Effort: llms.EffortLow}, MaxTokens: &small},
	}
	for param, req := range cases {
		t.Run(param, func(t *testing.T) {
			err := m.ValidateInference(req)
			var ie *llms.InferenceError
			require.ErrorAs(t, err, &ie)
			assert.Equal(t, param, ie.Param)
		})
	}
	legacy, err := New(WithClient(bedrockruntime.NewFromConfig(aws.Config{Region: "us-east-1"})), WithModel("fixture"))
	require.NoError(t, err)
	err = legacy.ValidateInference(textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, "provider.converse", ie.Param)
}

const (
	toolArgsJSON      = `{"n":1}`
	opaqueReasoning   = `{"reasoningText":{"text":"why"}}`
	transportFailure  = "dial tcp: connection refused"
	captureFailure    = "boom"
	decodeErrorPrefix = "decode bedrock inference: "
)

// requireInferenceError asserts err is an *llms.InferenceError with the exact status and message.
func requireInferenceError(t *testing.T, err error, status int, message string) {
	t.Helper()
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, status, ie.Status)
	assert.EqualError(t, err, message)
}

func TestValidateInferenceReasoningControls(t *testing.T) {
	m := (&fixture{t: t, response: textResponse}).model()
	temperature := 0.5
	budget := 1024
	atBudget := 1024
	aboveBudget := 2048

	err := m.ValidateInference(llms.InferenceRequest{
		Reasoning: &llms.InferenceReasoning{
			Effort: llms.EffortLow,
		},
		Temperature: &temperature,
	})
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, paramTemperature, ie.Param, "temperature cannot be combined with thinking")

	err = m.ValidateInference(llms.InferenceRequest{
		Reasoning: &llms.InferenceReasoning{
			BudgetTokens: &budget,
		},
		MaxTokens: &atBudget,
	})
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, paramMaxTokens, ie.Param, "explicit budget must stay below max tokens")

	require.NoError(t, m.ValidateInference(llms.InferenceRequest{
		Reasoning: &llms.InferenceReasoning{
			BudgetTokens: &budget,
		},
		MaxTokens: &aboveBudget,
	}))
}

func TestInferSamplingControlsAndExplicitBudget(t *testing.T) {
	f := &fixture{t: t, response: textResponse}
	m := f.model()
	topP := 0.5
	maxTokens := 4096
	req := textRequest("hi")
	req.TopP = &topP
	req.MaxTokens = &maxTokens
	req.Stop = []string{"END"}
	_, err := m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"topP":          0.5,
		"maxTokens":     float64(4096),
		"stopSequences": []any{"END"},
	}, f.requests[0]["inferenceConfig"])
	assert.NotContains(t, f.requests[0], "additionalModelRequestFields")

	budget := 2048
	req = textRequest("think")
	req.MaxTokens = &maxTokens
	req.Reasoning = &llms.InferenceReasoning{
		BudgetTokens: &budget,
	}
	_, err = m.Infer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": float64(2048),
		},
	}, f.requests[1]["additionalModelRequestFields"])
	assert.Equal(t, map[string]any{
		"maxTokens": float64(4096),
	}, f.requests[1]["inferenceConfig"])
}

func TestInferToolChoiceModesAndSchema(t *testing.T) {
	const schema = `{"type":"object","properties":{"n":{"type":"integer","enum":[1,2]}},"required":["n"]}`
	tests := []struct {
		mode     llms.ToolChoiceMode
		expected map[string]any
	}{
		{
			mode: llms.ToolChoiceAuto,
			expected: map[string]any{
				"auto": map[string]any{},
			},
		},
		{
			mode: llms.ToolChoiceRequired,
			expected: map[string]any{
				"any": map[string]any{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			f := &fixture{t: t, response: textResponse}
			m := f.model()
			req := textRequest("lookup")
			req.Tools = []llms.InferenceTool{
				{
					Name:        "lookup",
					Description: "Look up a number",
					Parameters:  json.RawMessage(schema),
				},
			}
			req.ToolChoice = &llms.InferenceToolChoice{
				Mode: tt.mode,
			}
			_, err := m.Infer(context.Background(), req)
			require.NoError(t, err)
			toolConfig := f.requests[0]["toolConfig"].(map[string]any)
			assert.Equal(t, tt.expected, toolConfig["toolChoice"])
			tools := toolConfig["tools"].([]any)
			require.Len(t, tools, 1)
			spec := tools[0].(map[string]any)["toolSpec"].(map[string]any)
			assert.Equal(t, "lookup", spec["name"])
			assert.Equal(t, "Look up a number", spec["description"])
			inputSchema, err := json.Marshal(spec["inputSchema"].(map[string]any)["json"])
			require.NoError(t, err)
			assert.JSONEq(t, schema, string(inputSchema), "array members keep their numbers")
		})
	}
}

func TestInferRejectsBeforeSending(t *testing.T) {
	f := &fixture{t: t, response: textResponse}
	m := f.model()

	legacy, err := New(WithClient(m.runtimeClient), WithModel("fixture"))
	require.NoError(t, err)
	_, err = legacy.Infer(context.Background(), textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, paramConverse, ie.Param)

	req := textRequest("hi")
	req.Messages = append(req.Messages, llms.InferenceMessage{
		Role: llms.InferenceRoleAssistant,
		Content: []llms.InferenceBlock{
			{
				Type:      llms.BlockFunctionCall,
				ID:        "call_1",
				Name:      "lookup",
				Arguments: "{",
			},
		},
	})
	_, err = m.Infer(context.Background(), req)
	requireInferenceError(t, err, http.StatusBadRequest, "decode bedrock tool arguments: decode document: unexpected EOF")
	assert.Empty(t, f.requests, "invalid requests never reach bedrock")
}

func TestInferTransportError(t *testing.T) {
	transportErr := errors.New(transportFailure)
	attempts := 0
	runtime := bedrockruntime.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("fixture", "fixture", ""),
		HTTPClient: &http.Client{
			Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				attempts++
				return nil, transportErr
			}),
		},
	})
	m, err := New(WithClient(runtime), WithModel("fixture"), WithConverse())
	require.NoError(t, err)
	_, err = m.Infer(context.Background(), textRequest("hi"))
	var ie *llms.InferenceError
	require.ErrorAs(t, err, &ie)
	// F-018: the SDK's placeholder response has status 0; no response is a bad gateway.
	assert.Equal(t, http.StatusBadGateway, ie.Status)
	assert.Empty(t, ie.Param)
	assert.ErrorIs(t, err, transportErr)
	assert.Contains(t, err.Error(), "bedrock converse inference: operation error Bedrock Runtime: Converse")
	assert.Equal(t, 1, attempts, "converse is never retried")
}

func TestConverseStatus(t *testing.T) {
	cause := errors.New(transportFailure)
	responseError := func(resp *smithyhttp.Response) error {
		return &awshttp.ResponseError{
			ResponseError: &smithyhttp.ResponseError{
				Response: resp,
				Err:      cause,
			},
		}
	}
	withStatus := func(status int) *smithyhttp.Response {
		return &smithyhttp.Response{
			Response: &http.Response{
				StatusCode: status,
			},
		}
	}
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{
			name:     "error without response",
			err:      cause,
			expected: http.StatusBadGateway,
		},
		{
			name:     "nil response",
			err:      responseError(nil),
			expected: http.StatusBadGateway,
		},
		{
			name:     "nil http response",
			err:      responseError(&smithyhttp.Response{}),
			expected: http.StatusBadGateway,
		},
		{
			name:     "placeholder response of a send failure",
			err:      responseError(withStatus(0)),
			expected: http.StatusBadGateway,
		},
		{
			name:     "service status",
			err:      responseError(withStatus(http.StatusServiceUnavailable)),
			expected: http.StatusServiceUnavailable,
		},
		{
			name:     "wrapped service status",
			err:      errors.WithMessage(responseError(withStatus(http.StatusTooManyRequests)), "operation error"),
			expected: http.StatusTooManyRequests,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, converseStatus(tt.err))
		})
	}
}

func TestConverseInputErrors(t *testing.T) {
	tests := []struct {
		name    string
		req     llms.InferenceRequest
		status  int
		message string
		param   string
	}{
		{
			name: "unknown reasoning effort",
			req: llms.InferenceRequest{
				Reasoning: &llms.InferenceReasoning{
					Effort: "extreme",
				},
			},
			status:  http.StatusBadRequest,
			message: "unsupported inference parameter: reasoning",
			param:   paramReasoning,
		},
		{
			name: "invalid tool schema",
			req: llms.InferenceRequest{
				Tools: []llms.InferenceTool{
					{
						Name:       "lookup",
						Parameters: json.RawMessage(`not json`),
					},
				},
			},
			status:  http.StatusBadRequest,
			message: "decode bedrock tool schema: decode document: invalid character 'o' in literal null (expecting 'u')",
		},
		{
			name: "invalid reasoning state",
			req: llms.InferenceRequest{
				Messages: []llms.InferenceMessage{
					{
						Role: llms.InferenceRoleAssistant,
						Content: []llms.InferenceBlock{
							{
								Type:   llms.BlockReasoning,
								Opaque: json.RawMessage(`[]`),
							},
						},
					},
				},
			},
			status:  http.StatusBadRequest,
			message: "decode reasoning state: json: cannot unmarshal array into Go value of type bedrock.reasoningOpaque",
		},
		{
			name: "unknown reasoning state",
			req: llms.InferenceRequest{
				Messages: []llms.InferenceMessage{
					{
						Role: llms.InferenceRoleAssistant,
						Content: []llms.InferenceBlock{
							{
								Type:   llms.BlockReasoning,
								Opaque: json.RawMessage(`{}`),
							},
						},
					},
				},
			},
			status:  http.StatusBadRequest,
			message: "decode reasoning state: unknown reasoning state",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := converseInput(tt.req)
			assert.Nil(t, in)
			requireInferenceError(t, err, tt.status, tt.message)
			var ie *llms.InferenceError
			require.ErrorAs(t, err, &ie)
			assert.Equal(t, tt.param, ie.Param)
		})
	}
}

func TestConverseBlocksSkipsStatelessReasoning(t *testing.T) {
	blocks, err := converseBlocks([]llms.InferenceBlock{
		{
			Type: llms.BlockReasoning,
			Text: "summary only",
		},
		{
			Type: llms.BlockRefusal,
			Text: "no",
		},
		{
			Type:   llms.BlockReasoning,
			Opaque: json.RawMessage(opaqueReasoning),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []types.ContentBlock{
		&types.ContentBlockMemberText{
			Value: "no",
		},
		&types.ContentBlockMemberReasoningContent{
			Value: &types.ReasoningContentBlockMemberReasoningText{
				Value: types.ReasoningTextBlock{
					Text: aws.String("why"),
				},
			},
		},
	}, blocks)
}

func TestConverseOutputErrors(t *testing.T) {
	reasoning := &types.ContentBlockMemberReasoningContent{
		Value: &types.ReasoningContentBlockMemberReasoningText{
			Value: types.ReasoningTextBlock{
				Text: aws.String("why"),
			},
		},
	}
	toolUse := &types.ContentBlockMemberToolUse{
		Value: types.ToolUseBlock{
			ToolUseId: aws.String("call_1"),
			Name:      aws.String("lookup"),
		},
	}
	tests := []struct {
		name    string
		output  types.ConverseOutput
		raw     string
		message string
	}{
		{
			name:    "missing message",
			output:  nil,
			raw:     `{}`,
			message: "missing message",
		},
		{
			name: "undecodable body",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						&types.ContentBlockMemberText{
							Value: "hi",
						},
					},
				},
			},
			raw:     `{`,
			message: "decode bedrock converse body: unexpected end of JSON input",
		},
		{
			name: "content length mismatch",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						&types.ContentBlockMemberText{
							Value: "hi",
						},
					},
				},
			},
			raw:     `{"output":{"message":{"content":[]}}}`,
			message: "bedrock content length mismatch",
		},
		{
			name: "text block with tool input",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						&types.ContentBlockMemberText{
							Value: "hi",
						},
					},
				},
			},
			raw:     `{"output":{"message":{"content":[{"toolUse":{"input":{}}}]}}}`,
			message: "unexpected tool input",
		},
		{
			name: "tool use without input",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						toolUse,
					},
				},
			},
			raw:     `{"output":{"message":{"content":[{"text":"hi"}]}}}`,
			message: "missing or invalid tool input",
		},
		{
			name: "tool use with non-object input",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						toolUse,
					},
				},
			},
			raw:     `{"output":{"message":{"content":[{"toolUse":{"input":[1]}}]}}}`,
			message: "missing or invalid tool input",
		},
		{
			name: "reasoning block with tool input",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						reasoning,
					},
				},
			},
			raw:     `{"output":{"message":{"content":[{"toolUse":{"input":{}}}]}}}`,
			message: "unexpected tool input",
		},
		{
			name: "unknown reasoning content",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						&types.ContentBlockMemberReasoningContent{
							Value: &types.UnknownUnionMember{
								Tag: "future",
							},
						},
					},
				},
			},
			raw:     `{"output":{"message":{"content":[{"reasoningContent":{"future":{}}}]}}}`,
			message: "unknown reasoning content",
		},
		{
			name: "non portable content",
			output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Content: []types.ContentBlock{
						&types.ContentBlockMemberImage{},
					},
				},
			},
			raw:     `{"output":{"message":{"content":[{"image":{}}]}}}`,
			message: "opaque content outside portable profile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := converseOutput(&bedrockruntime.ConverseOutput{
				StopReason: types.StopReasonEndTurn,
				Output:     tt.output,
			}, []byte(tt.raw))
			assert.Nil(t, out)
			requireInferenceError(t, err, http.StatusBadGateway, decodeErrorPrefix+tt.message)
		})
	}
}

func TestConverseFinishReasons(t *testing.T) {
	tests := map[types.StopReason]llms.FinishReason{
		types.StopReasonEndTurn:             llms.FinishStop,
		types.StopReasonStopSequence:        llms.FinishStop,
		types.StopReasonMaxTokens:           llms.FinishLength,
		types.StopReasonToolUse:             llms.FinishToolCalls,
		types.StopReasonContentFiltered:     llms.FinishContentFilter,
		types.StopReasonGuardrailIntervened: llms.FinishContentFilter,
	}
	for reason, expected := range tests {
		t.Run(string(reason), func(t *testing.T) {
			finish, err := converseFinishReason(reason)
			require.NoError(t, err)
			assert.Equal(t, expected, finish)
		})
	}
	finish, err := converseFinishReason("mystery")
	assert.Empty(t, finish)
	requireInferenceError(t, err, http.StatusBadGateway, `decode bedrock inference: unsupported stop reason "mystery"`)
}

// failingBody is a response body whose Read or Close fails.
type failingBody struct {
	readErr  error
	closeErr error
}

func (b failingBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return 0, io.EOF
}

func (b failingBody) Close() error { return b.closeErr }

// deserializeReturning is a next handler that yields the given raw response or error.
func deserializeReturning(raw any, err error) middleware.DeserializeHandler {
	return middleware.DeserializeHandlerFunc(func(context.Context, middleware.DeserializeInput) (
		middleware.DeserializeOutput, middleware.Metadata, error,
	) {
		return middleware.DeserializeOutput{
			RawResponse: raw,
		}, middleware.Metadata{}, err
	})
}

func TestCaptureConverseBody(t *testing.T) {
	assert.Equal(t, captureConverseBodyID, captureConverseBody{}.ID())
	failure := errors.New(captureFailure)

	httpResponse := func(body io.ReadCloser) *smithyhttp.Response {
		return &smithyhttp.Response{
			Response: &http.Response{
				StatusCode: http.StatusOK,
				Body:       body,
			},
		}
	}

	t.Run("captures and restores the body", func(t *testing.T) {
		var raw []byte
		ctx := context.WithValue(context.Background(), converseBodyKey{}, &raw)
		resp := httpResponse(io.NopCloser(strings.NewReader(textResponse)))
		out, _, err := captureConverseBody{}.HandleDeserialize(ctx, middleware.DeserializeInput{}, deserializeReturning(resp, nil))
		require.NoError(t, err)
		assert.Equal(t, textResponse, string(raw))
		restored, err := io.ReadAll(out.RawResponse.(*smithyhttp.Response).Body)
		require.NoError(t, err)
		assert.Equal(t, textResponse, string(restored), "the deserializer still sees the full body")
	})

	tests := []struct {
		name    string
		raw     any
		nextErr error
		message string
	}{
		{
			name:    "next handler error",
			raw:     nil,
			nextErr: failure,
			message: captureFailure,
		},
		{
			name:    "non http response",
			raw:     "raw",
			message: "",
		},
		{
			name: "nil body",
			raw: &smithyhttp.Response{
				Response: &http.Response{},
			},
			message: "",
		},
		{
			name: "read error",
			raw: httpResponse(failingBody{
				readErr: failure,
			}),
			message: "read bedrock converse body: " + captureFailure,
		},
		{
			name: "close error",
			raw: httpResponse(failingBody{
				closeErr: failure,
			}),
			message: "close bedrock converse body: " + captureFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte("untouched")
			ctx := context.WithValue(context.Background(), converseBodyKey{}, &raw)
			out, _, err := captureConverseBody{}.HandleDeserialize(ctx, middleware.DeserializeInput{}, deserializeReturning(tt.raw, tt.nextErr))
			if tt.message == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.message)
				assert.ErrorIs(t, err, failure)
			}
			assert.Equal(t, tt.raw, out.RawResponse)
			assert.Equal(t, "untouched", string(raw), "nothing is captured on this path")
		})
	}
}
