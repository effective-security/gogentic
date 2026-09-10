package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/router"
	"github.com/effective-security/gogentic/router/dialect"
)

// Response vocabulary.
const (
	typeMessage    = "message"
	idPrefix       = "msg_"
	routerIDPrefix = "resp_"

	stopEndTurn   = "end_turn"
	stopMaxTokens = "max_tokens"
	stopToolUse   = "tool_use"
	stopRefusal   = "refusal"
)

// EncodeMessages renders a router result as an Anthropic Messages response.
// Reasoning blocks become thinking blocks whose signature carries the router's
// sealed state; EncodeOptions.OmitReasoning drops them. Usage is always present
// with zero counters when the backend reported none.
func EncodeMessages(res *router.Result, opts dialect.EncodeOptions) ([]byte, error) {
	if res == nil {
		return nil, errors.New("anthropic: nil result")
	}
	content := make([]any, 0, len(res.Response.Content))
	for _, b := range res.Response.Content {
		block, err := encodeBlock(res.Model, b, opts)
		if err != nil {
			return nil, err
		}
		if block != nil {
			content = append(content, block)
		}
	}
	body := map[string]any{
		"id":            idPrefix + strings.TrimPrefix(res.ID, routerIDPrefix),
		"type":          typeMessage,
		"role":          roleAssistant,
		"model":         res.Model,
		"content":       content,
		"stop_reason":   stopReason(res.Response),
		"stop_sequence": nil,
		"usage":         encodeUsage(res.Response),
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, errors.WithMessage(err, "anthropic: encode message")
	}
	return out, nil
}

func encodeBlock(model string, b llms.InferenceBlock, opts dialect.EncodeOptions) (any, error) {
	switch b.Type {
	case llms.BlockText, llms.BlockRefusal:
		return map[string]any{
			"type": blockText,
			"text": b.Text,
		}, nil
	case llms.BlockFunctionCall:
		return map[string]any{
			"type":  blockToolUse,
			"id":    b.ID,
			"name":  b.Name,
			"input": json.RawMessage(b.Arguments),
		}, nil
	case llms.BlockReasoning:
		if opts.OmitReasoning || (b.Text == "" && len(b.Opaque) == 0) {
			return nil, nil
		}
		sealed := ""
		if len(b.Opaque) > 0 {
			var err error
			if sealed, err = dialect.SealOpaque(model, b.Opaque); err != nil {
				return nil, err
			}
		}
		if b.Text == "" {
			return map[string]any{
				"type": blockRedactedThinking,
				"data": sealed,
			}, nil
		}
		return map[string]any{
			"type":      blockThinking,
			"thinking":  b.Text,
			"signature": sealed,
		}, nil
	}
	return nil, errors.Errorf("anthropic: unsupported output block type %q", b.Type)
}

// stopReason maps the finish reason; a refusal block is reported as Anthropic's
// refusal stop reason because the dialect has no refusal content block.
func stopReason(r llms.InferenceResponse) string {
	for _, b := range r.Content {
		if b.Type == llms.BlockRefusal {
			return stopRefusal
		}
	}
	switch r.FinishReason {
	case llms.FinishLength:
		return stopMaxTokens
	case llms.FinishToolCalls:
		return stopToolUse
	case llms.FinishContentFilter:
		return stopRefusal
	}
	return stopEndTurn
}

func encodeUsage(r llms.InferenceResponse) map[string]any {
	if !r.UsageKnown {
		return map[string]any{
			"input_tokens":                0,
			"output_tokens":               0,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
		}
	}
	u := r.Usage
	input := u.InputTokens
	if cached := u.CacheReadTokens + u.CacheWriteTokens; cached <= input {
		input -= cached
	} else {
		input = 0
	}
	return map[string]any{
		"input_tokens":                input,
		"output_tokens":               u.OutputTokens,
		"cache_creation_input_tokens": u.CacheWriteTokens,
		"cache_read_input_tokens":     u.CacheReadTokens,
	}
}
