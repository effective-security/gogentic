package openai

import (
	"encoding/json"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	msgNilResult = "nil result"
	msgSeal      = "seal reasoning state"
)

func requireInternal(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err)
	re := router.AsError(err)
	assert.Equal(t, router.KindInternal, re.Kind)
	assert.Empty(t, re.Param)
	assert.Equal(t, msg, re.Message)
}

func blockResult(model string, finish llms.FinishReason, blocks ...llms.InferenceBlock) *router.Result {
	return &router.Result{
		ID:        "resp_1",
		CreatedAt: 1,
		Model:     model,
		Response: llms.InferenceResponse{
			FinishReason: finish,
			Content:      blocks,
		},
	}
}

func opaqueReasoning() llms.InferenceBlock {
	return llms.InferenceBlock{
		Type:   llms.BlockReasoning,
		Opaque: json.RawMessage(`{"s":1}`),
	}
}

func decodeMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestEncodeChatRejections(t *testing.T) {
	out, err := EncodeChat(nil, dialect.EncodeOptions{})
	requireInternal(t, err, msgNilResult)
	assert.Nil(t, out)

	out, err = EncodeChat(blockResult("", llms.FinishStop, opaqueReasoning()), dialect.EncodeOptions{})
	requireInternal(t, err, msgSeal)
	assert.Nil(t, out)
}

func TestEncodeChatMessageShapes(t *testing.T) {
	out, err := EncodeChat(blockResult("m", llms.FinishContentFilter, llms.InferenceBlock{
		Type: llms.BlockRefusal,
		Text: "no",
	}), dialect.EncodeOptions{})
	require.NoError(t, err)
	body := decodeMap(t, out)
	assert.NotContains(t, body, keyUsage, "unknown usage is omitted")
	choice := body[keyChoices].([]any)[0].(map[string]any)
	assert.Equal(t, string(llms.FinishContentFilter), choice[keyFinishReason])
	assert.Contains(t, choice, keyLogprobs)
	assert.Nil(t, choice[keyLogprobs])
	message := choice[keyMessage].(map[string]any)
	assert.Equal(t, map[string]any{
		keyRole:    roleAssistant,
		keyContent: nil,
		keyRefusal: "no",
	}, message, "a refusal-only message has null content and no tool calls")

	out, err = EncodeChat(blockResult("m", llms.FinishStop, llms.InferenceBlock{
		Type: llms.BlockReasoning,
		Text: "visible",
	}), dialect.EncodeOptions{})
	require.NoError(t, err)
	message = decodeMap(t, out)[keyChoices].([]any)[0].(map[string]any)[keyMessage].(map[string]any)
	assert.Equal(t, "", message[keyContent], "empty output still has string content")
	assert.Equal(t, []any{map[string]any{
		keyType:    detailSummary,
		keyID:      reasoningIDPrefix + "0",
		keySummary: "visible",
		keyFormat:  detailFormat,
	}}, message[keyReasoningDetails], "summary-only reasoning has no encrypted item")
}

func TestEncodeResponsesRejections(t *testing.T) {
	out, err := EncodeResponses(nil, router.Request{}, dialect.EncodeOptions{})
	requireInternal(t, err, msgNilResult)
	assert.Nil(t, out)

	out, err = EncodeResponses(blockResult("", llms.FinishStop, opaqueReasoning()), router.Request{}, dialect.EncodeOptions{})
	requireInternal(t, err, msgSeal)
	assert.Nil(t, out)

	req := router.Request{
		Input: llms.InferenceRequest{
			Tools: []llms.InferenceTool{{
				Name:       "f",
				Parameters: json.RawMessage(`{`),
			}},
		},
	}
	out, err = EncodeResponses(blockResult("m", llms.FinishStop, llms.InferenceBlock{
		Type: llms.BlockText,
		Text: "ok",
	}), req, dialect.EncodeOptions{})
	requireInternal(t, err, "serialize response")
	assert.Nil(t, out)
}

func TestEncodeResponsesEchoesControls(t *testing.T) {
	strictTool := true
	parallel := false
	req := router.Request{
		Input: llms.InferenceRequest{
			Tools: []llms.InferenceTool{{
				Name:        "f",
				Description: "d",
				Parameters:  emptyParameters,
				Strict:      &strictTool,
			}},
			ToolChoice: &llms.InferenceToolChoice{
				Mode: llms.ToolChoiceFunction,
				Name: "f",
			},
			ParallelToolCalls: &parallel,
			Format: &llms.InferenceFormat{
				Type: llms.FormatJSONObject,
			},
			Reasoning: &llms.InferenceReasoning{
				Effort: llms.EffortLow,
			},
		},
	}
	res := blockResult("m", llms.FinishContentFilter,
		llms.InferenceBlock{
			Type: llms.BlockText,
			Text: "partial",
		},
		llms.InferenceBlock{
			Type: llms.BlockRefusal,
			Text: "no",
		},
		llms.InferenceBlock{
			Type: llms.BlockReasoning,
		},
	)
	out, err := EncodeResponses(res, req, dialect.EncodeOptions{})
	require.NoError(t, err)
	body := decodeMap(t, out)
	assert.Equal(t, statusIncomplete, body[keyStatus])
	assert.Equal(t, map[string]any{keyReason: reasonFilter}, body[keyIncompleteDetails])
	assert.Equal(t, map[string]any{}, body[keyMetadata])
	assert.Equal(t, false, body[keyParallelToolCalls])
	assert.Equal(t, map[string]any{keyEffort: effortLow}, body[keyReasoning])
	assert.Equal(t, map[string]any{keyFormat: map[string]any{keyType: formatTypeJSON}}, body[keyText])
	assert.Equal(t, map[string]any{
		keyType: llms.InferenceFunction,
		keyName: "f",
	}, body[keyToolChoice])
	assert.Equal(t, []any{map[string]any{
		keyType:        llms.InferenceFunction,
		keyName:        "f",
		keyDescription: "d",
		keyParameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
		keyStrict: true,
	}}, body[keyTools])

	output := body[keyOutput].([]any)
	require.Len(t, output, 2, "message, then the flushed reasoning item")
	message := output[0].(map[string]any)
	assert.Equal(t, statusIncomplete, message[keyStatus])
	assert.Equal(t, []any{
		map[string]any{
			keyType:        partOutputText,
			keyText:        "partial",
			keyAnnotations: []any{},
		},
		map[string]any{
			keyType:    partRefusal,
			keyRefusal: "no",
		},
	}, message[keyContent])
	reasoning := output[1].(map[string]any)
	assert.Equal(t, itemReasoning, reasoning[keyType])
	assert.Equal(t, []any{}, reasoning[keySummary])
	assert.NotContains(t, reasoning, keyEncryptedContent, "no opaque state, no encrypted content")

	req.Input.ParallelToolCalls = nil
	req.Input.Reasoning = nil
	out, err = EncodeResponses(res, req, dialect.EncodeOptions{OmitReasoning: true})
	require.NoError(t, err)
	body = decodeMap(t, out)
	assert.Equal(t, true, body[keyParallelToolCalls], "parallel calls default to enabled")
	assert.NotContains(t, body, keyReasoning)
	assert.Len(t, body[keyOutput].([]any), 1)
}

func TestEncodeToolChoice(t *testing.T) {
	cases := []struct {
		name   string
		choice *llms.InferenceToolChoice
		want   any
	}{
		{
			name: "default",
			want: choiceAuto,
		},
		{
			name:   "auto",
			choice: &llms.InferenceToolChoice{Mode: llms.ToolChoiceAuto},
			want:   choiceAuto,
		},
		{
			name:   "none",
			choice: &llms.InferenceToolChoice{Mode: llms.ToolChoiceNone},
			want:   choiceNone,
		},
		{
			name:   "required",
			choice: &llms.InferenceToolChoice{Mode: llms.ToolChoiceRequired},
			want:   choiceRequired,
		},
		{
			name: "function",
			choice: &llms.InferenceToolChoice{
				Mode: llms.ToolChoiceFunction,
				Name: "f",
			},
			want: map[string]any{
				keyType: llms.InferenceFunction,
				keyName: "f",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, encodeToolChoice(tc.choice))
		})
	}
}

func TestEncodeTextConfig(t *testing.T) {
	text := map[string]any{keyFormat: map[string]any{keyType: formatTypeText}}
	assert.Equal(t, text, encodeTextConfig(nil))
	assert.Equal(t, text, encodeTextConfig(&llms.InferenceFormat{Type: llms.FormatText}))
	assert.Equal(t, map[string]any{keyFormat: map[string]any{keyType: formatTypeJSON}}, encodeTextConfig(&llms.InferenceFormat{Type: llms.FormatJSONObject}))
	schema := json.RawMessage(`{"type":"object"}`)
	assert.Equal(t, map[string]any{keyFormat: map[string]any{
		keyType:   formatTypeSchema,
		keyName:   "a",
		keySchema: schema,
		keyStrict: true,
	}}, encodeTextConfig(&llms.InferenceFormat{
		Type:   llms.FormatJSONSchema,
		Name:   "a",
		Schema: schema,
		Strict: true,
	}))
}
