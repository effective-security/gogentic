package anthropic

import (
	"context"
	"encoding/json"
	"net/http"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
)

// Anthropic Messages wire vocabulary used by the portable path.
const (
	messagesPath = "v1/messages"

	fieldModel         = "model"
	fieldMessages      = "messages"
	fieldSystem        = "system"
	fieldMaxTokens     = "max_tokens"
	fieldTemperature   = "temperature"
	fieldTopP          = "top_p"
	fieldStopSequences = "stop_sequences"
	fieldTools         = "tools"
	fieldToolChoice    = "tool_choice"
	fieldThinking      = "thinking"
	fieldOutputConfig  = "output_config"
	fieldFormat        = "format"
	fieldRole          = "role"
	fieldContent       = "content"
	fieldType          = "type"
	fieldText          = "text"
	fieldID            = "id"
	fieldName          = "name"
	fieldInput         = "input"
	fieldInputSchema   = "input_schema"
	fieldDescription   = "description"
	fieldStrict        = "strict"
	fieldSchema        = "schema"
	fieldToolUseID     = "tool_use_id"
	fieldBudgetTokens  = "budget_tokens"
	fieldDisableParall = "disable_parallel_tool_use"

	blockText             = "text"
	blockToolUse          = "tool_use"
	blockToolResult       = "tool_result"
	blockThinking         = "thinking"
	blockRedactedThinking = "redacted_thinking"

	choiceAuto = "auto"
	choiceAny  = "any"
	choiceTool = "tool"
	choiceNone = "none"

	thinkingEnabled  = "enabled"
	formatJSONSchema = "json_schema"

	stopEndTurn      = "end_turn"
	stopStopSequence = "stop_sequence"
	stopMaxTokens    = "max_tokens"
	stopToolUse      = "tool_use"
	stopRefusal      = "refusal"

	maxTemperature = 1

	// Unsupported parameter names reported to callers.
	paramSeed              = "seed"
	paramReasoning         = "reasoning"
	paramDeveloperRole     = "messages.role.developer"
	paramJSONObject        = "response_format.json_object"
	paramTemperatureTopP   = "temperature+top_p"
	paramToolChoiceForced  = "tool_choice"
	paramMaxTokensBudget   = "max_tokens"
	paramTemperatureThink  = "temperature"
	paramTopPThink         = "top_p"
	paramTemperatureRange  = "temperature"
	paramParallelToolCalls = "parallel_tool_calls"
)

// thinkingBudget resolves the extended-thinking budget requested by the caller.
func thinkingBudget(r *llms.InferenceReasoning) (int, bool) {
	if r == nil {
		return 0, false
	}
	if r.BudgetTokens != nil {
		return *r.BudgetTokens, true
	}
	return llms.ReasoningBudget(r.Effort)
}

// ValidateInference rejects nonportable Anthropic request controls.
func (o *LLM) ValidateInference(r llms.InferenceRequest) error {
	if r.Seed != nil {
		return llms.UnsupportedInference(paramSeed)
	}
	if r.Temperature != nil && *r.Temperature > maxTemperature {
		return llms.UnsupportedInference(paramTemperatureRange)
	}
	if r.Format != nil && r.Format.Type == llms.FormatJSONObject {
		return llms.UnsupportedInference(paramJSONObject)
	}
	if r.Temperature != nil && r.TopP != nil {
		return llms.UnsupportedInference(paramTemperatureTopP)
	}
	for _, m := range r.Messages {
		if m.Role == llms.InferenceRoleDeveloper {
			return llms.UnsupportedInference(paramDeveloperRole)
		}
	}
	if r.Reasoning == nil {
		return nil
	}
	budget, ok := thinkingBudget(r.Reasoning)
	if !ok || budget <= 0 {
		return llms.UnsupportedInference(paramReasoning)
	}
	// Extended thinking forbids sampling changes and forced tool use.
	if r.Temperature != nil {
		return llms.UnsupportedInference(paramTemperatureThink)
	}
	if r.TopP != nil {
		return llms.UnsupportedInference(paramTopPThink)
	}
	if c := r.ToolChoice; c != nil && (c.Mode == llms.ToolChoiceRequired || c.Mode == llms.ToolChoiceFunction) {
		return llms.UnsupportedInference(paramToolChoiceForced)
	}
	if r.MaxTokens != nil && *r.MaxTokens <= budget {
		return llms.UnsupportedInference(paramMaxTokensBudget)
	}
	return nil
}

// Infer uses the existing Anthropic SDK client with lossless schemas, thinking
// passthrough and no retries.
func (o *LLM) Infer(ctx context.Context, r llms.InferenceRequest) (*llms.InferenceResponse, error) {
	if err := o.ValidateInference(r); err != nil {
		return nil, err
	}
	body := o.messagesBody(r)
	var response struct {
		ID         string            `json:"id"`
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      *struct {
			Input  uint64 `json:"input_tokens"`
			Output uint64 `json:"output_tokens"`
			Read   uint64 `json:"cache_read_input_tokens"`
			Write  uint64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := o.Client.Post(ctx, messagesPath, body, &response, option.WithMaxRetries(0)); err != nil {
		status := http.StatusBadGateway
		var apiErr *sdk.Error
		if errors.As(err, &apiErr) && apiErr.StatusCode > 0 {
			status = apiErr.StatusCode
		}
		return nil, llms.InferenceFailure(err, status, "anthropic text inference")
	}
	out := &llms.InferenceResponse{
		UpstreamID: response.ID,
	}
	switch response.StopReason {
	case stopEndTurn, stopStopSequence:
		out.FinishReason = llms.FinishStop
	case stopMaxTokens:
		out.FinishReason = llms.FinishLength
	case stopToolUse:
		out.FinishReason = llms.FinishToolCalls
	case stopRefusal:
		out.FinishReason = llms.FinishContentFilter
	default:
		return nil, llms.InferenceFailure(errors.Errorf("unsupported stop reason %q", response.StopReason), http.StatusBadGateway, "decode anthropic inference")
	}
	for _, raw := range response.Content {
		var b struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
			Thinking string          `json:"thinking"`
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, llms.InferenceFailure(err, http.StatusBadGateway, "decode anthropic content block")
		}
		switch b.Type {
		case blockText:
			out.Content = append(out.Content, llms.InferenceBlock{
				Type: llms.BlockText,
				Text: b.Text,
			})
		case blockToolUse:
			out.Content = append(out.Content, llms.InferenceBlock{
				Type:      llms.BlockFunctionCall,
				ID:        b.ID,
				Name:      b.Name,
				Arguments: string(b.Input),
			})
		case blockThinking:
			out.Content = append(out.Content, llms.InferenceBlock{
				Type:   llms.BlockReasoning,
				Text:   b.Thinking,
				Opaque: raw,
			})
		case blockRedactedThinking:
			out.Content = append(out.Content, llms.InferenceBlock{
				Type:   llms.BlockReasoning,
				Opaque: raw,
			})
		default:
			return nil, llms.InferenceFailure(errors.Errorf("unsupported content block %q", b.Type), http.StatusBadGateway, "decode anthropic inference")
		}
	}
	if u := response.Usage; u != nil {
		input := u.Input + u.Read + u.Write
		out.UsageKnown = true
		out.Usage = llms.Usage{
			InputTokens:      input,
			OutputTokens:     u.Output,
			CacheReadTokens:  u.Read,
			CacheWriteTokens: u.Write,
			TotalTokens:      input + u.Output,
		}
	}
	return out, nil
}

func (o *LLM) messagesBody(r llms.InferenceRequest) map[string]any {
	var system []any
	var messages []map[string]any
	for _, m := range r.Messages {
		var blocks []any
		for _, b := range m.Content {
			switch b.Type {
			case llms.BlockText:
				blocks = append(blocks, map[string]any{
					fieldType: blockText,
					fieldText: b.Text,
				})
			case llms.BlockFunctionCall:
				blocks = append(blocks, map[string]any{
					fieldType:  blockToolUse,
					fieldID:    b.ID,
					fieldName:  b.Name,
					fieldInput: json.RawMessage(b.Arguments),
				})
			case llms.BlockFunctionOutput:
				blocks = append(blocks, map[string]any{
					fieldType:      blockToolResult,
					fieldToolUseID: b.ID,
					fieldContent:   b.Text,
				})
			case llms.BlockReasoning:
				if len(b.Opaque) > 0 {
					blocks = append(blocks, json.RawMessage(b.Opaque))
				}
			}
			// Refusal history has no Anthropic slot and is dropped.
		}
		if m.Role == llms.InferenceRoleSystem {
			system = append(system, blocks...)
			continue
		}
		if len(blocks) == 0 {
			continue
		}
		role := string(m.Role)
		if m.Role == llms.InferenceRoleTool {
			role = string(llms.InferenceRoleUser)
		}
		// Anthropic requires alternating roles: merge consecutive same-role turns.
		if n := len(messages); n > 0 && messages[n-1][fieldRole] == role {
			existing, _ := messages[n-1][fieldContent].([]any)
			messages[n-1][fieldContent] = append(existing, blocks...)
			continue
		}
		messages = append(messages, map[string]any{
			fieldRole:    role,
			fieldContent: blocks,
		})
	}
	budget, thinking := thinkingBudget(r.Reasoning)
	maxTokens := DefaultMaxTokens
	if thinking {
		maxTokens += budget
	}
	if r.MaxTokens != nil {
		maxTokens = *r.MaxTokens
	}
	body := map[string]any{
		fieldModel:     r.Model,
		fieldMessages:  messages,
		fieldMaxTokens: maxTokens,
	}
	if len(system) > 0 {
		body[fieldSystem] = system
	}
	if r.Temperature != nil {
		body[fieldTemperature] = *r.Temperature
	}
	if r.TopP != nil {
		body[fieldTopP] = *r.TopP
	}
	if len(r.Stop) > 0 {
		body[fieldStopSequences] = r.Stop
	}
	if thinking {
		body[fieldThinking] = map[string]any{
			fieldType:         thinkingEnabled,
			fieldBudgetTokens: budget,
		}
	}
	if toolCount := len(r.Tools); toolCount > 0 {
		ts := make([]any, 0, toolCount)
		for _, t := range r.Tools {
			v := map[string]any{
				fieldName:        t.Name,
				fieldDescription: t.Description,
				fieldInputSchema: t.Parameters,
			}
			if t.Strict != nil {
				v[fieldStrict] = *t.Strict
			}
			ts = append(ts, v)
		}
		body[fieldTools] = ts
	}
	if r.ToolChoice != nil || r.ParallelToolCalls != nil {
		choice := map[string]any{
			fieldType: choiceAuto,
		}
		if c := r.ToolChoice; c != nil {
			switch c.Mode {
			case llms.ToolChoiceRequired:
				choice[fieldType] = choiceAny
			case llms.ToolChoiceNone:
				choice[fieldType] = choiceNone
			case llms.ToolChoiceFunction:
				choice[fieldType] = choiceTool
				choice[fieldName] = c.Name
			}
		}
		if r.ParallelToolCalls != nil && choice[fieldType] != choiceNone {
			choice[fieldDisableParall] = !*r.ParallelToolCalls
		}
		body[fieldToolChoice] = choice
	}
	if f := r.Format; f != nil && f.Type == llms.FormatJSONSchema {
		body[fieldOutputConfig] = map[string]any{
			fieldFormat: map[string]any{
				fieldType:   formatJSONSchema,
				fieldSchema: f.Schema,
			},
		}
	}
	return body
}
