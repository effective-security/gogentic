package anthropic

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resultWith(model string, blocks ...llms.InferenceBlock) *router.Result {
	return &router.Result{
		ID:    "resp_1",
		Model: model,
		Response: llms.InferenceResponse{
			FinishReason: llms.FinishStop,
			Content:      blocks,
		},
	}
}

func TestEncodeMessagesRejections(t *testing.T) {
	out, err := EncodeMessages(resultWith("m", llms.InferenceBlock{
		Type: llms.BlockFunctionOutput,
		ID:   "c1",
	}), dialect.EncodeOptions{})
	require.EqualError(t, err, `anthropic: unsupported output block type "function_call_output"`)
	assert.Nil(t, out)

	out, err = EncodeMessages(resultWith("", llms.InferenceBlock{
		Type:   llms.BlockReasoning,
		Opaque: json.RawMessage(`{"s":1}`),
	}), dialect.EncodeOptions{})
	require.EqualError(t, err, "dialect: opaque state requires a source and a payload", "reasoning state needs a public model to seal")
	assert.Nil(t, out)

	out, err = EncodeMessages(resultWith("m", llms.InferenceBlock{
		Type:      llms.BlockFunctionCall,
		ID:        "c1",
		Name:      "f",
		Arguments: `{`,
	}), dialect.EncodeOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anthropic: encode message")
	assert.Nil(t, out)

	out, err = EncodeMessages(resultWith("", llms.InferenceBlock{
		Type: llms.BlockReasoning,
	}), dialect.EncodeOptions{})
	require.NoError(t, err)
	var msg map[string]any
	require.NoError(t, json.Unmarshal(out, &msg))
	assert.Equal(t, []any{}, msg["content"], "empty reasoning blocks are dropped")
}

func TestEncodeUsageClampsCachedInput(t *testing.T) {
	res := resultWith("m", llms.InferenceBlock{
		Type: llms.BlockText,
		Text: "ok",
	})
	res.Response.UsageKnown = true
	res.Response.Usage = llms.Usage{
		InputTokens:      2,
		OutputTokens:     1,
		TotalTokens:      3,
		CacheReadTokens:  2,
		CacheWriteTokens: 1,
	}
	out, err := EncodeMessages(res, dialect.EncodeOptions{})
	require.NoError(t, err)
	var msg struct {
		Usage map[string]uint64 `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(out, &msg))
	assert.Equal(t, map[string]uint64{
		"input_tokens":                0,
		"output_tokens":               1,
		"cache_creation_input_tokens": 1,
		"cache_read_input_tokens":     2,
	}, msg.Usage)
}

func TestEncodeErrorNil(t *testing.T) {
	status, body := EncodeError(nil)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.JSONEq(t, `{"type":"error","error":{"type":"api_error","message":"internal router error"},"request_id":null}`, string(body))
}
